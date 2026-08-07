package repository

import (
	"context"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type MessageCursor struct {
	Sequence int64
	ID       uuid.UUID
}

type MessageListQuery struct {
	DialogID uuid.UUID
	Before   *MessageCursor
	After    *MessageCursor
	Limit    int
}

type MessageChangeQuery struct {
	DialogID           uuid.UUID
	AfterEventSequence int64
	Limit              int
}

type MessageWindowQuery struct {
	DialogID       uuid.UUID
	AnchorSequence int64
	Before         int
	After          int
}

type MessageRepository interface {
	Create(ctx context.Context, message domain.Message) error
	GetByID(ctx context.Context, id uuid.UUID) (domain.Message, error)
	GetByIDForUpdate(ctx context.Context, id uuid.UUID) (domain.Message, error)
	GetByIdempotencyKey(ctx context.Context, senderID, key uuid.UUID) (domain.Message, error)
	LockIdempotencyKey(ctx context.Context, senderID, key uuid.UUID) error
	List(ctx context.Context, query MessageListQuery) ([]domain.Message, error)
	Window(ctx context.Context, query MessageWindowQuery) ([]domain.Message, error)
	ListChanges(ctx context.Context, query MessageChangeQuery) ([]domain.Message, error)
	FirstUnreadIncoming(ctx context.Context, dialogID, readerID uuid.UUID, afterSequence int64) (int64, error)
	CountUnreadIncoming(ctx context.Context, dialogID, readerID uuid.UUID, afterExclusive, throughInclusive int64) (int64, error)
	UpdateContent(ctx context.Context, message domain.Message, expectedVersion int) error
	MarkDeleted(ctx context.Context, message domain.Message, expectedVersion int) error
	AdvanceEvent(ctx context.Context, messageID uuid.UUID, eventSequence int64) (domain.Message, error)
	UpdateModerationStatus(ctx context.Context, message domain.Message, expectedStatus domain.MessageStatus, expectedVersion int) error
}
