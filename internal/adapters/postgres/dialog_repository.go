package postgres

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DialogRepository struct{ Pool *pgxpool.Pool }

func (r DialogRepository) Create(ctx context.Context, item domain.Dialog) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `INSERT INTO dialog (
id, space_id, type, status, personal_key, title, created_by, version, member_count,
message_count, max_message_sequence, max_event_sequence, last_message_id,
last_message_at, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		item.ID, item.SpaceID, item.Type, item.Status, nullableBytes(item.PersonalKey), nullableString(item.Title),
		item.CreatedBy, item.Version, item.MemberCount, item.MessageCount, item.MaxMessageSequence,
		item.MaxEventSequence, item.LastMessageID, item.LastMessageAt, item.CreatedAt, item.UpdatedAt)
	return mapError(err)
}

func (r DialogRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Dialog, error) {
	return scanDialog(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+dialogColumns+` FROM dialog WHERE id=$1`, id))
}

func (r DialogRepository) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (domain.Dialog, error) {
	return scanDialog(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+dialogColumns+` FROM dialog WHERE id=$1 FOR UPDATE`, id))
}

func (r DialogRepository) FindPersonalByKey(ctx context.Context, spaceID uuid.UUID, key []byte) (domain.Dialog, error) {
	return scanDialog(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+dialogColumns+`
FROM dialog WHERE space_id=$1 AND type=1 AND personal_key=$2`, spaceID, key))
}

func (r DialogRepository) LockPersonalKey(ctx context.Context, spaceID uuid.UUID, key []byte) error {
	lockKey := spaceID.String() + ":" + hex.EncodeToString(key)
	_, err := runner(ctx, r.Pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey)
	return err
}

func (r DialogRepository) ListForUser(ctx context.Context, query repository.DialogListQuery) ([]repository.DialogListItem, error) {
	if query.UserID == uuid.Nil || query.Limit < 1 {
		return nil, domain.ErrValidation
	}
	base := `SELECT ` + prefixedColumns("d", dialogColumns) + `, ` + prefixedColumns("m", memberColumns) + `
FROM dialog d JOIN dialog_member m ON m.dialog_id=d.id
WHERE m.user_id=$1 AND m.status=1 AND d.status<>3`
	args := []any{query.UserID}
	if query.SpaceID != nil {
		args = append(args, *query.SpaceID)
		base += fmt.Sprintf(" AND d.space_id=$%d", len(args))
	}
	if query.After != nil {
		args = append(args, query.After.ActivityAt, query.After.ID)
		base += fmt.Sprintf(" AND (COALESCE(d.last_message_at,d.created_at),d.id)<($%d,$%d)", len(args)-1, len(args))
	}
	args = append(args, query.Limit)
	base += fmt.Sprintf(" ORDER BY COALESCE(d.last_message_at,d.created_at) DESC,d.id DESC LIMIT $%d", len(args))
	rows, err := runner(ctx, r.Pool).Query(ctx, base, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]repository.DialogListItem, 0)
	for rows.Next() {
		item, member, err := scanDialogAndMember(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, repository.DialogListItem{Dialog: item, Member: member})
	}
	return result, rows.Err()
}

func (r DialogRepository) UpdateState(ctx context.Context, item domain.Dialog, expectedVersion int) error {
	command, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog SET
status=$1, title=$2, version=$3, member_count=$4, message_count=$5,
max_message_sequence=$6, max_event_sequence=$7, last_message_id=$8,
last_message_at=$9, updated_at=$10 WHERE id=$11 AND version=$12`,
		item.Status, nullableString(item.Title), item.Version, item.MemberCount, item.MessageCount,
		item.MaxMessageSequence, item.MaxEventSequence, item.LastMessageID, item.LastMessageAt,
		item.UpdatedAt, item.ID, expectedVersion)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() == 0 {
		return domain.ErrMessageConflict
	}
	return nil
}

func scanDialogAndMember(row interface{ Scan(...any) error }) (domain.Dialog, domain.Member, error) {
	var item domain.Dialog
	var member domain.Member
	var title *string
	err := row.Scan(
		&item.ID, &item.SpaceID, &item.Type, &item.Status, &item.PersonalKey, &title,
		&item.CreatedBy, &item.Version, &item.MemberCount, &item.MessageCount,
		&item.MaxMessageSequence, &item.MaxEventSequence, &item.LastMessageID, &item.LastMessageAt,
		&item.CreatedAt, &item.UpdatedAt,
		&member.DialogID, &member.UserID, &member.Role, &member.Status,
		&member.HistoryFromMessageSequence, &member.LastReadMessageSequence, &member.UnreadCount,
		&member.LastEventSequence, &member.LastReadAt, &member.MutedUntil, &member.ArchivedAt,
		&member.AddedBy, &member.JoinedAt, &member.LeftAt, &member.UpdatedAt,
	)
	if title != nil {
		item.Title = *title
	}
	return item, member, mapError(err)
}

func prefixedColumns(alias, columns string) string {
	result := ""
	for index, column := range splitColumns(columns) {
		if index > 0 {
			result += ", "
		}
		result += alias + "." + column
	}
	return result
}

func splitColumns(columns string) []string {
	result, current := []string{}, ""
	for _, char := range columns {
		switch char {
		case ',', '\n', '\t', ' ':
			if current != "" {
				result = append(result, current)
				current = ""
			}
		default:
			current += string(char)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

var _ repository.DialogRepository = (*DialogRepository)(nil)
