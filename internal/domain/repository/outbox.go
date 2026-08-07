package repository

import (
	"context"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type OutboxRepository interface {
	Add(ctx context.Context, event domain.OutboxEvent) error
	ClaimPending(ctx context.Context, now, leaseUntil time.Time, limit int) ([]domain.OutboxEvent, error)
	MarkPublished(ctx context.Context, eventID uuid.UUID, publishedAt time.Time) error
	MarkFailed(ctx context.Context, eventID uuid.UUID, nextAttemptAt time.Time, message string) error
}
