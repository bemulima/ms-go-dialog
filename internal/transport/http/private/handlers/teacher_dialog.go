package handlers

import (
	"encoding/json"
	common "github.com/bemulima/ms-go-dialog/internal/transport/http/common"
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

const maxAssistantUISourceRequestBytes = 4 * 1024

func (h TeacherDialogHandler) Ensure(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SpaceKey          string                    `json:"space_key"`
		StudentID         uuid.UUID                 `json:"student_id"`
		PersonalTeacherID uuid.UUID                 `json:"personal_teacher_id"`
		ContextType       domain.TeacherContextType `json:"context_type"`
		ContextID         *uuid.UUID                `json:"context_id"`
	}
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Dialogs.EnsureTeacher(r.Context(), dialoguc.EnsureTeacherInput{
		SpaceKey: request.SpaceKey, StudentID: request.StudentID, PersonalTeacherID: request.PersonalTeacherID,
		ContextType: request.ContextType, ContextID: request.ContextID,
	})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	common.WriteJSON(w, status, common.NewDialogResponse(result.View, true))
}

func (h TeacherDialogHandler) RequestContext(w http.ResponseWriter, r *http.Request) {
	dialogID, err := uuid.Parse(chi.URLParam(r, "dialogID"))
	if err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	sourceMessageID, err := uuid.Parse(chi.URLParam(r, "sourceMessageID"))
	if err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	personalTeacherID, err := uuid.Parse(r.URL.Query().Get("personal_teacher_id"))
	before := common.QueryInt(r, "before", 20)
	if err != nil || before < 0 || before >= messageuc.MaxTeacherContextMessages {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	context, err := h.Messages.GetTeacherRequestContext(r.Context(), dialogID, personalTeacherID, sourceMessageID, before)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	messages := make([]common.MessageResponse, 0, len(context.Messages))
	for _, item := range context.Messages {
		messages = append(messages, common.NewMessageResponse(messageuc.View{Message: item}))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"dialog_id": context.Dialog.ID, "student_id": context.Dialog.StudentID,
		"personal_teacher_id": context.Dialog.PersonalTeacherID,
		"context_type":        context.Dialog.TeacherContextType, "context_id": context.Dialog.ContextID,
		"source_message_id": context.Source.ID, "learning_action_id": context.Source.LearningActionID,
		"messages": messages,
	})
}

func (h TeacherDialogHandler) AssistantUISource(w http.ResponseWriter, r *http.Request) {
	dialogID, err := uuid.Parse(chi.URLParam(r, "dialogID"))
	if err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	var request struct {
		StudentID         uuid.UUID `json:"student_id"`
		PersonalTeacherID uuid.UUID `json:"personal_teacher_id"`
		MessageID         uuid.UUID `json:"message_id"`
	}
	if err := common.DecodeJSONLimit(w, r, &request, maxAssistantUISourceRequestBytes); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Messages.GetAssistantUISource(r.Context(), messageuc.AssistantUISourceInput{
		DialogID: dialogID, StudentID: request.StudentID,
		PersonalTeacherID: request.PersonalTeacherID, MessageID: request.MessageID,
	})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, result)
}

// CanonicalStudentTurn accepts only a trusted service command protected by the
// internal exact-token middleware. It returns the ordinary message shape and
// intentionally never serializes the private receipt/interaction ledger.
func (h TeacherDialogHandler) CanonicalStudentTurn(w http.ResponseWriter, r *http.Request) {
	dialogID, err := uuid.Parse(chi.URLParam(r, "dialogID"))
	if err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	var request struct {
		Identity      domain.CanonicalStudentTurnIdentity `json:"identity"`
		CanonicalBody string                              `json:"canonical_body"`
	}
	// Keep the ordinary JSON ceiling: canonical_body is subject to the active
	// space's normal content policy, not an accidental smaller route limit.
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	if request.Identity.DialogID != dialogID {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Messages.MaterializeCanonicalStudentTurn(r.Context(), messageuc.MaterializeCanonicalStudentTurnInput{
		Identity: request.Identity, CanonicalBody: request.CanonicalBody,
	})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	common.WriteJSON(w, status, common.NewMessageResponse(result.View))
}

func (h TeacherDialogHandler) AppendResponse(w http.ResponseWriter, r *http.Request) {
	dialogID, err := uuid.Parse(chi.URLParam(r, "dialogID"))
	if err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	var request struct {
		PersonalTeacherID uuid.UUID       `json:"personal_teacher_id"`
		SourceMessageID   uuid.UUID       `json:"source_message_id"`
		IdempotencyKey    uuid.UUID       `json:"idempotency_key"`
		Body              string          `json:"body"`
		AssistantUI       json.RawMessage `json:"assistant_ui"`
	}
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Messages.AppendTeacherResponse(r.Context(), messageuc.AppendTeacherResponseInput{
		DialogID: dialogID, PersonalTeacherID: request.PersonalTeacherID,
		SourceMessageID: request.SourceMessageID, IdempotencyKey: request.IdempotencyKey, Body: request.Body,
		AssistantUI: request.AssistantUI,
	})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	common.WriteJSON(w, status, common.NewMessageResponse(result.View))
}

func (h TeacherDialogHandler) AppendProactive(w http.ResponseWriter, r *http.Request) {
	dialogID, err := uuid.Parse(chi.URLParam(r, "dialogID"))
	if err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	var request struct {
		PersonalTeacherID uuid.UUID       `json:"personal_teacher_id"`
		IdempotencyKey    uuid.UUID       `json:"idempotency_key"`
		Body              string          `json:"body"`
		AssistantUI       json.RawMessage `json:"assistant_ui"`
	}
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Messages.AppendTeacherProactive(r.Context(), messageuc.AppendTeacherProactiveInput{
		DialogID: dialogID, PersonalTeacherID: request.PersonalTeacherID,
		IdempotencyKey: request.IdempotencyKey, Body: request.Body, AssistantUI: request.AssistantUI,
	})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	common.WriteJSON(w, status, common.NewMessageResponse(result.View))
}

func (h TeacherDialogHandler) AppendStudentChannelMessage(w http.ResponseWriter, r *http.Request) {
	dialogID, err := uuid.Parse(chi.URLParam(r, "dialogID"))
	if err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	var request struct {
		StudentID         uuid.UUID             `json:"student_id"`
		PersonalTeacherID uuid.UUID             `json:"personal_teacher_id"`
		IdempotencyKey    uuid.UUID             `json:"idempotency_key"`
		Channel           domain.MessageChannel `json:"channel"`
		Body              string                `json:"body"`
	}
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Messages.AppendStudentChannelMessage(r.Context(), messageuc.AppendStudentChannelMessageInput{
		DialogID: dialogID, StudentID: request.StudentID, PersonalTeacherID: request.PersonalTeacherID,
		IdempotencyKey: request.IdempotencyKey, Channel: request.Channel, Body: request.Body,
	})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	common.WriteJSON(w, status, common.NewMessageResponse(result.View))
}
