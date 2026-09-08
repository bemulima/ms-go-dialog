package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const DialogAssistantUISourceSchemaV1 = "dialog-assistant-ui-source.v1"

type TeacherContextMutation string

const (
	TeacherContextMutationUpdated TeacherContextMutation = "updated"
	TeacherContextMutationDeleted TeacherContextMutation = "deleted"
)

func (m TeacherContextMutation) Valid() bool {
	return m == TeacherContextMutationUpdated || m == TeacherContextMutationDeleted
}

// TeacherContextMutationPayload is deliberately body-free. It gives Teacher
// enough immutable identity and ordering data to invalidate provisional
// summaries without receiving message content on the event stream.
type TeacherContextMutationPayload struct {
	SchemaVersion     int16                  `json:"schema_version"`
	EventID           uuid.UUID              `json:"event_id"`
	EventType         EventSubject           `json:"event_type"`
	OccurredAt        time.Time              `json:"occurred_at"`
	DialogID          uuid.UUID              `json:"dialog_id"`
	EventSequence     int64                  `json:"event_sequence"`
	StudentID         uuid.UUID              `json:"student_id"`
	PersonalTeacherID uuid.UUID              `json:"personal_teacher_id"`
	ContextType       TeacherContextType     `json:"context_type"`
	ContextID         *uuid.UUID             `json:"context_id,omitempty"`
	MessageID         uuid.UUID              `json:"message_id"`
	MessageSequence   int64                  `json:"message_sequence"`
	MessageVersion    int                    `json:"message_version"`
	LastEventSequence int64                  `json:"last_event_sequence"`
	Mutation          TeacherContextMutation `json:"mutation"`
}

func NewTeacherContextMutationOutbox(dialog Dialog, message Message, mutation TeacherContextMutation, eventID uuid.UUID, occurredAt time.Time) (OutboxEvent, error) {
	if eventID == uuid.Nil || occurredAt.IsZero() || !validActiveTeacherBinding(dialog) ||
		dialog.ID != message.DialogID || dialog.MaxEventSequence != message.LastEventSequence ||
		message.AuthorType != MessageAuthorUser || message.SenderID != dialog.StudentID || message.ID == uuid.Nil ||
		message.MessageSequence < 1 || message.Version < 1 || message.LastEventSequence < message.MessageSequence || !mutation.Valid() ||
		(mutation == TeacherContextMutationUpdated && message.Status != MessageStatusActive) ||
		(mutation == TeacherContextMutationDeleted && message.Status != MessageStatusDeleted) {
		return OutboxEvent{}, fmt.Errorf("%w: invalid teacher context mutation", ErrValidation)
	}
	payload, err := json.Marshal(TeacherContextMutationPayload{
		SchemaVersion: 1, EventID: eventID, EventType: EventDialogTeacherContextMutated, OccurredAt: occurredAt.UTC(),
		DialogID: dialog.ID, EventSequence: message.LastEventSequence, StudentID: dialog.StudentID,
		PersonalTeacherID: dialog.PersonalTeacherID, ContextType: dialog.TeacherContextType, ContextID: dialog.ContextID,
		MessageID: message.ID, MessageSequence: message.MessageSequence, MessageVersion: message.Version,
		LastEventSequence: message.LastEventSequence, Mutation: mutation,
	})
	if err != nil {
		return OutboxEvent{}, err
	}
	event := OutboxEvent{
		ID: eventID, DialogID: dialog.ID, AggregateType: "teacher_context", AggregateID: message.ID,
		Subject: EventDialogTeacherContextMutated, EventSequence: message.LastEventSequence, SchemaVersion: 1,
		Payload: payload, NextAttemptAt: occurredAt.UTC(), CreatedAt: occurredAt.UTC(),
	}
	if err := event.Validate(); err != nil {
		return OutboxEvent{}, err
	}
	return event, nil
}

// DialogAssistantUISource is an allowlisted stored-source projection. Dialog
// validates only the assistant-ui.v1 transport envelope; block semantics remain
// opaque to this service.
type DialogAssistantUISource struct {
	Schema            string             `json:"schema"`
	DialogID          uuid.UUID          `json:"dialog_id"`
	StudentID         uuid.UUID          `json:"student_id"`
	PersonalTeacherID uuid.UUID          `json:"personal_teacher_id"`
	ContextType       TeacherContextType `json:"context_type"`
	ContextID         *uuid.UUID         `json:"context_id,omitempty"`
	MessageID         uuid.UUID          `json:"message_id"`
	MessageSequence   int64              `json:"message_sequence"`
	MessageVersion    int                `json:"message_version"`
	AssistantUI       json.RawMessage    `json:"assistant_ui"`
}

func NewDialogAssistantUISource(dialog Dialog, message Message) (DialogAssistantUISource, error) {
	if !validActiveTeacherBinding(dialog) || dialog.ID != message.DialogID ||
		message.ID == uuid.Nil || message.AuthorType != MessageAuthorPersonalTeacher || message.Status != MessageStatusActive ||
		message.PersonalTeacherID != dialog.PersonalTeacherID || message.MessageSequence < 1 || message.Version < 1 || len(message.AssistantUI) == 0 {
		return DialogAssistantUISource{}, fmt.Errorf("%w: assistant UI source is unavailable", ErrValidation)
	}
	assistantUI, err := NormalizeAssistantUI(message.AssistantUI)
	if err != nil || len(assistantUI) == 0 {
		return DialogAssistantUISource{}, fmt.Errorf("%w: assistant UI source is unavailable", ErrValidation)
	}
	return DialogAssistantUISource{
		Schema: DialogAssistantUISourceSchemaV1, DialogID: dialog.ID, StudentID: dialog.StudentID,
		PersonalTeacherID: dialog.PersonalTeacherID, ContextType: dialog.TeacherContextType, ContextID: dialog.ContextID,
		MessageID: message.ID, MessageSequence: message.MessageSequence, MessageVersion: message.Version,
		AssistantUI: assistantUI,
	}, nil
}

func validActiveTeacherBinding(dialog Dialog) bool {
	if dialog.ID == uuid.Nil || dialog.Type != DialogTypeTeacher || dialog.Status != DialogStatusActive ||
		dialog.StudentID == uuid.Nil || dialog.PersonalTeacherID == uuid.Nil || !dialog.TeacherContextType.Valid() {
		return false
	}
	if dialog.TeacherContextType == TeacherContextGeneralTeacher {
		return dialog.ContextID == nil
	}
	return dialog.ContextID != nil && *dialog.ContextID != uuid.Nil
}
