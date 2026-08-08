package realtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

type ticketSpaceStore struct {
	repository.SpaceRepository
	item domain.Space
}

func (s ticketSpaceStore) GetByID(_ context.Context, id uuid.UUID) (domain.Space, error) {
	if id != s.item.ID {
		return domain.Space{}, domain.ErrNotFound
	}
	return s.item, nil
}

type ticketMemberStore struct {
	repository.MemberRepository
	spaceID, userID uuid.UUID
	sequences       map[uuid.UUID]int64
}

func (s ticketMemberStore) ListActiveDialogSequencesForUser(_ context.Context, spaceID, userID uuid.UUID) (map[uuid.UUID]int64, error) {
	if spaceID != s.spaceID || userID != s.userID {
		return nil, domain.ErrForbidden
	}
	result := make(map[uuid.UUID]int64, len(s.sequences))
	for id, sequence := range s.sequences {
		result[id] = sequence
	}
	return result, nil
}

type memoryTicketStore struct {
	repository.RealtimeTicketRepository
	item         domain.RealtimeTicket
	consumeCalls int
}

func (s *memoryTicketStore) Store(_ context.Context, item domain.RealtimeTicket) error {
	s.item = item
	return nil
}

func (s *memoryTicketStore) Consume(_ context.Context, hash []byte, now time.Time) (domain.RealtimeTicket, error) {
	s.consumeCalls++
	if s.item.Hash == [32]byte{} || !bytes.Equal(hash, s.item.Hash[:]) || !s.item.ExpiresAt.After(now) {
		return domain.RealtimeTicket{}, domain.ErrNotFound
	}
	item := s.item
	s.item = domain.RealtimeTicket{}
	return item, nil
}

func TestTicketService_MintAndConsumeSingleUseHashedTicket(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	spaceID, userID, dialogID := uuid.New(), uuid.New(), uuid.New()
	secret := bytes.Repeat([]byte{0x42}, ticketBytes)
	space := domain.Space{
		ID: spaceID, Key: "platform", Name: "Platform", Status: domain.SpaceStatusActive,
		AllowedOrigins: []string{"https://client.example"}, Policy: domain.DefaultPolicy(),
		CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	tickets := &memoryTicketStore{}
	service := TicketService{
		Spaces: ticketSpaceStore{item: space},
		Members: ticketMemberStore{
			spaceID: spaceID, userID: userID, sequences: map[uuid.UUID]int64{dialogID: 73},
		},
		Tickets: tickets, Now: func() time.Time { return now }, Random: bytes.NewReader(secret), TTL: 15 * time.Second,
	}

	minted, err := service.Mint(context.Background(), domain.Actor{UserID: userID, Role: "USER"}, MintTicketInput{SpaceID: spaceID})
	if err != nil {
		t.Fatal(err)
	}
	wantOpaque := base64.RawURLEncoding.EncodeToString(secret)
	wantHash := sha256.Sum256(secret)
	if minted.Ticket != wantOpaque || minted.Protocol != "dialog.v1" || !minted.ExpiresAt.Equal(now.Add(15*time.Second)) {
		t.Fatalf("minted ticket mismatch: %+v", minted)
	}
	if tickets.item.Hash != wantHash || tickets.item.UserID != userID || tickets.item.SpaceID != spaceID {
		t.Fatalf("stored ticket mismatch: %+v", tickets.item)
	}
	if bytes.Equal(tickets.item.Hash[:], secret) {
		t.Fatal("raw ticket secret was persisted instead of its hash")
	}

	session, err := service.Consume(context.Background(), minted.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	if session.UserID != userID || session.SpaceID != spaceID || session.DialogSequences[dialogID] != 73 || len(session.AllowedOrigins) != 1 {
		t.Fatalf("session mismatch: %+v", session)
	}
	if _, err := service.Consume(context.Background(), minted.Ticket); !errors.Is(err, domain.ErrTicketInvalid) {
		t.Fatalf("reused ticket error=%v", err)
	}
}

func TestTicketService_RejectsMalformedTicketBeforeRepository(t *testing.T) {
	store := &memoryTicketStore{}
	service := TicketService{Tickets: store}

	if _, err := service.Consume(context.Background(), "not-a-ticket"); !errors.Is(err, domain.ErrTicketInvalid) {
		t.Fatalf("malformed ticket error=%v", err)
	}
	if store.consumeCalls != 0 {
		t.Fatalf("malformed ticket reached repository %d times", store.consumeCalls)
	}
}
