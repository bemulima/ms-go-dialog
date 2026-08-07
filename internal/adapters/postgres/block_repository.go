package postgres

import (
	"context"

	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BlockRepository struct{ Pool *pgxpool.Pool }

func (r BlockRepository) ExistsEitherDirection(ctx context.Context, first, second uuid.UUID) (bool, error) {
	var exists bool
	err := runner(ctx, r.Pool).QueryRow(ctx, `SELECT EXISTS(
SELECT 1 FROM dialog_user_block
WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1)
)`, first, second).Scan(&exists)
	return exists, err
}
func (r BlockRepository) Block(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `INSERT INTO dialog_user_block(blocker_id,blocked_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, blockerID, blockedID)
	return mapError(err)
}
func (r BlockRepository) Unblock(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `DELETE FROM dialog_user_block WHERE blocker_id=$1 AND blocked_id=$2`, blockerID, blockedID)
	return mapError(err)
}

var _ repository.BlockRepository = (*BlockRepository)(nil)
