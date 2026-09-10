package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestMetricsRecordsRoutePatternWithoutUserLabels(t *testing.T) {
	metrics := NewMetrics()
	router := chi.NewRouter()
	router.Use(metrics.HTTPMiddleware)
	router.Get("/items/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items/secret-user-id", nil))

	response := httptest.NewRecorder()
	metrics.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := response.Body.String()
	if !strings.Contains(body, `route="/items/{id}"`) || strings.Contains(body, "secret-user-id") {
		t.Fatalf("unexpected metrics: %s", body)
	}
}

func TestMetricsRejectsInvalidCounterName(t *testing.T) {
	metrics := NewMetrics()
	metrics.Increment("invalid metric\nleak", 1)

	response := httptest.NewRecorder()
	metrics.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if strings.Contains(response.Body.String(), "invalid metric") {
		t.Fatalf("invalid metric name was exposed: %s", response.Body.String())
	}
}

func TestMetricsIncludesTeacherRequestedV2CounterWithoutLabels(t *testing.T) {
	metrics := NewMetrics()
	metrics.Increment(TeacherRequestedV2Total, 1)

	response := httptest.NewRecorder()
	metrics.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(response.Body.String(), TeacherRequestedV2Total+" 1") || strings.Contains(response.Body.String(), TeacherRequestedV2Total+"{") {
		t.Fatalf("unexpected teacher requested v2 metrics: %s", response.Body.String())
	}
}
