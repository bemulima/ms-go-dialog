package message

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

const (
	DefaultPageLimit = 20
	MaxPageLimit     = 100
)

type Service struct {
	Spaces      repository.SpaceRepository
	Dialogs     repository.DialogRepository
	Members     repository.MemberRepository
	Messages    repository.MessageRepository
	Attachments repository.AttachmentRepository
	Outbox      repository.OutboxRepository
	Tx          repository.TransactionManager
	Now         func() time.Time
	NewID       func() uuid.UUID
}

type CreateInput struct {
	DialogID         uuid.UUID
	ReplyToMessageID *uuid.UUID
	Body             string
	AttachmentIDs    []uuid.UUID
	IdempotencyKey   uuid.UUID
}

type UpdateInput struct {
	MessageID       uuid.UUID
	Body            string
	ExpectedVersion int
}

type DeleteInput struct {
	MessageID       uuid.UUID
	ExpectedVersion int
}

type View struct {
	Message     domain.Message
	Attachments []domain.Attachment
}

type CreateResult struct {
	View    View
	Created bool
}

type ReadState struct {
	LastReadMessageSequence    int64      `json:"last_read_message_sequence"`
	FirstUnreadMessageSequence *int64     `json:"first_unread_message_sequence"`
	UnreadCount                int64      `json:"unread_count"`
	LastReadAt                 *time.Time `json:"last_read_at"`
	MaxMessageSequence         int64      `json:"max_message_sequence"`
	MaxEventSequence           int64      `json:"max_event_sequence"`
}

type Window struct {
	Items     []View
	ReadState ReadState
}

func (s Service) Create(ctx context.Context, actor domain.Actor, in CreateInput) (CreateResult, error) {
	if err := actor.Validate(); err != nil {
		return CreateResult{}, err
	}
	if in.DialogID == uuid.Nil || in.IdempotencyKey == uuid.Nil {
		return CreateResult{}, fmt.Errorf("%w: dialog and idempotency UUIDs are required", domain.ErrValidation)
	}
	if err := uniqueIDs(in.AttachmentIDs); err != nil {
		return CreateResult{}, err
	}

	var result CreateResult
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.Messages.LockIdempotencyKey(txCtx, actor.UserID, in.IdempotencyKey); err != nil {
			return err
		}
		existing, err := s.Messages.GetByIdempotencyKey(txCtx, actor.UserID, in.IdempotencyKey)
		if err == nil {
			return s.resolveReplay(txCtx, existing, in, &result)
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		dialogItem, space, _, err := s.loadWritableContext(txCtx, actor, in.DialogID, true)
		if err != nil {
			return err
		}
		attachments, imageCount, fileCount, err := s.bindableAttachments(txCtx, actor.UserID, in.DialogID, in.AttachmentIDs, space.Policy)
		if err != nil {
			return err
		}
		content := domain.AnalyzeMessageContent(in.Body, imageCount, fileCount)
		if err := content.Validate(space.Policy); err != nil {
			return err
		}
		if in.ReplyToMessageID != nil {
			reply, err := s.Messages.GetByID(txCtx, *in.ReplyToMessageID)
			if err != nil || reply.DialogID != in.DialogID || reply.Status == domain.MessageStatusHidden {
				return domain.ErrMessageNotFound
			}
		}

		now := s.now()
		messageSequence := dialogItem.MaxMessageSequence + 1
		eventSequence := dialogItem.MaxEventSequence + 1
		item := domain.Message{
			ID: s.newID(), DialogID: in.DialogID, SenderID: actor.UserID, ReplyToMessageID: in.ReplyToMessageID,
			Body: content.Body, Links: content.Links, Status: domain.MessageStatusActive, Version: 1,
			MessageSequence: messageSequence, LastEventSequence: eventSequence,
			IdempotencyKey: in.IdempotencyKey, CreatedAt: now, UpdatedAt: now,
		}
		if err := item.Validate(); err != nil {
			return err
		}
		if err := s.Messages.Create(txCtx, item); err != nil {
			return err
		}
		for index := range attachments {
			if err := s.Attachments.BindToMessage(txCtx, attachments[index].ID, item.DialogID, item.ID, actor.UserID); err != nil {
				return err
			}
			attachments[index].MessageID = &item.ID
			attachments[index].Status = domain.AttachmentStatusProcessing
			attachments[index].UpdatedAt = now
		}
		dialogItem.MessageCount++
		dialogItem.MaxMessageSequence = messageSequence
		dialogItem.MaxEventSequence = eventSequence
		dialogItem.LastMessageID = &item.ID
		dialogItem.LastMessageAt = &now
		dialogItem.Version++
		dialogItem.UpdatedAt = now
		if err := s.Dialogs.UpdateState(txCtx, dialogItem, dialogItem.Version-1); err != nil {
			return err
		}
		if err := s.Members.IncrementUnreadRecipients(txCtx, item.DialogID, actor.UserID, eventSequence); err != nil {
			return err
		}
		if err := s.addEvent(txCtx, item, attachments, domain.EventDialogMessageCreated, actor.UserID, now); err != nil {
			return err
		}
		result = CreateResult{Created: true, View: View{Message: item, Attachments: attachments}}
		return nil
	})
	return result, err
}

