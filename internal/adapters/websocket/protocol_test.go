package websocket

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

func TestLifecycleEnvelopeCarriesAssistantUIAdditively(t *testing.T) {
	dialogID, eventID := uuid.New(), uuid.New()
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"event_id":       eventID,
		"occurred_at":    time.Now().UTC(),
		"dialog_id":      dialogID,
		"event_sequence": 1,
		"message_id":     uuid.New(),
		"body":           "plain fallback",
		"assistant_ui":   json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[{"type":"future"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, frame, err := lifecycleEnvelope(domain.EventDialogMessageCreated, payload)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		V    int                        `json:"v"`
		Type string                     `json:"type"`
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(frame, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.V != 1 || envelope.Type != "message.created" || len(envelope.Data["assistant_ui"]) == 0 || len(envelope.Data["body"]) == 0 {
		t.Fatalf("assistant UI lifecycle projection mismatch: %s", frame)
	}
}

func TestLifecycleEnvelopeCarriesVersionedLessonContextAdditively(t *testing.T) {
	dialogID, eventID, courseID, lessonID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	selection := "  exact selection\n"
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"event_id":       eventID,
		"occurred_at":    time.Now().UTC(),
		"dialog_id":      dialogID,
		"event_sequence": 1,
		"message_id":     uuid.New(),
		"body":           "question",
		"lesson_context": domain.LessonMessageContext{
			Schema: domain.LessonMessageContextSchemaV1, Mode: domain.LessonMessageContextSelection,
			CourseID: &courseID, LessonID: &lessonID, ContentRevision: "2026-09-04T08:00:00Z", SelectedText: &selection,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, frame, err := lifecycleEnvelope(domain.EventDialogMessageCreated, payload)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		V    int                        `json:"v"`
		Type string                     `json:"type"`
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(frame, &envelope); err != nil {
		t.Fatal(err)
	}
	var context domain.LessonMessageContext
	if err := json.Unmarshal(envelope.Data["lesson_context"], &context); err != nil {
		t.Fatal(err)
	}
	if envelope.V != 1 || envelope.Type != "message.created" || context.SelectedText == nil || *context.SelectedText != selection {
		t.Fatalf("lesson lifecycle projection mismatch: %s", frame)
	}
}
