package postgres

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MessageRepository struct{ Pool *pgxpool.Pool }

func (r MessageRepository) Create(ctx context.Context, item domain.Message) error {
	links, err := json.Marshal(item.Links)
	if err != nil {
		return err
	}
	var lessonContext any
	if item.LessonContext != nil {
		lessonContext, err = json.Marshal(item.LessonContext)
		if err != nil {
			return err
		}
	}
	_, err = runner(ctx, r.Pool).Exec(ctx, `INSERT INTO dialog_message (
id, dialog_id, author_type, channel, sender_id, personal_teacher_id, learning_action_id, lesson_context, reply_to_message_id, body, links, status, version,
message_sequence, last_event_sequence, idempotency_key, edited_at, deleted_at,
created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		item.ID, item.DialogID, item.AuthorType, item.Channel, nullableUUID(item.SenderID), nullableUUID(item.PersonalTeacherID), item.LearningActionID, lessonContext,
		item.ReplyToMessageID, item.Body, links, item.Status, item.Version, item.MessageSequence, item.LastEventSequence,
		item.IdempotencyKey, item.EditedAt, item.DeletedAt, item.CreatedAt, item.UpdatedAt)
	return mapError(err)
}

func (r MessageRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Message, error) {
	return scanMessage(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+messageColumns+` FROM dialog_message WHERE id=$1`, id))
}

func (r MessageRepository) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (domain.Message, error) {
	return scanMessage(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+messageColumns+` FROM dialog_message WHERE id=$1 FOR UPDATE`, id))
}

func (r MessageRepository) GetByIdempotencyKey(ctx context.Context, senderID, key uuid.UUID) (domain.Message, error) {
	return scanMessage(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+messageColumns+`
FROM dialog_message WHERE sender_id=$1 AND idempotency_key=$2`, senderID, key))
}

func (r MessageRepository) LockIdempotencyKey(ctx context.Context, senderID, key uuid.UUID) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, senderID.String()+":"+key.String())
	return err
}

func (r MessageRepository) GetByTeacherIdempotencyKey(ctx context.Context, personalTeacherID, key uuid.UUID) (domain.Message, error) {
	return scanMessage(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+messageColumns+`
FROM dialog_message WHERE author_type='personal_teacher' AND personal_teacher_id=$1 AND idempotency_key=$2`, personalTeacherID, key))
}

func (r MessageRepository) LockTeacherIdempotencyKey(ctx context.Context, personalTeacherID, key uuid.UUID) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "personal_teacher:"+personalTeacherID.String()+":"+key.String())
	return err
}

func (r MessageRepository) List(ctx context.Context, query repository.MessageListQuery) ([]domain.Message, error) {
	base := `SELECT ` + messageColumns + ` FROM dialog_message WHERE dialog_id=$1 AND message_sequence>=$2 AND status<>3`
	args := []any{query.DialogID, query.FromSequence}
	descending := false
	if query.Before != nil {
		args = append(args, query.Before.Sequence, query.Before.ID, query.Limit)
		base += ` AND (message_sequence,id)<($3,$4) ORDER BY message_sequence DESC,id DESC LIMIT $5`
		descending = true
	} else if query.After != nil {
		args = append(args, query.After.Sequence, query.After.ID, query.Limit)
		base += ` AND (message_sequence,id)>($3,$4) ORDER BY message_sequence,id LIMIT $5`
	} else {
		args = append(args, query.Limit)
		base += ` ORDER BY message_sequence DESC,id DESC LIMIT $3`
		descending = true
	}
	rows, err := runner(ctx, r.Pool).Query(ctx, base, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanMessages(rows)
	if descending {
		reverseMessages(items)
	}
	return items, err
}

func (r MessageRepository) Window(ctx context.Context, query repository.MessageWindowQuery) ([]domain.Message, error) {
	rows, err := runner(ctx, r.Pool).Query(ctx, `SELECT `+messageColumns+` FROM (
    (SELECT `+messageColumns+` FROM dialog_message
     WHERE dialog_id=$1 AND message_sequence>=$5 AND message_sequence<$2 AND status<>3
     ORDER BY message_sequence DESC,id DESC LIMIT $3)
    UNION ALL
    (SELECT `+messageColumns+` FROM dialog_message
     WHERE dialog_id=$1 AND message_sequence>=$5 AND message_sequence>=$2 AND status<>3
     ORDER BY message_sequence,id LIMIT $4)
) AS message_window ORDER BY message_sequence,id`, query.DialogID, query.AnchorSequence, query.Before, query.After, query.FromSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (r MessageRepository) ListChanges(ctx context.Context, query repository.MessageChangeQuery) ([]domain.Message, error) {
	rows, err := runner(ctx, r.Pool).Query(ctx, `SELECT `+messageColumns+`
FROM dialog_message WHERE dialog_id=$1 AND message_sequence>=$2 AND last_event_sequence>$3
ORDER BY last_event_sequence,id LIMIT $4`, query.DialogID, query.FromSequence, query.AfterEventSequence, query.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (r MessageRepository) FirstUnreadIncoming(ctx context.Context, dialogID, readerID uuid.UUID, afterSequence int64) (int64, error) {
	var sequence int64
	err := runner(ctx, r.Pool).QueryRow(ctx, `SELECT message_sequence FROM dialog_message
WHERE dialog_id=$1 AND (sender_id IS NULL OR sender_id<>$2) AND message_sequence>$3 AND status=1
ORDER BY message_sequence LIMIT 1`, dialogID, readerID, afterSequence).Scan(&sequence)
	return sequence, mapError(err)
}

func (r MessageRepository) CountUnreadIncoming(ctx context.Context, dialogID, readerID uuid.UUID, afterExclusive, throughInclusive int64) (int64, error) {
	if throughInclusive < afterExclusive {
		return 0, domain.ErrInvalidReadSequence
	}
	var count int64
	err := runner(ctx, r.Pool).QueryRow(ctx, `SELECT COUNT(*) FROM dialog_message
WHERE dialog_id=$1 AND (sender_id IS NULL OR sender_id<>$2) AND message_sequence>$3 AND message_sequence<=$4 AND status=1`,
		dialogID, readerID, afterExclusive, throughInclusive).Scan(&count)
	return count, err
}

func (r MessageRepository) UpdateContent(ctx context.Context, item domain.Message, expectedVersion int) error {
	links, err := json.Marshal(item.Links)
	if err != nil {
		return err
	}
	command, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_message SET
body=$1, links=$2, version=$3, last_event_sequence=$4, edited_at=$5, updated_at=$6
WHERE id=$7 AND version=$8 AND status=1`, item.Body, links, item.Version,
		item.LastEventSequence, item.EditedAt, item.UpdatedAt, item.ID, expectedVersion)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() == 0 {
		return domain.ErrMessageConflict
	}
	return nil
}

