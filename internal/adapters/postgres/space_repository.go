package postgres

import (
	"context"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SpaceRepository struct{ Pool *pgxpool.Pool }

func (r SpaceRepository) Create(ctx context.Context, item domain.Space) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `INSERT INTO dialog_space (
id, key, name, status, allowed_origins, allow_personal, allow_groups,
allow_images, allow_files, allow_links, max_group_members, max_body_length,
max_attachments, max_image_bytes, max_file_bytes, allowed_file_mime_types,
edit_window_seconds, created_by, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		item.ID, item.Key, item.Name, item.Status, item.AllowedOrigins,
		item.Policy.AllowPersonal, item.Policy.AllowGroups, item.Policy.AllowImages,
		item.Policy.AllowFiles, item.Policy.AllowLinks, item.Policy.MaxGroupMembers,
		item.Policy.MaxBodyLength, item.Policy.MaxAttachments, item.Policy.MaxImageBytes,
		item.Policy.MaxFileBytes, item.Policy.AllowedFileMIMETypes,
		item.Policy.EditWindowSeconds, item.CreatedBy, item.CreatedAt, item.UpdatedAt)
	return mapError(err)
}

func (r SpaceRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Space, error) {
	return scanSpace(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+spaceColumns+` FROM dialog_space WHERE id=$1`, id))
}

func (r SpaceRepository) GetByKey(ctx context.Context, key string) (domain.Space, error) {
	return scanSpace(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+spaceColumns+` FROM dialog_space WHERE key=$1`, key))
}

var _ repository.SpaceRepository = (*SpaceRepository)(nil)
