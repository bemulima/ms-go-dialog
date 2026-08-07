package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type RealtimeTicket struct {
	Hash      [32]byte
	UserID    uuid.UUID
	SpaceID   uuid.UUID
	ExpiresAt time.Time
	CreatedAt time.Time
}

func (t RealtimeTicket) Validate() error {
	if t.UserID == uuid.Nil || t.SpaceID == uuid.Nil || t.Hash == [32]byte{} || !t.ExpiresAt.After(t.CreatedAt) || t.ExpiresAt.Sub(t.CreatedAt) > 30*time.Second {
		return fmt.Errorf("%w: invalid realtime ticket", ErrValidation)
	}
	return nil
}

type UserBlock struct {
	BlockerID uuid.UUID
	BlockedID uuid.UUID
	CreatedAt time.Time
}

func (b UserBlock) Validate() error {
	if b.BlockerID == uuid.Nil || b.BlockedID == uuid.Nil || b.BlockerID == b.BlockedID {
		return fmt.Errorf("%w: invalid user block", ErrValidation)
	}
	return nil
}
