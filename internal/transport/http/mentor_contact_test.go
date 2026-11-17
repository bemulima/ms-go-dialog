package http

import (
	"net/http/httptest"
	"strings"
	"testing"

	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/google/uuid"
)

type mentorContactAllow struct{}

func (mentorContactAllow) Allow(uuid.UUID) bool { return true }

func TestMentorContactRoutePurposeTokenAndDisabledGate(t *testing.T) {
	const body = `{"schema":"mentor-contact.ensure.v1","student_id":"11111111-1111-4111-8111-111111111111","mentor_id":"22222222-2222-4222-8222-222222222222"}`
	deps := RouterDependencies{DialogService: &dialoguc.Service{}, MessageService: &messageuc.Service{}, InternalToken: "teacher-token", MentorInternalToken: "mentor-token", MentorContactService: &dialoguc.MentorContactService{}, UserRateLimiter: mentorContactAllow{}}
	for _, tc := range []struct {
		name, method, path, token string
		duplicate                 bool
		want                      int
	}{
		{"no token", "POST", "/internal/v1/mentor-dialog/ensure-personal", "", false, 401},
		{"teacher token", "POST", "/internal/v1/mentor-dialog/ensure-personal", "teacher-token", false, 401},
		{"purpose disabled", "POST", "/internal/v1/mentor-dialog/ensure-personal", "mentor-token", false, 409},
		{"duplicate", "POST", "/internal/v1/mentor-dialog/ensure-personal", "mentor-token", true, 401},
		{"wrong method", "GET", "/internal/v1/mentor-dialog/ensure-personal", "mentor-token", false, 405},
		{"public alias", "POST", "/api/v1/mentor-dialog/ensure-personal", "mentor-token", false, 404},
		{"no teacher privilege", "PUT", "/internal/v1/teacher-dialog/ensure", "mentor-token", false, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-User-ID", uuid.NewString())
			r.Header.Set("X-User-Role", "ADMIN")
			if tc.token != "" {
				r.Header.Set("X-Internal-Token", tc.token)
			}
			if tc.duplicate {
				r.Header.Add("X-Internal-Token", "mentor-token")
			}
			w := httptest.NewRecorder()
			NewRouter(deps).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("%s got %d %s", tc.name, w.Code, w.Body.String())
			}
		})
	}
	deps.MentorInternalToken = deps.InternalToken
	r := httptest.NewRequest("POST", "/internal/v1/mentor-dialog/ensure-personal", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Internal-Token", deps.InternalToken)
	w := httptest.NewRecorder()
	NewRouter(deps).ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("shared token registered Mentor route: %d", w.Code)
	}
}
