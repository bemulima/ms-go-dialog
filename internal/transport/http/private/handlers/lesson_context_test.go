package handlers

import (
	"context"
	"encoding/json"
	apihandlers "github.com/bemulima/ms-go-dialog/internal/transport/http/api/v1/handlers"
	common "github.com/bemulima/ms-go-dialog/internal/transport/http/common"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestMessageResponseCarriesVersionedLessonContextWithoutCrossModeLeakage(t *testing.T) {
	now := time.Now().UTC()
	courseID, lessonID := uuid.New(), uuid.New()
	base := domain.Message{
		ID: uuid.New(), DialogID: uuid.New(), AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelWeb,
		SenderID: uuid.New(), Body: "fallback", Status: domain.MessageStatusActive, Version: 1,
		MessageSequence: 1, LastEventSequence: 1, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now,
	}

	base.LessonContext = &domain.LessonMessageContext{
		Schema: domain.LessonMessageContextSchemaV1, Mode: domain.LessonMessageContextOverview,
		CourseID: &courseID, LessonID: &lessonID, ContentRevision: "2026-09-04T08:00:00Z",
	}
	overview, err := json.Marshal(common.NewMessageResponse(messageuc.View{Message: base}))
	if err != nil {
		t.Fatal(err)
	}
	var overviewPayload map[string]json.RawMessage
	if err := json.Unmarshal(overview, &overviewPayload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(overviewPayload["lesson_context"]), "selected_text") {
		t.Fatalf("overview leaked selected_text: %s", overview)
	}

	selection := "  exact selection\n"
	base.LessonContext.Mode = domain.LessonMessageContextSelection
	base.LessonContext.SelectedText = &selection
	selected, err := json.Marshal(common.NewMessageResponse(messageuc.View{Message: base}))
	if err != nil {
		t.Fatal(err)
	}
	var selectedPayload struct {
		LessonContext domain.LessonMessageContext `json:"lesson_context"`
	}
	if err := json.Unmarshal(selected, &selectedPayload); err != nil {
		t.Fatal(err)
	}
	if selectedPayload.LessonContext.SelectedText == nil || *selectedPayload.LessonContext.SelectedText != selection {
		t.Fatalf("selection response was reformatted: %s", selected)
	}
}

func TestMessageCreateRejectsUnknownLessonContextField(t *testing.T) {
	dialogID, courseID, lessonID := uuid.New(), uuid.New(), uuid.New()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/message/create", strings.NewReader(`{
		"dialog_id":"`+dialogID.String()+`",
		"body":"question",
		"attachment_ids":[],
		"idempotency_key":"`+uuid.NewString()+`",
		"lesson_context":{
			"schema":"lesson-message-context.v1",
			"mode":"lesson_overview",
			"course_id":"`+courseID.String()+`",
			"lesson_id":"`+lessonID.String()+`",
			"content_revision":"2026-09-04T08:00:00Z",
			"unknown":true
		}
	}`))
	response := httptest.NewRecorder()
	(apihandlers.MessageHandler{}).Create(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown lesson context field accepted: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAssistantAppendRejectsLessonContext(t *testing.T) {
	dialogID := uuid.New()
	request := httptest.NewRequest(http.MethodPost, "/internal/dialogs/"+dialogID.String()+"/teacher-response", strings.NewReader(`{
		"personal_teacher_id":"`+uuid.NewString()+`",
		"source_message_id":"`+uuid.NewString()+`",
		"idempotency_key":"`+uuid.NewString()+`",
		"body":"answer",
		"lesson_context":{"content_revision":"2026-09-04T08:00:00Z","selected_text":"x"}
	}`))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("dialogID", dialogID.String())
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	response := httptest.NewRecorder()
	(TeacherDialogHandler{}).AppendResponse(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("assistant append accepted lesson_context: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMessageUpdateCannotReplaceLessonContext(t *testing.T) {
	messageID := uuid.New()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/message/update/"+messageID.String(), strings.NewReader(`{
		"body":"edited body",
		"version":1,
		"lesson_context":{"content_revision":"2026-09-04T08:00:00Z","selected_text":"replacement"}
	}`))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("messageID", messageID.String())
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	response := httptest.NewRecorder()
	(apihandlers.MessageHandler{}).Update(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("message update accepted lesson_context replacement: status=%d body=%s", response.Code, response.Body.String())
	}
}
