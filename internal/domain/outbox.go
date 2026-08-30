package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type EventSubject string

const (
	EventDialogCreated           EventSubject = "dialog.created"
	EventDialogUpdated           EventSubject = "dialog.updated"
	EventDialogClosed            EventSubject = "dialog.closed"
	EventDialogMemberAdded       EventSubject = "dialog.member.added"
	EventDialogMemberRemoved     EventSubject = "dialog.member.removed"
	EventDialogMemberRoleUpdated EventSubject = "dialog.member.role_updated"
	EventDialogMessageCreated    EventSubject = "dialog.message.created"
	EventDialogMessageUpdated    EventSubject = "dialog.message.updated"
	EventDialogMessageDeleted    EventSubject = "dialog.message.deleted"
	EventDialogMessageHidden     EventSubject = "dialog.message.hidden"
	EventDialogMessageRestored   EventSubject = "dialog.message.restored"
	EventDialogAttachmentReady   EventSubject = "dialog.attachment.ready"
	EventDialogAttachmentFailed  EventSubject = "dialog.attachment.failed"
	EventDialogReadUpdated       EventSubject = "dialog.read.updated"
	EventDialogTeacherRequested  EventSubject = "dialog.teacher.requested"
)

func (s EventSubject) Valid() bool {
	switch s {
	case EventDialogCreated, EventDialogUpdated, EventDialogClosed,
		EventDialogMemberAdded, EventDialogMemberRemoved, EventDialogMemberRoleUpdated,
		EventDialogMessageCreated, EventDialogMessageUpdated, EventDialogMessageDeleted,
		EventDialogMessageHidden, EventDialogMessageRestored,
		EventDialogAttachmentReady, EventDialogAttachmentFailed, EventDialogReadUpdated,
		EventDialogTeacherRequested:
		return true
	default:
		return false
	}
}

type OutboxEvent struct {
	ID            uuid.UUID
	DialogID      uuid.UUID
	AggregateType string
	AggregateID   uuid.UUID
	Subject       EventSubject
	EventSequence int64
	SchemaVersion int16
	Payload       json.RawMessage
	Attempts      int
	NextAttemptAt time.Time
	PublishedAt   *time.Time
	LastError     string
	CreatedAt     time.Time
}

func (e OutboxEvent) Validate() error {
	trimmed := bytes.TrimSpace(e.Payload)
	if e.ID == uuid.Nil || e.DialogID == uuid.Nil || e.AggregateID == uuid.Nil || strings.TrimSpace(e.AggregateType) == "" ||
		!e.Subject.Valid() || e.EventSequence < 1 || e.SchemaVersion < 1 || e.Attempts < 0 ||
		!json.Valid(trimmed) || len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return fmt.Errorf("%w: invalid outbox event", ErrValidation)
	}
	return nil
}