func (r MessageRepository) MarkDeleted(ctx context.Context, item domain.Message, expectedVersion int) error {
	command, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_message SET
body='', links='[]'::jsonb, lesson_context=NULL, status=$1, version=$2, last_event_sequence=$3,
deleted_at=$4, updated_at=$5 WHERE id=$6 AND version=$7 AND status=1`,
		item.Status, item.Version, item.LastEventSequence, item.DeletedAt,
		item.UpdatedAt, item.ID, expectedVersion)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() == 0 {
		return domain.ErrMessageConflict
	}
	return nil
}

func (r MessageRepository) AdvanceEvent(ctx context.Context, messageID uuid.UUID, eventSequence int64) (domain.Message, error) {
	return scanMessage(runner(ctx, r.Pool).QueryRow(ctx, `UPDATE dialog_message SET
version=version+1,last_event_sequence=$2,updated_at=NOW()
WHERE id=$1 AND status=1 RETURNING `+messageColumns, messageID, eventSequence))
}

func (r MessageRepository) UpdateModerationStatus(ctx context.Context, item domain.Message, expectedStatus domain.MessageStatus, expectedVersion int) error {
	command, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_message SET status=$1,version=$2,last_event_sequence=$3,updated_at=$4
WHERE id=$5 AND status=$6 AND version=$7`, item.Status, item.Version, item.LastEventSequence, item.UpdatedAt, item.ID, expectedStatus, expectedVersion)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() == 0 {
		return domain.ErrMessageConflict
	}
	return nil
}

func reverseMessages(items []domain.Message) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].MessageSequence == items[j].MessageSequence {
			return items[i].ID.String() < items[j].ID.String()
		}
		return items[i].MessageSequence < items[j].MessageSequence
	})
}

var _ repository.MessageRepository = (*MessageRepository)(nil)
var _ repository.TeacherMessageRepository = (*MessageRepository)(nil)
