package handlers

import (
	"context"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	adminuc "github.com/bemulima/ms-go-dialog/internal/usecase/admin"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMentorEducationAdminTransitionDoesNotReturnPayload(t *testing.T) {
	id, b := uuid.New(), uuid.New()
	r := httptest.NewRequest("PUT", "/admin/v1/message/hide/"+id.String(), nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("messageID", id.String())
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
	w := httptest.NewRecorder()
	AdminHandler{}.messageStatus(w, r, func(context.Context, domain.Actor, uuid.UUID) (adminuc.ModerationView, error) {
		return adminuc.ModerationView{Message: domain.Message{ID: id, MentorBindingID: &b, Body: "educational-secret"}, Attachments: []domain.Attachment{{OriginalFilename: "private-file.pdf"}}}, nil
	})
	if w.Code != 200 || strings.Contains(w.Body.String(), "educational-secret") || strings.Contains(w.Body.String(), "private-file.pdf") {
		t.Fatalf("moderator read educational payload: %d %s", w.Code, w.Body.String())
	}
}
