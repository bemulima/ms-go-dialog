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
	Spaces                repository.SpaceRepository
	Dialogs               repository.DialogRepository
	Members               repository.MemberRepository
	Messages              repository.MessageRepository
	TeacherMessages       repository.TeacherMessageRepository
	CanonicalStudentTurns repository.CanonicalStudentTurnRepository
	Attachments           repository.AttachmentRepository
	Outbox                repository.OutboxRepository
	Blocks                repository.BlockRepository
	Tx                    repository.TransactionManager
	Now                   func() time.Time
	NewID                 func() uuid.UUID
	// TeacherOrderingV2Enabled is the sole rollout switch for both private
	// teacher_turn_sequence allocation and exactly one V2 request emission.
	// When false, the V1 request code path remains byte-compatible.
	TeacherOrderingV2Enabled bool
	// CanonicalStudentTurnEnabled gates only new trusted receipt
	// materializations. Exact committed replays remain recoverable when it is
	// disabled; it is valid only together with the V2 ordering producer.
	CanonicalStudentTurnEnabled bool
	OnTeacherRequestV2          func(TeacherRequestV2Observation)
	OnCanonicalStudentTurn      func(CanonicalStudentTurnObservation)
}

// TeacherRequestV2Observation is emitted only after the transaction that
// stored the source message, dense sequence, and V2 outbox request commits.
// It is for existing structured logs and zero-label metrics only; it is not a
// browser or event contract.
type TeacherRequestV2Observation struct {
	DialogID                  uuid.UUID
	CanonicalStudentMessageID uuid.UUID
	TeacherTurnSequence       int64
	CorrelationID             uuid.UUID
	CausationID               uuid.UUID
	SourceEventID             uuid.UUID
}

// CanonicalStudentTurnObservation is emitted after the one transaction that
// committed the private ledger, normal message, sequence, and V2 trigger.
// It is structured-log evidence only; IDs never become metric labels.
type CanonicalStudentTurnObservation struct {
	ActionReceiptID            uuid.UUID
	CorrelationID              uuid.UUID
	CausationID                uuid.UUID
	SourcePromptMessageID      uuid.UUID
	SourcePromptMessageVersion int
	BlockID                    string
	ActionID                   string
	SourceUIDigest             string
	CanonicalStudentMessageID  uuid.UUID
	TeacherTurnSequence        int64
}

type CreateInput struct {
	DialogID         uuid.UUID
	ReplyToMessageID *uuid.UUID
	Body             string
	AttachmentIDs    []uuid.UUID
	IdempotencyKey   uuid.UUID
	LearningActionID *uuid.UUID
	LessonContext    *domain.LessonMessageContext
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
	HasOlder  bool
	HasNewer  bool
}

const MaxTeacherContextMessages = 50

type TeacherRequestContext struct {
	Dialog   domain.Dialog
	Source   domain.Message
	Messages []domain.Message
}

type AssistantUISourceInput struct {
	DialogID          uuid.UUID
	StudentID         uuid.UUID
	PersonalTeacherID uuid.UUID
	MessageID         uuid.UUID
}

type AppendTeacherResponseInput struct {
	DialogID          uuid.UUID
	PersonalTeacherID uuid.UUID
	SourceMessageID   uuid.UUID
	IdempotencyKey    uuid.UUID
	Body              string
	AssistantUI       json.RawMessage
}

type AppendTeacherProactiveInput struct {
	DialogID          uuid.UUID
	PersonalTeacherID uuid.UUID
	IdempotencyKey    uuid.UUID
	Body              string
	AssistantUI       json.RawMessage
}

type AppendStudentChannelMessageInput struct {
	DialogID          uuid.UUID
	StudentID         uuid.UUID
	PersonalTeacherID uuid.UUID
	IdempotencyKey    uuid.UUID
	Channel           domain.MessageChannel
	Body              string
}

// MaterializeCanonicalStudentTurnInput is internal-only trusted input. The
// normal message response deliberately does not expose its private identity.
type MaterializeCanonicalStudentTurnInput struct {
	Identity      domain.CanonicalStudentTurnIdentity
	CanonicalBody string
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
	lessonContext, err := domain.NormalizeLessonMessageContext(in.LessonContext)
	if err != nil {
		return CreateResult{}, err
	}
	in.LessonContext = lessonContext

	var result CreateResult
	var teacherRequestV2 *TeacherRequestV2Observation
	err = s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
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

		dialogItem, space, currentMember, err := s.loadWritableContext(txCtx, actor, in.DialogID, true)
		if err != nil {
			return err
		}
		if dialogItem.Type == domain.DialogTypePersonal && s.Blocks != nil {
			members, err := s.Members.ListActive(txCtx, dialogItem.ID)
			if err != nil {
				return err
			}
			for _, member := range members {
				if member.UserID == actor.UserID {
					continue
				}
				blocked, err := s.Blocks.ExistsEitherDirection(txCtx, actor.UserID, member.UserID)
				if err != nil {
					return err
				}
				if blocked {
					return domain.ErrBlocked
				}
			}
		}
		if err := validateLearningContextBinding(dialogItem, in.LearningActionID, in.LessonContext); err != nil {
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
			if err != nil || reply.DialogID != in.DialogID || reply.Status == domain.MessageStatusHidden || reply.MessageSequence < currentMember.HistoryFromMessageSequence {
				return domain.ErrMessageNotFound
			}
		}

		var teacherTurnSequence *int64
		if dialogItem.Type == domain.DialogTypeTeacher {
			teacherTurnSequence, err = s.allocateTeacherTurnSequence(&dialogItem)
			if err != nil {
				return err
			}
		}
		now := s.now()
		messageSequence := dialogItem.MaxMessageSequence + 1
		eventSequence := dialogItem.MaxEventSequence + 1
		item := domain.Message{
			ID: s.newID(), DialogID: in.DialogID, AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelWeb, SenderID: actor.UserID,
			LearningActionID: in.LearningActionID, LessonContext: in.LessonContext, ReplyToMessageID: in.ReplyToMessageID,
			Body: content.Body, Links: content.Links, Status: domain.MessageStatusActive, Version: 1,
			MessageSequence: messageSequence, LastEventSequence: eventSequence, TeacherTurnSequence: teacherTurnSequence,
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
		if dialogItem.Type == domain.DialogTypeTeacher {
			observation, err := s.addTeacherRequestedEvent(txCtx, dialogItem, item, item.ID, nil, now)
			if err != nil {
				return err
			}
			teacherRequestV2 = observation
		}
		result = CreateResult{Created: true, View: View{Message: item, Attachments: attachments}}
		return nil
	})
	if err == nil {
		s.observeTeacherRequestV2(teacherRequestV2)
	}
	return result, err
}

