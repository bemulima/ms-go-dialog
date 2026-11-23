package dialog

import (
	"bytes"
	"context"
	"fmt"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

// MentorContactService provisions ordinary personal conversations only. The
// purpose-authenticated Mentor caller owns approved Offer selection; no input
// here grants access to Course progress, submissions, or an assignment.
type MentorContactService struct {
	Dialogs  *Service
	Enabled  bool
	SpaceKey string
}

type MentorContactInput struct {
	StudentID uuid.UUID
	MentorID  uuid.UUID
}

type MentorContact struct {
	DialogID      uuid.UUID
	DialogVersion int
	StudentID     uuid.UUID
	MentorID      uuid.UUID
}

func (s MentorContactService) Ensure(ctx context.Context, in MentorContactInput) (MentorContact, error) {
	if !s.Enabled {
		return MentorContact{}, domain.ErrFeatureDisabled
	}
	if in.StudentID == uuid.Nil || in.MentorID == uuid.Nil || in.StudentID == in.MentorID {
		return MentorContact{}, domain.ErrValidation
	}
	if err := ctx.Err(); err != nil {
		return MentorContact{}, err
	}
	d := s.Dialogs
	if d == nil || d.Spaces == nil || d.Dialogs == nil || d.Members == nil || d.Blocks == nil || d.Participants == nil || d.Tx == nil || d.Outbox == nil || s.SpaceKey == "" {
		return MentorContact{}, domain.ErrDependencyUnavailable
	}
	if err := d.Participants.RequireActiveUsers(ctx, []uuid.UUID{in.StudentID, in.MentorID}); err != nil {
		return MentorContact{}, err
	}
	localCurrent := func(checkCtx context.Context) error {
		if err := checkCtx.Err(); err != nil {
			return err
		}
		space, err := d.activeSpace(checkCtx, s.SpaceKey)
		if err != nil {
			return err
		}
		if !space.Policy.AllowPersonal {
			return domain.ErrForbidden
		}
		blocked, err := d.Blocks.ExistsEitherDirection(checkCtx, in.StudentID, in.MentorID)
		if err != nil {
			return err
		}
		if blocked {
			return domain.ErrBlocked
		}
		return nil
	}
	if err := localCurrent(ctx); err != nil {
		return MentorContact{}, err
	}
	result, err := d.ensurePersonal(ctx, domain.Actor{UserID: in.StudentID, Role: "STUDENT"}, EnsurePersonalInput{SpaceKey: s.SpaceKey, ParticipantID: in.MentorID}, localCurrent)
	if err != nil {
		return MentorContact{}, err
	}
	// External participant availability is deliberately checked outside the
	// local transaction. A late denial suppresses the reply; it does not erase a
	// conversation that may already have committed. Pair retry is idempotent.
	if err := d.Participants.RequireActiveUsers(ctx, []uuid.UUID{in.StudentID, in.MentorID}); err != nil {
		return MentorContact{}, err
	}
	if err := localCurrent(ctx); err != nil {
		return MentorContact{}, err
	}
	view, err := d.Get(ctx, domain.Actor{UserID: in.StudentID, Role: "STUDENT"}, result.View.Dialog.ID)
	if err != nil {
		return MentorContact{}, err
	}
	key, _ := domain.BuildPersonalKey(in.StudentID, in.MentorID)
	if view.Dialog.Type != domain.DialogTypePersonal || view.Dialog.Status != domain.DialogStatusActive ||
		view.Dialog.SpaceID != view.Space.ID || view.Space.Key != s.SpaceKey || !view.Space.Policy.AllowPersonal ||
		view.Dialog.Version < 1 || view.Dialog.Version > 2147483647 || view.Dialog.MemberCount != 2 || len(view.Members) != 2 ||
		!bytes.Equal(view.Dialog.PersonalKey, key[:]) || view.Dialog.StudentID != uuid.Nil || view.Dialog.PersonalTeacherID != uuid.Nil ||
		view.Dialog.TeacherContextType != "" || view.Dialog.ContextID != nil {
		return MentorContact{}, fmt.Errorf("%w: conversation is not the exact active personal pair", domain.ErrForbidden)
	}
	seen := map[uuid.UUID]bool{}
	for _, member := range view.Members {
		if member.DialogID != view.Dialog.ID || member.Status != domain.MemberStatusActive ||
			(member.UserID != in.StudentID && member.UserID != in.MentorID) || seen[member.UserID] {
			return MentorContact{}, domain.ErrForbidden
		}
		seen[member.UserID] = true
	}
	if err := localCurrent(ctx); err != nil {
		return MentorContact{}, err
	}
	return MentorContact{DialogID: view.Dialog.ID, DialogVersion: view.Dialog.Version, StudentID: in.StudentID, MentorID: in.MentorID}, nil
}
