package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewTeacherContextMutationOutboxIsBodyFreeAndBound(t *testing.T) {
	now := time.Date(2026, 9, 4, 14, 0, 0, 0, time.UTC)
	dialogID, studentID, teacherID, contextID, messageID, eventID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := Dialog{
		ID: dialogID, Type: DialogTypeTeacher, Status: DialogStatusActive, StudentID: studentID,
		PersonalTeacherID: teacherID, TeacherContextType: TeacherContextLesson, ContextID: &contextID,
		MaxEventSequence: 11,
	}
	message := Message{
		ID: messageID, DialogID: dialogID, AuthorType: MessageAuthorUser, SenderID: studentID,
		Status: MessageStatusActive, Version: 3, MessageSequence: 7, LastEventSequence: 11,
		Body: "must not leak", AssistantUI: json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[]}`),
	}
	event, err := NewTeacherContextMutationOutbox(dialogItem, message, TeacherContextMutationUpdated, eventID, now)
	if err != nil {
		t.Fatal(err)
	}
	if event.Subject != EventDialogTeacherContextMutated || event.EventSequence != 11 || event.AggregateID != messageID {
		t.Fatalf("event binding mismatch: %+v", event)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"schema_version": true, "event_id": true, "event_type": true, "occurred_at": true,
		"dialog_id": true, "event_sequence": true, "student_id": true, "personal_teacher_id": true,
		"context_type": true, "context_id": true, "message_id": true, "message_sequence": true,
		"message_version": true, "last_event_sequence": true, "mutation": true,
	}
	if len(payload) != len(want) {
		t.Fatalf("payload fields=%v", payload)
	}
	for key := range payload {
		if !want[key] {
			t.Fatalf("body-free payload contains unexpected field %q: %s", key, event.Payload)
		}
	}
	if string(payload["event_type"]) != `"dialog.teacher.context-mutated"` || string(payload["mutation"]) != `"updated"` ||
		string(payload["message_version"]) != "3" || string(payload["message_sequence"]) != "7" {
		t.Fatalf("payload values mismatch: %s", event.Payload)
	}
}

func TestNewTeacherRequestedV2OutboxIsBodyFreeAndCausallyBound(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	dialogID, studentID, teacherID, contextID, messageID, eventID, learningActionID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	sequence := int64(4)
	dialogItem := Dialog{
		ID: dialogID, Type: DialogTypeTeacher, Status: DialogStatusActive, StudentID: studentID,
		PersonalTeacherID: teacherID, TeacherContextType: TeacherContextPracticeTask, ContextID: &contextID,
		MaxTeacherTurnSequence: sequence,
	}
	message := Message{
		ID: messageID, DialogID: dialogID, AuthorType: MessageAuthorUser, Channel: MessageChannelWeb, SenderID: studentID,
		LearningActionID: &learningActionID,
		Body:             "must not leak", AssistantUI: json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[]}`),
		Status: MessageStatusActive, Version: 1, MessageSequence: 9, LastEventSequence: 12,
		TeacherTurnSequence: &sequence,
	}
	dialogItem.MaxEventSequence = message.LastEventSequence
	event, err := NewTeacherRequestedV2Outbox(dialogItem, message, eventID, messageID, now)
	if err != nil {
		t.Fatal(err)
	}
	if event.Subject != EventDialogTeacherRequested || event.SchemaVersion != TeacherRequestedSchemaVersionV2 || event.AggregateID != messageID {
		t.Fatalf("event binding mismatch: %+v", event)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"schema_version": true, "event_id": true, "occurred_at": true, "dialog_id": true, "event_sequence": true,
		"source_message_id": true, "source_message_sequence": true, "source_message_version": true, "reply_to_message_id": true,
		"student_id": true, "personal_teacher_id": true,
		"context_type": true, "context_id": true, "learning_action_id": true, "channel": true,
		"canonical_student_message_id": true, "teacher_turn_sequence": true, "correlation_id": true, "causation_id": true,
	}
	if len(payload) != len(want) {
		t.Fatalf("payload fields=%v", payload)
	}
	for key := range payload {
		if !want[key] {
			t.Fatalf("body-free payload contains unexpected field %q: %s", key, event.Payload)
		}
	}
	if string(payload["canonical_student_message_id"]) != `"`+messageID.String()+`"` ||
		string(payload["correlation_id"]) != `"`+messageID.String()+`"` ||
		string(payload["causation_id"]) != `"`+messageID.String()+`"` ||
		string(payload["teacher_turn_sequence"]) != "4" ||
		string(payload["source_message_version"]) != "1" ||
		string(payload["reply_to_message_id"]) != "null" {
		t.Fatalf("causal payload mismatch: %s", event.Payload)
	}
	if _, exists := payload["action_receipt_id"]; exists {
		t.Fatalf("direct text v2 must not invent action receipt evidence: %s", event.Payload)
	}
	message.TeacherTurnSequence = nil
	if _, err := NewTeacherRequestedV2Outbox(dialogItem, message, eventID, messageID, now); err == nil {
		t.Fatal("V2 request accepted without allocated teacher turn sequence")
	}
	message.TeacherTurnSequence = &sequence
	message.Version = 0
	if _, err := NewTeacherRequestedV2Outbox(dialogItem, message, eventID, messageID, now); err == nil {
		t.Fatal("V2 request accepted source message without a positive version")
	}
	message.Version = 1
	invalidReplyID := uuid.Nil
	message.ReplyToMessageID = &invalidReplyID
	if _, err := NewTeacherRequestedV2Outbox(dialogItem, message, eventID, messageID, now); err == nil {
		t.Fatal("V2 request accepted a nil reply message UUID")
	}
}