// AppendStudentChannelMessage accepts a message from a trusted transport
// adapter for an already bound student. It is deliberately restricted to the
// general teacher dialog and produces the ordinary durable teacher request.
func (s Service) AppendStudentChannelMessage(ctx context.Context, in AppendStudentChannelMessageInput) (CreateResult, error) {
	if in.DialogID == uuid.Nil || in.StudentID == uuid.Nil || in.PersonalTeacherID == uuid.Nil ||
		in.IdempotencyKey == uuid.Nil || in.Channel != domain.MessageChannelTelegram {
		return CreateResult{}, fmt.Errorf("%w: invalid channel message identity", domain.ErrValidation)
	}
	var result CreateResult
	var teacherRequestV2 *TeacherRequestV2Observation
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.Messages.LockIdempotencyKey(txCtx, in.StudentID, in.IdempotencyKey); err != nil {
			return err
		}
		existing, err := s.Messages.GetByIdempotencyKey(txCtx, in.StudentID, in.IdempotencyKey)
		if err == nil {
			if existing.DialogID != in.DialogID || existing.Body != in.Body || existing.Channel != in.Channel ||
				existing.AuthorType != domain.MessageAuthorUser || existing.SenderID != in.StudentID ||
				existing.LearningActionID != nil || existing.LessonContext != nil || existing.ReplyToMessageID != nil {
				return domain.ErrIdempotencyConflict
			}
			result = CreateResult{View: View{Message: existing}}
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		dialogItem, err := s.Dialogs.GetByIDForUpdate(txCtx, in.DialogID)
		if err != nil || dialogItem.Type != domain.DialogTypeTeacher || dialogItem.Status == domain.DialogStatusHidden {
			return domain.ErrDialogNotFound
		}
		if dialogItem.Status != domain.DialogStatusActive {
			return domain.ErrDialogClosed
		}
		if dialogItem.StudentID != in.StudentID || dialogItem.PersonalTeacherID != in.PersonalTeacherID ||
			dialogItem.TeacherContextType != domain.TeacherContextGeneralTeacher {
			return domain.ErrForbidden
		}
		space, err := s.Spaces.GetByID(txCtx, dialogItem.SpaceID)
		if err != nil || space.Status != domain.SpaceStatusActive {
			return domain.ErrDialogNotFound
		}
		content := domain.AnalyzeMessageContent(in.Body, 0, 0)
		if err := content.Validate(space.Policy); err != nil {
			return err
		}
		teacherTurnSequence, err := s.allocateTeacherTurnSequence(&dialogItem)
		if err != nil {
			return err
		}
		now := s.now()
		item := domain.Message{
			ID: s.newID(), DialogID: in.DialogID, AuthorType: domain.MessageAuthorUser, Channel: in.Channel,
			SenderID: in.StudentID, Body: content.Body, Links: content.Links,
			Status: domain.MessageStatusActive, Version: 1,
			MessageSequence: dialogItem.MaxMessageSequence + 1, LastEventSequence: dialogItem.MaxEventSequence + 1, TeacherTurnSequence: teacherTurnSequence,
			IdempotencyKey: in.IdempotencyKey, CreatedAt: now, UpdatedAt: now,
		}
		if err := item.Validate(); err != nil {
			return err
		}
		if err := s.Messages.Create(txCtx, item); err != nil {
			return err
		}
		dialogItem.MessageCount++
		dialogItem.MaxMessageSequence = item.MessageSequence
		dialogItem.MaxEventSequence = item.LastEventSequence
		dialogItem.LastMessageID = &item.ID
		dialogItem.LastMessageAt = &now
		dialogItem.Version++
		dialogItem.UpdatedAt = now
		if err := s.Dialogs.UpdateState(txCtx, dialogItem, dialogItem.Version-1); err != nil {
			return err
		}
		if err := s.Members.IncrementUnreadRecipients(txCtx, item.DialogID, in.StudentID, item.LastEventSequence); err != nil {
			return err
		}
		if err := s.addEvent(txCtx, item, nil, domain.EventDialogMessageCreated, in.StudentID, now); err != nil {
			return err
		}
		observation, err := s.addTeacherRequestedEvent(txCtx, dialogItem, item, item.ID, nil, now)
		if err != nil {
			return err
		}
		teacherRequestV2 = observation
		result = CreateResult{Created: true, View: View{Message: item}}
		return nil
	})
	if err == nil {
		s.observeTeacherRequestV2(teacherRequestV2)
	}
	return result, err
}

