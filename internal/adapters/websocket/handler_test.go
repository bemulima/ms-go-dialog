package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	realtimeuc "github.com/bemulima/ms-go-dialog/internal/usecase/realtime"
	"github.com/google/uuid"
	gorillaws "github.com/gorilla/websocket"
)

func TestTicketProtocolRejectsQueryCredentials(t *testing.T) {
	request := httptest.NewRequest("GET", "http://example.test/api/v1/ws?ticket=leak", nil)
	response := httptest.NewRecorder()
	Handler{}.ServeHTTP(response, request)
	if response.Code != 400 {
		t.Fatalf("status=%d", response.Code)
	}
}

type ticketConsumerStub struct{ session realtimeuc.Session }

func (s ticketConsumerStub) Consume(context.Context, string) (realtimeuc.Session, error) {
	return s.session, nil
}

type connectionMetricsStub struct {
	opened chan struct{}
	closed chan struct{}
}

func (s connectionMetricsStub) WebSocketOpened() { close(s.opened) }
func (s connectionMetricsStub) WebSocketClosed() { close(s.closed) }

func TestHandlerTracksEstablishedConnectionLifecycle(t *testing.T) {
	const origin = "https://client.example"
	hub := NewHub(2, 8)
	defer hub.Close()
	metrics := connectionMetricsStub{opened: make(chan struct{}), closed: make(chan struct{})}
	dialogID := uuid.New()
	handler := Handler{
		Tickets: ticketConsumerStub{session: realtimeuc.Session{
			UserID: uuid.New(), SpaceID: uuid.New(), DialogSequences: map[uuid.UUID]int64{dialogID: 19}, AllowedOrigins: []string{origin},
		}},
		Hub: hub, Metrics: metrics,
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	dialer := gorillaws.Dialer{Subprotocols: []string{dialogProtocol, "ticket.test-ticket"}}
	header := http.Header{"Origin": []string{origin}}
	connection, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	if err != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("dial WebSocket: status=%d err=%v", status, err)
	}
	select {
	case <-metrics.opened:
	case <-time.After(time.Second):
		t.Fatal("established connection was not recorded")
	}
	var ready struct {
		V       int                 `json:"v"`
		Type    string              `json:"type"`
		Dialogs map[uuid.UUID]int64 `json:"dialogs"`
	}
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	if err := connection.ReadJSON(&ready); err != nil {
		t.Fatal(err)
	}
	if ready.V != protocolVersion || ready.Type != "connection.ready" || ready.Dialogs[dialogID] != 19 {
		t.Fatalf("ready frame mismatch: %+v", ready)
	}
	_ = connection.Close()
	select {
	case <-metrics.closed:
	case <-time.After(time.Second):
		t.Fatal("closed connection was not recorded")
	}
}
