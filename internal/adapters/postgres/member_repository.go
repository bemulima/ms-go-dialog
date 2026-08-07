package postgres

import (
	"context"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MemberRepository struct{ Pool *pgxpool.Pool }

func (r MemberRepository) CreateMany(ctx context.Context, members []domain.Member) error {
	for _, member := range members {
		if err := r.Create(ctx, member); err != nil {
			return err
		}
	}
	return nil
}

func (r MemberRepository) Create(ctx context.Context, item domain.Member) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `INSERT INTO dialog_member (
dialog_id, user_id, role, status, history_from_message_sequence,
last_read_message_sequence, unread_count, last_event_sequence, last_read_at,
muted_until, archived_at, added_by, joined_at, left_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		item.DialogID, item.UserID, item.Role, item.Status, item.HistoryFromMessageSequence,
		item.LastReadMessageSequence, item.UnreadCount, item.LastEventSequence, item.LastReadAt,
		item.MutedUntil, item.ArchivedAt, item.AddedBy, item.JoinedAt, item.LeftAt, item.UpdatedAt)
	return mapError(err)
}

func (r MemberRepository) Get(ctx context.Context, dialogID, userID uuid.UUID) (domain.Member, error) {
	return scanMember(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+memberColumns+`
FROM dialog_member WHERE dialog_id=$1 AND user_id=$2`, dialogID, userID))
}

func (r MemberRepository) GetForUpdate(ctx context.Context, dialogID, userID uuid.UUID) (domain.Member, error) {
	return scanMember(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+memberColumns+`
FROM dialog_member WHERE dialog_id=$1 AND user_id=$2 FOR UPDATE`, dialogID, userID))
}

func (r MemberRepository) ListActive(ctx context.Context, dialogID uuid.UUID) ([]domain.Member, error) {
	rows, err := runner(ctx, r.Pool).Query(ctx, `SELECT `+memberColumns+`
FROM dialog_member WHERE dialog_id=$1 AND status=1 ORDER BY joined_at,user_id`, dialogID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Member, 0)
	for rows.Next() {
		item, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r MemberRepository) Update(ctx context.Context, item domain.Member) error {
	command, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_member SET
role=$1, status=$2, history_from_message_sequence=$3, last_read_message_sequence=$4,
unread_count=$5, last_event_sequence=$6, last_read_at=$7, muted_until=$8,
archived_at=$9, left_at=$10, updated_at=$11
WHERE dialog_id=$12 AND user_id=$13`,
		item.Role, item.Status, item.HistoryFromMessageSequence, item.LastReadMessageSequence,
		item.UnreadCount, item.LastEventSequence, item.LastReadAt, item.MutedUntil,
		item.ArchivedAt, item.LeftAt, item.UpdatedAt, item.DialogID, item.UserID)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r MemberRepository) IncrementUnreadRecipients(ctx context.Context, dialogID, senderID uuid.UUID, eventSequence int64) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_member SET
unread_count=unread_count+1, last_event_sequence=$3, updated_at=NOW()
WHERE dialog_id=$1 AND user_id<>$2 AND status=1`, dialogID, senderID, eventSequence)
	return mapError(err)
}

func (r MemberRepository) DecrementUnreadForDeletedMessage(ctx context.Context, dialogID, senderID uuid.UUID, messageSequence, eventSequence int64) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_member SET
unread_count=GREATEST(unread_count-1,0), last_event_sequence=$4, updated_at=NOW()
WHERE dialog_id=$1 AND user_id<>$2 AND status=1
AND last_read_message_sequence<$3 AND unread_count>0`, dialogID, senderID, messageSequence, eventSequence)
	return mapError(err)
}

func (r MemberRepository) CountActiveOwners(ctx context.Context, dialogID uuid.UUID) (int, error) {
	var count int
	err := runner(ctx, r.Pool).QueryRow(ctx, `SELECT COUNT(*) FROM dialog_member
WHERE dialog_id=$1 AND status=1 AND role=1`, dialogID).Scan(&count)
	return count, err
}

var _ repository.MemberRepository = (*MemberRepository)(nil)