func TestNewTeacherContextMutationOutboxRejectsNonUserAndWrongLifecycle(t *testing.T) {
	now := time.Now().UTC()
	studentID, teacherID := uuid.New(), uuid.New()
	dialogItem := Dialog{ID: uuid.New(), Type: DialogTypeTeacher, Status: DialogStatusActive, StudentID: studentID, PersonalTeacherID: teacherID, TeacherContextType: TeacherContextGeneralTeacher, MaxEventSequence: 2}
	message := Message{ID: uuid.New(), DialogID: dialogItem.ID, AuthorType: MessageAuthorPersonalTeacher, PersonalTeacherID: teacherID, Status: MessageStatusActive, Version: 1, MessageSequence: 1, LastEventSequence: 2}
	if _, err := NewTeacherContextMutationOutbox(dialogItem, message, TeacherContextMutationUpdated, uuid.New(), now); err == nil {
		t.Fatal("PersonalTeacher message produced a context mutation")
	}
	message.AuthorType, message.PersonalTeacherID, message.SenderID = MessageAuthorUser, uuid.Nil, studentID
	if _, err := NewTeacherContextMutationOutbox(dialogItem, message, TeacherContextMutationDeleted, uuid.New(), now); err == nil {
		t.Fatal("active message was accepted as deleted mutation")
	}
}

func TestNewDialogAssistantUISourceReturnsOnlyAllowlistedProjection(t *testing.T) {
	studentID, teacherID, contextID := uuid.New(), uuid.New(), uuid.New()
	dialogItem := Dialog{
		ID: uuid.New(), Type: DialogTypeTeacher, Status: DialogStatusActive, StudentID: studentID,
		PersonalTeacherID: teacherID, TeacherContextType: TeacherContextPracticeTask, ContextID: &contextID,
	}
	message := Message{
		ID: uuid.New(), DialogID: dialogItem.ID, AuthorType: MessageAuthorPersonalTeacher, PersonalTeacherID: teacherID,
		Status: MessageStatusActive, Version: 4, MessageSequence: 9, Body: "private body",
		AssistantUI: json.RawMessage(`{"blocks":[{"future":{"opaque":true}}],"schema":"assistant-ui.v1"}`),
	}
	source, err := NewDialogAssistantUISource(dialogItem, message)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"schema": true, "dialog_id": true, "student_id": true, "personal_teacher_id": true,
		"context_type": true, "context_id": true, "message_id": true, "message_sequence": true,
		"message_version": true, "assistant_ui": true,
	}
	if len(fields) != len(want) {
		t.Fatalf("projection fields=%v body=%s", fields, encoded)
	}
	for key := range fields {
		if !want[key] {
			t.Fatalf("projection leaked unexpected field %q: %s", key, encoded)
		}
	}
	if string(fields["schema"]) != `"dialog-assistant-ui-source.v1"` || string(source.AssistantUI) != `{"blocks":[{"future":{"opaque":true}}],"schema":"assistant-ui.v1"}` {
		t.Fatalf("projection mismatch: %s", encoded)
	}
}
