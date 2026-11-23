package postgres

import (
	"context"
	"encoding/json"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ReportRepository struct{ Pool *pgxpool.Pool }

const reportColumns = `snapshot,command_digest,mentor_binding_id,resolution_digest`

func scanReport(row pgx.Row) (domain.MessageReport, error) {
	var r domain.MessageReport
	var raw []byte
	err := row.Scan(&raw, &r.CommandDigest, &r.MentorBindingID, &r.ResolutionDigest)
	if err != nil {
		return r, mapError(err)
	}
	if json.Unmarshal(raw, &r) != nil || !r.Valid() {
		return domain.MessageReport{}, domain.ErrDependencyUnavailable
	}
	return r, nil
}
func (r ReportRepository) LockCommand(ctx context.Context, actor, key uuid.UUID) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "report:"+actor.String()+":"+key.String())
	return err
}
func (r ReportRepository) FindCommand(ctx context.Context, actor, key uuid.UUID) (domain.MessageReport, error) {
	return scanReport(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+reportColumns+` FROM dialog_message_report WHERE reporter_id=$1 AND request_id=$2`, actor, key))
}
func (r ReportRepository) GetForUpdate(ctx context.Context, id uuid.UUID) (domain.MessageReport, error) {
	return scanReport(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+reportColumns+` FROM dialog_message_report WHERE id=$1 FOR UPDATE`, id))
}
func (r ReportRepository) Create(ctx context.Context, v domain.MessageReport) error {
	if !v.Valid() || v.Revision != 1 {
		return domain.ErrValidation
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = runner(ctx, r.Pool).Exec(ctx, `INSERT INTO dialog_message_report(id,reporter_id,request_id,message_id,created_at,revision,command_digest,mentor_binding_id,snapshot) VALUES($1,$2,$3,$4,$5,1,$6,$7,$8)`, v.ID, v.ReporterID, v.RequestID, v.MessageID, v.CreatedAt, v.CommandDigest, v.MentorBindingID, raw)
	return mapError(err)
}
func (r ReportRepository) Resolve(ctx context.Context, v domain.MessageReport) error {
	if !v.Valid() || v.Revision != 2 {
		return domain.ErrValidation
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tag, err := runner(ctx, r.Pool).Exec(ctx, `UPDATE dialog_message_report SET revision=2,resolution_digest=$2,resolution_actor_id=$3,resolution_request_id=$4,snapshot=$5 WHERE id=$1 AND revision=1`, v.ID, v.ResolutionDigest, v.Resolution.ActorID, v.Resolution.RequestID, raw)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrModerationConflict
	}
	return nil
}
func (r ReportRepository) List(ctx context.Context, before *uuid.UUID, limit int) ([]domain.MessageReport, error) {
	if limit < 1 || limit > 51 {
		return nil, domain.ErrValidation
	}
	rows, err := runner(ctx, r.Pool).Query(ctx, `SELECT `+reportColumns+` FROM dialog_message_report WHERE ($1::uuid IS NULL OR (created_at,id)<(SELECT created_at,id FROM dialog_message_report WHERE id=$1)) ORDER BY created_at DESC,id DESC LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.MessageReport{}
	for rows.Next() {
		v, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

var _ repository.ReportRepository = ReportRepository{}
