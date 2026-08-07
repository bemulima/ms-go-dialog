package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

type messageCursorPayload struct {
	V         int       `json:"v"`
	DialogID  uuid.UUID `json:"dialog_id"`
	Sequence  int64     `json:"sequence"`
	ID        uuid.UUID `json:"id"`
	Direction string    `json:"direction"`
}

func encodeMessageCursor(dialogID uuid.UUID, cursor repository.MessageCursor, direction string) string {
	payload, _ := json.Marshal(messageCursorPayload{V: 1, DialogID: dialogID, Sequence: cursor.Sequence, ID: cursor.ID, Direction: direction})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeMessageCursor(raw string, dialogID uuid.UUID, direction string) (*repository.MessageCursor, error) {
	if raw == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	var value messageCursorPayload
	if json.Unmarshal(payload, &value) != nil || value.V != 1 || value.DialogID != dialogID || value.Sequence < 1 || value.ID == uuid.Nil || value.Direction != direction {
		return nil, fmt.Errorf("invalid cursor")
	}
	return &repository.MessageCursor{Sequence: value.Sequence, ID: value.ID}, nil
}

type dialogCursorPayload struct {
	V          int       `json:"v"`
	ActivityAt time.Time `json:"activity_at"`
	ID         uuid.UUID `json:"id"`
}

func encodeDialogCursor(cursor repository.DialogCursor) string {
	payload, _ := json.Marshal(dialogCursorPayload{V: 1, ActivityAt: cursor.ActivityAt, ID: cursor.ID})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeDialogCursor(raw string) (*repository.DialogCursor, error) {
	if raw == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	var value dialogCursorPayload
	if json.Unmarshal(payload, &value) != nil || value.V != 1 || value.ActivityAt.IsZero() || value.ID == uuid.Nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	return &repository.DialogCursor{ActivityAt: value.ActivityAt, ID: value.ID}, nil
}
