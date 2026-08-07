package admin

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

const DefaultListLimit = 20
const MaxListLimit = 100

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
type CreateSpaceInput struct {
	Key, Name      string
	AllowedOrigins []string
	Policy         domain.Policy
}
type UpdateSpaceInput struct {
	ID             uuid.UUID
	Name           string
	Status         domain.SpaceStatus
	AllowedOrigins []string
	Policy         domain.Policy
}
type ModerationView struct {
	Message     domain.Message
	Attachments []domain.Attachment
}

func (s Service) CreateSpace(ctx context.Context, actor domain.Actor, in CreateSpaceInput) (domain.Space, error) {
	if err := requireAdmin(actor); err != nil {
		return domain.Space{}, err
	}
	now := s.now()
	item := domain.Space{ID: s.newID(), Key: strings.TrimSpace(in.Key), Name: strings.TrimSpace(in.Name), Status: domain.SpaceStatusActive, AllowedOrigins: in.AllowedOrigins, Policy: in.Policy, CreatedBy: actor.UserID, CreatedAt: now, UpdatedAt: now}
	if err := item.Validate(); err != nil {
		return domain.Space{}, err
	}
	if err := s.Spaces.Create(ctx, item); err != nil {
		return domain.Space{}, err
	}
	return item, nil
}
func (s Service) ListSpaces(ctx context.Context, actor domain.Actor, limit, offset int) ([]domain.Space, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	if limit < 1 || limit > MaxListLimit || offset < 0 {
		return nil, domain.ErrValidation
	}
	return s.Spaces.List(ctx, limit, offset)
}
func (s Service) ListDialogs(ctx context.Context, actor domain.Actor, query repository.AdminDialogListQuery) ([]domain.Dialog, error) {
	if err := requireModerator(actor); err != nil {
		return nil, err
	}
	if query.Limit < 1 || query.Limit > MaxListLimit || query.Offset < 0 {
		return nil, domain.ErrValidation
	}
	return s.Dialogs.ListAdmin(ctx, query)
}
func (s Service) UpdateSpace(ctx context.Context, actor domain.Actor, in UpdateSpaceInput) (domain.Space, error) {
	if err := requireAdmin(actor); err != nil {
		return domain.Space{}, err
	}
	item, err := s.Spaces.GetByID(ctx, in.ID)
	if err != nil {
		return domain.Space{}, domain.ErrSpaceNotFound
	}
	item.Name = strings.TrimSpace(in.Name)
	item.Status = in.Status
	item.AllowedOrigins = in.AllowedOrigins
	item.Policy = in.Policy
	item.UpdatedAt = s.now()
	if err = item.Validate(); err != nil {
		return domain.Space{}, err
	}
	if err = s.Spaces.Update(ctx, item); err != nil {
		return domain.Space{}, err
	}
	return item, nil
}
func (s Service) CloseDialog(ctx context.Context, actor domain.Actor, id uuid.UUID) (domain.Dialog, error) {
	return s.setDialogStatus(ctx, actor, id, domain.DialogStatusClosed, domain.EventDialogClosed)
}
func (s Service) ReopenDialog(ctx context.Context, actor domain.Actor, id uuid.UUID) (domain.Dialog, error) {
	return s.setDialogStatus(ctx, actor, id, domain.DialogStatusActive, domain.EventDialogUpdated)
}
func (s Service) setDialogStatus(ctx context.Context, actor domain.Actor, id uuid.UUID, status domain.DialogStatus, subject domain.EventSubject) (domain.Dialog, error) {
	if err := requireAdmin(actor); err != nil {
		return domain.Dialog{}, err
	}
	var result domain.Dialog
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		item, err := s.Dialogs.GetByIDForUpdate(txCtx, id)
		if err != nil {
			return domain.ErrDialogNotFound
		}
		if item.Status == domain.DialogStatusHidden {
			return domain.ErrModerationConflict
		}
		if item.Status == status {
			result = item
			return nil
		}
		now := s.now()
		item.Status = status
		item.MaxEventSequence++
		item.Version++
		item.UpdatedAt = now
		if err := s.Dialogs.UpdateState(txCtx, item, item.Version-1); err != nil {
			return err
		}
		eventID := s.newID()
		payload, err := json.Marshal(map[string]any{"schema_version": 1, "event_id": eventID, "occurred_at": now, "dialog_id": item.ID, "space_id": item.SpaceID, "event_sequence": item.MaxEventSequence, "status": item.Status, "actor_id": actor.UserID})
		if err != nil {
			return err
		}
		if err = s.Outbox.Add(txCtx, domain.OutboxEvent{ID: eventID, DialogID: item.ID, AggregateType: "dialog", AggregateID: item.ID, Subject: subject, EventSequence: item.MaxEventSequence, SchemaVersion: 1, Payload: payload, NextAttemptAt: now, CreatedAt: now}); err != nil {
			return err
		}
		result = item
		return nil
	})
	return result, err
}
func (s Service) HideMessage(ctx context.Context, actor domain.Actor, id uuid.UUID) (ModerationView, error) {
	return s.moderate(ctx, actor, id, domain.MessageStatusHidden, domain.EventDialogMessageHidden)
}
func (s Service) RestoreMessage(ctx context.Context, actor domain.Actor, id uuid.UUID) (ModerationView, error) {
	return s.moderate(ctx, actor, id, domain.MessageStatusActive, domain.EventDialogMessageRestored)
}
func (s Service) moderate(ctx context.Context, actor domain.Actor, id uuid.UUID, target domain.MessageStatus, subject domain.EventSubject) (ModerationView, error) {
	if err := requireModerator(actor); err != nil {
		return ModerationView{}, err
	}
	var result ModerationView
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		item, err := s.Messages.GetByIDForUpdate(txCtx, id)
		if err != nil {
			return domain.ErrMessageNotFound
		}
		if item.Status == domain.MessageStatusDeleted {
			return domain.ErrModerationConflict
		}
		dialogItem, err := s.Dialogs.GetByIDForUpdate(txCtx, item.DialogID)
		if err != nil {
			return domain.ErrDialogNotFound
		}
		attachments, err := s.Attachments.ListByMessage(txCtx, item.DialogID, item.ID)
		if err != nil {
			return err
		}
		if item.Status == target {
			result = ModerationView{Message: item, Attachments: attachments}
			return nil
		}
		previous, version := item.Status, item.Version
		now := s.now()
		eventSequence := dialogItem.MaxEventSequence + 1
		item.Status = target
		item.Version++
		item.LastEventSequence = eventSequence
		item.UpdatedAt = now
		if err := s.Messages.UpdateModerationStatus(txCtx, item, previous, version); err != nil {
			return err
		}
		if target == domain.MessageStatusHidden {
			if err := s.Members.DecrementUnreadForDeletedMessage(txCtx, item.DialogID, item.SenderID, item.MessageSequence, eventSequence); err != nil {
				return err
			}
		} else {
			if err := s.Members.IncrementUnreadForRestoredMessage(txCtx, item.DialogID, item.SenderID, item.MessageSequence, eventSequence); err != nil {
				return err
			}
		}
		dialogItem.MaxEventSequence = eventSequence
		dialogItem.Version++
		dialogItem.UpdatedAt = now
		if err := s.Dialogs.UpdateState(txCtx, dialogItem, dialogItem.Version-1); err != nil {
			return err
		}
		eventID := s.newID()
		payload, err := json.Marshal(map[string]any{"schema_version": 1, "event_id": eventID, "occurred_at": now, "dialog_id": item.DialogID, "space_id": dialogItem.SpaceID, "event_sequence": eventSequence, "message_sequence": item.MessageSequence, "message_id": item.ID, "sender_id": item.SenderID, "status": item.Status, "version": item.Version, "actor_id": actor.UserID, "body": item.Body, "links": item.Links, "attachments": attachments})
		if err != nil {
			return err
		}
		if err = s.Outbox.Add(txCtx, domain.OutboxEvent{ID: eventID, DialogID: item.DialogID, AggregateType: "message", AggregateID: item.ID, Subject: subject, EventSequence: eventSequence, SchemaVersion: 1, Payload: payload, NextAttemptAt: now, CreatedAt: now}); err != nil {
			return err
		}
		result = ModerationView{Message: item, Attachments: attachments}
		return nil
	})
	return result, err
}
func requireAdmin(actor domain.Actor) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if !actor.IsAdmin() {
		return domain.ErrForbidden
	}
	return nil
}
func requireModerator(actor domain.Actor) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if !actor.CanModerate() {
		return domain.ErrForbidden
	}
	return nil
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
