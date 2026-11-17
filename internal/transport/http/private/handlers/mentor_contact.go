package handlers

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	common "github.com/bemulima/ms-go-dialog/internal/transport/http/common"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common/middleware"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	"github.com/google/uuid"
)

type MentorContactEnsurer interface {
	Ensure(context.Context, dialoguc.MentorContactInput) (dialoguc.MentorContact, error)
}

type MentorContactHandler struct {
	Service MentorContactEnsurer
	Limiter middleware.ActorLimiter
}

// Ensure is reachable only behind the distinct Mentor purpose credential. The
// verified Student comes from that owner command, never from public role headers.
func (h MentorContactHandler) Ensure(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || r.URL.RawQuery != "" || r.Header.Get("Content-Encoding") != "" {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	in, err := decodeMentorContact(r.Body)
	if err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	if h.Service == nil || h.Limiter == nil {
		common.WriteError(w, r, domain.ErrDependencyUnavailable)
		return
	}
	if !h.Limiter.Allow(in.StudentID) {
		w.Header().Set("Retry-After", "1")
		common.WriteError(w, r, domain.ErrRateLimited)
		return
	}
	result, err := h.Service.Ensure(r.Context(), in)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	if result.DialogID == uuid.Nil || result.DialogVersion < 1 || result.DialogVersion > 2147483647 || result.StudentID != in.StudentID || result.MentorID != in.MentorID {
		common.WriteError(w, r, domain.ErrDependencyUnavailable)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"schema": "mentor-contact.dialog.v1", "dialog_id": result.DialogID, "dialog_version": result.DialogVersion,
		"student_id": result.StudentID, "mentor_id": result.MentorID,
	}})
}

// A token decoder rejects duplicate/escaped-duplicate keys, nulls, arrays,
// objects, unknown fields and trailing JSON before any provisioning call.
func decodeMentorContact(body io.Reader) (dialoguc.MentorContactInput, error) {
	decoder := json.NewDecoder(body)
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return dialoguc.MentorContactInput{}, domain.ErrValidation
	}
	values := map[string]string{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return dialoguc.MentorContactInput{}, domain.ErrValidation
		}
		name, ok := key.(string)
		if !ok || (name != "schema" && name != "student_id" && name != "mentor_id") {
			return dialoguc.MentorContactInput{}, domain.ErrValidation
		}
		if _, exists := values[name]; exists {
			return dialoguc.MentorContactInput{}, domain.ErrValidation
		}
		value, err := decoder.Token()
		if err != nil {
			return dialoguc.MentorContactInput{}, domain.ErrValidation
		}
		text, ok := value.(string)
		if !ok {
			return dialoguc.MentorContactInput{}, domain.ErrValidation
		}
		values[name] = text
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') || len(values) != 3 || values["schema"] != "mentor-contact.ensure.v1" {
		return dialoguc.MentorContactInput{}, domain.ErrValidation
	}
	if _, err := decoder.Token(); err != io.EOF {
		return dialoguc.MentorContactInput{}, domain.ErrValidation
	}
	studentID, err := uuid.Parse(values["student_id"])
	if err != nil || studentID == uuid.Nil || studentID.String() != values["student_id"] {
		return dialoguc.MentorContactInput{}, domain.ErrValidation
	}
	mentorID, err := uuid.Parse(values["mentor_id"])
	if err != nil || mentorID == uuid.Nil || mentorID == studentID || mentorID.String() != values["mentor_id"] {
		return dialoguc.MentorContactInput{}, domain.ErrValidation
	}
	return dialoguc.MentorContactInput{StudentID: studentID, MentorID: mentorID}, nil
}
