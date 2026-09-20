package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	realtimeuc "github.com/bemulima/ms-go-dialog/internal/usecase/realtime"
	"github.com/google/uuid"
	gorillaws "github.com/gorilla/websocket"
)

const dialogProtocol = "dialog.v1"

type TicketConsumer interface {
	Consume(context.Context, string) (realtimeuc.Session, error)
}
type TypingPublisher interface {
	PublishTyping(context.Context, string, []byte) error
}
type ConnectionMetrics interface {
	WebSocketOpened()
	WebSocketClosed()
}
type Handler struct {
	Tickets                                 TicketConsumer
	Hub                                     *Hub
	Typing                                  TypingPublisher
	Metrics                                 ConnectionMetrics
	MaxFrameBytes                           int64
	WriteTimeout, PongTimeout, PingInterval time.Duration
	Now                                     func() time.Time
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("ticket") != "" || r.URL.Query().Get("access_token") != "" {
		http.Error(w, "credentials in query string are forbidden", http.StatusBadRequest)
		return
	}
	ticket, ok := ticketProtocol(r)
	if !ok {
		http.Error(w, "required WebSocket subprotocols are missing", http.StatusUnauthorized)
		return
	}
	session, err := h.Tickets.Consume(r.Context(), ticket)
	if err != nil {
		http.Error(w, "realtime ticket is invalid or expired", http.StatusUnauthorized)
		return
	}
	if !originAllowed(r.Header.Get("Origin"), session.AllowedOrigins) {
		http.Error(w, "origin is not allowed", http.StatusForbidden)
		return
	}
	if h.Hub == nil {
		http.Error(w, "realtime hub is unavailable", http.StatusServiceUnavailable)
		return
	}
	item, err := h.Hub.register(session)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, errConnectionLimit) {
			status = http.StatusTooManyRequests
		}
		http.Error(w, err.Error(), status)
		return
	}
	defer h.Hub.unregister(item)
	upgrader := gorillaws.Upgrader{Subprotocols: []string{dialogProtocol}, CheckOrigin: func(*http.Request) bool { return true }, HandshakeTimeout: h.writeTimeout()}
	connection, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = connection.Close() }()
	if h.Metrics != nil {
		h.Metrics.WebSocketOpened()
		defer h.Metrics.WebSocketClosed()
	}
	initial := h.Hub.activate(item, readyFrame(session.SpaceID, session.DialogSequences, h.now()))
	writerDone := make(chan struct{})
	go func() { defer close(writerDone); h.writeLoop(connection, item, initial); _ = connection.Close() }()
	h.readLoop(r.Context(), connection, item)
	h.Hub.unregister(item)
	_ = connection.Close()
	<-writerDone
}

type clientFrame struct {
	V         int       `json:"v"`
	Type      string    `json:"type"`
	RequestID string    `json:"request_id,omitempty"`
	DialogID  uuid.UUID `json:"dialog_id,omitempty"`
}

func (h Handler) readLoop(ctx context.Context, connection *gorillaws.Conn, item *client) {
	connection.SetReadLimit(h.maxFrameBytes())
	_ = connection.SetReadDeadline(h.now().Add(h.pongTimeout()))
	connection.SetPongHandler(func(string) error { return connection.SetReadDeadline(h.now().Add(h.pongTimeout())) })
	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			return
		}
		_ = connection.SetReadDeadline(h.now().Add(h.pongTimeout()))
		if messageType != gorillaws.TextMessage {
			h.enqueue(item, errorFrame("", "invalid_frame", "only JSON text frames are supported"))
			continue
		}
		var frame clientFrame
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&frame) != nil || decoder.Decode(&struct{}{}) != io.EOF || frame.V != protocolVersion {
			h.enqueue(item, errorFrame(frame.RequestID, "invalid_frame", "invalid protocol frame"))
			continue
		}
		switch frame.Type {
		case "pong":
			continue
		case "typing.start", "typing.stop":
			if frame.DialogID == uuid.Nil || !item.hasDialog(frame.DialogID) {
				h.enqueue(item, errorFrame(frame.RequestID, "dialog_forbidden", "active dialog membership is required"))
				continue
			}
			if h.Typing == nil {
				h.enqueue(item, errorFrame(frame.RequestID, "realtime_unavailable", "typing delivery is unavailable"))
				continue
			}
			eventType := "typing.started"
			if frame.Type == "typing.stop" {
				eventType = "typing.stopped"
			}
			message := typingFrame(eventType, frame.DialogID, item.session.UserID, uuid.New(), h.now())
			if err := h.Typing.PublishTyping(ctx, frame.DialogID.String(), message); err != nil {
				h.enqueue(item, errorFrame(frame.RequestID, "realtime_unavailable", "typing delivery failed"))
			}
		default:
			h.enqueue(item, errorFrame(frame.RequestID, "unsupported_message", "message type is not supported"))
		}
	}
}
func (h Handler) writeLoop(connection *gorillaws.Conn, item *client, initial [][]byte) {
	for _, payload := range initial {
		_ = connection.SetWriteDeadline(h.now().Add(h.writeTimeout()))
		if connection.WriteMessage(gorillaws.TextMessage, payload) != nil {
			return
		}
	}
	ticker := time.NewTicker(h.pingInterval())
	defer ticker.Stop()
	for {
		select {
		case <-item.done:
			_ = connection.SetWriteDeadline(h.now().Add(h.writeTimeout()))
			_ = connection.WriteMessage(gorillaws.CloseMessage, gorillaws.FormatCloseMessage(gorillaws.CloseNormalClosure, "shutdown"))
			return
		case payload := <-item.send:
			_ = connection.SetWriteDeadline(h.now().Add(h.writeTimeout()))
			if connection.WriteMessage(gorillaws.TextMessage, payload) != nil {
				return
			}
		case <-ticker.C:
			_ = connection.SetWriteDeadline(h.now().Add(h.writeTimeout()))
			if connection.WriteMessage(gorillaws.TextMessage, pingFrame(h.now())) != nil {
				return
			}
		}
	}
}
func (h Handler) enqueue(item *client, payload []byte) {
	select {
	case item.send <- payload:
	default:
		if h.Hub != nil {
			h.Hub.unregister(item)
		}
	}
}
func ticketProtocol(r *http.Request) (string, bool) {
	var ticket string
	hasDialog := false
	for _, protocol := range gorillaws.Subprotocols(r) {
		switch {
		case protocol == dialogProtocol:
			hasDialog = true
		case strings.HasPrefix(protocol, "ticket.") && len(protocol) > len("ticket."):
			if ticket != "" {
				return "", false
			}
			ticket = strings.TrimPrefix(protocol, "ticket.")
		}
	}
	return ticket, hasDialog && ticket != "" && len(ticket) <= 128
}
func originAllowed(origin string, allowed []string) bool {
	if origin == "" {
		return false
	}
	for _, candidate := range allowed {
		if origin == candidate {
			return true
		}
	}
	return false
}
func (h Handler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}
func (h Handler) maxFrameBytes() int64 {
	if h.MaxFrameBytes > 0 {
		return h.MaxFrameBytes
	}
	return 16 << 10
}
func (h Handler) writeTimeout() time.Duration {
	if h.WriteTimeout > 0 {
		return h.WriteTimeout
	}
	return 10 * time.Second
}
func (h Handler) pongTimeout() time.Duration {
	if h.PongTimeout > 0 {
		return h.PongTimeout
	}
	return 60 * time.Second
}
func (h Handler) pingInterval() time.Duration {
	if h.PingInterval > 0 {
		return h.PingInterval
	}
	return 25 * time.Second
}
