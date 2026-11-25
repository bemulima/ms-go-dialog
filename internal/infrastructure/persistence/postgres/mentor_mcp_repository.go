package postgres

import (
	"context"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

func (r MentorRepository) ListOwnedMentorBindings(ctx context.Context, mentor, after uuid.UUID, limit int) ([]domain.MentorBinding, error) {
	rows, err := runner(ctx, r.Pool).Query(ctx, `SELECT `+mentorBindingColumns+`
FROM dialog_mentor_binding WHERE mentor_id=$1 AND id>$2 ORDER BY id LIMIT $3`, mentor, after, limit)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	items := make([]domain.MentorBinding, 0, limit)
	for rows.Next() {
		binding, err := scanMentorBinding(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, binding)
	}
	return items, mapError(rows.Err())
}
