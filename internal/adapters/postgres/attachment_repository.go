package postgres

import (
	"context"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AttachmentRepository struct{ Pool *pgxpool.Pool }

func (r AttachmentRepository) Create(ctx context.Context, item domain.Attachment) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `INSERT INTO dialog_attachment (
id, dialog_id, message_id, uploader_id, filestorage_id, kind, status, mime_type,
size_bytes, width, height, checksum_sha256, original_filename, expires_at,
activated_at, deleted_at, activation_attempts, activation_next_attempt_at,
delete_attempts, delete_next_attempt_at, last_error, storage_deleted_at,
created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)`,
		item.ID, item.DialogID, item.MessageID, item.UploaderID, item.FileStorageID,
		item.Kind, item.Status, item.MIMEType, item.SizeBytes, item.Width, item.Height,
		nullableBytes(item.ChecksumSHA256), item.OriginalFilename, item.ExpiresAt,
		item.ActivatedAt, item.DeletedAt, item.ActivationAttempts, item.ActivationNextAttemptAt,
		item.DeleteAttempts, item.DeleteNextAttemptAt, nullableString(item.LastError),
		item.StorageDeletedAt, item.CreatedAt, item.UpdatedAt)
	return mapError(err)
}

func (r AttachmentRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Attachment, error) {
	return scanAttachment(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+attachmentColumns+` FROM dialog_attachment WHERE id=$1`, id))
}

func (r AttachmentRepository) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (domain.Attachment, error) {
	return scanAttachment(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+attachmentColumns+` FROM dialog_attachment WHERE id=$1 FOR UPDATE`, id))
}

func (r AttachmentRepository) ListByMessage(ctx context.Context, dialogID, messageID uuid.UUID) ([]domain.Attachment, error) {
	rows, err := runner(ctx, r.Pool).Query(ctx, `SELECT `+attachmentColumns+`
FROM dialog_attachment WHERE dialog_id=$1 AND message_id=$2 ORDER BY created_at,id`, dialogID, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Attachment, 0)
	for rows.Next() {
		item, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r AttachmentRepository) CountPendingByUploader(ctx context.Context, dialogID, uploaderID uuid.UUID, now time.Time) (int, error) {
	var count int
	err := runner(ctx, r.Pool).QueryRow(ctx, `SELECT COUNT(*) FROM dialog_attachment
WHERE dialog_id=$1 AND uploader_id=$2 AND message_id IS NULL AND status=1 AND expires_at>$3`, dialogID, uploaderID, now).Scan(&count)
	return count, err
}

func (r AttachmentRepository) BindToMessage(ctx context.Context, attachmentID, dialogID, messageID, uploaderID uuid.UUID) error {
	command, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_attachment SET
message_id=$1, status=3, activation_next_attempt_at=NOW(), updated_at=NOW()
WHERE id=$2 AND dialog_id=$3 AND uploader_id=$4 AND message_id IS NULL
AND status=1 AND expires_at>NOW()`, messageID, attachmentID, dialogID, uploaderID)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() == 0 {
		return domain.ErrInvalidAttachment
	}
	return nil
}

func (r AttachmentRepository) MarkMessageAttachmentsDeleted(ctx context.Context, dialogID, messageID uuid.UUID, now time.Time) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_attachment SET
status=6, deleted_at=$3, delete_next_attempt_at=$3, updated_at=$3
WHERE dialog_id=$1 AND message_id=$2 AND status<>6`, dialogID, messageID, now)
	return mapError(err)
}

func (r AttachmentRepository) UpdateStatus(ctx context.Context, item domain.Attachment) error {
	command, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_attachment SET
status=$1, activated_at=$2, deleted_at=$3, activation_attempts=$4,
activation_next_attempt_at=$5, delete_attempts=$6, delete_next_attempt_at=$7,
last_error=$8, storage_deleted_at=$9, updated_at=$10 WHERE id=$11`,
		item.Status, item.ActivatedAt, item.DeletedAt, item.ActivationAttempts,
		item.ActivationNextAttemptAt, item.DeleteAttempts, item.DeleteNextAttemptAt,
		nullableString(item.LastError), item.StorageDeletedAt, item.UpdatedAt, item.ID)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

var _ repository.AttachmentRepository = (*AttachmentRepository)(nil)
