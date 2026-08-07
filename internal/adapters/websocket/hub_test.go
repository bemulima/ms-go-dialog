package websocket

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	realtimeuc "github.com/bemulima/ms-go-dialog/internal/usecase/realtime"
	"github.com/google/uuid"
)

func TestHubCreatedAndRemovedMembership(t *testing.T) {
	hub := NewHub(5, 8)
	spaceID, dialogID, userID := uuid.New(), uuid.New(), uuid.New()
	item, err := hub.register(realtimeuc.Session{UserID: userID, SpaceID: spaceID, DialogSequences: map[uuid.UUID]int64{}})
	if err != nil {
		t.Fatal(err)
	}
	hub.activate(item)
	hub.BroadcastLifecycle(domain.EventDialogCreated, lifecyclePayload(t, dialogID, spaceID, userID, true))
	if !item.hasDialog(dialogID) {
		t.Fatal("created dialog was not subscribed")
	}
	select {
	case <-item.send:
	default:
		t.Fatal("created event was not delivered")
	}
	hub.BroadcastLifecycle(domain.EventDialogMemberRemoved, lifecyclePayload(t, dialogID, spaceID, userID, false))
	select {
	case <-item.send:
	default:
		t.Fatal("removal event was not delivered before revocation")
	}
	if item.hasDialog(dialogID) {
		t.Fatal("removed member remains subscribed")
	}
}

func lifecyclePayload(t *testing.T, dialogID, spaceID, userID uuid.UUID, created bool) []byte {
	t.Helper()
	payload := map[string]any{"schema_version": 1, "event_id": uuid.New(), "occurred_at": time.Now().UTC(), "dialog_id": dialogID, "space_id": spaceID, "event_sequence": int64(1), "user_id": userID}
	if created {
		payload["participant_ids"] = []uuid.UUID{userID}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
