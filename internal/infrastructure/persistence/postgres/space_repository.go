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

func (r SpaceRepository) Update(ctx context.Context, item domain.Space) error {
	command, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_space SET
name=$1,status=$2,allowed_origins=$3,allow_personal=$4,allow_groups=$5,
allow_images=$6,allow_files=$7,allow_links=$8,max_group_members=$9,
max_body_length=$10,max_attachments=$11,max_image_bytes=$12,max_file_bytes=$13,
allowed_file_mime_types=$14,edit_window_seconds=$15,updated_at=$16 WHERE id=$17`,
		item.Name, item.Status, item.AllowedOrigins, item.Policy.AllowPersonal, item.Policy.AllowGroups,
		item.Policy.AllowImages, item.Policy.AllowFiles, item.Policy.AllowLinks, item.Policy.MaxGroupMembers,
		item.Policy.MaxBodyLength, item.Policy.MaxAttachments, item.Policy.MaxImageBytes, item.Policy.MaxFileBytes,
		item.Policy.AllowedFileMIMETypes, item.Policy.EditWindowSeconds, item.UpdatedAt, item.ID)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r SpaceRepository) List(ctx context.Context, limit, offset int) ([]domain.Space, error) {
	rows, err := runner(ctx, r.Pool).Query(ctx, `SELECT `+spaceColumns+` FROM dialog_space ORDER BY created_at DESC,id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Space, 0)
	for rows.Next() {
		item, err := scanSpace(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

var _ repository.SpaceRepository = (*SpaceRepository)(nil)