// MaterializeCanonicalStudentTurn is an internal-only, trusted command. It
// turns one Teacher-validated action receipt into one ordinary immutable
// Student message; it never accepts a browser action and never exposes the
// private ledger through the returned View.
func (s Service) MaterializeCanonicalStudentTurn(ctx context.Context, in MaterializeCanonicalStudentTurnInput) (CreateResult, error) {
	identity, err := domain.NewCanonicalStudentTurnIdentity(in.Identity)
	if err != nil {
		return CreateResult{}, err
	}
	in.Identity = identity
	if s.CanonicalStudentTurns == nil {
		return CreateResult{}, domain.ErrFeatureDisabled
	}

	var result CreateResult
	var teacherRequestV2 *TeacherRequestV2Observation
	var canonicalObservation *CanonicalStudentTurnObservation
	err = s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.CanonicalStudentTurns.LockIdentity(txCtx, identity.ActionReceiptID, identity.CanonicalMessageCommandID); err != nil {
			return err
		}
		if err := s.CanonicalStudentTurns.LockSourceAction(txCtx, identity.DialogID, identity.Interaction.SourcePromptMessageID, identity.Interaction.BlockID, identity.Interaction.ActionID); err != nil {
			return err
		}
		replayed, handled, err := s.resolveCanonicalStudentTurnReplay(txCtx, identity, in.CanonicalBody)
		if err != nil || handled {
			if handled {
				result = replayed
			}
			return err
		}

		// The feature gate is deliberately after exact replay. A committed
		// receipt stays recoverable during a rollback/kill switch, while a new
		// receipt has no effect unless the same process emits the V2 trigger.
		if !s.CanonicalStudentTurnEnabled || !s.TeacherOrderingV2Enabled {
			return domain.ErrFeatureDisabled
		}
		if err := s.Messages.LockIdempotencyKey(txCtx, identity.StudentID, identity.CanonicalMessageCommandID); err != nil {
			return err
		}
		if existing, err := s.Messages.GetByIdempotencyKey(txCtx, identity.StudentID, identity.CanonicalMessageCommandID); err == nil {
			// A key owned by any non-ledger message can never be adopted by a
			// receipt because doing so could make a second lifecycle.
			_ = existing
			return domain.ErrIdempotencyConflict
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		dialogItem, err := s.Dialogs.GetByIDForUpdate(txCtx, identity.DialogID)
		if err != nil || dialogItem.Type != domain.DialogTypeTeacher || dialogItem.Status == domain.DialogStatusHidden {
			return domain.ErrDialogNotFound
		}
		if dialogItem.Status != domain.DialogStatusActive {
			return domain.ErrDialogClosed
		}
		if dialogItem.StudentID != identity.StudentID || dialogItem.PersonalTeacherID != identity.PersonalTeacherID ||
			dialogItem.TeacherContextType != domain.TeacherContextGeneralTeacher || dialogItem.ContextID != nil {
			return domain.ErrForbidden
		}
		space, err := s.Spaces.GetByID(txCtx, dialogItem.SpaceID)
		if err != nil || space.Status != domain.SpaceStatusActive {
			return domain.ErrDialogNotFound
		}
		source, err := s.Messages.GetByIDForUpdate(txCtx, identity.Interaction.SourcePromptMessageID)
		if err != nil || source.DialogID != dialogItem.ID || source.AuthorType != domain.MessageAuthorPersonalTeacher ||
			source.PersonalTeacherID != dialogItem.PersonalTeacherID || source.Status != domain.MessageStatusActive || len(source.AssistantUI) == 0 {
			return domain.ErrMessageNotFound
		}
		if source.Version != identity.Interaction.SourcePromptMessageVersion {
			return domain.ErrMessageConflict
		}
		digest, err := domain.CanonicalAssistantUIDigest(source.AssistantUI)
		if err != nil {
			return domain.ErrMessageNotFound
		}
		if digest != identity.Interaction.SourceUIDigest {
			return domain.ErrMessageConflict
		}
		content := domain.AnalyzeMessageContent(in.CanonicalBody, 0, 0)
		if err := content.Validate(space.Policy); err != nil {
			return err
		}
		teacherTurnSequence, err := s.allocateTeacherTurnSequence(&dialogItem)
		if err != nil || teacherTurnSequence == nil {
			if err != nil {
				return err
			}
			return fmt.Errorf("%w: canonical student turn requires V2 ordering", domain.ErrValidation)
		}
		now := s.now()
		item := domain.Message{
			ID: s.newID(), DialogID: dialogItem.ID, AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelWeb,
			SenderID: identity.StudentID, ReplyToMessageID: &source.ID, Body: content.Body, Links: content.Links,
			Status: domain.MessageStatusActive, Version: 1, MessageSequence: dialogItem.MaxMessageSequence + 1,
			LastEventSequence: dialogItem.MaxEventSequence + 1, TeacherTurnSequence: teacherTurnSequence,
			IdempotencyKey: identity.CanonicalMessageCommandID, CreatedAt: now, UpdatedAt: now,
		}
		if err := item.Validate(); err != nil {
			return err
		}
		if err := s.Messages.Create(txCtx, item); err != nil {
			return err
		}
		ledger, err := domain.NewCanonicalStudentTurn(domain.CanonicalStudentTurn{
			Identity: identity, CanonicalStudentMessageID: item.ID, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		if err := s.CanonicalStudentTurns.Create(txCtx, ledger); err != nil {
			return err
		}
		dialogItem.MessageCount++
		dialogItem.MaxMessageSequence = item.MessageSequence
		dialogItem.MaxEventSequence = item.LastEventSequence
		dialogItem.LastMessageID = &item.ID
		dialogItem.LastMessageAt = &now
		dialogItem.Version++
		dialogItem.UpdatedAt = now
		if err := s.Dialogs.UpdateState(txCtx, dialogItem, dialogItem.Version-1); err != nil {
			return err
		}
		if err := s.Members.IncrementUnreadRecipients(txCtx, item.DialogID, identity.StudentID, item.LastEventSequence); err != nil {
			return err
		}
		if err := s.addEvent(txCtx, item, nil, domain.EventDialogMessageCreated, identity.StudentID, now); err != nil {
			return err
		}
		observation, err := s.addTeacherRequestedEvent(txCtx, dialogItem, item, ledger.Identity.CorrelationID, &ledger.Identity.ActionReceiptID, now)
		if err != nil {
			return err
		}
		teacherRequestV2 = observation
		canonicalObservation = &CanonicalStudentTurnObservation{
			ActionReceiptID: identity.ActionReceiptID, CorrelationID: identity.CorrelationID, CausationID: identity.CausationID,
			SourcePromptMessageID: identity.Interaction.SourcePromptMessageID, SourcePromptMessageVersion: identity.Interaction.SourcePromptMessageVersion,
			BlockID: identity.Interaction.BlockID, ActionID: identity.Interaction.ActionID, SourceUIDigest: identity.Interaction.SourceUIDigest,
			CanonicalStudentMessageID: item.ID, TeacherTurnSequence: *teacherTurnSequence,
		}
		result = CreateResult{Created: true, View: View{Message: item}}
		return nil
	})
	if err == nil {
		s.observeTeacherRequestV2(teacherRequestV2)
		s.observeCanonicalStudentTurn(canonicalObservation)
	}
	return result, err
}

func (s Service) resolveCanonicalStudentTurnReplay(ctx context.Context, identity domain.CanonicalStudentTurnIdentity, body string) (CreateResult, bool, error) {
	var found *domain.CanonicalStudentTurn
	lookups := []func() (domain.CanonicalStudentTurn, error){
		func() (domain.CanonicalStudentTurn, error) {
			return s.CanonicalStudentTurns.GetByActionReceiptID(ctx, identity.ActionReceiptID)
		},
		func() (domain.CanonicalStudentTurn, error) {
			return s.CanonicalStudentTurns.GetByCanonicalMessageCommandID(ctx, identity.CanonicalMessageCommandID)
		},
		func() (domain.CanonicalStudentTurn, error) {
			return s.CanonicalStudentTurns.GetBySourceAction(ctx, identity.DialogID, identity.Interaction.SourcePromptMessageID, identity.Interaction.BlockID, identity.Interaction.ActionID)
		},
	}
	for _, lookup := range lookups {
		candidate, err := lookup()
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				continue
			}
			return CreateResult{}, false, err
		}
		if !domain.SameCanonicalStudentTurnIdentity(candidate.Identity, identity) {
			return CreateResult{}, false, domain.ErrIdempotencyConflict
		}
		if found != nil && found.CanonicalStudentMessageID != candidate.CanonicalStudentMessageID {
			return CreateResult{}, false, domain.ErrIdempotencyConflict
		}
		copy := candidate
		found = &copy
	}
	if found == nil {
		return CreateResult{}, false, nil
	}
	item, err := s.Messages.GetByID(ctx, found.CanonicalStudentMessageID)
	if err != nil || item.DialogID != identity.DialogID || item.AuthorType != domain.MessageAuthorUser ||
		item.SenderID != identity.StudentID || item.Channel != domain.MessageChannelWeb || item.Body != body ||
		item.ReplyToMessageID == nil || *item.ReplyToMessageID != identity.Interaction.SourcePromptMessageID ||
		item.LearningActionID != nil || item.LessonContext != nil ||
		item.IdempotencyKey != identity.CanonicalMessageCommandID {
		return CreateResult{}, false, domain.ErrIdempotencyConflict
	}
	return CreateResult{View: View{Message: item}}, true, nil
}

