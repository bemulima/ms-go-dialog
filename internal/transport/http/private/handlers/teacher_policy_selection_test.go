package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type selectedSourceDialog struct {
	repository.DialogRepository
	item domain.Dialog
}

func (s selectedSourceDialog) GetByID(context.Context, uuid.UUID) (domain.Dialog, error) {
	return s.item, nil
}

type selectedSourceMessage struct {
	repository.MessageRepository
	item domain.Message
}

func (s selectedSourceMessage) GetByID(context.Context, uuid.UUID) (domain.Message, error) {
	return s.item, nil
}
func (s selectedSourceMessage) Window(context.Context, repository.MessageWindowQuery) ([]domain.Message, error) {
	return []domain.Message{s.item}, nil
}
func TestTeacherPolicySelectionPrivateHTTPProducerVector(t *testing.T) {
	now := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
	dialogID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	sourceID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	studentID := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	teacherID := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	contextID := uuid.MustParse("66666666-6666-4666-8666-666666666666")
	actionID := uuid.MustParse("77777777-7777-4777-8777-777777777777")
	for _, kind := range []string{"offer", "cohort"} {
		t.Run(kind, func(t *testing.T) {
			selection := &domain.TeacherPolicySelection{Kind: "OFFER", OfferingID: "11111111-aaaa-4aaa-8aaa-aaaaaaaaaaaa", AssignmentID: "22222222-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}
			if kind == "cohort" {
				selection = &domain.TeacherPolicySelection{Kind: "COHORT", CohortID: "11111111-aaaa-4aaa-8aaa-aaaaaaaaaaaa", EnrollmentID: "22222222-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}
			}
			d := domain.Dialog{ID: dialogID, Type: domain.DialogTypeTeacher, Status: domain.DialogStatusActive, StudentID: studentID, PersonalTeacherID: teacherID, TeacherContextType: domain.TeacherContextPracticeTask, ContextID: &contextID}
			m := domain.Message{ID: sourceID, DialogID: dialogID, AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelWeb, SenderID: studentID, LearningActionID: &actionID, TeacherPolicySelection: selection, Body: "Explain the exact current step", Status: domain.MessageStatusActive, Version: 1, MessageSequence: 2, LastEventSequence: 4, CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)}
			h := TeacherDialogHandler{Messages: &messageuc.Service{Dialogs: selectedSourceDialog{item: d}, Messages: selectedSourceMessage{item: m}}}
			r := httptest.NewRequest(http.MethodGet, "/internal/v1/teacher-dialog/"+dialogID.String()+"/request/"+sourceID.String()+"?personal_teacher_id="+teacherID.String()+"&before=0", nil)
			route := chi.NewRouteContext()
			route.URLParams.Add("dialogID", dialogID.String())
			route.URLParams.Add("sourceMessageID", sourceID.String())
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
			w := httptest.NewRecorder()
			h.RequestContext(w, r)
			if w.Code != 200 {
				t.Fatalf("private owner producer: %d %s", w.Code, w.Body.String())
			}
			var data map[string]any
			if json.Unmarshal(w.Body.Bytes(), &data) != nil {
				t.Fatal("json")
			}
			messages := data["messages"].([]any)
			echo := messages[0].(map[string]any)["teacher_policy_selection"].(map[string]any)
			if len(echo) != 3 || echo["kind"] != selection.Kind {
				t.Fatal("typed selection lost")
			}
			raw, e := os.ReadFile("testdata/mentor-policy-source-" + kind + ".json")
			if e != nil {
				t.Fatal(e)
			}
			var expected map[string]any
			if json.Unmarshal(raw, &expected) != nil || !reflect.DeepEqual(data, expected) {
				t.Fatal("actual private producer drifted from frozen consumer vector")
			}
		})
	}
}
