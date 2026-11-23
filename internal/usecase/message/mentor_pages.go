package message

import (
	"context"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
)

type MentorMessagePage struct {
	Items     []View
	Next      *repository.MessageCursor
	NextAfter int64
	HasMore   bool
}

// A page scans at most 31 rows. Cursors advance over the observed page even
// when every author's current membership was revoked. They use the already
// known binding ID, never a hidden message ID. The SQL selector orders by the
// unique message sequence for this cursor format; a cursor is not a grant.
func (s Service) MentorPage(ctx context.Context, actor domain.Actor, q repository.MessageListQuery) (MentorMessagePage, error) {
	b := domain.SelectedMentorBinding(ctx)
	if err := actor.Validate(); err != nil {
		return MentorMessagePage{}, err
	}
	if b == nil || q.Limit < 1 || q.Limit > 30 || q.Before != nil && q.After != nil || q.Before != nil && q.Before.ID != *b || q.After != nil && q.After.ID != *b {
		return MentorMessagePage{}, domain.ErrValidation
	}
	ctx, _, err := s.mentorStart(ctx, actor.UserID, q.DialogID)
	if err != nil {
		return MentorMessagePage{}, err
	}
	_, _, member, err := s.loadReadableContext(ctx, actor, q.DialogID)
	if err != nil {
		return MentorMessagePage{}, err
	}
	q.FromSequence = member.HistoryFromMessageSequence
	limit := q.Limit
	q.Limit++
	rows, err := s.Messages.List(ctx, q)
	if err != nil {
		return MentorMessagePage{}, err
	}
	if len(rows) > q.Limit {
		return MentorMessagePage{}, domain.ErrDependencyUnavailable
	}
	page := MentorMessagePage{HasMore: len(rows) > limit}
	if page.HasMore {
		if q.After != nil {
			rows = rows[:limit]
		} else {
			rows = rows[len(rows)-limit:]
		}
	}
	if page.HasMore && len(rows) > 0 {
		m := rows[0]
		if q.After != nil {
			m = rows[len(rows)-1]
		}
		page.Next = &repository.MessageCursor{ID: *b, Sequence: m.MessageSequence}
	}
	views, err := s.views(ctx, rows)
	if err != nil {
		return MentorMessagePage{}, err
	}
	page.Items, err = s.mentorViews(ctx, actor.UserID, q.DialogID, views)
	if err != nil {
		return MentorMessagePage{}, err
	}
	return page, nil
}
func (s Service) MentorChanges(ctx context.Context, actor domain.Actor, q repository.MessageChangeQuery) (MentorMessagePage, error) {
	if err := actor.Validate(); err != nil {
		return MentorMessagePage{}, err
	}
	if domain.SelectedMentorBinding(ctx) == nil || q.Limit < 1 || q.Limit > 30 || q.AfterEventSequence < 0 {
		return MentorMessagePage{}, domain.ErrValidation
	}
	ctx, _, err := s.mentorStart(ctx, actor.UserID, q.DialogID)
	if err != nil {
		return MentorMessagePage{}, err
	}
	_, _, member, err := s.loadReadableContext(ctx, actor, q.DialogID)
	if err != nil {
		return MentorMessagePage{}, err
	}
	q.FromSequence = member.HistoryFromMessageSequence
	limit := q.Limit
	q.Limit++
	rows, err := s.Messages.ListChanges(ctx, q)
	if err != nil {
		return MentorMessagePage{}, err
	}
	if len(rows) > q.Limit {
		return MentorMessagePage{}, domain.ErrDependencyUnavailable
	}
	page := MentorMessagePage{HasMore: len(rows) > limit, NextAfter: q.AfterEventSequence}
	if page.HasMore {
		rows = rows[:limit]
	}
	for _, m := range rows {
		page.NextAfter = max(page.NextAfter, m.LastEventSequence)
	}
	views, err := s.views(ctx, rows)
	if err != nil {
		return MentorMessagePage{}, err
	}
	page.Items, err = s.mentorViews(ctx, actor.UserID, q.DialogID, views)
	if err != nil {
		return MentorMessagePage{}, err
	}
	return page, nil
}
