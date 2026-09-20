package user

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

func TestClientRequireActiveUsersSendsProducerContract(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != resolveActiveUsersPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("X-Internal-Token"); got != "secret" {
			t.Errorf("internal token=%q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type=%q", got)
		}
		var request resolveRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		if len(request.UserIDs) != 2 || request.UserIDs[0] != first || request.UserIDs[1] != second {
			t.Errorf("user ids=%v", request.UserIDs)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"active_user_ids":["` + first.String() + `","` + second.String() + `"],"unavailable_user_ids":[]}}`))
	}))
	defer server.Close()

	err := (Client{BaseURL: server.URL + "/", InternalToken: " secret ", HTTPClient: server.Client()}).RequireActiveUsers(context.Background(), []uuid.UUID{first, second})
	if err != nil {
		t.Fatalf("resolve active users: %v", err)
	}
}

func TestClientRequireActiveUsersMapsUnavailableParticipant(t *testing.T) {
	active, unavailable := uuid.New(), uuid.New()
	server := responseServer(http.StatusOK, `{"data":{"active_user_ids":["`+active.String()+`"],"unavailable_user_ids":["`+unavailable.String()+`"]}}`)
	defer server.Close()

	err := (Client{BaseURL: server.URL, InternalToken: "secret", HTTPClient: server.Client()}).RequireActiveUsers(context.Background(), []uuid.UUID{active, unavailable})
	if !errors.Is(err, domain.ErrParticipantUnavailable) {
		t.Fatalf("error=%v, want participant unavailable", err)
	}
}

func TestClientRequireActiveUsersFailsClosedForDependencyProblems(t *testing.T) {
	requested, unknown := uuid.New(), uuid.New()
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "non success status", status: http.StatusInternalServerError, body: `{"error":"internal"}`},
		{name: "missing partition member", status: http.StatusOK, body: `{"data":{"active_user_ids":[],"unavailable_user_ids":[]}}`},
		{name: "unknown member", status: http.StatusOK, body: `{"data":{"active_user_ids":["` + unknown.String() + `"],"unavailable_user_ids":[]}}`},
		{name: "duplicate member", status: http.StatusOK, body: `{"data":{"active_user_ids":["` + requested.String() + `"],"unavailable_user_ids":["` + requested.String() + `"]}}`},
		{name: "unknown field", status: http.StatusOK, body: `{"data":{"active_user_ids":["` + requested.String() + `"],"unavailable_user_ids":[],"extra":true}}`},
		{name: "trailing json", status: http.StatusOK, body: `{"data":{"active_user_ids":["` + requested.String() + `"],"unavailable_user_ids":[]}} {}`},
		{name: "oversized response", status: http.StatusOK, body: strings.Repeat(" ", maxResponseBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := responseServer(test.status, test.body)
			defer server.Close()
			err := (Client{BaseURL: server.URL, InternalToken: "secret", HTTPClient: server.Client()}).RequireActiveUsers(context.Background(), []uuid.UUID{requested})
			if !errors.Is(err, domain.ErrDependencyUnavailable) {
				t.Fatalf("error=%v, want dependency unavailable", err)
			}
		})
	}
}

func TestClientRequireActiveUsersRejectsInvalidInputWithoutRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, InternalToken: "secret", HTTPClient: server.Client()}
	id := uuid.New()

	for _, ids := range [][]uuid.UUID{nil, {uuid.Nil}, {id, id}} {
		if err := client.RequireActiveUsers(context.Background(), ids); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("ids=%v error=%v, want validation", ids, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid input sent %d requests", requests.Load())
	}
}

func TestClientRequireActiveUsersRejectsInvalidConfiguration(t *testing.T) {
	err := (Client{BaseURL: "not-a-url", InternalToken: "secret"}).RequireActiveUsers(context.Background(), []uuid.UUID{uuid.New()})
	if !errors.Is(err, domain.ErrDependencyUnavailable) {
		t.Fatalf("error=%v, want dependency unavailable", err)
	}
}

func responseServer(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}
