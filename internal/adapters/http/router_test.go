package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	httpadapter "github.com/bemulima/ms-go-dialog/internal/adapters/http"
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