// rejectCanonicalStudentTurnMutation keeps an accepted action receipt's
// canonical conversation turn immutable to public edit/delete paths. A nil
// repository preserves legacy/unit construction; production wires the private
// ledger whenever migration 011 is installed.
func (s Service) rejectCanonicalStudentTurnMutation(ctx context.Context, messageID uuid.UUID) error {
	if s.CanonicalStudentTurns == nil {
		return nil
	}
	_, err := s.CanonicalStudentTurns.GetByCanonicalMessageID(ctx, messageID)
	if err == nil {
		return domain.ErrMessageConflict
	}
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	return err
}

func (s Service) GetTeacherRequestContext(ctx context.Context, dialogID, personalTeacherID, sourceMessageID uuid.UUID, before int) (TeacherRequestContext, error) {
	if dialogID == uuid.Nil || personalTeacherID == uuid.Nil || sourceMessageID == uuid.Nil || before < 0 || before >= MaxTeacherContextMessages {
		return TeacherRequestContext{}, fmt.Errorf("%w: invalid teacher request context", domain.ErrValidation)
	}
	dialogItem, err := s.Dialogs.GetByID(ctx, dialogID)
	if err != nil || dialogItem.Type != domain.DialogTypeTeacher || dialogItem.Status == domain.DialogStatusHidden {
		return TeacherRequestContext{}, domain.ErrDialogNotFound
	}
	if dialogItem.Status != domain.DialogStatusActive || dialogItem.PersonalTeacherID != personalTeacherID {
		return TeacherRequestContext{}, domain.ErrForbidden
	}
	source, err := s.Messages.GetByID(ctx, sourceMessageID)
	if err != nil || source.DialogID != dialogID || source.Status != domain.MessageStatusActive ||
		source.AuthorType != domain.MessageAuthorUser || source.SenderID != dialogItem.StudentID {
		return TeacherRequestContext{}, domain.ErrMessageNotFound
	}
	if err := validateLearningContextBinding(dialogItem, source.LearningActionID, source.LessonContext); err != nil {
		return TeacherRequestContext{}, err
	}
	items, err := s.Messages.Window(ctx, repository.MessageWindowQuery{
		DialogID: dialogID, FromSequence: 0, AnchorSequence: source.MessageSequence,
		Before: before, After: 1,
	})
	if err != nil {
		return TeacherRequestContext{}, err
	}
	if len(items) == 0 || items[len(items)-1].ID != source.ID {
		return TeacherRequestContext{}, domain.ErrMessageNotFound
	}
	return TeacherRequestContext{Dialog: dialogItem, Source: source, Messages: items}, nil
}

