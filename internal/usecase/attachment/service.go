package attachment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

const (
	defaultTTLMinutes        = 60
	defaultSignedURLMinutes  = 5
	defaultActivationRetries = 5
)

type TemporaryFileInput struct {
	OwnerID, DialogID  uuid.UUID
	Filename, MIMEType string
	Data               []byte
	TTLMinutes         int
}

type StoredFile struct{ ID uuid.UUID }

type FileStorage interface {
	UploadTemporary(context.Context, TemporaryFileInput) (StoredFile, error)
	Activate(context.Context, uuid.UUID) error
	SignedGETURL(context.Context, uuid.UUID, int) (string, error)
	Delete(context.Context, uuid.UUID) error
}

type FileScanner interface {
	Scan(context.Context, []byte) error
}

type Service struct {
	Spaces                repository.SpaceRepository
	Dialogs               repository.DialogRepository
	Members               repository.MemberRepository
	Messages              repository.MessageRepository
	Attachments           repository.AttachmentRepository
	Outbox                repository.OutboxRepository
	Tx                    repository.TransactionManager
	Files                 FileStorage
	Scanner               FileScanner
	Now                   func() time.Time
	NewID                 func() uuid.UUID
	TTLMinutes            int
	SignedURLMinutes      int
	ActivationMaxAttempts int
	WorkerLease           time.Duration
}

type UploadInput struct {
	DialogID uuid.UUID
	Filename string
	Data     []byte
}
type SignedURL struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}
type WorkResult struct {
	Activated int
	Failed    int
	Deleted   int
}

func (s Service) Upload(ctx context.Context, actor domain.Actor, input UploadInput) (domain.Attachment, error) {
	if err := actor.Validate(); err != nil {
		return domain.Attachment{}, err
	}
	if s.Files == nil {
		return domain.Attachment{}, errors.New("filestorage adapter is not configured")
	}
	dialogItem, space, _, err := s.readContext(ctx, actor, input.DialogID)
	if err != nil {
		return domain.Attachment{}, err
	}
	if dialogItem.Status != domain.DialogStatusActive {
		return domain.Attachment{}, domain.ErrDialogClosed
	}
	metadata, err := domain.InspectAttachment(input.Data, input.Filename, space.Policy)
	if err != nil {
		return domain.Attachment{}, err
	}
	if metadata.Kind == domain.AttachmentKindFile {
		if s.Scanner == nil {
			return domain.Attachment{}, domain.ErrFileScanUnavailable
		}
		if err := s.Scanner.Scan(ctx, input.Data); err != nil {
			return domain.Attachment{}, err
		}
	}
	now := s.now()
	pending, err := s.Attachments.CountPendingByUploader(ctx, input.DialogID, actor.UserID, now)
	if err != nil {
		return domain.Attachment{}, err
	}
	if pending >= int(space.Policy.MaxAttachments) {
		return domain.Attachment{}, domain.ErrInvalidAttachment
	}
	id, ttl := s.newID(), s.ttlMinutes()
	stored, err := s.Files.UploadTemporary(ctx, TemporaryFileInput{OwnerID: id, DialogID: input.DialogID, Filename: metadata.OriginalFilename, MIMEType: metadata.MIMEType, Data: input.Data, TTLMinutes: ttl})
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("upload temporary attachment: %w", err)
	}
	item := domain.Attachment{ID: id, DialogID: input.DialogID, UploaderID: actor.UserID, FileStorageID: stored.ID,
		Kind: metadata.Kind, Status: domain.AttachmentStatusPending, MIMEType: metadata.MIMEType, SizeBytes: metadata.SizeBytes,
		Width: metadata.Width, Height: metadata.Height, ChecksumSHA256: metadata.ChecksumSHA256,
		OriginalFilename: metadata.OriginalFilename, ExpiresAt: now.Add(time.Duration(ttl) * time.Minute), CreatedAt: now, UpdatedAt: now}
	if err := item.Validate(space.Policy); err != nil {
		return domain.Attachment{}, err
	}
	if err := s.Attachments.Create(ctx, item); err != nil {
		return domain.Attachment{}, err
	}
	return item, nil
}

