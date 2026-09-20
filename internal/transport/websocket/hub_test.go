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

func TestHubActivationOrdersReadyBeforeBufferedLifecycle(t *testing.T) {
	hub := NewHub(5, 8)
	spaceID, dialogID, userID := uuid.New(), uuid.New(), uuid.New()
	item, err := hub.register(realtimeuc.Session{UserID: userID, SpaceID: spaceID, DialogSequences: map[uuid.UUID]int64{dialogID: 4}})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.unregister(item)

	hub.BroadcastLifecycle(domain.EventDialogUpdated, lifecyclePayload(t, dialogID, spaceID, userID, false))
	frames := hub.activate(item, readyFrame(spaceID, map[uuid.UUID]int64{dialogID: 4}, time.Now().UTC()))
	if len(frames) != 2 || frameType(t, frames[0]) != "connection.ready" || frameType(t, frames[1]) != "dialog.updated" {
		t.Fatalf("activation order mismatch: %q", frames)
	}
}

func TestHubLifecycleIsIsolatedByDialog(t *testing.T) {
	hub := NewHub(5, 8)
	spaceID, dialogID, otherDialogID := uuid.New(), uuid.New(), uuid.New()
	first, err := hub.register(realtimeuc.Session{UserID: uuid.New(), SpaceID: spaceID, DialogSequences: map[uuid.UUID]int64{dialogID: 1}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := hub.register(realtimeuc.Session{UserID: uuid.New(), SpaceID: spaceID, DialogSequences: map[uuid.UUID]int64{dialogID: 1}})
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := hub.register(realtimeuc.Session{UserID: uuid.New(), SpaceID: spaceID, DialogSequences: map[uuid.UUID]int64{otherDialogID: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.unregister(first)
	defer hub.unregister(second)
	defer hub.unregister(outsider)
	hub.activate(first)
	hub.activate(second)
	hub.activate(outsider)

	hub.BroadcastLifecycle(domain.EventDialogUpdated, lifecyclePayload(t, dialogID, spaceID, uuid.New(), false))
	for name, item := range map[string]*client{"first": first, "second": second} {
		select {
		case <-item.send:
		default:
			t.Fatalf("%s participant did not receive dialog event", name)
		}
	}
	select {
	case <-outsider.send:
		t.Fatal("unrelated dialog received lifecycle event")
	default:
	}
}

func frameType(t *testing.T, payload []byte) string {
	t.Helper()
	var frame struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &frame); err != nil {
		t.Fatal(err)
	}
	return frame.Type
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
