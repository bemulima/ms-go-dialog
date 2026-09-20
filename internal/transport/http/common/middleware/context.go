package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type actorKey struct{}
type requestIDKey struct{}

type ErrorWriter func(http.ResponseWriter, *http.Request, error)

func RequireActor(writeError ErrorWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := uuid.Parse(strings.TrimSpace(r.Header.Get("X-User-ID")))
			actor := domain.Actor{UserID: userID, Role: strings.TrimSpace(r.Header.Get("X-User-Role"))}
			if err != nil || actor.Validate() != nil {
				writeError(w, r, domain.ErrAuthentication)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, actor)))
		})
	}
}

func Actor(r *http.Request) domain.Actor {
	actor, _ := r.Context().Value(actorKey{}).(domain.Actor)
	return actor
}

func RequestID(r *http.Request) string {
	requestID, _ := r.Context().Value(requestIDKey{}).(string)
	return requestID
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}