func (s Service) GetSignedURL(ctx context.Context, actor domain.Actor, id uuid.UUID) (SignedURL, error) {
	if err := actor.Validate(); err != nil {
		return SignedURL{}, err
	}
	if s.Files == nil {
		return SignedURL{}, errors.New("filestorage adapter is not configured")
	}
	item, err := s.Attachments.GetByID(ctx, id)
	if err != nil {
		return SignedURL{}, domain.ErrAttachmentNotFound
	}
	if item.Status != domain.AttachmentStatusReady || item.MessageID == nil {
		return SignedURL{}, domain.ErrAttachmentNotReady
	}
	message, err := s.Messages.GetByID(ctx, *item.MessageID)
	if err != nil || message.Status != domain.MessageStatusActive {
		return SignedURL{}, domain.ErrAttachmentNotFound
	}
	_, _, member, err := s.readContext(ctx, actor, item.DialogID)
	if err != nil {
		return SignedURL{}, err
	}
	if message.MessageSequence < member.HistoryFromMessageSequence {
		return SignedURL{}, domain.ErrAttachmentNotFound
	}
	minutes := s.signedMinutes()
	url, err := s.Files.SignedGETURL(ctx, item.FileStorageID, minutes)
	if err != nil {
		return SignedURL{}, fmt.Errorf("request signed attachment URL: %w", err)
	}
	return SignedURL{URL: url, ExpiresAt: s.now().Add(time.Duration(minutes) * time.Minute)}, nil
}

func (s Service) Delete(ctx context.Context, actor domain.Actor, id uuid.UUID) (domain.Attachment, error) {
	if err := actor.Validate(); err != nil {
		return domain.Attachment{}, err
	}
	var result domain.Attachment
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		item, err := s.Attachments.GetByIDForUpdate(txCtx, id)
		if err != nil {
			return domain.ErrAttachmentNotFound
		}
		if item.UploaderID != actor.UserID {
			return domain.ErrForbidden
		}
		if _, _, _, err := s.readContext(txCtx, actor, item.DialogID); err != nil {
			return err
		}
		if item.Status == domain.AttachmentStatusDeleted {
			result = item
			return nil
		}
		if item.MessageID != nil {
			return fmt.Errorf("%w: bound attachments are deleted with their message", domain.ErrMessageConflict)
		}
		now := s.now()
		item.Status = domain.AttachmentStatusDeleted
		item.DeletedAt = &now
		item.DeleteNextAttemptAt = &now
		item.UpdatedAt = now
		item.LastError = ""
		if err := s.Attachments.UpdateStatus(txCtx, item); err != nil {
			return err
		}
		result = item
		return nil
	})
	return result, err
}

func (s Service) Process(ctx context.Context, limit int) (WorkResult, error) {
	if limit < 1 {
		return WorkResult{}, domain.ErrValidation
	}
	if s.Files == nil {
		return WorkResult{}, errors.New("filestorage adapter is not configured")
	}
	result := WorkResult{}
	var workErrors []error
	if err := s.expirePending(ctx, limit); err != nil {
		workErrors = append(workErrors, err)
	}
	claimTime := s.now()
	items, err := s.Attachments.ClaimForActivation(ctx, claimTime, claimTime.Add(s.workerLease()), limit)
	if err != nil {
		return result, err
	}
	for _, item := range items {
		if err := s.Files.Activate(ctx, item.FileStorageID); err != nil {
			terminal, recordErr := s.recordActivationFailure(ctx, item, err)
			if terminal {
				result.Failed++
			}
			if recordErr != nil {
				workErrors = append(workErrors, recordErr)
			}
			continue
		}
		changed, readyErr := s.markReady(ctx, item)
		if readyErr != nil {
			workErrors = append(workErrors, readyErr)
		} else if changed {
			result.Activated++
		}
	}
	claimTime = s.now()
	deletions, err := s.Attachments.ClaimForDeletion(ctx, claimTime, claimTime.Add(s.workerLease()), limit)
	if err != nil {
		return result, errors.Join(append(workErrors, err)...)
	}
	for _, item := range deletions {
		if err := s.Files.Delete(ctx, item.FileStorageID); err != nil {
			next := s.now().Add(retryDelay(item.DeleteAttempts + 1))
			if e := s.Attachments.RecordDeleteFailure(ctx, item.ID, next, truncate(err)); e != nil {
				workErrors = append(workErrors, e)
			}
			continue
		}
		if err := s.Attachments.MarkStorageDeleted(ctx, item.ID, s.now()); err != nil && !errors.Is(err, domain.ErrNotFound) {
			workErrors = append(workErrors, err)
			continue
		}
		result.Deleted++
	}
	return result, errors.Join(workErrors...)
}

