package repository

import (
	"context"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type ReportRepository interface {
	LockCommand(context.Context, uuid.UUID, uuid.UUID) error
	FindCommand(context.Context, uuid.UUID, uuid.UUID) (domain.MessageReport, error)
	GetForUpdate(context.Context, uuid.UUID) (domain.MessageReport, error)
	Create(context.Context, domain.MessageReport) error
	Resolve(context.Context, domain.MessageReport) error
	List(context.Context, *uuid.UUID, int) ([]domain.MessageReport, error)
}