func (s Service) GetAssistantUISource(ctx context.Context, in AssistantUISourceInput) (domain.DialogAssistantUISource, error) {
	if in.DialogID == uuid.Nil || in.StudentID == uuid.Nil || in.PersonalTeacherID == uuid.Nil || in.MessageID == uuid.Nil {
		return domain.DialogAssistantUISource{}, fmt.Errorf("%w: assistant UI source identities are required", domain.ErrValidation)
	}
	dialogItem, err := s.Dialogs.GetByID(ctx, in.DialogID)
	if err != nil || dialogItem.Type != domain.DialogTypeTeacher || dialogItem.Status != domain.DialogStatusActive ||
		dialogItem.StudentID != in.StudentID || dialogItem.PersonalTeacherID != in.PersonalTeacherID {
		return domain.DialogAssistantUISource{}, domain.ErrMessageNotFound
	}
	item, err := s.Messages.GetByID(ctx, in.MessageID)
	if err != nil || item.DialogID != dialogItem.ID || item.AuthorType != domain.MessageAuthorPersonalTeacher ||
		item.Status != domain.MessageStatusActive || item.PersonalTeacherID != dialogItem.PersonalTeacherID || len(item.AssistantUI) == 0 {
		return domain.DialogAssistantUISource{}, domain.ErrMessageNotFound
	}
	result, err := domain.NewDialogAssistantUISource(dialogItem, item)
	if err != nil {
		return domain.DialogAssistantUISource{}, domain.ErrMessageNotFound
	}
	return result, nil
}

func (s Service) AppendTeacherResponse(ctx context.Context, in AppendTeacherResponseInput) (CreateResult, error) {
	if in.DialogID == uuid.Nil || in.PersonalTeacherID == uuid.Nil || in.SourceMessageID == uuid.Nil || in.IdempotencyKey == uuid.Nil {
		return CreateResult{}, fmt.Errorf("%w: teacher response identities are required", domain.ErrValidation)
	}
	assistantUI, err := domain.NormalizeAssistantUI(in.AssistantUI)
	if err != nil {
		return CreateResult{}, err
	}
	in.AssistantUI = assistantUI
	var result CreateResult
	err = s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if s.TeacherMessages == nil {
			return fmt.Errorf("teacher message repository is not configured")
		}
		if err := s.TeacherMessages.LockTeacherIdempotencyKey(txCtx, in.PersonalTeacherID, in.IdempotencyKey); err != nil {
			return err
		}
		existing, err := s.TeacherMessages.GetByTeacherIdempotencyKey(txCtx, in.PersonalTeacherID, in.IdempotencyKey)
		if err == nil {
			if existing.DialogID != in.DialogID || existing.Body != in.Body || !sameAssistantUI(existing.AssistantUI, in.AssistantUI) ||
				existing.ReplyToMessageID == nil || *existing.ReplyToMessageID != in.SourceMessageID {
				return domain.ErrIdempotencyConflict
			}
			result = CreateResult{View: View{Message: existing}}
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		dialogItem, err := s.Dialogs.GetByIDForUpdate(txCtx, in.DialogID)
		if err != nil || dialogItem.Type != domain.DialogTypeTeacher || dialogItem.Status == domain.DialogStatusHidden {
			return domain.ErrDialogNotFound
		}
		if dialogItem.Status != domain.DialogStatusActive {
			return domain.ErrDialogClosed
		}
		if dialogItem.PersonalTeacherID != in.PersonalTeacherID {
			return domain.ErrForbidden
		}
		space, err := s.Spaces.GetByID(txCtx, dialogItem.SpaceID)
		if err != nil || space.Status != domain.SpaceStatusActive {
			return domain.ErrDialogNotFound
		}
		source, err := s.Messages.GetByID(txCtx, in.SourceMessageID)
		if err != nil || source.DialogID != in.DialogID || source.Status != domain.MessageStatusActive ||
			source.AuthorType != domain.MessageAuthorUser || source.SenderID != dialogItem.StudentID {
			return domain.ErrMessageNotFound
		}
		if err := validateLearningContextBinding(dialogItem, source.LearningActionID, source.LessonContext); err != nil {
			return err
		}
		content := domain.AnalyzeMessageContent(in.Body, 0, 0)
		if err := content.Validate(space.Policy); err != nil {
			return err
		}

		now := s.now()
		item := domain.Message{
			ID: s.newID(), DialogID: in.DialogID, AuthorType: domain.MessageAuthorPersonalTeacher, Channel: source.Channel,
			PersonalTeacherID: in.PersonalTeacherID, LearningActionID: source.LearningActionID,
			AssistantUI: in.AssistantUI, ReplyToMessageID: &source.ID, Body: content.Body, Links: content.Links,
			Status: domain.MessageStatusActive, Version: 1,
			MessageSequence: dialogItem.MaxMessageSequence + 1, LastEventSequence: dialogItem.MaxEventSequence + 1,
			IdempotencyKey: in.IdempotencyKey, CreatedAt: now, UpdatedAt: now,
		}
		if err := item.Validate(); err != nil {
			return err
		}
		if err := s.Messages.Create(txCtx, item); err != nil {
			return err
		}
		dialogItem.MessageCount++
		dialogItem.MaxMessageSequence = item.MessageSequence
		dialogItem.MaxEventSequence = item.LastEventSequence
		dialogItem.LastMessageID = &item.ID
		dialogItem.LastMessageAt = &now
		dialogItem.Version++
		dialogItem.UpdatedAt = now
		if err := s.Dialogs.UpdateState(txCtx, dialogItem, dialogItem.Version-1); err != nil {
			return err
		}
		if err := s.Members.IncrementUnreadRecipients(txCtx, item.DialogID, uuid.Nil, item.LastEventSequence); err != nil {
			return err
		}
		if err := s.addEvent(txCtx, item, nil, domain.EventDialogMessageCreated, uuid.Nil, now); err != nil {
			return err
		}
		result = CreateResult{Created: true, View: View{Message: item}}
		return nil
	})
	return result, err
}

