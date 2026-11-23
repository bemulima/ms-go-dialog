package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	"github.com/google/uuid"
)

type contactEnsureStub struct {
	calls  int
	input  dialoguc.MentorContactInput
	change func(*dialoguc.MentorContact)
	err    error
}

func (s *contactEnsureStub) Ensure(_ context.Context, in dialoguc.MentorContactInput) (dialoguc.MentorContact, error) {
	s.calls++
	s.input = in
	result := dialoguc.MentorContact{DialogID: uuid.MustParse("33333333-3333-4333-8333-333333333333"), DialogVersion: 3, StudentID: in.StudentID, MentorID: in.MentorID}
	if s.change != nil {
		s.change(&result)
	}
	return result, s.err
}

type contactLimiter struct {
	allowed bool
	subject uuid.UUID
}

func (s *contactLimiter) Allow(id uuid.UUID) bool { s.subject = id; return s.allowed }

const contactBody = `{"schema":"mentor-contact.ensure.v1","student_id":"11111111-1111-4111-8111-111111111111","mentor_id":"22222222-2222-4222-8222-222222222222"}`

func TestMentorContactHTTPExactReplyAndSubject(t *testing.T) {
	s := &contactEnsureStub{}
	limiter := &contactLimiter{allowed: true}
	h := MentorContactHandler{Service: s, Limiter: limiter}
	r := httptest.NewRequest(http.MethodPost, "/internal/v1/mentor-dialog/ensure-personal", strings.NewReader(contactBody))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-User-ID", uuid.NewString())
	r.Header.Set("X-User-Role", "ADMIN")
	w := httptest.NewRecorder()
	h.Ensure(w, r)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || s.calls != 1 || limiter.subject != s.input.StudentID {
		t.Fatalf("reply=%d %s", w.Code, w.Body.String())
	}
	var result map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	data := result["data"]
	if len(result) != 1 || len(data) != 5 || data["schema"] != "mentor-contact.dialog.v1" || data["student_id"] != s.input.StudentID.String() || data["mentor_id"] != s.input.MentorID.String() || data["dialog_version"] != float64(3) {
		t.Fatalf("open response: %s", w.Body.String())
	}
}

func TestMentorContactHTTPRejectsUntrustedShapeBeforeEnsure(t *testing.T) {
	cases := map[string]string{
		"unknown role":      strings.TrimSuffix(contactBody, "}") + `,"role":"ADMIN"}`,
		"unknown consent":   strings.TrimSuffix(contactBody, "}") + `,"consent":true}`,
		"unknown space":     strings.TrimSuffix(contactBody, "}") + `,"space_key":"private"}`,
		"duplicate":         strings.TrimSuffix(contactBody, "}") + `,"student_id":"11111111-1111-4111-8111-111111111111"}`,
		"escaped duplicate": strings.TrimSuffix(contactBody, "}") + `,"student\u005fid":"11111111-1111-4111-8111-111111111111"}`,
		"null":              strings.Replace(contactBody, `"mentor-contact.ensure.v1"`, `null`, 1),
		"array":             `[]`, "nested": strings.Replace(contactBody, `"mentor-contact.ensure.v1"`, `{}`, 1),
		"trailing": contactBody + `{}`, "missing": `{"schema":"mentor-contact.ensure.v1"}`,
		"nil uuid":          strings.Replace(contactBody, "11111111-1111-4111-8111-111111111111", "00000000-0000-0000-0000-000000000000", 1),
		"same pair":         strings.Replace(contactBody, "22222222-2222-4222-8222-222222222222", "11111111-1111-4111-8111-111111111111", 1),
		"noncanonical uuid": strings.Replace(contactBody, "11111111-1111-4111-8111-111111111111", "{11111111-1111-4111-8111-111111111111}", 1),
		"oversized":         strings.Repeat(" ", 1025) + contactBody,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			s := &contactEnsureStub{}
			h := MentorContactHandler{Service: s, Limiter: &contactLimiter{allowed: true}}
			r := httptest.NewRequest("POST", "/internal/v1/mentor-dialog/ensure-personal", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.Ensure(w, r)
			if w.Code != 400 || s.calls != 0 {
				t.Fatalf("accepted shape %s: %d %s", name, w.Code, w.Body.String())
			}
		})
	}
}

func TestMentorContactHTTPDeniesQueryContentTypeAndRate(t *testing.T) {
	for _, name := range []string{"query", "content type", "encoding", "rate", "missing limiter", "missing service"} {
		t.Run(name, func(t *testing.T) {
			s := &contactEnsureStub{}
			limiter := &contactLimiter{allowed: true}
			h := MentorContactHandler{Service: s, Limiter: limiter}
			path := "/internal/v1/mentor-dialog/ensure-personal"
			want := 400
			switch name {
			case "query":
				path += "?student_id=other"
			case "rate":
				limiter.allowed = false
				want = 429
			case "missing limiter":
				h.Limiter = nil
				want = 503
			case "missing service":
				h.Service = nil
				want = 503
			}
			r := httptest.NewRequest("POST", path, strings.NewReader(contactBody))
			if name != "content type" {
				r.Header.Set("Content-Type", "application/json")
			}
			if name == "encoding" {
				r.Header.Set("Content-Encoding", "gzip")
			}
			w := httptest.NewRecorder()
			h.Ensure(w, r)
			if w.Code != want || s.calls != 0 {
				t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
			}
		})
	}
}

func TestMentorContactHTTPDeniesMismatchedOwnerResult(t *testing.T) {
	for _, name := range []string{"student", "mentor", "dialog", "version", "version overflow", "blocked", "disabled"} {
		t.Run(name, func(t *testing.T) {
			s := &contactEnsureStub{}
			want := 503
			switch name {
			case "student":
				s.change = func(r *dialoguc.MentorContact) { r.StudentID = uuid.New() }
			case "mentor":
				s.change = func(r *dialoguc.MentorContact) { r.MentorID = uuid.New() }
			case "dialog":
				s.change = func(r *dialoguc.MentorContact) { r.DialogID = uuid.Nil }
			case "version":
				s.change = func(r *dialoguc.MentorContact) { r.DialogVersion = 0 }
			case "version overflow":
				s.change = func(r *dialoguc.MentorContact) { r.DialogVersion = 2147483648 }
			case "blocked":
				s.err = domain.ErrBlocked
				want = 403
			case "disabled":
				s.err = domain.ErrFeatureDisabled
				want = 409
			}
			r := httptest.NewRequest("POST", "/internal/v1/mentor-dialog/ensure-personal", strings.NewReader(contactBody))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			MentorContactHandler{Service: s, Limiter: &contactLimiter{allowed: true}}.Ensure(w, r)
			if w.Code != want || strings.Contains(w.Body.String(), `"dialog_id"`) {
				t.Fatalf("%s response leak: %d %s", name, w.Code, w.Body.String())
			}
		})
	}
}
