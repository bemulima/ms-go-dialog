package repository

import (
	"context"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type AttachmentRepository interface {
	Create(ctx context.Context, attachment domain.Attachment) error
	GetByID(ctx context.Context, id uuid.UUID) (domain.Attachment, error)
	GetByIDForUpdate(ctx context.Context, id uuid.UUID) (domain.Attachment, error)
	ListByMessage(ctx context.Context, dialogID, messageID uuid.UUID) ([]domain.Attachment, error)
	CountPendingByUploader(ctx context.Context, dialogID, uploaderID uuid.UUID, now time.Time) (int, error)
	BindToMessage(ctx context.Context, attachmentID, dialogID, messageID, uploaderID uuid.UUID) error
	MarkMessageAttachmentsDeleted(ctx context.Context, dialogID, messageID uuid.UUID, now time.Time) error
	UpdateStatus(ctx context.Context, attachment domain.Attachment) error
}
