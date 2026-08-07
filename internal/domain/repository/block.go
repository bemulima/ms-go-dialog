package repository

import (
	"context"

	"github.com/google/uuid"
)

type BlockRepository interface {
	ExistsEitherDirection(ctx context.Context, first, second uuid.UUID) (bool, error)
}
