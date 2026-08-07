package repository

import (
	"context"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
)

type RealtimeTicketRepository interface {
	Store(ctx context.Context, ticket domain.RealtimeTicket) error
	Consume(ctx context.Context, ticketHash []byte, now time.Time) (domain.RealtimeTicket, error)
	DeleteExpired(ctx context.Context, now time.Time, limit int) (int, error)
}
