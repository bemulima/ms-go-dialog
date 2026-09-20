package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	httpadapter "github.com/bemulima/ms-go-dialog/internal/transport/http"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/google/uuid"
)

type assistantUISourceDialogs struct {
	repository.DialogRepository
	item domain.Dialog
}

func (r assistantUISourceDialogs) GetByID(_ context.Context, id uuid.UUID) (domain.Dialog, error) {
	if id != r.item.ID {
		return domain.Dialog{}, domain.ErrNotFound
	}
	return r.item, nil
}

type assistantUISourceMessages struct {
	repository.MessageRepository
	item domain.Message
}

func (r assistantUISourceMessages) GetByID(_ context.Context, id uuid.UUID) (domain.Message, error) {
	if id != r.item.ID {
		return domain.Message{}, domain.ErrNotFound
	}
	return r.item, nil
}

func TestAssistantUISourceRouteIsInternalStrictAndBodyFree(t *testing.T) {
	now := time.Now().UTC()
	dialogID, studentID, teacherID, contextID, messageID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := domain.Dialog{
		ID: dialogID, Type: domain.DialogTypeTeacher, Status: domain.DialogStatusActive,
		StudentID: studentID, PersonalTeacherID: teacherID, TeacherContextType: domain.TeacherContextLesson,
		ContextID: &contextID,
	}
	message := domain.Message{
		ID: messageID, DialogID: dialogID, AuthorType: domain.MessageAuthorPersonalTeacher,
		PersonalTeacherID: teacherID, Channel: domain.MessageChannelWeb, Body: "private fallback",
		AssistantUI: json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[{"opaque":{"future":true}}]}`),
		Status:      domain.MessageStatusActive, Version: 2, MessageSequence: 3, LastEventSequence: 3,
		IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now,
	}
	messages := &messageuc.Service{Dialogs: assistantUISourceDialogs{item: dialogItem}, Messages: assistantUISourceMessages{item: message}}
	router := httpadapter.NewRouter(httpadapter.RouterDependencies{DialogService: &dialoguc.Service{}, MessageService: messages, InternalToken: "source-secret"})
	path := "/internal/v1/teacher-dialog/" + dialogID.String() + "/assistant-ui-source"
	body := `{"student_id":"` + studentID.String() + `","personal_teacher_id":"` + teacherID.String() + `","message_id":"` + messageID.String() + `"}`

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status=%d body=%s", response.Code, response.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("X-Internal-Token", "source-secret")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("success status=%d body=%s", response.Code, response.Body.String())
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"schema": true, "dialog_id": true, "student_id": true, "personal_teacher_id": true,
		"context_type": true, "context_id": true, "message_id": true, "message_sequence": true,
		"message_version": true, "assistant_ui": true,
	}
	if len(fields) != len(want) || string(fields["schema"]) != `"dialog-assistant-ui-source.v1"` {
		t.Fatalf("unexpected exact response: %s", response.Body.String())
	}
	for key := range fields {
		if !want[key] {
			t.Fatalf("source response leaked %q: %s", key, response.Body.String())
		}
	}

	bad := httptest.NewRequest(http.MethodPost, path, strings.NewReader(strings.TrimSuffix(body, "}")+`,"body":"forbidden"}`))
	bad.Header.Set("X-Internal-Token", "source-secret")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, bad)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d body=%s", response.Code, response.Body.String())
	}

	oversized := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"student_id":"`+studentID.String()+`","personal_teacher_id":"`+teacherID.String()+`","message_id":"`+messageID.String()+`","padding":"`+strings.Repeat("x", 4096)+`"}`))
	oversized.Header.Set("X-Internal-Token", "source-secret")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, oversized)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized body status=%d body=%s", response.Code, response.Body.String())
	}

	public := httptest.NewRequest(http.MethodPost, "/api/v1/teacher-dialog/"+dialogID.String()+"/assistant-ui-source", strings.NewReader(body))
	public.Header.Set("X-User-ID", studentID.String())
	public.Header.Set("X-User-Role", "STUDENT")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, public)
	if response.Code != http.StatusNotFound {
		t.Fatalf("source route leaked into browser API: status=%d", response.Code)
	}
}

func TestAssistantUISourceCrossBindingReturnsSameNotFound(t *testing.T) {
	now := time.Now().UTC()
	dialogID, studentID, teacherID, messageID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := domain.Dialog{ID: dialogID, Type: domain.DialogTypeTeacher, Status: domain.DialogStatusActive, StudentID: studentID, PersonalTeacherID: teacherID, TeacherContextType: domain.TeacherContextGeneralTeacher}
	message := domain.Message{ID: messageID, DialogID: dialogID, AuthorType: domain.MessageAuthorPersonalTeacher, PersonalTeacherID: teacherID, AssistantUI: json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[]}`), Status: domain.MessageStatusActive, Version: 1, MessageSequence: 1, LastEventSequence: 1, Channel: domain.MessageChannelWeb, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now}
	service := &messageuc.Service{Dialogs: assistantUISourceDialogs{item: dialogItem}, Messages: assistantUISourceMessages{item: message}}
	router := httpadapter.NewRouter(httpadapter.RouterDependencies{DialogService: &dialoguc.Service{}, MessageService: service, InternalToken: "secret"})
	path := "/internal/v1/teacher-dialog/" + dialogID.String() + "/assistant-ui-source"
	for name, ids := range map[string][3]uuid.UUID{
		"student": {uuid.New(), teacherID, messageID},
		"teacher": {studentID, uuid.New(), messageID},
		"message": {studentID, teacherID, uuid.New()},
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"student_id":"` + ids[0].String() + `","personal_teacher_id":"` + ids[1].String() + `","message_id":"` + ids[2].String() + `"}`
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			request.Header.Set("X-Internal-Token", "secret")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"error":"message_not_found"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