func (s Service) Update(ctx context.Context, actor domain.Actor, in UpdateInput) (View, error) {
	if err := actor.Validate(); err != nil {
		return View{}, err
	}
	var result View
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		item, err := s.Messages.GetByIDForUpdate(txCtx, in.MessageID)
		if err != nil {
			return domain.ErrMessageNotFound
		}
		dialogItem, space, _, err := s.loadWritableContext(txCtx, actor, item.DialogID, true)
		if err != nil {
			return err
		}
		if item.SenderID != actor.UserID || item.Status != domain.MessageStatusActive {
			return domain.ErrForbidden
		}
		if in.ExpectedVersion < 1 || item.Version != in.ExpectedVersion {
			return domain.ErrMessageConflict
		}
		now := s.now()
		if space.Policy.EditWindowSeconds == 0 || now.After(item.CreatedAt.Add(time.Duration(space.Policy.EditWindowSeconds)*time.Second)) {
			return domain.ErrForbidden
		}
		attachments, err := s.listAttachments(txCtx, item.DialogID, item.ID)
		if err != nil {
			return err
		}
		images, files := attachmentKinds(attachments)
		content := domain.AnalyzeMessageContent(in.Body, images, files)
		if err := content.Validate(space.Policy); err != nil {
			return err
		}
		eventSequence := dialogItem.MaxEventSequence + 1
		item.Body, item.Links, item.LastEventSequence = content.Body, content.Links, eventSequence
		item.Version++
		item.EditedAt, item.UpdatedAt = &now, now
		if err := s.Messages.UpdateContent(txCtx, item, in.ExpectedVersion); err != nil {
			return err
		}
		dialogItem.MaxEventSequence, dialogItem.Version, dialogItem.UpdatedAt = eventSequence, dialogItem.Version+1, now
		if err := s.Dialogs.UpdateState(txCtx, dialogItem, dialogItem.Version-1); err != nil {
			return err
		}
		if err := s.addEvent(txCtx, item, attachments, domain.EventDialogMessageUpdated, actor.UserID, now); err != nil {
			return err
		}
		result = View{Message: item, Attachments: attachments}
		return nil
	})
	return result, err
}

