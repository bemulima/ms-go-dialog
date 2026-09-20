package handlers

import (
	common "github.com/bemulima/ms-go-dialog/internal/transport/http/common"
	"net/http"
	"strconv"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common/middleware"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type MessageHandler struct{ Service *messageuc.Service }

func (h MessageHandler) Create(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DialogID         uuid.UUID                    `json:"dialog_id"`
		ReplyToMessageID *uuid.UUID                   `json:"reply_to_message_id"`
		Body             string                       `json:"body"`
		AttachmentIDs    []uuid.UUID                  `json:"attachment_ids"`
		IdempotencyKey   uuid.UUID                    `json:"idempotency_key"`
		LearningActionID *uuid.UUID                   `json:"learning_action_id"`
		LessonContext    *domain.LessonMessageContext `json:"lesson_context"`
	}
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Service.Create(r.Context(), middleware.Actor(r), messageuc.CreateInput{
		DialogID: request.DialogID, ReplyToMessageID: request.ReplyToMessageID,
		Body: request.Body, AttachmentIDs: request.AttachmentIDs, IdempotencyKey: request.IdempotencyKey,
		LearningActionID: request.LearningActionID, LessonContext: request.LessonContext,
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

func (h MessageHandler) Update(w http.ResponseWriter, r *http.Request) {
	messageID, ok := common.PathUUID(w, r, "messageID")
	if !ok {
		return
	}
	var request struct {
		Body    string `json:"body"`
		Version int    `json:"version"`
	}
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	view, err := h.Service.Update(r.Context(), middleware.Actor(r), messageuc.UpdateInput{MessageID: messageID, Body: request.Body, ExpectedVersion: request.Version})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.NewMessageResponse(view))
}

func (h MessageHandler) Delete(w http.ResponseWriter, r *http.Request) {
	messageID, ok := common.PathUUID(w, r, "messageID")
	if !ok {
		return
	}
	var request struct {
		Version int `json:"version"`
	}
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	view, err := h.Service.Delete(r.Context(), middleware.Actor(r), messageuc.DeleteInput{MessageID: messageID, ExpectedVersion: request.Version})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.NewMessageResponse(view))
}

func (h MessageHandler) Get(w http.ResponseWriter, r *http.Request) {
	messageID, err := uuid.Parse(chi.URLParam(r, "messageID"))
	if err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	view, err := h.Service.Get(r.Context(), middleware.Actor(r), messageID)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.NewMessageResponse(view))
}

func (h MessageHandler) Window(w http.ResponseWriter, r *http.Request) {
	dialogID, err := uuid.Parse(r.URL.Query().Get("dialog_id"))
	before, after := common.QueryInt(r, "before", 10), common.QueryInt(r, "after", 20)
	if err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	window, err := h.Service.Window(r.Context(), middleware.Actor(r), dialogID, before, after)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	items := make([]common.MessageResponse, 0, len(window.Items))
	for _, item := range window.Items {
		items = append(items, common.NewMessageResponse(item))
	}
	var older, newer *string
	if len(window.Items) > 0 {
		first, last := window.Items[0].Message, window.Items[len(window.Items)-1].Message
		if window.HasOlder {
			value := common.EncodeMessageCursor(dialogID, repository.MessageCursor{Sequence: first.MessageSequence, ID: first.ID}, "before")
			older = &value
		}
		if window.HasNewer {
			value := common.EncodeMessageCursor(dialogID, repository.MessageCursor{Sequence: last.MessageSequence, ID: last.ID}, "after")
			newer = &value
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "older_cursor": older, "newer_cursor": newer, "read_state": window.ReadState})
}

func (h MessageHandler) List(w http.ResponseWriter, r *http.Request) {
	dialogID, err := uuid.Parse(r.URL.Query().Get("dialog_id"))
	limit := common.QueryInt(r, "limit", messageuc.DefaultPageLimit)
	beforeRaw, afterRaw := r.URL.Query().Get("before_cursor"), r.URL.Query().Get("after_cursor")
	if err != nil || limit < 1 || limit > messageuc.MaxPageLimit || (beforeRaw != "" && afterRaw != "") {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	before, err := common.DecodeMessageCursor(beforeRaw, dialogID, "before")
	if err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	after, err := common.DecodeMessageCursor(afterRaw, dialogID, "after")
	if err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	items, err := h.Service.List(r.Context(), middleware.Actor(r), repository.MessageListQuery{DialogID: dialogID, Before: before, After: after, Limit: limit + 1})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		if after != nil {
			items = items[:limit]
		} else {
			items = items[len(items)-limit:]
		}
	}
	responses := make([]common.MessageResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, common.NewMessageResponse(item))
	}
	var next *string
	if hasMore && len(items) > 0 {
		item, direction := items[0].Message, "before"
		if after != nil {
			item, direction = items[len(items)-1].Message, "after"
		}
		value := common.EncodeMessageCursor(dialogID, repository.MessageCursor{Sequence: item.MessageSequence, ID: item.ID}, direction)
		next = &value
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": responses, "next_cursor": next})
}

func (h MessageHandler) Changes(w http.ResponseWriter, r *http.Request) {
	dialogID, err := uuid.Parse(r.URL.Query().Get("dialog_id"))
	after, parseErr := strconv.ParseInt(r.URL.Query().Get("after_event_sequence"), 10, 64)
	if r.URL.Query().Get("after_event_sequence") == "" {
		after, parseErr = 0, nil
	}
	limit := common.QueryInt(r, "limit", messageuc.DefaultPageLimit)
	if err != nil || parseErr != nil || limit < 1 || limit > messageuc.MaxPageLimit {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	items, err := h.Service.ListChanges(r.Context(), middleware.Actor(r), repository.MessageChangeQuery{DialogID: dialogID, AfterEventSequence: after, Limit: limit + 1})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	responses := make([]common.MessageResponse, 0, len(items))
	next := after
	for _, item := range items {
		responses = append(responses, common.NewMessageResponse(item))
		if item.Message.LastEventSequence > next {
			next = item.Message.LastEventSequence
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": responses, "next_after_event_sequence": next, "has_more": hasMore})
}

func (h MessageHandler) Read(w http.ResponseWriter, r *http.Request) {
	dialogID, ok := common.PathUUID(w, r, "dialogID")
	if !ok {
		return
	}
	var request struct {
		Through int64 `json:"through_message_sequence"`
	}
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	state, err := h.Service.ReadThrough(r.Context(), middleware.Actor(r), dialogID, request.Through)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, state)
}

func (h MessageHandler) ReadAll(w http.ResponseWriter, r *http.Request) {
	dialogID, ok := common.PathUUID(w, r, "dialogID")
	if !ok {
		return
	}
	if err := common.DecodeOptionalEmptyBody(r); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	state, err := h.Service.ReadAll(r.Context(), middleware.Actor(r), dialogID)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, state)
}
