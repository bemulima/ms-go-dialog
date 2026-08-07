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
