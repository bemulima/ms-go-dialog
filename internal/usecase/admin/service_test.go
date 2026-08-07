package admin

import (
	"errors"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
	"testing"
)

func TestAdminAndModeratorBoundaries(t *testing.T) {
	user := domain.Actor{UserID: uuid.New(), Role: "USER"}
	moderator := domain.Actor{UserID: uuid.New(), Role: "MODERATOR"}
	admin := domain.Actor{UserID: uuid.New(), Role: "ADMIN"}
	if !errors.Is(requireAdmin(user), domain.ErrForbidden) || !errors.Is(requireAdmin(moderator), domain.ErrForbidden) || requireAdmin(admin) != nil {
		t.Fatal("admin boundary mismatch")
	}
	if !errors.Is(requireModerator(user), domain.ErrForbidden) || requireModerator(moderator) != nil || requireModerator(admin) != nil {
		t.Fatal("moderator boundary mismatch")
	}
}
