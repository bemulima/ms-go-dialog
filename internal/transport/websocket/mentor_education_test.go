package websocket

import (
	"context"
	"errors"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	realtimeuc "github.com/bemulima/ms-go-dialog/internal/usecase/realtime"
	"github.com/google/uuid"
	"testing"
)

type educationVisibility struct {
	allowed bool
	err     error
	calls   int
}

func (v *educationVisibility) AllowPublic(context.Context, uuid.UUID, *uuid.UUID) (bool, error) {
	v.calls++
	return v.allowed, v.err
}
func TestMentorEducationRealtimeNeverProjectsProtectedMetadata(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		hub := NewHub(5, 8)
		guard := &educationVisibility{}
		if unavailable {
			guard.err = errors.New("owner unavailable")
		}
		hub.Visibility = guard
		space, dialog, user := uuid.New(), uuid.New(), uuid.New()
		client, err := hub.register(realtimeuc.Session{UserID: user, SpaceID: space, DialogSequences: map[uuid.UUID]int64{dialog: 1}})
		if err != nil {
			t.Fatal(err)
		}
		hub.activate(client)
		hub.BroadcastLifecycle(domain.EventDialogUpdated, lifecyclePayload(t, dialog, space, user, false))
		select {
		case frame := <-client.send:
			t.Fatalf("protected cohort metadata reached generic WebSocket: %s", frame)
		default:
		}
		if guard.calls != 1 {
			t.Fatal("visibility owner not called")
		}
		hub.Close()
	}
}
