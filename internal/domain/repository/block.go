package repository

import (
	"context"

	"github.com/google/uuid"
)

type BlockRepository interface {
	ExistsEitherDirection(ctx context.Context, first, second uuid.UUID) (bool, error)
	Block(ctx context.Context, blockerID, blockedID uuid.UUID) error
	Unblock(ctx context.Context, blockerID, blockedID uuid.UUID) error
}
