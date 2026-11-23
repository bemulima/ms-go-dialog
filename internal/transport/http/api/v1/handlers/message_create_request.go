package handlers

import (
	"bytes"
	"encoding/json"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
	"io"
)

type messageCreateRequest struct {
	DialogID               uuid.UUID                    `json:"dialog_id"`
	ReplyToMessageID       *uuid.UUID                   `json:"reply_to_message_id"`
	Body                   string                       `json:"body"`
	AttachmentIDs          []uuid.UUID                  `json:"attachment_ids"`
	IdempotencyKey         uuid.UUID                    `json:"idempotency_key"`
	LearningActionID       *uuid.UUID                   `json:"learning_action_id"`
	LessonContext          *domain.LessonMessageContext `json:"lesson_context"`
	TeacherPolicySelection json.RawMessage              `json:"teacher_policy_selection"`
}

func (r *messageCreateRequest) UnmarshalJSON(raw []byte) error {
	// The existing body limit applies before this decoder. Reject duplicate and
	// case-alias keys so an explicit selector cannot be silently overwritten.
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return domain.ErrValidation
	}
	seen := map[string]bool{}
	for d.More() {
		token, e = d.Token()
		key, ok := token.(string)
		if e != nil || !ok || seen[key] {
			return domain.ErrValidation
		}
		seen[key] = true
		switch key {
		case "dialog_id", "reply_to_message_id", "body", "attachment_ids", "idempotency_key", "learning_action_id", "lesson_context", "teacher_policy_selection":
		default:
			return domain.ErrValidation
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return domain.ErrValidation
		}
	}
	if token, e = d.Token(); e != nil || token != json.Delim('}') {
		return domain.ErrValidation
	}
	if _, e = d.Token(); e != io.EOF {
		return domain.ErrValidation
	}
	type plain messageCreateRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode((*plain)(r))
}
