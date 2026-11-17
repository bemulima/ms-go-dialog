package repository

import (
	"context"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
	"time"
)

type MentorRepository interface {
	Lock(context.Context, domain.MentorContextKind, uuid.UUID) error
	Get(context.Context, uuid.UUID) (domain.MentorBinding, error)
	Find(context.Context, domain.MentorContextKind, uuid.UUID) (domain.MentorBinding, error)
	Protected(context.Context, uuid.UUID) (bool, error)
	Put(context.Context, domain.MentorBinding) error
	ReadSequence(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (int64, error)
	AdvanceRead(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int64) error
}
type PublicDialogProjection struct {
	MessageCount, MaxMessageSequence, MaxEventSequence, UnreadCount int64
	LastMessageID                                                   *uuid.UUID
	LastMessageAt                                                   *time.Time
}
type PublicDialogProjectionReader interface {
	PublicProjection(context.Context, uuid.UUID, uuid.UUID, int64) (PublicDialogProjection, error)
}
