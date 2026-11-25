package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dial "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	msg "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/google/uuid"
)

type mentorMCPReadFixture struct {
	calls     int
	lastAfter uuid.UUID
	next      uuid.UUID
}

func (f *mentorMCPReadFixture) Allow(uuid.UUID) bool { return true }

func (f *mentorMCPReadFixture) MentorMCPBinding(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}

func (f *mentorMCPReadFixture) MentorMCPDialogs(_ context.Context, _, after uuid.UUID, _ int) ([]dial.MentorMCPDialog, uuid.UUID, error) {
	f.calls++
	f.lastAfter = after
	return []dial.MentorMCPDialog{}, f.next, nil
}

func (f *mentorMCPReadFixture) MentorMCPMessages(context.Context, msg.MentorMCPBindingVerifier, uuid.UUID, uuid.UUID, uuid.UUID, int64, int) (msg.MentorMCPMessagePage, error) {
	panic("not used")
}

func TestMentorEducationMCPPrivateReadCursorIsBoundToMentor(t *testing.T) {
	token := strings.Repeat("r", 40)
	f := &mentorMCPReadFixture{next: uuid.New()}
	h := MentorMCPReadHandler{Token: token, Limiter: f, Contexts: f, Messages: f}
	owner := uuid.New()
	invoke := func(id uuid.UUID, path, cursor, provided string, inject bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(mentorMCPDialogsInput{MentorID: id, Cursor: cursor, Limit: 3})
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
		req.Header.Set(mentorMCPReadHeader, provided)
		req.Header.Set("Content-Type", "application/json")
		if inject {
			req.Header.Set("X-User-ID", owner.String())
		}
		w := httptest.NewRecorder()
		h.ListDialogs(w, req)
		return w
	}
	for _, tc := range []struct {
		path, token string
		inject      bool
		status      int
	}{
		{MentorMCPDialogsPath, strings.Repeat("x", 40), false, 401},
		{MentorMCPDialogsPath, token, true, 401},
		{MentorMCPDialogsPath + "?", token, false, 400},
	} {
		if w := invoke(owner, tc.path, "", tc.token, tc.inject); w.Code != tc.status || f.calls != 0 {
			t.Fatal("ambiguous call reached context owner", w.Code, w.Body.String(), f.calls)
		}
	}
	w := invoke(owner, MentorMCPDialogsPath, "", token, false)
	var out struct {
		Next string `json:"next_cursor"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Next) != 59 ||
		strings.Contains(w.Body.String(), f.next.String()) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private binding leaked or page failed", w.Code, w.Body.String())
	}
	if w = invoke(uuid.New(), MentorMCPDialogsPath, out.Next, token, false); w.Code != 400 || f.calls != 1 {
		t.Fatal("foreign mentor reused cursor", w.Code, w.Body.String())
	}
	if w = invoke(owner, MentorMCPDialogsPath, out.Next, token, false); w.Code != 200 || f.calls != 2 || f.lastAfter != f.next {
		t.Fatal("own opaque cursor did not advance", w.Code, w.Body.String())
	}
}
