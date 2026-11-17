package http

import (
	adminuc "github.com/bemulima/ms-go-dialog/internal/usecase/admin"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	reportuc "github.com/bemulima/ms-go-dialog/internal/usecase/report"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMentorEducationHTTPRejectsAmbiguousCommands(t *testing.T) {
	router := NewRouter(RouterDependencies{MentorEducationService: &dialoguc.MentorEducationService{}, ReportService: &reportuc.Service{}, AdminService: &adminuc.Service{}})
	ensure := `{"kind":"ASSIGNMENT","id":"11111111-1111-4111-8111-111111111111"}`
	report := `{"request_id":"11111111-1111-4111-8111-111111111111","message_id":"22222222-2222-4222-8222-222222222222","category":"OTHER","details":"Report details"}`
	for _, test := range []struct {
		name, path, body string
		status           int
		mutate           func(*http.Request)
	}{
		{"disabled ensure", "/api/v1/dialogs/mentor-context", ensure, 409, nil}, {"duplicate field", "/api/v1/dialogs/mentor-context", strings.Replace(ensure, `"kind":"ASSIGNMENT"`, `"kind":"ASSIGNMENT","kind":"COHORT"`, 1), 400, nil},
		{"case alias", "/api/v1/dialogs/mentor-context", strings.Replace(ensure, `"kind"`, `"Kind"`, 1), 400, nil},
		{"selector is not authority", "/api/v1/dialogs/mentor-context", ensure, 400, func(r *http.Request) { r.Header.Set("X-Mentor-Dialog-Context", uuid.NewString()) }},
		{"disabled report", "/api/v1/message-report", report, 409, nil},
		{"report key missing", "/api/v1/message-report", report, 400, func(r *http.Request) { r.Header.Del("Idempotency-Key") }},
		{"report duplicate key", "/api/v1/message-report", report, 400, func(r *http.Request) { r.Header.Add("Idempotency-Key", "11111111-1111-4111-8111-111111111111") }},
		{"report actor field", "/api/v1/message-report", strings.Replace(report, `"category":"OTHER"`, `"category":"OTHER","actor_id":"33333333-3333-4333-8333-333333333333"`, 1), 400, nil},
		{"query rejected", "/api/v1/message-report?ignored=1", report, 400, nil},
		{"encoding rejected", "/api/v1/message-report", report, 400, func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-User-ID", uuid.NewString())
			r.Header.Set("X-User-Role", "STUDENT")
			r.Header.Set("Idempotency-Key", "11111111-1111-4111-8111-111111111111")
			if test.mutate != nil {
				test.mutate(r)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("status=%d %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("cacheable private result")
			}
		})
	}
	r := httptest.NewRequest("GET", "/admin/v1/message-report/list", nil)
	r.Header.Set("X-User-ID", uuid.NewString())
	r.Header.Set("X-User-Role", "STUDENT")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("student queue=%d", w.Code)
	}
}
