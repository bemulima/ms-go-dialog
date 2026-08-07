package domain

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Actor struct {
	UserID uuid.UUID
	Role   string
}

func (a Actor) Validate() error {
	role := strings.ToUpper(strings.TrimSpace(a.Role))
	if a.UserID == uuid.Nil || role == "" || role == "GUEST" {
		return fmt.Errorf("%w: a non-guest UUID actor is required", ErrAuthentication)
	}
	return nil
}

func (a Actor) IsAdmin() bool {
	return strings.EqualFold(a.Role, "ADMIN")
}

func (a Actor) CanModerate() bool {
	return a.IsAdmin() || strings.EqualFold(a.Role, "MODERATOR")
}
