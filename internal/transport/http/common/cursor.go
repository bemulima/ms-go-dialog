package common

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type MessageCursorPayload struct {
	V         int       `json:"v"`
	DialogID  uuid.UUID `json:"dialog_id"`
	Sequence  int64     `json:"sequence"`
	ID        uuid.UUID `json:"id"`
	Direction string    `json:"direction"`
}

func EncodeMessageCursor(dialogID uuid.UUID, cursor repository.MessageCursor, direction string) string {
	payload, _ := json.Marshal(MessageCursorPayload{V: 1, DialogID: dialogID, Sequence: cursor.Sequence, ID: cursor.ID, Direction: direction})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func DecodeMessageCursor(raw string, dialogID uuid.UUID, direction string) (*repository.MessageCursor, error) {
	if raw == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	var value MessageCursorPayload
	if json.Unmarshal(payload, &value) != nil || value.V != 1 || value.DialogID != dialogID || value.Sequence < 1 || value.ID == uuid.Nil || value.Direction != direction {
		return nil, fmt.Errorf("invalid cursor")
	}
	return &repository.MessageCursor{Sequence: value.Sequence, ID: value.ID}, nil
}

type DialogCursorPayload struct {
	V          int       `json:"v"`
	ActivityAt time.Time `json:"activity_at"`
	ID         uuid.UUID `json:"id"`
}

func EncodeDialogCursor(cursor repository.DialogCursor) string {
	payload, _ := json.Marshal(DialogCursorPayload{V: 1, ActivityAt: cursor.ActivityAt, ID: cursor.ID})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func DecodeDialogCursor(raw string) (*repository.DialogCursor, error) {
	if raw == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	var value DialogCursorPayload
	if json.Unmarshal(payload, &value) != nil || value.V != 1 || value.ActivityAt.IsZero() || value.ID == uuid.Nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	return &repository.DialogCursor{ActivityAt: value.ActivityAt, ID: value.ID}, nil
}

func PathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	value, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		WriteError(w, r, domain.ErrValidation)
		return uuid.Nil, false
	}
	return value, true
}

func QueryInt(r *http.Request, name string, fallback int) int {
	value := r.URL.Query().Get(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return parsed
}
