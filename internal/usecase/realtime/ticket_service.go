package realtime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

const defaultTicketTTL = 30 * time.Second
const ticketBytes = 32

type TicketService struct {
	Spaces  repository.SpaceRepository
	Members repository.MemberRepository
	Tickets repository.RealtimeTicketRepository
	Now     func() time.Time
	Random  io.Reader
	TTL     time.Duration
}
type MintTicketInput struct{ SpaceID uuid.UUID }
type MintedTicket struct {
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expires_at"`
	Protocol  string    `json:"protocol"`
}
type Session struct {
	UserID          uuid.UUID
	SpaceID         uuid.UUID
	DialogSequences map[uuid.UUID]int64
	AllowedOrigins  []string
}

func (s TicketService) Mint(ctx context.Context, actor domain.Actor, input MintTicketInput) (MintedTicket, error) {
	if err := actor.Validate(); err != nil {
		return MintedTicket{}, err
	}
	space, err := s.Spaces.GetByID(ctx, input.SpaceID)
	if err != nil || space.Status != domain.SpaceStatusActive {
		return MintedTicket{}, domain.ErrDialogNotFound
	}
	secret := make([]byte, ticketBytes)
	reader := s.Random
	if reader == nil {
		reader = rand.Reader
	}
	if _, err = io.ReadFull(reader, secret); err != nil {
		return MintedTicket{}, fmt.Errorf("generate realtime ticket: %w", err)
	}
	now := s.now()
	hash := sha256.Sum256(secret)
	ticket := domain.RealtimeTicket{Hash: hash, UserID: actor.UserID, SpaceID: space.ID, CreatedAt: now, ExpiresAt: now.Add(s.ttl())}
	if err = ticket.Validate(); err != nil {
		return MintedTicket{}, err
	}
	if err = s.Tickets.Store(ctx, ticket); err != nil {
		return MintedTicket{}, err
	}
	return MintedTicket{Ticket: base64.RawURLEncoding.EncodeToString(secret), ExpiresAt: ticket.ExpiresAt, Protocol: "dialog.v1"}, nil
}
func (s TicketService) Consume(ctx context.Context, opaque string) (Session, error) {
	secret, err := base64.RawURLEncoding.DecodeString(opaque)
	if err != nil || len(secret) != ticketBytes {
		return Session{}, domain.ErrTicketInvalid
	}
	hash := sha256.Sum256(secret)
	ticket, err := s.Tickets.Consume(ctx, hash[:], s.now())
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return Session{}, domain.ErrTicketInvalid
		}
		return Session{}, err
	}
	space, err := s.Spaces.GetByID(ctx, ticket.SpaceID)
	if err != nil || space.Status != domain.SpaceStatusActive {
		return Session{}, domain.ErrTicketInvalid
	}
	dialogs, err := s.Members.ListActiveDialogSequencesForUser(ctx, ticket.SpaceID, ticket.UserID)
	if err != nil {
		return Session{}, err
	}
	return Session{UserID: ticket.UserID, SpaceID: ticket.SpaceID, DialogSequences: dialogs, AllowedOrigins: append([]string(nil), space.AllowedOrigins...)}, nil
}
func (s TicketService) DeleteExpired(ctx context.Context, limit int) (int, error) {
	if limit < 1 {
		return 0, domain.ErrValidation
	}
	return s.Tickets.DeleteExpired(ctx, s.now(), limit)
}
func (s TicketService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s TicketService) ttl() time.Duration {
	if s.TTL > 0 && s.TTL <= defaultTicketTTL {
		return s.TTL
	}
	return defaultTicketTTL
}
