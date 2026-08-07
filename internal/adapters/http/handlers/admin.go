package handlers

import (
	"context"
	"github.com/bemulima/ms-go-dialog/internal/adapters/http/middleware"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	adminuc "github.com/bemulima/ms-go-dialog/internal/usecase/admin"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/google/uuid"
	"net/http"
	"strconv"
	"strings"
)

type AdminHandler struct{ Service *adminuc.Service }
type policyRequest struct {
	AllowPersonal        bool     `json:"allow_personal"`
	AllowGroups          bool     `json:"allow_groups"`
	AllowImages          bool     `json:"allow_images"`
	AllowFiles           bool     `json:"allow_files"`
	AllowLinks           bool     `json:"allow_links"`
	MaxGroupMembers      int      `json:"max_group_members"`
	MaxBodyLength        int      `json:"max_body_length"`
	MaxAttachments       int16    `json:"max_attachments"`
	MaxImageBytes        int64    `json:"max_image_bytes"`
	MaxFileBytes         int64    `json:"max_file_bytes"`
	AllowedFileMIMETypes []string `json:"allowed_file_mime_types"`
	EditWindowSeconds    int      `json:"edit_window_seconds"`
}
type spaceWriteRequest struct {
	Key            string        `json:"key,omitempty"`
	Name           string        `json:"name"`
	Status         string        `json:"status,omitempty"`
	AllowedOrigins []string      `json:"allowed_origins"`
	Policy         policyRequest `json:"policy"`
}

