package middleware_test

import (
	"github.com/bemulima/ms-go-dialog/internal/adapters/http/middleware"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestActorRateLimiterEnforcesBurst(t *testing.T) {
	limiter, err := middleware.NewActorRateLimiter(1, 2, 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	actor := uuid.New()
	if !limiter.Allow(actor) || !limiter.Allow(actor) || limiter.Allow(actor) {
		t.Fatal("burst was not enforced")
	}
}
