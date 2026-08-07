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

var _ repository.BlockRepository = (*BlockRepository)(nil)
