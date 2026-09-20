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

func (r AttachmentRepository) ListExpired(ctx context.Context, before time.Time, limit int) ([]domain.Attachment, error) {
	rows, err := runner(ctx, r.Pool).Query(ctx, `SELECT `+attachmentColumns+`
FROM dialog_attachment WHERE status=1 AND expires_at<$1 ORDER BY expires_at,id LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAttachmentRows(rows)
}

func (r AttachmentRepository) ClaimForActivation(ctx context.Context, now, leaseUntil time.Time, limit int) ([]domain.Attachment, error) {
	rows, err := runner(ctx, r.Pool).Query(ctx, `WITH candidates AS (
    SELECT id FROM dialog_attachment WHERE status=3
    AND (activation_next_attempt_at IS NULL OR activation_next_attempt_at<=$1)
    ORDER BY activation_next_attempt_at NULLS FIRST,id
    FOR UPDATE SKIP LOCKED LIMIT $3
)
UPDATE dialog_attachment a SET activation_next_attempt_at=$2,updated_at=$1
FROM candidates c WHERE a.id=c.id RETURNING `+prefixedColumns("a", attachmentColumns), now, leaseUntil, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAttachmentRows(rows)
}

func (r AttachmentRepository) MarkReadyIfProcessing(ctx context.Context, id uuid.UUID, now time.Time) (domain.Attachment, bool, error) {
	item, err := scanAttachment(runner(ctx, r.Pool).QueryRow(ctx, `UPDATE dialog_attachment SET
status=4,activated_at=$2,activation_next_attempt_at=NULL,last_error=NULL,updated_at=$2
WHERE id=$1 AND status=3 RETURNING `+attachmentColumns, id, now))
	if err == domain.ErrNotFound {
		return domain.Attachment{}, false, nil
	}
	return item, err == nil, err
}

func (r AttachmentRepository) RecordActivationFailure(ctx context.Context, id uuid.UUID, next time.Time, message string, maxAttempts int) (domain.Attachment, bool, error) {
	item, err := scanAttachment(runner(ctx, r.Pool).QueryRow(ctx, `UPDATE dialog_attachment SET
activation_attempts=activation_attempts+1,
status=CASE WHEN activation_attempts+1 >= $4 THEN 5 ELSE 3 END,
activation_next_attempt_at=CASE WHEN activation_attempts+1 >= $4 THEN NULL ELSE $2 END,
last_error=left($3,1000),updated_at=NOW()
WHERE id=$1 AND status=3 RETURNING `+attachmentColumns, id, next, message, maxAttempts))
	if err == domain.ErrNotFound {
		return domain.Attachment{}, false, nil
	}
	if err != nil {
		return domain.Attachment{}, false, err
	}
	return item, item.Status == domain.AttachmentStatusFailed, nil
}

func (r AttachmentRepository) ClaimForDeletion(ctx context.Context, now, leaseUntil time.Time, limit int) ([]domain.Attachment, error) {
	rows, err := runner(ctx, r.Pool).Query(ctx, `WITH candidates AS (
    SELECT id FROM dialog_attachment WHERE status=6 AND storage_deleted_at IS NULL
    AND (delete_next_attempt_at IS NULL OR delete_next_attempt_at<=$1)
    ORDER BY delete_next_attempt_at NULLS FIRST,id
    FOR UPDATE SKIP LOCKED LIMIT $3
)
UPDATE dialog_attachment a SET delete_next_attempt_at=$2,updated_at=$1
FROM candidates c WHERE a.id=c.id RETURNING `+prefixedColumns("a", attachmentColumns), now, leaseUntil, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAttachmentRows(rows)
}

func (r AttachmentRepository) MarkStorageDeleted(ctx context.Context, id uuid.UUID, now time.Time) error {
	command, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_attachment SET
storage_deleted_at=$2,delete_next_attempt_at=NULL,last_error=NULL,updated_at=$2
WHERE id=$1 AND status=6 AND storage_deleted_at IS NULL`, id, now)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r AttachmentRepository) RecordDeleteFailure(ctx context.Context, id uuid.UUID, next time.Time, message string) error {
	command, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_attachment SET
delete_attempts=delete_attempts+1,delete_next_attempt_at=$2,last_error=left($3,1000),updated_at=NOW()
WHERE id=$1 AND status=6 AND storage_deleted_at IS NULL`, id, next, message)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func scanAttachmentRows(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]domain.Attachment, error) {
	items := make([]domain.Attachment, 0)
	for rows.Next() {
		item, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

var _ repository.AttachmentRepository = (*AttachmentRepository)(nil)
