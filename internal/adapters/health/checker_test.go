package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckerReady(t *testing.T) {
	checker := NewChecker(time.Second, map[string]Check{
		"postgres": func(context.Context) error { return nil },
	})
	response := httptest.NewRecorder()
	checker.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"postgres":"ok"`) {
		t.Fatalf("unexpected readiness response: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCheckerDoesNotExposeDependencyError(t *testing.T) {
	checker := NewChecker(time.Second, map[string]Check{
		"nats": func(context.Context) error { return errors.New("secret-host:4222 rejected credentials") },
	})
	response := httptest.NewRecorder()
	checker.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"nats":"failed"`) {
		t.Fatalf("unexpected readiness response: status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret-host") {
		t.Fatalf("dependency error leaked: %s", response.Body.String())
	}
}

func TestCheckerHonorsTimeout(t *testing.T) {
	checker := NewChecker(10*time.Millisecond, map[string]Check{
		"postgres": func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	started := time.Now()
	status, checks := checker.Run(context.Background())
	if status != "unavailable" || checks["postgres"] != "failed" {
		t.Fatalf("unexpected result: status=%s checks=%v", status, checks)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("readiness timeout was not honored: %s", elapsed)
	}
}
