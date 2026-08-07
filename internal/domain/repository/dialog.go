package repository

import (
	"context"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type DialogCursor struct {
	ActivityAt time.Time
	ID         uuid.UUID
}

type DialogListQuery struct {
	UserID  uuid.UUID
	SpaceID *uuid.UUID
	After   *DialogCursor
	Limit   int
}

type DialogListItem struct {
	Dialog domain.Dialog
	Member domain.Member
}
type AdminDialogListQuery struct {
	SpaceID       *uuid.UUID
	Status        *domain.DialogStatus
	Limit, Offset int
}

type DialogRepository interface {
	Create(ctx context.Context, dialog domain.Dialog) error
	GetByID(ctx context.Context, id uuid.UUID) (domain.Dialog, error)
	GetByIDForUpdate(ctx context.Context, id uuid.UUID) (domain.Dialog, error)
	FindPersonalByKey(ctx context.Context, spaceID uuid.UUID, key []byte) (domain.Dialog, error)
	LockPersonalKey(ctx context.Context, spaceID uuid.UUID, key []byte) error
	ListForUser(ctx context.Context, query DialogListQuery) ([]DialogListItem, error)
	UpdateState(ctx context.Context, dialog domain.Dialog, expectedVersion int) error
	ListAdmin(ctx context.Context, query AdminDialogListQuery) ([]domain.Dialog, error)
}
