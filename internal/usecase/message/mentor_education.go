package message

import (
	"context"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

func (s Service) mentorStart(ctx context.Context, user, dialog uuid.UUID) (context.Context, *domain.MentorAccess, error) {
	a, err := domain.CheckMentorRead(ctx, s.Education, user, dialog, nil)
	if err != nil {
		return ctx, nil, err
	}
	return domain.WithMentorAccess(ctx, a), a, nil
}
func (s Service) mentorViews(ctx context.Context, user, dialog uuid.UUID, items []View) ([]View, error) {
	before := domain.CurrentMentorAccess(ctx)
	authors := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for _, v := range items {
		if v.Message.MentorBindingID != nil && !seen[v.Message.SenderID] {
			seen[v.Message.SenderID] = true
			authors = append(authors, v.Message.SenderID)
		}
	}
	if len(authors) > 32 {
		return nil, domain.ErrValidation
	}
	current, err := domain.CheckMentorRead(ctx, s.Education, user, dialog, authors)
	if err != nil {
		return nil, err
	}
	if (before == nil) != (current == nil) || before != nil && !before.SameCurrent(*current) {
		return nil, domain.ErrForbidden
	}
	result := make([]View, 0, len(items))
	parents := make(map[uuid.UUID]domain.Message)
	for _, v := range items {
		if !current.Allows(v.Message) {
			continue
		}
		if v.Message.Status == domain.MessageStatusHidden {
			v.Message.Body = ""
			v.Message.Links = nil
			v.Message.LessonContext = nil
			v.Message.ContentAnchor = nil
			v.Message.AssistantUI = nil
			v.Attachments = nil
			v.Message.ReplyToMessageID = nil
		}
		if current != nil && v.Message.ReplyToMessageID != nil {
			parent, err := s.Messages.GetByID(ctx, *v.Message.ReplyToMessageID)
			if err != nil && err != domain.ErrNotFound {
				return nil, err
			}
			// Do not disclose an inaccessible parent's UUID through an otherwise
			// visible child. Unknown authors are omitted from this bounded page.
			if err != nil || parent.Status == domain.MessageStatusHidden || !current.Allows(parent) {
				v.Message.ReplyToMessageID = nil
			} else {
				parents[parent.ID] = parent
			}
		}
		result = append(result, v)
	}
	if current != nil {
		fresh, err := domain.CheckMentorRead(ctx, s.Education, user, dialog, authors)
		if err != nil {
			return nil, err
		}
		if fresh == nil || !current.SameCurrent(*fresh) {
			return nil, domain.ErrForbidden
		}
		for i, v := range result {
			if !fresh.Allows(v.Message) {
				return nil, domain.ErrForbidden
			}
			if v.Message.ReplyToMessageID != nil {
				parent, found := parents[*v.Message.ReplyToMessageID]
				if !found || !fresh.Allows(parent) {
					result[i].Message.ReplyToMessageID = nil
				}
			}
		}
	}
	return result, nil
}
func (s Service) mentorMessage(ctx context.Context, user uuid.UUID, item domain.Message) (context.Context, error) {
	next, _, err := s.mentorStart(ctx, user, item.DialogID)
	if err != nil {
		return ctx, err
	}
	views, err := s.mentorViews(next, user, item.DialogID, []View{{Message: item}})
	if err != nil {
		return ctx, err
	}
	if len(views) != 1 {
		return ctx, domain.ErrMessageNotFound
	}
	return next, nil
}
func (s Service) publicReadProjection(ctx context.Context, item domain.Dialog, member domain.Member) (domain.Dialog, domain.Member, error) {
	if domain.SelectedMentorBinding(ctx) != nil {
		member.UnreadCount = 0
		member.LastReadMessageSequence = 0
		member.LastReadAt = nil
		return item, member, nil
	}
	if s.Education == nil {
		return item, member, nil
	}
	reader, ok := s.Dialogs.(repository.PublicDialogProjectionReader)
	if !ok {
		return item, member, domain.ErrDependencyUnavailable
	}
	p, err := reader.PublicProjection(ctx, item.ID, member.UserID, member.LastReadMessageSequence)
	if err != nil {
		return item, member, err
	}
	item.MessageCount, item.MaxMessageSequence, item.MaxEventSequence = p.MessageCount, p.MaxMessageSequence, p.MaxEventSequence
	item.LastMessageID, item.LastMessageAt = p.LastMessageID, p.LastMessageAt
	member.UnreadCount = p.UnreadCount
	return item, member, nil
}

// Educational read cursors belong to the exact current enrollment/consent.
// They never change personal-history unread state or publish unguarded WS data.
func (s Service) mentorAdvanceRead(ctx context.Context, actor domain.Actor, id uuid.UUID, requested *int64) (ReadState, error) {
	ctx, access, err := s.mentorStart(ctx, actor.UserID, id)
	if err != nil {
		return ReadState{}, err
	}
	if access == nil || s.MentorReads == nil {
		return ReadState{}, domain.ErrFeatureDisabled
	}
	if _, _, _, err = s.loadReadableContext(ctx, actor, id); err != nil {
		return ReadState{}, err
	}
	var through int64
	if requested == nil {
		latest, err := s.Window(ctx, actor, id, 30, 0)
		if err != nil {
			return ReadState{}, err
		}
		for _, v := range latest.Items {
			through = max(through, v.Message.MessageSequence)
		}
	} else {
		through = *requested
		if through < 0 {
			return ReadState{}, domain.ErrInvalidReadSequence
		}
		if through > 0 {
			rows, err := s.Messages.List(ctx, repository.MessageListQuery{DialogID: id, FromSequence: 1, After: &repository.MessageCursor{Sequence: through - 1, ID: access.Binding.ID}, Limit: 1})
			if err != nil {
				return ReadState{}, err
			}
			views, err := s.views(ctx, rows)
			if err != nil {
				return ReadState{}, err
			}
			views, err = s.mentorViews(ctx, actor.UserID, id, views)
			if err != nil {
				return ReadState{}, err
			}
			if len(views) != 1 || views[0].Message.MessageSequence != through {
				return ReadState{}, domain.ErrInvalidReadSequence
			}
		}
	}
	if _, err = s.mentorViews(ctx, actor.UserID, id, nil); err != nil {
		return ReadState{}, err
	}
	if err = s.MentorReads.AdvanceRead(ctx, access.Binding.ID, actor.UserID, access.MembershipID, through); err != nil {
		return ReadState{}, err
	}
	if _, err = s.mentorViews(ctx, actor.UserID, id, nil); err != nil {
		return ReadState{}, err
	}
	stored, err := s.MentorReads.ReadSequence(ctx, access.Binding.ID, actor.UserID, access.MembershipID)
	if err != nil {
		return ReadState{}, err
	}
	if _, err = s.mentorViews(ctx, actor.UserID, id, nil); err != nil {
		return ReadState{}, err
	}
	return ReadState{LastReadMessageSequence: stored, MaxMessageSequence: max(stored, through)}, nil
}
