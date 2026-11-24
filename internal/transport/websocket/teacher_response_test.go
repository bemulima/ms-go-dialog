package websocket

import (
	"encoding/json"
	"testing"
	"time"

	realtimeuc "github.com/bemulima/ms-go-dialog/internal/usecase/realtime"
	"github.com/google/uuid"
)

func TestHubTeacherResponseReplayIsOwnerScopedAndFenced(t *testing.T) {
	hub := NewHub(5, 16)
	t.Cleanup(hub.Close)
	dialogID, studentID, spaceID := uuid.New(), uuid.New(), uuid.New()
	connect := func(user uuid.UUID) (*client, [][]byte) {
		t.Helper()
		item, err := hub.register(realtimeuc.Session{
			UserID: user, SpaceID: spaceID, DialogSequences: map[uuid.UUID]int64{dialogID: 1},
		})
		if err != nil {
			t.Fatal(err)
		}
		return item, hub.activateWithTeacherPreview(item, []byte("ready"))
	}
	owner, _ := connect(studentID)
	other, _ := connect(uuid.New())
	event := teacherResponsePreview{StudentID: studentID, teacherResponseFrame: teacherResponseFrame{
		Version: protocolVersion, DialogID: dialogID, MessageID: uuid.New(), MessageSequence: 7,
		Attempt: 1, AttemptID: uuid.New(), ResponseID: uuid.New(), CorrelationID: uuid.New(),
		Type: "teacher.response.delta", Text: "First snapshot", Sequence: 2, OccurredAt: time.Now().UTC(),
	}}
	send := func() {
		t.Helper()
		payload, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		hub.BroadcastTeacherResponse(payload)
	}
	read := func(item *client, expectedType string) teacherResponseFrame {
		t.Helper()
		select {
		case raw := <-item.send:
			var frame teacherResponseFrame
			if err := json.Unmarshal(raw, &frame); err != nil || frame.Type != expectedType {
				t.Fatalf("unexpected frame %s: %v", raw, err)
			}
			if routing := map[string]json.RawMessage{}; json.Unmarshal(raw, &routing) != nil || routing["student_id"] != nil ||
				routing["event_sequence"] != nil {
				t.Fatalf("unexpected private routing or durable cursor: %s", raw)
			}
			return frame
		default:
			t.Fatalf("missing %s frame", expectedType)
			return teacherResponseFrame{}
		}
	}
	send()
	send()
	if frame := read(owner, event.Type); frame.Text != event.Text || len(owner.send) != 0 || len(other.send) != 0 {
		t.Fatal("preview lost, duplicated, or escaped the owner")
	}
	reconnected, initial := connect(studentID)
	if len(initial) != 2 || frameType(t, initial[1]) != event.Type {
		t.Fatalf("reconnected learner did not receive a snapshot: %q", initial)
	}
	if _, foreign := connect(uuid.New()); len(foreign) != 1 {
		t.Fatal("reconnection leaked the learner preview")
	}

	event.Type, event.Text, event.Sequence = "teacher.response.cancelled", "", 3
	send()
	read(owner, event.Type)
	read(reconnected, event.Type)
	_, afterCancellation := connect(studentID)
	if len(afterCancellation) != 2 || frameType(t, afterCancellation[1]) != event.Type {
		t.Fatal("reconnected learner did not receive the content-free terminal state")
	}
	event.Type, event.Text, event.Sequence = "teacher.response.delta", "Late duplicate", 4
	send()
	if len(owner.send) != 0 || len(reconnected.send) != 0 {
		t.Fatal("late delta survived cancellation")
	}

	event.Attempt, event.AttemptID, event.Sequence = 2, uuid.New(), 2
	event.Text = "<script>unsafe browser content</script>"
	send()
	if len(owner.send) != 0 {
		t.Fatal("preview bypassed Dialog content validation")
	}
	event.Text = "Second attempt"
	send()
	if frame := read(owner, event.Type); frame.Attempt != 2 || frame.Text != event.Text {
		t.Fatal("retry did not start with its own snapshot")
	}
}
