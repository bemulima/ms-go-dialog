package handlers

import (
	"context"
	"encoding/json"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTeacherPolicySelectionHTTPRejectsNullAmbiguousAndUpdateMutation(t *testing.T) {
	a, b := uuid.NewString(), uuid.NewString()
	base := `"dialog_id":"` + uuid.NewString() + `","body":"hello","idempotency_key":"` + uuid.NewString() + `"`
	for _, raw := range []string{`{` + base + `,"teacher_policy_selection":null}`, `{` + base + `,"teacher_policy_selection":{}}`, `{` + base + `,"Teacher_policy_selection":{}}`, `{` + base + `,"teacher_policy_selection":{"kind":"OFFER","offering_id":"` + a + `","assignment_id":"` + b + `"},"teacher_policy_selection":null}`} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/message/create", strings.NewReader(raw))
		response := httptest.NewRecorder()
		(MessageHandler{}).Create(response, request)
		if response.Code != 400 {
			t.Fatalf("ambiguous intent passed decode: %d", response.Code)
		}
	}
	var valid messageCreateRequest
	if e := json.Unmarshal([]byte(`{`+base+`,"teacher_policy_selection":{"kind":"OFFER","offering_id":"`+a+`","assignment_id":"`+b+`"}}`), &valid); e != nil || len(valid.TeacherPolicySelection) == 0 {
		t.Fatal("explicit DTO not decoded")
	}
	update := httptest.NewRequest(http.MethodPut, "/api/v1/message/update/"+a, strings.NewReader(`{"body":"edit","version":1,"teacher_policy_selection":null}`))
	route := chi.NewRouteContext()
	route.URLParams.Add("messageID", a)
	update = update.WithContext(context.WithValue(update.Context(), chi.RouteCtxKey, route))
	reply := httptest.NewRecorder()
	(MessageHandler{}).Update(reply, update)
	if reply.Code != 400 {
		t.Fatal("edit accepted policy replacement")
	}
}
func TestTeacherPolicySelectionRESTEchoIsOptionalAndExact(t *testing.T) {
	s := &domain.TeacherPolicySelection{Kind: "COHORT", CohortID: uuid.NewString(), EnrollmentID: uuid.NewString()}
	for _, selection := range []*domain.TeacherPolicySelection{s, nil} {
		raw, e := json.Marshal(common.NewMessageResponse(messageuc.View{Message: domain.Message{TeacherPolicySelection: selection}}))
		if e != nil {
			t.Fatal(e)
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(raw, &result) != nil {
			t.Fatal("response")
		}
		value, found := result["teacher_policy_selection"]
		if found != (selection != nil) {
			t.Fatal("optional echo changed")
		}
		if found {
			var decoded domain.TeacherPolicySelection
			if json.Unmarshal(value, &decoded) != nil || decoded != *s {
				t.Fatal("echo changed union")
			}
		}
	}
}
