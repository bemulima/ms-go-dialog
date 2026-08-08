package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bemulima/ms-go-dialog/internal/adapters/http/middleware"
	"github.com/google/uuid"
)

func TestRequireActor_RejectsGuestAndAcceptsVerifiedUser(t *testing.T) {
	writeError := func(w http.ResponseWriter, _ *http.Request, _ error) { w.WriteHeader(http.StatusUnauthorized) }
	handler := middleware.RequireActor(writeError)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if actor := middleware.Actor(r); actor.UserID == uuid.Nil {
			t.Fatal("actor was not stored in context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-User-ID", uuid.NewString())
	request.Header.Set("X-User-Role", "GUEST")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("guest status=%d", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-User-ID", uuid.NewString())
	request.Header.Set("X-User-Role", "STUDENT")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("authenticated status=%d", recorder.Code)
	}
}

func TestAssignRequestID_PreservesTrustedUUIDAndReplacesInvalidValue(t *testing.T) {
	handler := middleware.AssignRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Observed-Request-ID", middleware.RequestID(r))
		w.WriteHeader(http.StatusNoContent)
	}))

	trusted := uuid.NewString()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", trusted)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("X-Request-ID") != trusted || response.Header().Get("Observed-Request-ID") != trusted {
		t.Fatalf("trusted request ID was not preserved: %v", response.Header())
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "untrusted-value")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	replacement := response.Header().Get("X-Request-ID")
	if replacement == "untrusted-value" || uuid.Validate(replacement) != nil {
		t.Fatalf("invalid request ID was not replaced: %q", replacement)
	}
}
