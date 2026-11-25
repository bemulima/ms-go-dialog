package dialog

import (
	"bytes"
	"context"
	"errors"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

const MentorMCPPageLimit = 20

type MentorMCPDialog struct {
	DialogID  uuid.UUID                `json:"dialog_id"`
	BindingID uuid.UUID                `json:"binding_id"`
	Kind      domain.MentorContextKind `json:"kind"`
	SourceID  uuid.UUID                `json:"source_id"`
}

type mentorMCPBindingReader interface {
	ListOwnedMentorBindings(context.Context, uuid.UUID, uuid.UUID, int) ([]domain.MentorBinding, error)
}

// MentorMCPDialogs enumerates only existing typed bindings. Each visible
// binding passes the same current consent, membership and block fences as Get.
func (s *MentorEducationService) MentorMCPDialogs(ctx context.Context, mentor, after uuid.UUID, limit int) ([]MentorMCPDialog, uuid.UUID, error) {
	if err := s.ready(); err != nil {
		return nil, uuid.Nil, err
	}
	if mentor == uuid.Nil || limit < 1 || limit > MentorMCPPageLimit {
		return nil, uuid.Nil, domain.ErrValidation
	}
	bindings, ok := s.Bindings.(mentorMCPBindingReader)
	if !ok || s.Dialogs.Education == nil {
		return nil, uuid.Nil, domain.ErrDependencyUnavailable
	}
	rows, err := bindings.ListOwnedMentorBindings(ctx, mentor, after, limit+1)
	if err != nil {
		return nil, uuid.Nil, err
	}
	if len(rows) > limit+1 {
		return nil, uuid.Nil, domain.ErrDependencyUnavailable
	}
	more := len(rows) > limit
	items := make([]MentorMCPDialog, 0, min(len(rows), limit))
	for _, b := range rows[:min(len(rows), limit)] {
		if !b.Valid() || b.MentorID != mentor || bytes.Compare(b.ID[:], after[:]) <= 0 {
			return nil, uuid.Nil, domain.ErrDependencyUnavailable
		}
		after = b.ID
		readCtx := domain.WithMentorBinding(ctx, b.ID)
		actor := domain.Actor{UserID: mentor, Role: "MENTOR"}
		if _, err = s.Dialogs.Get(readCtx, actor, b.DialogID); err != nil {
			if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrBlocked) || errors.Is(err, domain.ErrDialogNotFound) || errors.Is(err, domain.ErrNotFound) {
				continue
			}
			return nil, uuid.Nil, err
		}
		items = append(items, MentorMCPDialog{DialogID: b.DialogID, BindingID: b.ID, Kind: b.Kind, SourceID: b.SourceID})
	}
	if !more {
		after = uuid.Nil
	}
	return items, after, nil
}

// MentorMCPBinding checks the Mentor side of an existing binding. The message
// use case must still check current context and author access on every page.
func (s *MentorEducationService) MentorMCPBinding(ctx context.Context, mentor, dialog, binding uuid.UUID) error {
	if err := s.ready(); err != nil {
		return err
	}
	if mentor == uuid.Nil || dialog == uuid.Nil || binding == uuid.Nil {
		return domain.ErrValidation
	}
	row, err := s.Bindings.Get(ctx, binding)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if !row.Valid() || row.DialogID != dialog || row.MentorID != mentor || row.ID != binding {
		return domain.ErrDialogNotFound
	}
	return nil
}
