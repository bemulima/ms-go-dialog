package postgres

import (
	"context"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OutboxRepository struct{ Pool *pgxpool.Pool }

func (r OutboxRepository) Add(ctx context.Context, item domain.OutboxEvent) error {
	if err := item.Validate(); err != nil {
		return err
	}
	_, err := runner(ctx, r.Pool).Exec(ctx, `INSERT INTO dialog_outbox (
id, dialog_id, aggregate_type, aggregate_id, subject, event_sequence,
schema_version, payload, attempts, next_attempt_at, published_at, last_error, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		item.ID, item.DialogID, item.AggregateType, item.AggregateID, item.Subject,
		item.EventSequence, item.SchemaVersion, item.Payload, item.Attempts,
		item.NextAttemptAt, item.PublishedAt, nullableString(item.LastError), item.CreatedAt)
	return mapError(err)
}

func (r OutboxRepository) ClaimPending(ctx context.Context, now, leaseUntil time.Time, limit int) ([]domain.OutboxEvent, error) {
	rows, err := runner(ctx, r.Pool).Query(ctx, `WITH candidates AS (
    SELECT id FROM dialog_outbox
    WHERE published_at IS NULL AND next_attempt_at<=$1
    ORDER BY next_attempt_at,created_at,id
    FOR UPDATE SKIP LOCKED LIMIT $2
)
UPDATE dialog_outbox o SET next_attempt_at=$3
FROM candidates c WHERE o.id=c.id RETURNING `+prefixedColumns("o", outboxColumns), now, limit, leaseUntil)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.OutboxEvent, 0)
	for rows.Next() {
		item, err := scanOutbox(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r OutboxRepository) MarkPublished(ctx context.Context, eventID uuid.UUID, publishedAt time.Time) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_outbox SET published_at=$2,last_error=NULL WHERE id=$1`, eventID, publishedAt)
	return mapError(err)
}

func (r OutboxRepository) MarkFailed(ctx context.Context, eventID uuid.UUID, nextAttemptAt time.Time, message string) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_outbox SET
attempts=attempts+1,next_attempt_at=$2,last_error=left($3,2048) WHERE id=$1`, eventID, nextAttemptAt, message)
	return mapError(err)
}

var _ repository.OutboxRepository = (*OutboxRepository)(nil)
