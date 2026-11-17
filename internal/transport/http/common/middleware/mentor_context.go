package middleware

import (
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
	"net/http"
)

// The header is a selector only. Every use case obtains current owner proof;
// neither the selector nor a gateway role can grant educational access.
func MentorContext(writeError ErrorWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			values := r.Header.Values("X-Mentor-Dialog-Context")
			if len(values) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			if len(values) != 1 {
				writeError(w, r, domain.ErrValidation)
				return
			}
			id, err := uuid.Parse(values[0])
			if err != nil || id == uuid.Nil || id.String() != values[0] {
				writeError(w, r, domain.ErrValidation)
				return
			}
			w.Header().Set("Cache-Control", "private, no-store")
			next.ServeHTTP(w, r.WithContext(domain.WithMentorBinding(r.Context(), id)))
		})
	}
}
