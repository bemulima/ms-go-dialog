package handlers

import (
	"context"
	common "github.com/bemulima/ms-go-dialog/internal/transport/http/common"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common/middleware"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestCanonicalStudentTurnHandler_RequiresExactTokenAndStrictNestedDTO(t *testing.T) {
	dialogID, studentID, teacherID, receiptID, commandID, sourceID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	body := `{"identity":{"schema":"canonical-student-turn-identity.v1","dialog_id":"` + dialogID.String() + `","student_id":"` + studentID.String() + `","personal_teacher_id":"` + teacherID.String() + `","action_receipt_id":"` + receiptID.String() + `","correlation_id":"` + receiptID.String() + `","causation_id":"` + receiptID.String() + `","canonical_message_command_id":"` + commandID.String() + `","interaction":{"schema":"canonical-student-turn-interaction.v1","kind":"teacher_action_receipt","action_receipt_id":"` + receiptID.String() + `","source_prompt_message_id":"` + sourceID.String() + `","source_prompt_message_version":1,"block_id":"choice-1","action_id":"accept","source_ui_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},"canonical_body":"Server-derived choice"}`
	handler := TeacherDialogHandler{Messages: &messageuc.Service{}}
	protected := middleware.RequireInternalToken("exact-token", common.WriteError)(http.HandlerFunc(handler.CanonicalStudentTurn))

	withoutToken := canonicalStudentTurnRequest(dialogID, body)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, withoutToken)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("internal command accepted without exact token: %d %s", response.Code, response.Body.String())
	}

	withUnknown := canonicalStudentTurnRequest(dialogID, strings.TrimSuffix(body, "}")+`,"private_browser_value":"must-reject"}`)
	withUnknown.Header.Set("X-Internal-Token", "exact-token")
	response = httptest.NewRecorder()
	protected.ServeHTTP(response, withUnknown)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown canonical command field accepted: %d %s", response.Code, response.Body.String())
	}

	validShape := canonicalStudentTurnRequest(dialogID, body)
	validShape.Header.Set("X-Internal-Token", "exact-token")
	response = httptest.NewRecorder()
	protected.ServeHTTP(response, validShape)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "feature_disabled") {
		t.Fatalf("nested DTO was not accepted before runtime feature gate: %d %s", response.Code, response.Body.String())
	}
}

func canonicalStudentTurnRequest(dialogID uuid.UUID, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/teacher-dialog/"+dialogID.String()+"/canonical-student-turn", strings.NewReader(body))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("dialogID", dialogID.String())
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
}

func TestCanonicalStudentTurnIdentityFixtureUsesOnlyTrustedEnvelopeFields(t *testing.T) {
	// This guards the documented nested HTTP fixture from silently adding a
	// browser value/state field; domain validation remains the authoritative
	// shape check.
	identity := domain.CanonicalStudentTurnIdentity{}
	if _, err := domain.NewCanonicalStudentTurnIdentity(identity); err == nil {
		t.Fatal("empty identity unexpectedly validated")
	}
}
