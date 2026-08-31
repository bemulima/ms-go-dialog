package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type frame struct {
	Version  int              `json:"v"`
	Type     string           `json:"type"`
	DialogID uuid.UUID        `json:"dialog_id"`
	Dialogs  map[string]int64 `json:"dialogs"`
}

func main() {
	url := flag.String("url", "", "gateway WebSocket URL")
	origin := flag.String("origin", "", "exact Origin registered for the dialog space")
	ticket := flag.String("ticket", "", "single-use realtime ticket")
	dialogID := flag.String("dialog-id", "", "active dialog expected in the session")
	flag.Parse()

	id, err := uuid.Parse(*dialogID)
	if *url == "" || *origin == "" || *ticket == "" || err != nil {
		fatalf("url, origin, ticket, and a valid dialog-id are required")
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		Subprotocols:     []string{"dialog.v1", "ticket." + *ticket},
	}
	header := http.Header{"Origin": []string{*origin}}
	connection, response, err := dialer.Dial(*url, header)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		fatalf("connect: %v", err)
	}
	if connection.Subprotocol() != "dialog.v1" {
		_ = connection.Close()
		fatalf("unexpected negotiated subprotocol %q", connection.Subprotocol())
	}

	if err := connection.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		_ = connection.Close()
		fatalf("set read deadline: %v", err)
	}
	ready, err := readFrame(connection)
	if err != nil {
		_ = connection.Close()
		fatalf("read connection.ready: %v", err)
	}
	if ready.Version != 1 || ready.Type != "connection.ready" {
		_ = connection.Close()
		fatalf("unexpected initial frame type %q", ready.Type)
	}
	if _, ok := ready.Dialogs[id.String()]; !ok {
		_ = connection.Close()
		fatalf("active dialog is absent from connection.ready")
	}

	payload, _ := json.Marshal(map[string]any{
		"v": 1, "type": "typing.start", "request_id": uuid.NewString(), "dialog_id": id,
	})
	if err := connection.WriteMessage(websocket.TextMessage, payload); err != nil {
		_ = connection.Close()
		fatalf("send typing.start: %v", err)
	}
	if err := waitForTyping(connection, id); err != nil {
		_ = connection.Close()
		fatalf("typing round trip: %v", err)
	}
	_ = connection.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "probe complete"))
	_ = connection.Close()

	reused, reusedResponse, reusedErr := dialer.Dial(*url, header)
	if reused != nil {
		_ = reused.Close()
		fatalf("single-use ticket was accepted twice")
	}
	if reusedResponse != nil && reusedResponse.Body != nil {
		defer func() { _ = reusedResponse.Body.Close() }()
	}
	if reusedErr == nil || reusedResponse == nil || reusedResponse.StatusCode != http.StatusUnauthorized {
		fatalf("reused ticket must return HTTP 401")
	}

	fmt.Println("websocket ready, typing, and single-use ticket checks passed")
}

func waitForTyping(connection *websocket.Conn, dialogID uuid.UUID) error {
	for {
		item, err := readFrame(connection)
		if err != nil {
			return err
		}
		switch item.Type {
		case "typing.started":
			if item.Version != 1 || item.DialogID != dialogID {
				return fmt.Errorf("typing frame does not match the active dialog")
			}
			return nil
		case "ping":
			payload, _ := json.Marshal(map[string]any{"v": 1, "type": "pong"})
			if err := connection.WriteMessage(websocket.TextMessage, payload); err != nil {
				return err
			}
		}
	}
}

func readFrame(connection *websocket.Conn) (frame, error) {
	_, payload, err := connection.ReadMessage()
	if err != nil {
		return frame{}, err
	}
	var item frame
	if err := json.Unmarshal(payload, &item); err != nil {
		return frame{}, err
	}
	return item, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
