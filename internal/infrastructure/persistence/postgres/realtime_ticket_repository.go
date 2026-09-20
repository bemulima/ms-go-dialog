package postgres

import (
	"context"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RealtimeTicketRepository struct{ Pool *pgxpool.Pool }

func (r RealtimeTicketRepository) Store(ctx context.Context, item domain.RealtimeTicket) error {
	if err := item.Validate(); err != nil {
		return err
	}
	_, err := runner(ctx, r.Pool).Exec(ctx, `INSERT INTO dialog_ws_ticket
(ticket_hash,user_id,space_id,expires_at,created_at) VALUES ($1,$2,$3,$4,$5)`,
		item.Hash[:], item.UserID, item.SpaceID, item.ExpiresAt, item.CreatedAt)
	return mapError(err)
}

func (r RealtimeTicketRepository) Consume(ctx context.Context, hash []byte, now time.Time) (domain.RealtimeTicket, error) {
	var item domain.RealtimeTicket
	var storedHash []byte
	err := runner(ctx, r.Pool).QueryRow(ctx, `DELETE FROM dialog_ws_ticket
WHERE ticket_hash=$1 AND expires_at>$2
RETURNING ticket_hash,user_id,space_id,expires_at,created_at`, hash, now).Scan(
		&storedHash, &item.UserID, &item.SpaceID, &item.ExpiresAt, &item.CreatedAt)
	if err != nil {
		return domain.RealtimeTicket{}, mapError(err)
	}
	if len(storedHash) != len(item.Hash) {
		return domain.RealtimeTicket{}, domain.ErrTicketInvalid
	}
	copy(item.Hash[:], storedHash)
	return item, nil
}

func (r RealtimeTicketRepository) DeleteExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	command, err := runner(ctx, r.Pool).Exec(ctx, `DELETE FROM dialog_ws_ticket WHERE ticket_hash IN (
SELECT ticket_hash FROM dialog_ws_ticket WHERE expires_at<=$1 ORDER BY expires_at LIMIT $2
FOR UPDATE SKIP LOCKED)`, now, limit)
	if err != nil {
		return 0, err
	}
	return int(command.RowsAffected()), nil
}

var _ repository.RealtimeTicketRepository = (*RealtimeTicketRepository)(nil)