func (h AdminHandler) CreateSpace(w http.ResponseWriter, r *http.Request) {
	var request spaceWriteRequest
	if decodeJSON(w, r, &request) != nil || request.Status != "" {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	item, err := h.Service.CreateSpace(r.Context(), middleware.Actor(r), adminuc.CreateSpaceInput{Key: request.Key, Name: request.Name, AllowedOrigins: request.AllowedOrigins, Policy: request.Policy.domain()})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, spaceResponse(item))
}
func (h AdminHandler) ListSpaces(w http.ResponseWriter, r *http.Request) {
	limit, err := strconv.Atoi(defaultValue(r.URL.Query().Get("limit"), "20"))
	if err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	offset, err := strconv.Atoi(defaultValue(r.URL.Query().Get("offset"), "0"))
	if err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	items, err := h.Service.ListSpaces(r.Context(), middleware.Actor(r), limit, offset)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	result := make([]any, 0, len(items))
	for _, item := range items {
		result = append(result, spaceResponse(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result, "limit": limit, "offset": offset})
}
func (h AdminHandler) ListDialogs(w http.ResponseWriter, r *http.Request) {
	limit, err := strconv.Atoi(defaultValue(r.URL.Query().Get("limit"), "20"))
	if err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	offset, err := strconv.Atoi(defaultValue(r.URL.Query().Get("offset"), "0"))
	if err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	var spaceID *uuid.UUID
	if raw := r.URL.Query().Get("space_id"); raw != "" {
		parsed, e := uuid.Parse(raw)
		if e != nil {
			WriteError(w, r, domain.ErrValidation)
			return
		}
		spaceID = &parsed
	}
	var status *domain.DialogStatus
	if raw := r.URL.Query().Get("status"); raw != "" {
		parsed, ok := parseDialogStatus(raw)
		if !ok {
			WriteError(w, r, domain.ErrValidation)
			return
		}
		status = &parsed
	}
	items, err := h.Service.ListDialogs(r.Context(), middleware.Actor(r), repository.AdminDialogListQuery{SpaceID: spaceID, Status: status, Limit: limit, Offset: offset})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	result := make([]any, 0, len(items))
	for _, item := range items {
		result = append(result, adminDialogResponse(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result, "limit": limit, "offset": offset})
}
func (h AdminHandler) UpdateSpace(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "spaceID")
	if !ok {
		return
	}
	var request spaceWriteRequest
	if decodeJSON(w, r, &request) != nil || request.Key != "" {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	status, ok := parseSpaceStatus(request.Status)
	if !ok {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	item, err := h.Service.UpdateSpace(r.Context(), middleware.Actor(r), adminuc.UpdateSpaceInput{ID: id, Name: request.Name, Status: status, AllowedOrigins: request.AllowedOrigins, Policy: request.Policy.domain()})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, spaceResponse(item))
}
func (h AdminHandler) CloseDialog(w http.ResponseWriter, r *http.Request) {
	h.dialogStatus(w, r, h.Service.CloseDialog)
}
func (h AdminHandler) ReopenDialog(w http.ResponseWriter, r *http.Request) {
	h.dialogStatus(w, r, h.Service.ReopenDialog)
}
func (h AdminHandler) dialogStatus(w http.ResponseWriter, r *http.Request, action func(context.Context, domain.Actor, uuid.UUID) (domain.Dialog, error)) {
	id, ok := pathUUID(w, r, "dialogID")
	if !ok {
		return
	}
	if decodeOptionalEmptyBody(r) != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	item, err := action(r.Context(), middleware.Actor(r), id)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, adminDialogResponse(item))
}
func (h AdminHandler) HideMessage(w http.ResponseWriter, r *http.Request) {
	h.messageStatus(w, r, h.Service.HideMessage)
}
func (h AdminHandler) RestoreMessage(w http.ResponseWriter, r *http.Request) {
	h.messageStatus(w, r, h.Service.RestoreMessage)
}
func (h AdminHandler) messageStatus(w http.ResponseWriter, r *http.Request, action func(context.Context, domain.Actor, uuid.UUID) (adminuc.ModerationView, error)) {
	id, ok := pathUUID(w, r, "messageID")
	if !ok {
		return
	}
	if decodeOptionalEmptyBody(r) != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	view, err := action(r.Context(), middleware.Actor(r), id)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newMessageResponse(messageuc.View{Message: view.Message, Attachments: view.Attachments}))
}
func (p policyRequest) domain() domain.Policy {
	return domain.Policy{AllowPersonal: p.AllowPersonal, AllowGroups: p.AllowGroups, AllowImages: p.AllowImages, AllowFiles: p.AllowFiles, AllowLinks: p.AllowLinks, MaxGroupMembers: p.MaxGroupMembers, MaxBodyLength: p.MaxBodyLength, MaxAttachments: p.MaxAttachments, MaxImageBytes: p.MaxImageBytes, MaxFileBytes: p.MaxFileBytes, AllowedFileMIMETypes: p.AllowedFileMIMETypes, EditWindowSeconds: p.EditWindowSeconds}
}
func spaceResponse(item domain.Space) map[string]any {
	return map[string]any{"id": item.ID, "key": item.Key, "name": item.Name, "status": spaceStatusName(item.Status), "allowed_origins": item.AllowedOrigins, "policy": policyRequest{AllowPersonal: item.Policy.AllowPersonal, AllowGroups: item.Policy.AllowGroups, AllowImages: item.Policy.AllowImages, AllowFiles: item.Policy.AllowFiles, AllowLinks: item.Policy.AllowLinks, MaxGroupMembers: item.Policy.MaxGroupMembers, MaxBodyLength: item.Policy.MaxBodyLength, MaxAttachments: item.Policy.MaxAttachments, MaxImageBytes: item.Policy.MaxImageBytes, MaxFileBytes: item.Policy.MaxFileBytes, AllowedFileMIMETypes: item.Policy.AllowedFileMIMETypes, EditWindowSeconds: item.Policy.EditWindowSeconds}, "created_by": item.CreatedBy, "created_at": item.CreatedAt, "updated_at": item.UpdatedAt}
}

func adminDialogResponse(item domain.Dialog) map[string]any {
	return map[string]any{
		"id": item.ID, "space_id": item.SpaceID, "type": item.Type, "status": item.Status, "title": item.Title,
		"version": item.Version, "member_count": item.MemberCount, "message_count": item.MessageCount,
		"max_message_sequence": item.MaxMessageSequence, "max_event_sequence": item.MaxEventSequence,
		"last_message_id": item.LastMessageID, "last_message_at": item.LastMessageAt, "created_by": item.CreatedBy,
		"created_at": item.CreatedAt, "updated_at": item.UpdatedAt,
	}
}
func parseSpaceStatus(raw string) (domain.SpaceStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "active":
		return domain.SpaceStatusActive, true
	case "disabled":
		return domain.SpaceStatusDisabled, true
	default:
		return 0, false
	}
}
func parseDialogStatus(raw string) (domain.DialogStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "active":
		return domain.DialogStatusActive, true
	case "closed":
		return domain.DialogStatusClosed, true
	case "hidden":
		return domain.DialogStatusHidden, true
	default:
		return 0, false
	}
}
func spaceStatusName(status domain.SpaceStatus) string {
	if status == domain.SpaceStatusActive {
		return "active"
	}
	return "disabled"
}
func defaultValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
