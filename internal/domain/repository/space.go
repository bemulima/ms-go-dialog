package repository

import (
	"context"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type SpaceRepository interface {
	Create(ctx context.Context, space domain.Space) error
	GetByID(ctx context.Context, id uuid.UUID) (domain.Space, error)
	GetByKey(ctx context.Context, key string) (domain.Space, error)
}