func (s Service) expirePending(ctx context.Context, limit int) error {
	items, err := s.Attachments.ListExpired(ctx, s.now(), limit)
	if err != nil {
		return err
	}
	var errs []error
	for _, source := range items {
		err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
			item, e := s.Attachments.GetByIDForUpdate(txCtx, source.ID)
			if e != nil {
				return e
			}
			if item.Status != domain.AttachmentStatusPending || item.ExpiresAt.After(s.now()) {
				return nil
			}
			now := s.now()
			item.Status = domain.AttachmentStatusDeleted
			item.DeletedAt = &now
			item.DeleteNextAttemptAt = &now
			item.UpdatedAt = now
			return s.Attachments.UpdateStatus(txCtx, item)
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s Service) markReady(ctx context.Context, source domain.Attachment) (bool, error) {
	changed := false
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		message, dialogItem, err := s.lockLifecycle(txCtx, source)
		if err != nil {
			return err
		}
		item, updated, err := s.Attachments.MarkReadyIfProcessing(txCtx, source.ID, s.now())
		if err != nil || !updated {
			return err
		}
		if message.Status == domain.MessageStatusActive {
			if err := s.emitLifecycleLocked(txCtx, item, domain.EventDialogAttachmentReady, dialogItem, message); err != nil {
				return err
			}
		}
		if message.Status == domain.MessageStatusDeleted {
			return domain.ErrMessageConflict
		}
		changed = true
		return nil
	})
	return changed, err
}
func (s Service) recordActivationFailure(ctx context.Context, source domain.Attachment, cause error) (bool, error) {
	terminal := false
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		message, dialogItem, err := s.lockLifecycle(txCtx, source)
		if err != nil {
			return err
		}
		item, ended, err := s.Attachments.RecordActivationFailure(txCtx, source.ID, s.now().Add(retryDelay(source.ActivationAttempts+1)), truncate(cause), s.maxAttempts())
		if err != nil {
			return err
		}
		terminal = ended
		if ended && message.Status == domain.MessageStatusActive {
			return s.emitLifecycleLocked(txCtx, item, domain.EventDialogAttachmentFailed, dialogItem, message)
		}
		return nil
	})
	return terminal, err
}

func (s Service) lockLifecycle(ctx context.Context, source domain.Attachment) (domain.Message, domain.Dialog, error) {
	if source.MessageID == nil {
		return domain.Message{}, domain.Dialog{}, domain.ErrInvalidAttachment
	}
	message, err := s.Messages.GetByIDForUpdate(ctx, *source.MessageID)
	if err != nil || message.DialogID != source.DialogID {
		return domain.Message{}, domain.Dialog{}, domain.ErrMessageNotFound
	}
	dialogItem, err := s.Dialogs.GetByIDForUpdate(ctx, source.DialogID)
	if err != nil {
		return domain.Message{}, domain.Dialog{}, err
	}
	return message, dialogItem, nil
}

func (s Service) emitLifecycleLocked(ctx context.Context, item domain.Attachment, subject domain.EventSubject, dialogItem domain.Dialog, lockedMessage domain.Message) error {
	if item.MessageID == nil || lockedMessage.ID != *item.MessageID || lockedMessage.DialogID != item.DialogID || dialogItem.ID != item.DialogID {
		return domain.ErrInvalidAttachment
	}
	eventSequence := dialogItem.MaxEventSequence + 1
	message, err := s.Messages.AdvanceEvent(ctx, lockedMessage.ID, eventSequence)
	if err != nil {
		return err
	}
	dialogItem.MaxEventSequence = eventSequence
	dialogItem.Version++
	dialogItem.UpdatedAt = s.now()
	if err := s.Dialogs.UpdateState(ctx, dialogItem, dialogItem.Version-1); err != nil {
		return err
	}
	attachments, err := s.Attachments.ListByMessage(ctx, item.DialogID, message.ID)
	if err != nil {
		return err
	}
	eventID := s.newID()
	payload, err := json.Marshal(map[string]any{"schema_version": 1, "event_id": eventID, "occurred_at": s.now(), "dialog_id": item.DialogID, "event_sequence": eventSequence, "message_sequence": message.MessageSequence, "message_id": message.ID, "attachment_id": item.ID, "attachment_status": item.Status, "attachments": attachments})
	if err != nil {
		return err
	}
	return s.Outbox.Add(ctx, domain.OutboxEvent{ID: eventID, DialogID: item.DialogID, AggregateType: "attachment", AggregateID: item.ID, Subject: subject, EventSequence: eventSequence, SchemaVersion: 1, Payload: payload, NextAttemptAt: s.now(), CreatedAt: s.now()})
}
func (s Service) readContext(ctx context.Context, actor domain.Actor, dialogID uuid.UUID) (domain.Dialog, domain.Space, domain.Member, error) {
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
func (s Service) ttlMinutes() int {
	if s.TTLMinutes > 0 {
		return s.TTLMinutes
	}
	return defaultTTLMinutes
}
func (s Service) signedMinutes() int {
	if s.SignedURLMinutes > 0 {
		return s.SignedURLMinutes
	}
	return defaultSignedURLMinutes
}
func (s Service) maxAttempts() int {
	if s.ActivationMaxAttempts > 0 {
		return s.ActivationMaxAttempts
	}
	return defaultActivationRetries
}
func (s Service) workerLease() time.Duration {
	if s.WorkerLease > 0 {
		return s.WorkerLease
	}
	return 2 * time.Minute
}
func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	return min(5*time.Second*time.Duration(1<<min(attempt-1, 6)), 5*time.Minute)
}
func truncate(err error) string {
	message := err.Error()
	if len(message) > 1000 {
		return message[:1000]
	}
	return message
}
