package repository

import (
	"context"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type MemberRepository interface {
	CreateMany(ctx context.Context, members []domain.Member) error
	Create(ctx context.Context, member domain.Member) error
	Get(ctx context.Context, dialogID, userID uuid.UUID) (domain.Member, error)
	GetForUpdate(ctx context.Context, dialogID, userID uuid.UUID) (domain.Member, error)
	ListActive(ctx context.Context, dialogID uuid.UUID) ([]domain.Member, error)
	Update(ctx context.Context, member domain.Member) error
	IncrementUnreadRecipients(ctx context.Context, dialogID, senderID uuid.UUID, eventSequence int64) error
	DecrementUnreadForDeletedMessage(ctx context.Context, dialogID, senderID uuid.UUID, messageSequence, eventSequence int64) error
	CountActiveOwners(ctx context.Context, dialogID uuid.UUID) (int, error)
}
