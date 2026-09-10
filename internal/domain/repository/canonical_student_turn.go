package repository

import (
	"context"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

// CanonicalStudentTurnRepository owns only private receipt evidence. It must
// never be used by REST/WebSocket projections or ordinary lifecycle events.
type CanonicalStudentTurnRepository interface {
	LockIdentity(ctx context.Context, actionReceiptID, canonicalMessageCommandID uuid.UUID) error
	LockSourceAction(ctx context.Context, dialogID, sourcePromptMessageID uuid.UUID, blockID, actionID string) error
	GetByActionReceiptID(ctx context.Context, actionReceiptID uuid.UUID) (domain.CanonicalStudentTurn, error)
	GetByCanonicalMessageCommandID(ctx context.Context, canonicalMessageCommandID uuid.UUID) (domain.CanonicalStudentTurn, error)
	GetBySourceAction(ctx context.Context, dialogID, sourcePromptMessageID uuid.UUID, blockID, actionID string) (domain.CanonicalStudentTurn, error)
	GetByCanonicalMessageID(ctx context.Context, canonicalStudentMessageID uuid.UUID) (domain.CanonicalStudentTurn, error)
	Create(ctx context.Context, item domain.CanonicalStudentTurn) error
}
