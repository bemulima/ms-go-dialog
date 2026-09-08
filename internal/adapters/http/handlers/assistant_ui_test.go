package handlers

import (
	"context"
	"encoding/json"
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

func TestNewMessageResponseCarriesOpaqueAssistantUI(t *testing.T) {
	now := time.Now().UTC()
	assistantUI := json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[{"type":"future","opaque":true}]}`)
	response := newMessageResponse(messageuc.View{Message: domain.Message{
		ID: uuid.New(), DialogID: uuid.New(), AuthorType: domain.MessageAuthorPersonalTeacher,
		Channel: domain.MessageChannelWeb, PersonalTeacherID: uuid.New(), AssistantUI: assistantUI,
		Body: "plain fallback", Status: domain.MessageStatusActive, Version: 1,
		MessageSequence: 1, LastEventSequence: 1, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now,
	}})
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if string(payload["assistant_ui"]) != string(assistantUI) || string(payload["body"]) != `"plain fallback"` {
		t.Fatalf("message response lost structured UI or fallback: %s", encoded)
	}
}

func TestBrowserMessageCommandsRejectAssistantUI(t *testing.T) {
	dialogID, messageID := uuid.New(), uuid.New()
	create := httptest.NewRequest(http.MethodPost, "/api/v1/message/create", strings.NewReader(`{
		"dialog_id":"`+dialogID.String()+`",
		"body":"browser",
		"attachment_ids":[],
		"idempotency_key":"`+uuid.NewString()+`",
		"assistant_ui":{"schema":"assistant-ui.v1","blocks":[]}
	}`))
	createResponse := httptest.NewRecorder()
	(MessageHandler{}).Create(createResponse, create)
	if createResponse.Code != http.StatusBadRequest {
		t.Fatalf("browser create accepted assistant_ui: status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}

	update := httptest.NewRequest(http.MethodPut, "/api/v1/message/update/"+messageID.String(), strings.NewReader(`{
		"body":"browser edit",
		"version":1,
		"assistant_ui":{"schema":"assistant-ui.v1","blocks":[]}
	}`))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("messageID", messageID.String())
	update = update.WithContext(context.WithValue(update.Context(), chi.RouteCtxKey, routeContext))
	updateResponse := httptest.NewRecorder()
	(MessageHandler{}).Update(updateResponse, update)
	if updateResponse.Code != http.StatusBadRequest {
		t.Fatalf("browser update accepted assistant_ui: status=%d body=%s", updateResponse.Code, updateResponse.Body.String())
	}
}
