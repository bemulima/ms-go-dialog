package realtime

import (
	"context"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

// Generic fan-out never carries an educational capability. Even old queued
// attachment/moderation events are checked against the durable message binding.
type PublicVisibility struct {
	Bindings repository.MentorRepository
	Messages repository.MessageRepository
	Dialogs  repository.DialogRepository
	Spaces   repository.SpaceRepository
}

func (v PublicVisibility) AllowPublic(ctx context.Context, dialog uuid.UUID, message *uuid.UUID) (bool, error) {
	if v.Bindings == nil || v.Messages == nil {
		return false, domain.ErrDependencyUnavailable
	}
	protected, err := v.Bindings.Protected(ctx, dialog)
	if err != nil || protected {
		return false, err
	}
	if message != nil {
		m, err := v.Messages.GetByID(ctx, *message)
		if err != nil {
			return false, err
		}
		if m.DialogID != dialog || m.MentorBindingID != nil {
			return false, nil
		}
	}
	return true, ctx.Err()
}

// TeacherPreviewPolicy verifies the authoritative owner and source before any
// generated preview can use a dialog's public realtime transport.
func (v PublicVisibility) TeacherPreviewPolicy(ctx context.Context, dialogID, studentID, messageID uuid.UUID, sequence int64) (domain.Policy, error) {
	if v.Dialogs == nil || v.Spaces == nil {
		return domain.Policy{}, domain.ErrDependencyUnavailable
	}
	allowed, err := v.AllowPublic(ctx, dialogID, nil)
	if err != nil {
		return domain.Policy{}, err
	}
	if !allowed {
		return domain.Policy{}, domain.ErrForbidden
	}
	dialog, err := v.Dialogs.GetByID(ctx, dialogID)
	if err != nil || dialog.Type != domain.DialogTypeTeacher || dialog.Status != domain.DialogStatusActive ||
		dialog.StudentID != studentID || dialog.TeacherContextType != domain.TeacherContextGeneralTeacher {
		return domain.Policy{}, domain.ErrForbidden
	}
	source, err := v.Messages.GetByID(ctx, messageID)
	if err != nil || source.DialogID != dialogID || source.MentorBindingID != nil ||
		source.Status != domain.MessageStatusActive || source.AuthorType != domain.MessageAuthorUser ||
		source.SenderID != studentID || source.MessageSequence != sequence || source.Channel != domain.MessageChannelWeb {
		return domain.Policy{}, domain.ErrForbidden
	}
	space, err := v.Spaces.GetByID(ctx, dialog.SpaceID)
	if err != nil || space.Status != domain.SpaceStatusActive {
		return domain.Policy{}, domain.ErrForbidden
	}
	return space.Policy, ctx.Err()
}