func (s Service) Delete(ctx context.Context, actor domain.Actor, in DeleteInput) (View, error) {
	if err := actor.Validate(); err != nil {
		return View{}, err
	}
	var result View
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		item, err := s.Messages.GetByIDForUpdate(txCtx, in.MessageID)
		if err != nil {
			return domain.ErrMessageNotFound
		}
		dialogItem, space, _, err := s.loadWritableContext(txCtx, actor, item.DialogID, true)
		if err != nil {
			return err
		}
		if item.SenderID != actor.UserID || item.Status != domain.MessageStatusActive || item.Version != in.ExpectedVersion {
			return domain.ErrMessageConflict
		}
		now := s.now()
		if space.Policy.EditWindowSeconds == 0 || now.After(item.CreatedAt.Add(time.Duration(space.Policy.EditWindowSeconds)*time.Second)) {
			return domain.ErrForbidden
		}
		eventSequence := dialogItem.MaxEventSequence + 1
		item.Body, item.Links, item.Status, item.LastEventSequence = "", nil, domain.MessageStatusDeleted, eventSequence
		item.Version++
		item.DeletedAt, item.UpdatedAt = &now, now
		if err := s.Messages.MarkDeleted(txCtx, item, in.ExpectedVersion); err != nil {
			return err
		}
		if s.Attachments != nil {
			if err := s.Attachments.MarkMessageAttachmentsDeleted(txCtx, item.DialogID, item.ID, now); err != nil {
				return err
			}
		}
		if err := s.Members.DecrementUnreadForDeletedMessage(txCtx, item.DialogID, item.SenderID, item.MessageSequence, eventSequence); err != nil {
			return err
		}
		dialogItem.MaxEventSequence, dialogItem.Version, dialogItem.UpdatedAt = eventSequence, dialogItem.Version+1, now
		if err := s.Dialogs.UpdateState(txCtx, dialogItem, dialogItem.Version-1); err != nil {
			return err
		}
		if err := s.addEvent(txCtx, item, nil, domain.EventDialogMessageDeleted, actor.UserID, now); err != nil {
			return err
		}
		result = View{Message: item}
		return nil
	})
	return result, err
}

func (s Service) ReadThrough(ctx context.Context, actor domain.Actor, dialogID uuid.UUID, through int64) (ReadState, error) {
	return s.advanceRead(ctx, actor, dialogID, &through)
}

func (s Service) ReadAll(ctx context.Context, actor domain.Actor, dialogID uuid.UUID) (ReadState, error) {
	return s.advanceRead(ctx, actor, dialogID, nil)
}

func (s Service) advanceRead(ctx context.Context, actor domain.Actor, dialogID uuid.UUID, requested *int64) (ReadState, error) {
	if err := actor.Validate(); err != nil {
		return ReadState{}, err
	}
	var state ReadState
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		dialogItem, err := s.Dialogs.GetByIDForUpdate(txCtx, dialogID)
		if err != nil || dialogItem.Status == domain.DialogStatusHidden {
			return domain.ErrDialogNotFound
		}
		member, err := s.Members.GetForUpdate(txCtx, dialogID, actor.UserID)
		if err != nil || member.Status != domain.MemberStatusActive {
			return domain.ErrForbidden
		}
		through := dialogItem.MaxMessageSequence
		if requested != nil {
			through = *requested
		}
		newlyRead, err := s.Messages.CountUnreadIncoming(txCtx, dialogID, actor.UserID, member.LastReadMessageSequence, through)
		if err != nil {
			return err
		}
		now := s.now()
		var changed bool
		if requested == nil {
			changed, err = member.MarkAllRead(dialogItem.MaxMessageSequence, now)
		} else {
			changed, err = member.AdvanceRead(through, dialogItem.MaxMessageSequence, newlyRead, now)
		}
		if err != nil {
			return err
		}
		if changed {
			eventSequence := dialogItem.MaxEventSequence + 1
			member.LastEventSequence = eventSequence
			if err := s.Members.Update(txCtx, member); err != nil {
				return err
			}
			dialogItem.MaxEventSequence, dialogItem.Version, dialogItem.UpdatedAt = eventSequence, dialogItem.Version+1, now
			if err := s.Dialogs.UpdateState(txCtx, dialogItem, dialogItem.Version-1); err != nil {
				return err
			}
			if err := s.addReadEvent(txCtx, dialogItem, member, now); err != nil {
				return err
			}
		}
		state, err = s.readState(txCtx, dialogItem, member)
		return err
	})
	return state, err
}

