package handlers

import (
	common "github.com/bemulima/ms-go-dialog/internal/transport/http/common"
	"net/http"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common/middleware"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type DialogHandler struct{ Service *dialoguc.Service }

func (h DialogHandler) BlockUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := common.PathUUID(w, r, "userID")
	if !ok {
		return
	}
	if common.DecodeOptionalEmptyBody(r) != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	if err := h.Service.BlockUser(r.Context(), middleware.Actor(r), userID); err != nil {
		common.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h DialogHandler) UnblockUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := common.PathUUID(w, r, "userID")
	if !ok {
		return
	}
	if common.DecodeOptionalEmptyBody(r) != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	if err := h.Service.UnblockUser(r.Context(), middleware.Actor(r), userID); err != nil {
		common.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h DialogHandler) Update(w http.ResponseWriter, r *http.Request) {
	dialogID, ok := common.PathUUID(w, r, "dialogID")
	if !ok {
		return
	}
	var request struct {
		Title   string `json:"title"`
		Version int    `json:"version"`
	}
	if common.DecodeJSON(w, r, &request) != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	view, err := h.Service.UpdateGroup(r.Context(), middleware.Actor(r), dialoguc.UpdateGroupInput{DialogID: dialogID, Title: request.Title, ExpectedVersion: request.Version})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.NewDialogResponse(view, true))
}

func (h DialogHandler) EnsurePersonal(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SpaceKey      string    `json:"space_key"`
		ParticipantID uuid.UUID `json:"participant_id"`
	}
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Service.EnsurePersonal(r.Context(), middleware.Actor(r), dialoguc.EnsurePersonalInput{
		SpaceKey: request.SpaceKey, ParticipantID: request.ParticipantID,
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

func (h DialogHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SpaceKey       string      `json:"space_key"`
		Title          string      `json:"title"`
		ParticipantIDs []uuid.UUID `json:"participant_ids"`
	}
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	view, err := h.Service.CreateGroup(r.Context(), middleware.Actor(r), dialoguc.CreateGroupInput{
		SpaceKey: request.SpaceKey, Title: request.Title, ParticipantIDs: request.ParticipantIDs,
	})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, http.StatusCreated, common.NewDialogResponse(view, true))
}

func (h DialogHandler) Get(w http.ResponseWriter, r *http.Request) {
	dialogID, err := uuid.Parse(chi.URLParam(r, "dialogID"))
	if err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	view, err := h.Service.Get(r.Context(), middleware.Actor(r), dialogID)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.NewDialogResponse(view, true))
}

func (h DialogHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := common.QueryInt(r, "limit", dialoguc.DefaultPageLimit)
	cursor, err := common.DecodeDialogCursor(r.URL.Query().Get("cursor"))
	if err != nil || limit < 1 || limit > dialoguc.MaxPageLimit {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	query := repository.DialogListQuery{After: cursor, Limit: limit + 1}
	if raw := r.URL.Query().Get("space_id"); raw != "" {
		spaceID, err := uuid.Parse(raw)
		if err != nil {
			common.WriteError(w, r, domain.ErrValidation)
			return
		}
		query.SpaceID = &spaceID
	}
	items, err := h.Service.List(r.Context(), middleware.Actor(r), query)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		activity := last.Dialog.CreatedAt
		if last.Dialog.LastMessageAt != nil {
			activity = *last.Dialog.LastMessageAt
		}
		encoded := common.EncodeDialogCursor(repository.DialogCursor{ActivityAt: activity, ID: last.Dialog.ID})
		next = &encoded
	}
	responses := make([]common.DialogResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, common.NewDialogResponse(dialoguc.View{Dialog: item.Dialog, CurrentMember: item.Member}, false))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": responses, "next_cursor": next})
}

func (h DialogHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	dialogID, ok := common.PathUUID(w, r, "dialogID")
	if !ok {
		return
	}
	var request struct {
		UserID uuid.UUID `json:"user_id"`
	}
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	view, err := h.Service.AddMember(r.Context(), middleware.Actor(r), dialoguc.AddMemberInput{DialogID: dialogID, UserID: request.UserID})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, http.StatusCreated, common.NewDialogResponse(view, true))
}

func (h DialogHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	dialogID, ok := common.PathUUID(w, r, "dialogID")
	if !ok {
		return
	}
	userID, ok := common.PathUUID(w, r, "userID")
	if !ok {
		return
	}
	if err := common.DecodeOptionalEmptyBody(r); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	view, err := h.Service.RemoveMember(r.Context(), middleware.Actor(r), dialogID, userID)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.NewDialogResponse(view, true))
}

func (h DialogHandler) ChangeRole(w http.ResponseWriter, r *http.Request) {
	dialogID, ok := common.PathUUID(w, r, "dialogID")
	if !ok {
		return
	}
	userID, ok := common.PathUUID(w, r, "userID")
	if !ok {
		return
	}
	var request struct {
		Role domain.MemberRole `json:"role"`
	}
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	view, err := h.Service.ChangeRole(r.Context(), middleware.Actor(r), dialoguc.ChangeRoleInput{DialogID: dialogID, UserID: userID, Role: request.Role})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.NewDialogResponse(view, true))
}

func (h DialogHandler) Leave(w http.ResponseWriter, r *http.Request) {
	dialogID, ok := common.PathUUID(w, r, "dialogID")
	if !ok {
		return
	}
	if err := common.DecodeOptionalEmptyBody(r); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	if err := h.Service.Leave(r.Context(), middleware.Actor(r), dialogID); err != nil {
		common.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
