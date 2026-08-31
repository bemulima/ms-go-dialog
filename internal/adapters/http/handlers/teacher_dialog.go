package handlers

import (
	"net/http"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type TeacherDialogHandler struct {
	Dialogs  *dialoguc.Service
	Messages *messageuc.Service
}

func (h TeacherDialogHandler) Ensure(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SpaceKey          string                    `json:"space_key"`
		StudentID         uuid.UUID                 `json:"student_id"`
		PersonalTeacherID uuid.UUID                 `json:"personal_teacher_id"`
		ContextType       domain.TeacherContextType `json:"context_type"`
		ContextID         *uuid.UUID                `json:"context_id"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Dialogs.EnsureTeacher(r.Context(), dialoguc.EnsureTeacherInput{
		SpaceKey: request.SpaceKey, StudentID: request.StudentID, PersonalTeacherID: request.PersonalTeacherID,
		ContextType: request.ContextType, ContextID: request.ContextID,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, newDialogResponse(result.View, true))
}

func (h TeacherDialogHandler) RequestContext(w http.ResponseWriter, r *http.Request) {
	dialogID, err := uuid.Parse(chi.URLParam(r, "dialogID"))
	if err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	sourceMessageID, err := uuid.Parse(chi.URLParam(r, "sourceMessageID"))
	if err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	personalTeacherID, err := uuid.Parse(r.URL.Query().Get("personal_teacher_id"))
	before := queryInt(r, "before", 20)
	if err != nil || before < 0 || before >= messageuc.MaxTeacherContextMessages {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	context, err := h.Messages.GetTeacherRequestContext(r.Context(), dialogID, personalTeacherID, sourceMessageID, before)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	messages := make([]messageResponse, 0, len(context.Messages))
	for _, item := range context.Messages {
		messages = append(messages, newMessageResponse(messageuc.View{Message: item}))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dialog_id": context.Dialog.ID, "student_id": context.Dialog.StudentID,
		"personal_teacher_id": context.Dialog.PersonalTeacherID,
		"context_type":        context.Dialog.TeacherContextType, "context_id": context.Dialog.ContextID,
		"source_message_id": context.Source.ID, "learning_action_id": context.Source.LearningActionID,
		"messages": messages,
	})
}

func (h TeacherDialogHandler) AppendResponse(w http.ResponseWriter, r *http.Request) {
	dialogID, err := uuid.Parse(chi.URLParam(r, "dialogID"))
	if err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	var request struct {
		PersonalTeacherID uuid.UUID `json:"personal_teacher_id"`
		SourceMessageID   uuid.UUID `json:"source_message_id"`
		IdempotencyKey    uuid.UUID `json:"idempotency_key"`
		Body              string    `json:"body"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Messages.AppendTeacherResponse(r.Context(), messageuc.AppendTeacherResponseInput{
		DialogID: dialogID, PersonalTeacherID: request.PersonalTeacherID,
		SourceMessageID: request.SourceMessageID, IdempotencyKey: request.IdempotencyKey, Body: request.Body,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, newMessageResponse(result.View))
}

func (h TeacherDialogHandler) AppendProactive(w http.ResponseWriter, r *http.Request) {
	dialogID, err := uuid.Parse(chi.URLParam(r, "dialogID"))
	if err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	var request struct {
		PersonalTeacherID uuid.UUID `json:"personal_teacher_id"`
		IdempotencyKey    uuid.UUID `json:"idempotency_key"`
		Body              string    `json:"body"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Messages.AppendTeacherProactive(r.Context(), messageuc.AppendTeacherProactiveInput{
		DialogID: dialogID, PersonalTeacherID: request.PersonalTeacherID,
		IdempotencyKey: request.IdempotencyKey, Body: request.Body,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, newMessageResponse(result.View))
}