func (s Service) Window(ctx context.Context, actor domain.Actor, dialogID uuid.UUID, before, after int) (Window, error) {
	if err := actor.Validate(); err != nil {
		return Window{}, err
	}
	if before < 0 || after < 0 || before+after < 1 || before+after > MaxPageLimit {
		return Window{}, fmt.Errorf("%w: invalid message window", domain.ErrValidation)
	}
	dialogItem, _, member, err := s.loadReadableContext(ctx, actor, dialogID)
	if err != nil {
		return Window{}, err
	}
	anchor := dialogItem.MaxMessageSequence + 1
	if member.UnreadCount > 0 {
		first, err := s.Messages.FirstUnreadIncoming(ctx, dialogID, actor.UserID, member.LastReadMessageSequence)
		if err == nil {
			anchor = first
		} else if !errors.Is(err, domain.ErrNotFound) {
			return Window{}, err
		}
	}
	items, err := s.Messages.Window(ctx, repository.MessageWindowQuery{DialogID: dialogID, AnchorSequence: anchor, Before: before, After: after})
	if err != nil {
		return Window{}, err
	}
	views, err := s.views(ctx, items)
	if err != nil {
		return Window{}, err
	}
	state, err := s.readState(ctx, dialogItem, member)
	return Window{Items: views, ReadState: state}, err
}

func (s Service) List(ctx context.Context, actor domain.Actor, query repository.MessageListQuery) ([]View, error) {
	if err := actor.Validate(); err != nil {
		return nil, err
	}
	if query.Limit < 1 || query.Limit > MaxPageLimit+1 {
		return nil, fmt.Errorf("%w: invalid page limit", domain.ErrValidation)
	}
	if _, _, _, err := s.loadReadableContext(ctx, actor, query.DialogID); err != nil {
		return nil, err
	}
	items, err := s.Messages.List(ctx, query)
	if err != nil {
		return nil, err
	}
	return s.views(ctx, items)
}

func (s Service) ListChanges(ctx context.Context, actor domain.Actor, query repository.MessageChangeQuery) ([]View, error) {
	if err := actor.Validate(); err != nil {
		return nil, err
	}
	if query.AfterEventSequence < 0 || query.Limit < 1 || query.Limit > MaxPageLimit+1 {
		return nil, fmt.Errorf("%w: invalid changes cursor or limit", domain.ErrValidation)
	}
	if _, _, _, err := s.loadReadableContext(ctx, actor, query.DialogID); err != nil {
		return nil, err
	}
	items, err := s.Messages.ListChanges(ctx, query)
	if err != nil {
		return nil, err
	}
	return s.views(ctx, items)
}

func (s Service) Get(ctx context.Context, actor domain.Actor, messageID uuid.UUID) (View, error) {
	if err := actor.Validate(); err != nil {
		return View{}, err
	}
	item, err := s.Messages.GetByID(ctx, messageID)
	if err != nil || item.Status == domain.MessageStatusHidden {
		return View{}, domain.ErrMessageNotFound
	}
	if _, _, _, err := s.loadReadableContext(ctx, actor, item.DialogID); err != nil {
		return View{}, err
	}
	attachments, err := s.listAttachments(ctx, item.DialogID, item.ID)
	return View{Message: item, Attachments: attachments}, err
}