// AppendTeacherProactive appends a bounded proactive offer to the student's
// general teacher dialog. It never creates a synthetic student source message.
func (s Service) AppendTeacherProactive(ctx context.Context, in AppendTeacherProactiveInput) (CreateResult, error) {
	if in.DialogID == uuid.Nil || in.PersonalTeacherID == uuid.Nil || in.IdempotencyKey == uuid.Nil {
		return CreateResult{}, fmt.Errorf("%w: proactive teacher identities are required", domain.ErrValidation)
	}
	assistantUI, err := domain.NormalizeAssistantUI(in.AssistantUI)
	if err != nil {
		return CreateResult{}, err
	}
	in.AssistantUI = assistantUI
	var result CreateResult
	err = s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if s.TeacherMessages == nil {
			return fmt.Errorf("teacher message repository is not configured")
		}
		if err := s.TeacherMessages.LockTeacherIdempotencyKey(txCtx, in.PersonalTeacherID, in.IdempotencyKey); err != nil {
			return err
		}
		existing, err := s.TeacherMessages.GetByTeacherIdempotencyKey(txCtx, in.PersonalTeacherID, in.IdempotencyKey)
		if err == nil {
			if existing.DialogID != in.DialogID || existing.Body != in.Body || !sameAssistantUI(existing.AssistantUI, in.AssistantUI) ||
				existing.ReplyToMessageID != nil || existing.LearningActionID != nil {
				return domain.ErrIdempotencyConflict
			}
			result = CreateResult{View: View{Message: existing}}
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		dialogItem, err := s.Dialogs.GetByIDForUpdate(txCtx, in.DialogID)
		if err != nil || dialogItem.Type != domain.DialogTypeTeacher || dialogItem.Status == domain.DialogStatusHidden {
			return domain.ErrDialogNotFound
		}
		if dialogItem.Status != domain.DialogStatusActive {
			return domain.ErrDialogClosed
		}
		if dialogItem.PersonalTeacherID != in.PersonalTeacherID || dialogItem.TeacherContextType != domain.TeacherContextGeneralTeacher {
			return domain.ErrForbidden
		}
		space, err := s.Spaces.GetByID(txCtx, dialogItem.SpaceID)
		if err != nil || space.Status != domain.SpaceStatusActive {
			return domain.ErrDialogNotFound
		}
		content := domain.AnalyzeMessageContent(in.Body, 0, 0)
		if err := content.Validate(space.Policy); err != nil {
			return err
		}
		now := s.now()
		item := domain.Message{
			ID: s.newID(), DialogID: in.DialogID, AuthorType: domain.MessageAuthorPersonalTeacher, Channel: domain.MessageChannelWeb,
			PersonalTeacherID: in.PersonalTeacherID, AssistantUI: in.AssistantUI, Body: content.Body, Links: content.Links,
			Status: domain.MessageStatusActive, Version: 1,
			MessageSequence: dialogItem.MaxMessageSequence + 1, LastEventSequence: dialogItem.MaxEventSequence + 1,
			IdempotencyKey: in.IdempotencyKey, CreatedAt: now, UpdatedAt: now,
		}
		if err := item.Validate(); err != nil {
			return err
		}
		if err := s.Messages.Create(txCtx, item); err != nil {
			return err
		}
		dialogItem.MessageCount++
		dialogItem.MaxMessageSequence = item.MessageSequence
		dialogItem.MaxEventSequence = item.LastEventSequence
		dialogItem.LastMessageID = &item.ID
		dialogItem.LastMessageAt = &now
		dialogItem.Version++
		dialogItem.UpdatedAt = now
		if err := s.Dialogs.UpdateState(txCtx, dialogItem, dialogItem.Version-1); err != nil {
			return err
		}
		if err := s.Members.IncrementUnreadRecipients(txCtx, item.DialogID, uuid.Nil, item.LastEventSequence); err != nil {
			return err
		}
		if err := s.addEvent(txCtx, item, nil, domain.EventDialogMessageCreated, uuid.Nil, now); err != nil {
			return err
		}
		result = CreateResult{Created: true, View: View{Message: item}}
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
		if err := s.rejectCanonicalStudentTurnMutation(txCtx, item.ID); err != nil {
			return err
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
		if err := s.addTeacherContextMutatedEvent(txCtx, dialogItem, item, domain.TeacherContextMutationUpdated, now); err != nil {
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
		if err := s.rejectCanonicalStudentTurnMutation(txCtx, item.ID); err != nil {
			return err
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
		item.Body, item.Links, item.LessonContext, item.AssistantUI, item.Status, item.LastEventSequence = "", nil, nil, nil, domain.MessageStatusDeleted, eventSequence
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
		if err := s.addTeacherContextMutatedEvent(txCtx, dialogItem, item, domain.TeacherContextMutationDeleted, now); err != nil {
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
		space, err := s.Spaces.GetByID(txCtx, dialogItem.SpaceID)
		if err != nil || space.Status != domain.SpaceStatusActive {
			return domain.ErrDialogNotFound
		}
		member, err := s.Members.GetForUpdate(txCtx, dialogID, actor.UserID)
		if err != nil || member.Status != domain.MemberStatusActive {
			return domain.ErrForbidden
		}
		through := dialogItem.MaxMessageSequence
		if requested != nil {
			through = *requested
			if through < 0 || through > dialogItem.MaxMessageSequence {
				return domain.ErrInvalidReadSequence
			}
			// Read requests can arrive out of order from debounced viewport
			// observers or another device. An older cursor is an idempotent
			// no-op, not a client error.
			if through <= member.LastReadMessageSequence {
				state, err = s.readState(txCtx, dialogItem, member)
				return err
			}
		}
		newlyRead := int64(0)
		if requested != nil {
			newlyRead, err = s.Messages.CountUnreadIncoming(txCtx, dialogID, actor.UserID, member.LastReadMessageSequence, through)
			if err != nil {
				return err
			}
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
	windowBefore, windowAfter := before+after, 0
	if member.UnreadCount > 0 {
		first, err := s.Messages.FirstUnreadIncoming(ctx, dialogID, actor.UserID, member.LastReadMessageSequence)
		if err == nil {
			anchor = first
			windowBefore, windowAfter = before, after
		} else if !errors.Is(err, domain.ErrNotFound) {
			return Window{}, err
		}
	}
	items, err := s.Messages.Window(ctx, repository.MessageWindowQuery{DialogID: dialogID, FromSequence: member.HistoryFromMessageSequence, AnchorSequence: anchor, Before: windowBefore + 1, After: windowAfter + 1})
	if err != nil {
		return Window{}, err
	}
	olderCount := sort.Search(len(items), func(index int) bool {
		return items[index].MessageSequence >= anchor
	})
	hasOlder := olderCount > windowBefore
	if hasOlder {
		items = items[1:]
		olderCount--
	}
	hasNewer := len(items)-olderCount > windowAfter
	if hasNewer {
		items = items[:len(items)-1]
	}
	views, err := s.views(ctx, items)
	if err != nil {
		return Window{}, err
	}
	state, err := s.readState(ctx, dialogItem, member)
	return Window{Items: views, ReadState: state, HasOlder: hasOlder, HasNewer: hasNewer}, err
}

func (s Service) List(ctx context.Context, actor domain.Actor, query repository.MessageListQuery) ([]View, error) {
	if err := actor.Validate(); err != nil {
		return nil, err
	}
	if query.Limit < 1 || query.Limit > MaxPageLimit+1 {
		return nil, fmt.Errorf("%w: invalid page limit", domain.ErrValidation)
	}
	_, _, member, err := s.loadReadableContext(ctx, actor, query.DialogID)
	if err != nil {
		return nil, err
	}
	query.FromSequence = member.HistoryFromMessageSequence
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
	_, _, member, err := s.loadReadableContext(ctx, actor, query.DialogID)
	if err != nil {
		return nil, err
	}
	query.FromSequence = member.HistoryFromMessageSequence
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
	_, _, member, err := s.loadReadableContext(ctx, actor, item.DialogID)
	if err != nil {
		return View{}, err
	}
	if item.MessageSequence < member.HistoryFromMessageSequence {
		return View{}, domain.ErrMessageNotFound
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
	if existing.DialogID != in.DialogID || existing.AuthorType != domain.MessageAuthorUser || existing.Channel != domain.MessageChannelWeb ||
		existing.Body != in.Body || !sameUUID(existing.ReplyToMessageID, in.ReplyToMessageID) ||
		!sameUUID(existing.LearningActionID, in.LearningActionID) || !sameLessonContext(existing.LessonContext, in.LessonContext) ||
		!sameAttachmentIDs(attachments, in.AttachmentIDs) {
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
	eventPayload := map[string]any{
		"schema_version": 1, "event_id": eventID, "occurred_at": now,
		"dialog_id": item.DialogID, "event_sequence": item.LastEventSequence,
		"message_sequence": item.MessageSequence, "message_id": item.ID,
		"author_type": item.AuthorType, "channel": item.Channel, "sender_id": nullableEventUUID(item.SenderID),
		"personal_teacher_id": nullableEventUUID(item.PersonalTeacherID), "learning_action_id": item.LearningActionID,
		"actor_id": nullableEventUUID(actorID), "status": item.Status,
		"version": item.Version, "body": item.Body, "links": item.Links, "attachments": attachments,
	}
	if len(item.AssistantUI) > 0 {
		eventPayload["assistant_ui"] = item.AssistantUI
	}
	if item.LessonContext != nil {
		eventPayload["lesson_context"] = item.LessonContext
	}
	payload, err := json.Marshal(eventPayload)
	if err != nil {
		return err
	}
	return s.Outbox.Add(ctx, domain.OutboxEvent{
		ID: eventID, DialogID: item.DialogID, AggregateType: "message", AggregateID: item.ID,
		Subject: subject, EventSequence: item.LastEventSequence, SchemaVersion: 1,
		Payload: payload, NextAttemptAt: now, CreatedAt: now,
	})
}

// addTeacherRequestedEvent emits exactly one request trigger for the committed
// source message. Direct messages correlate to themselves; trusted canonical
// action materialization passes its receipt identity instead.
func (s Service) addTeacherRequestedEvent(ctx context.Context, dialogItem domain.Dialog, item domain.Message, correlationID uuid.UUID, actionReceiptID *uuid.UUID, now time.Time) (*TeacherRequestV2Observation, error) {
	eventID := s.newID()
	if s.TeacherOrderingV2Enabled {
		var event domain.OutboxEvent
		var err error
		if actionReceiptID != nil {
			event, err = domain.NewTeacherRequestedV2OutboxFromActionReceipt(dialogItem, item, eventID, correlationID, *actionReceiptID, now)
		} else {
			event, err = domain.NewTeacherRequestedV2Outbox(dialogItem, item, eventID, correlationID, now)
		}
		if err != nil {
			return nil, err
		}
		if err := s.Outbox.Add(ctx, event); err != nil {
			return nil, err
		}
		return &TeacherRequestV2Observation{
			DialogID: dialogItem.ID, CanonicalStudentMessageID: item.ID,
			TeacherTurnSequence: *item.TeacherTurnSequence, CorrelationID: correlationID,
			CausationID: item.ID, SourceEventID: event.ID,
		}, nil
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "event_id": eventID, "occurred_at": now,
		"dialog_id": dialogItem.ID, "event_sequence": item.LastEventSequence,
		"source_message_id": item.ID, "source_message_sequence": item.MessageSequence,
		"student_id": dialogItem.StudentID, "personal_teacher_id": dialogItem.PersonalTeacherID,
		"context_type": dialogItem.TeacherContextType, "context_id": dialogItem.ContextID,
		"learning_action_id": item.LearningActionID, "channel": item.Channel,
	})
	if err != nil {
		return nil, err
	}
	if err := s.Outbox.Add(ctx, domain.OutboxEvent{
		ID: eventID, DialogID: item.DialogID, AggregateType: "teacher_request", AggregateID: item.ID,
		Subject: domain.EventDialogTeacherRequested, EventSequence: item.LastEventSequence, SchemaVersion: 1,
		Payload: payload, NextAttemptAt: now, CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s Service) allocateTeacherTurnSequence(dialogItem *domain.Dialog) (*int64, error) {
	if !s.TeacherOrderingV2Enabled {
		return nil, nil
	}
	if dialogItem == nil || dialogItem.Type != domain.DialogTypeTeacher || dialogItem.MaxTeacherTurnSequence < 0 {
		return nil, fmt.Errorf("%w: invalid teacher turn allocation", domain.ErrValidation)
	}
	dialogItem.MaxTeacherTurnSequence++
	sequence := dialogItem.MaxTeacherTurnSequence
	return &sequence, nil
}

func (s Service) observeTeacherRequestV2(observation *TeacherRequestV2Observation) {
	if observation != nil && s.OnTeacherRequestV2 != nil {
		s.OnTeacherRequestV2(*observation)
	}
}

func (s Service) observeCanonicalStudentTurn(observation *CanonicalStudentTurnObservation) {
	if observation != nil && s.OnCanonicalStudentTurn != nil {
		s.OnCanonicalStudentTurn(*observation)
	}
}

func (s Service) addTeacherContextMutatedEvent(ctx context.Context, dialogItem domain.Dialog, item domain.Message, mutation domain.TeacherContextMutation, now time.Time) error {
	if dialogItem.Type != domain.DialogTypeTeacher || dialogItem.Status != domain.DialogStatusActive ||
		item.AuthorType != domain.MessageAuthorUser || item.SenderID != dialogItem.StudentID {
		return nil
	}
	event, err := domain.NewTeacherContextMutationOutbox(dialogItem, item, mutation, s.newID(), now)
	if err != nil {
		return err
	}
	return s.Outbox.Add(ctx, event)
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
		switch item.Kind {
		case domain.AttachmentKindImage:
			images++
		case domain.AttachmentKindFile:
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

func nullableEventUUID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

func validateLearningContextBinding(item domain.Dialog, learningActionID *uuid.UUID, lessonContext *domain.LessonMessageContext) error {
	if learningActionID != nil && *learningActionID == uuid.Nil {
		return fmt.Errorf("%w: learning action UUID is invalid", domain.ErrValidation)
	}
	if item.Type != domain.DialogTypeTeacher {
		if learningActionID != nil || lessonContext != nil {
			return fmt.Errorf("%w: learning context is limited to teacher dialogs", domain.ErrValidation)
		}
		return nil
	}
	if item.TeacherContextType == domain.TeacherContextLesson {
		if learningActionID != nil || lessonContext == nil {
			return fmt.Errorf("%w: lesson context does not match teacher dialog", domain.ErrValidation)
		}
		if (lessonContext.Schema == domain.LessonMessageContextSchemaV1 || lessonContext.Schema == domain.LessonMessageContextSchemaV2) &&
			(item.ContextID == nil || lessonContext.LessonID == nil || *lessonContext.LessonID != *item.ContextID) {
			return fmt.Errorf("%w: lesson anchor does not match teacher dialog", domain.ErrValidation)
		}
		return nil
	}
	if lessonContext != nil {
		return fmt.Errorf("%w: lesson context does not match teacher dialog", domain.ErrValidation)
	}
	if item.TeacherContextType.RequiresLearningAction() != (learningActionID != nil) {
		return fmt.Errorf("%w: learning action does not match teacher dialog context", domain.ErrValidation)
	}
	return nil
}

func sameLessonContext(first, second *domain.LessonMessageContext) bool {
	return domain.LessonMessageContextsEqual(first, second)
}

func sameAssistantUI(first, second json.RawMessage) bool {
	left, leftErr := domain.NormalizeAssistantUI(first)
	right, rightErr := domain.NormalizeAssistantUI(second)
	return leftErr == nil && rightErr == nil && string(left) == string(right)
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
