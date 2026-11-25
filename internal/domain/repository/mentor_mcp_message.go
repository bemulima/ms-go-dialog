package repository

import (
	"context"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type MentorMCPMessageReceipts interface {
	GetMentorMCPMessageReceipt(context.Context, uuid.UUID, uuid.UUID) (domain.MentorMCPMessageReceipt, error)
	PutMentorMCPMessageReceipt(context.Context, domain.MentorMCPMessageReceipt) error
}