func (s Service) loadReadableContext(ctx context.Context, actor domain.Actor, dialogID uuid.UUID) (domain.Dialog, domain.Space, domain.Member, error) {
	item, err := s.Dialogs.GetByID(ctx, dialogID)
	if err != nil || item.Status == domain.DialogStatusHidden {
		return domain.Dialog{}, domain.Space{}, domain.Member{}, domain.ErrDialogNotFound
	}
	space, err := s.Spaces.GetByID(ctx, item.SpaceID)
	if err != nil || space.Status != domain.SpaceStatusActive {
		return domain.Dialog{}, domain.Space{}, domain.Member{}, domain.ErrDialogNotFound
	}
	member, err := s.Members.Get(ctx, dialogID, actor.UserID)
	if err != nil || member.Status != domain.MemberStatusActive {
		return domain.Dialog{}, domain.Space{}, domain.Member{}, domain.ErrForbidden
	}
	return item, space, member, nil
}

func (s Service) loadWritableContext(ctx context.Context, actor domain.Actor, dialogID uuid.UUID, forUpdate bool) (domain.Dialog, domain.Space, domain.Member, error) {
	var item domain.Dialog
	var err error
	if forUpdate {
		item, err = s.Dialogs.GetByIDForUpdate(ctx, dialogID)
	} else {
		item, err = s.Dialogs.GetByID(ctx, dialogID)
	}
	if err != nil || item.Status == domain.DialogStatusHidden {
		return domain.Dialog{}, domain.Space{}, domain.Member{}, domain.ErrDialogNotFound
	}
	if item.Status != domain.DialogStatusActive {
		return domain.Dialog{}, domain.Space{}, domain.Member{}, domain.ErrDialogClosed
	}
	space, err := s.Spaces.GetByID(ctx, item.SpaceID)
	if err != nil || space.Status != domain.SpaceStatusActive {
		return domain.Dialog{}, domain.Space{}, domain.Member{}, domain.ErrDialogNotFound
	}
	member, err := s.Members.Get(ctx, dialogID, actor.UserID)
	if err != nil || member.Status != domain.MemberStatusActive {
		return domain.Dialog{}, domain.Space{}, domain.Member{}, domain.ErrForbidden
	}
	return item, space, member, nil
}

func (s Service) bindableAttachments(ctx context.Context, uploaderID, dialogID uuid.UUID, ids []uuid.UUID, policy domain.Policy) ([]domain.Attachment, int, int, error) {
	if len(ids) == 0 {
		return nil, 0, 0, nil
	}
	if s.Attachments == nil || len(ids) > int(policy.MaxAttachments) {
		return nil, 0, 0, domain.ErrInvalidAttachment
	}
	result := make([]domain.Attachment, 0, len(ids))
	for _, id := range ids {
		item, err := s.Attachments.GetByIDForUpdate(ctx, id)
		if err != nil || item.DialogID != dialogID || item.UploaderID != uploaderID || item.MessageID != nil || item.Status != domain.AttachmentStatusPending || !item.ExpiresAt.After(s.now()) {
			return nil, 0, 0, domain.ErrInvalidAttachment
		}
		if err := item.Validate(policy); err != nil {
			return nil, 0, 0, err
		}
		result = append(result, item)
	}
	images, files := attachmentKinds(result)
	return result, images, files, nil
}

func (s Service) resolveReplay(ctx context.Context, existing domain.Message, in CreateInput, result *CreateResult) error {
	attachments, err := s.listAttachments(ctx, existing.DialogID, existing.ID)
	if err != nil {
		return err
	}
	if existing.DialogID != in.DialogID || existing.Body != in.Body || !sameUUID(existing.ReplyToMessageID, in.ReplyToMessageID) || !sameAttachmentIDs(attachments, in.AttachmentIDs) {
		return domain.ErrIdempotencyConflict
	}
	*result = CreateResult{View: View{Message: existing, Attachments: attachments}}
	return nil
}

func (s Service) views(ctx context.Context, items []domain.Message) ([]View, error) {
	result := make([]View, 0, len(items))
	for _, item := range items {
		attachments, err := s.listAttachments(ctx, item.DialogID, item.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, View{Message: item, Attachments: attachments})
	}
	return result, nil
}

