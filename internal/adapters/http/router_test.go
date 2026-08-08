package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/adapters/health"
	httpadapter "github.com/bemulima/ms-go-dialog/internal/adapters/http"
	"github.com/bemulima/ms-go-dialog/internal/adapters/observability"
)

func TestRouter_HealthAndUnknownRoute(t *testing.T) {
	router := httpadapter.NewRouter(httpadapter.RouterDependencies{})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("X-Request-ID") == "" {
		t.Fatalf("health response mismatch: status=%d headers=%v", recorder.Code, recorder.Header())
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown route status=%d", recorder.Code)
	}
}

func TestRouter_ReadinessAndMetrics(t *testing.T) {
	metrics := observability.NewMetrics()
	readiness := health.NewChecker(time.Second, map[string]health.Check{
		"postgres": func(context.Context) error { return nil },
	})
	router := httpadapter.NewRouter(httpadapter.RouterDependencies{Readiness: readiness, Metrics: metrics})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"postgres":"ok"`) {
		t.Fatalf("readiness response mismatch: status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `route="/readyz"`) {
		t.Fatalf("metrics response mismatch: status=%d body=%s", response.Code, response.Body.String())
	}
}
