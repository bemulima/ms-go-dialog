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