func (s Service) listAttachments(ctx context.Context, dialogID, messageID uuid.UUID) ([]domain.Attachment, error) {
	if s.Attachments == nil {
		return nil, nil
	}
	return s.Attachments.ListByMessage(ctx, dialogID, messageID)
}

func (s Service) readState(ctx context.Context, item domain.Dialog, member domain.Member) (ReadState, error) {
	state := ReadState{
		LastReadMessageSequence: member.LastReadMessageSequence, UnreadCount: member.UnreadCount,
		LastReadAt: member.LastReadAt, MaxMessageSequence: item.MaxMessageSequence, MaxEventSequence: item.MaxEventSequence,
	}
	if member.UnreadCount > 0 {
		first, err := s.Messages.FirstUnreadIncoming(ctx, item.ID, member.UserID, member.LastReadMessageSequence)
		if err == nil {
			state.FirstUnreadMessageSequence = &first
		} else if !errors.Is(err, domain.ErrNotFound) {
			return ReadState{}, err
		}
	}
	return state, nil
}

func (s Service) addEvent(ctx context.Context, item domain.Message, attachments []domain.Attachment, subject domain.EventSubject, actorID uuid.UUID, now time.Time) error {
	eventID := s.newID()
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "event_id": eventID, "occurred_at": now,
		"dialog_id": item.DialogID, "event_sequence": item.LastEventSequence,
		"message_sequence": item.MessageSequence, "message_id": item.ID,
		"sender_id": item.SenderID, "actor_id": actorID, "status": item.Status,
		"version": item.Version, "body": item.Body, "links": item.Links, "attachments": attachments,
	})
	if err != nil {
		return err
	}
	return s.Outbox.Add(ctx, domain.OutboxEvent{
		ID: eventID, DialogID: item.DialogID, AggregateType: "message", AggregateID: item.ID,
		Subject: subject, EventSequence: item.LastEventSequence, SchemaVersion: 1,
		Payload: payload, NextAttemptAt: now, CreatedAt: now,
	})
}

func (s Service) addReadEvent(ctx context.Context, item domain.Dialog, member domain.Member, now time.Time) error {
	eventID := s.newID()
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "event_id": eventID, "occurred_at": now,
		"dialog_id": item.ID, "event_sequence": item.MaxEventSequence,
		"user_id": member.UserID, "read_through_message_sequence": member.LastReadMessageSequence,
		"unread_count": member.UnreadCount, "read_at": member.LastReadAt,
	})
	if err != nil {
		return err
	}
	return s.Outbox.Add(ctx, domain.OutboxEvent{
		ID: eventID, DialogID: item.ID, AggregateType: "member", AggregateID: member.UserID,
		Subject: domain.EventDialogReadUpdated, EventSequence: item.MaxEventSequence,
		SchemaVersion: 1, Payload: payload, NextAttemptAt: now, CreatedAt: now,
	})
}

func attachmentKinds(items []domain.Attachment) (int, int) {
	images, files := 0, 0
	for _, item := range items {
		if item.Kind == domain.AttachmentKindImage {
			images++
		} else if item.Kind == domain.AttachmentKindFile {
			files++
		}
	}
	return images, files
}

func uniqueIDs(ids []uuid.UUID) error {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return fmt.Errorf("%w: attachment UUID is required", domain.ErrValidation)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%w: duplicate attachment UUID", domain.ErrValidation)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func sameUUID(first, second *uuid.UUID) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return *first == *second
}

func sameAttachmentIDs(items []domain.Attachment, ids []uuid.UUID) bool {
	if len(items) != len(ids) {
		return false
	}
	left, right := make([]string, len(items)), make([]string, len(ids))
	for index := range items {
		left[index] = items[index].ID.String()
	}
	for index := range ids {
		right[index] = ids[index].String()
	}
	sort.Strings(left)
	sort.Strings(right)
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Service) newID() uuid.UUID {
	if s.NewID != nil {
		return s.NewID()
	}
	return uuid.New()
}
