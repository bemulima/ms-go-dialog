package postgres

import (
	"context"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MentorRepository struct{ Pool *pgxpool.Pool }

func (r MentorRepository) Lock(ctx context.Context, kind domain.MentorContextKind, id uuid.UUID) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "mentor:"+string(kind)+":"+id.String())
	return err
}
func scanMentorBinding(row pgx.Row) (domain.MentorBinding, error) {
	var b domain.MentorBinding
	var student *uuid.UUID
	err := row.Scan(&b.ID, &b.DialogID, &b.Kind, &b.SourceID, &b.MentorID, &student)
	if student != nil {
		b.StudentID = *student
	}
	if err != nil {
		return b, mapError(err)
	}
	if !b.Valid() {
		return b, domain.ErrDependencyUnavailable
	}
	return b, nil
}

const mentorBindingColumns = `id,dialog_id,source_kind,source_id,mentor_id,student_id`

func (r MentorRepository) Get(ctx context.Context, id uuid.UUID) (domain.MentorBinding, error) {
	return scanMentorBinding(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+mentorBindingColumns+` FROM dialog_mentor_binding WHERE id=$1`, id))
}
func (r MentorRepository) Find(ctx context.Context, kind domain.MentorContextKind, id uuid.UUID) (domain.MentorBinding, error) {
	return scanMentorBinding(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+mentorBindingColumns+` FROM dialog_mentor_binding WHERE source_kind=$1 AND source_id=$2`, kind, id))
}
func (r MentorRepository) Protected(ctx context.Context, id uuid.UUID) (bool, error) {
	var protected bool
	err := runner(ctx, r.Pool).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM dialog_mentor_binding WHERE dialog_id=$1 AND source_kind='COHORT')`, id).Scan(&protected)
	return protected, err
}
func (r MentorRepository) Put(ctx context.Context, b domain.MentorBinding) error {
	if !b.Valid() {
		return domain.ErrValidation
	}
	_, err := runner(ctx, r.Pool).Exec(ctx, `INSERT INTO dialog_mentor_binding(id,dialog_id,source_kind,source_id,mentor_id,student_id) VALUES($1,$2,$3,$4,$5,$6)`, b.ID, b.DialogID, b.Kind, b.SourceID, b.MentorID, nullableUUID(b.StudentID))
	return mapError(err)
}
func (r MentorRepository) ReadSequence(ctx context.Context, b, user, membership uuid.UUID) (int64, error) {
	var value int64
	err := runner(ctx, r.Pool).QueryRow(ctx, `SELECT COALESCE((SELECT last_read_sequence FROM dialog_mentor_read_state WHERE binding_id=$1 AND user_id=$2 AND membership_id=$3),0)`, b, user, membership).Scan(&value)
	return value, err
}
func (r MentorRepository) AdvanceRead(ctx context.Context, b, user, membership uuid.UUID, through int64) error {
	if through < 0 {
		return domain.ErrInvalidReadSequence
	}
	_, err := runner(ctx, r.Pool).Exec(ctx, `INSERT INTO dialog_mentor_read_state(binding_id,user_id,membership_id,last_read_sequence) VALUES($1,$2,$3,$4) ON CONFLICT(binding_id,user_id,membership_id) DO UPDATE SET last_read_sequence=GREATEST(dialog_mentor_read_state.last_read_sequence,EXCLUDED.last_read_sequence)`, b, user, membership, through)
	return err
}
func (r DialogRepository) PublicProjection(ctx context.Context, id, user uuid.UUID, lastRead int64) (repository.PublicDialogProjection, error) {
	var p repository.PublicDialogProjection
	err := runner(ctx, r.Pool).QueryRow(ctx, `SELECT count(*),COALESCE(max(message_sequence),0),COALESCE(max(last_event_sequence),0),count(*) FILTER(WHERE status=1 AND (sender_id IS NULL OR sender_id<>$2) AND message_sequence>$3) FROM dialog_message WHERE dialog_id=$1 AND mentor_binding_id IS NULL AND status<>3`, id, user, lastRead).Scan(&p.MessageCount, &p.MaxMessageSequence, &p.MaxEventSequence, &p.UnreadCount)
	if err != nil {
		return p, err
	}
	err = runner(ctx, r.Pool).QueryRow(ctx, `SELECT id,created_at FROM dialog_message WHERE dialog_id=$1 AND mentor_binding_id IS NULL AND status<>3 ORDER BY message_sequence DESC LIMIT 1`, id).Scan(&p.LastMessageID, &p.LastMessageAt)
	if err == pgx.ErrNoRows {
		err = nil
	}
	return p, err
}
func mentorMessageScope(ctx context.Context) (*uuid.UUID, int64) {
	id := domain.SelectedMentorBinding(ctx)
	if id == nil {
		return nil, 0
	}
	proof := domain.CurrentMentorAccess(ctx)
	if proof == nil || proof.Binding.ID != *id {
		return id, 1 << 62
	}
	return id, proof.HistoryFromRevision
}

var _ repository.MentorRepository = MentorRepository{}
