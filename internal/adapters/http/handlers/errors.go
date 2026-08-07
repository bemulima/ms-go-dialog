package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bemulima/ms-go-dialog/internal/adapters/http/middleware"
	"github.com/bemulima/ms-go-dialog/internal/domain"
)

type errorResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Details   any    `json:"details"`
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := classifyError(err)
	writeJSON(w, status, errorResponse{Error: code, Message: message, RequestID: middleware.RequestID(r), Details: nil})
}

func classifyError(err error) (int, string, string) {
	switch {
	case errors.Is(err, domain.ErrAuthentication):
		return http.StatusUnauthorized, "authentication_required", "authenticated user is required"
	case errors.Is(err, domain.ErrDialogNotFound):
		return http.StatusNotFound, "dialog_not_found", "dialog was not found"
	case errors.Is(err, domain.ErrMessageNotFound):
		return http.StatusNotFound, "message_not_found", "message was not found"
	case errors.Is(err, domain.ErrMemberNotFound):
		return http.StatusNotFound, "member_not_found", "dialog member was not found"
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, "dialog_forbidden", "dialog membership or permission is required"
	case errors.Is(err, domain.ErrBlocked):
		return http.StatusForbidden, "user_blocked", "messaging is blocked for this user pair"
	case errors.Is(err, domain.ErrDialogClosed):
		return http.StatusConflict, "dialog_closed", "dialog is not writable"
	case errors.Is(err, domain.ErrMemberLimit):
		return http.StatusConflict, "member_limit_exceeded", "dialog member limit is exceeded"
	case errors.Is(err, domain.ErrLastOwner):
		return http.StatusConflict, "last_owner_required", "group must retain an active owner"
	case errors.Is(err, domain.ErrMessageConflict), errors.Is(err, domain.ErrIdempotencyConflict), errors.Is(err, domain.ErrAlreadyExists):
		return http.StatusConflict, "conflict", "request conflicts with current state"
	case errors.Is(err, domain.ErrInvalidReadSequence):
		return http.StatusBadRequest, "invalid_read_sequence", "read sequence is invalid"
	case errors.Is(err, domain.ErrLinksDisabled):
		return http.StatusBadRequest, "links_disabled", "links are disabled"
	case errors.Is(err, domain.ErrImagesDisabled):
		return http.StatusBadRequest, "images_disabled", "images are disabled"
	case errors.Is(err, domain.ErrFilesDisabled):
		return http.StatusBadRequest, "files_disabled", "files are disabled"
	case errors.Is(err, domain.ErrInvalidAttachment):
		return http.StatusBadRequest, "attachment_invalid", "attachment is invalid"
	case errors.Is(err, domain.ErrValidation), errors.Is(err, domain.ErrInvalidContent):
		return http.StatusBadRequest, "invalid_request", "request is invalid"
	default:
		return http.StatusInternalServerError, "internal_error", "internal server error"
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
