package message

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

func TestT14FrozenLessonSourceReplayAndProjection(t *testing.T) {
	for _, mode := range []domain.LessonMessageContextMode{domain.LessonMessageContextOverview, domain.LessonMessageContextSelection} {
		t.Run(string(mode), func(t *testing.T) { testT14FrozenLessonSourceReplayAndProjection(t, mode) })
	}
}

func testT14FrozenLessonSourceReplayAndProjection(t *testing.T, mode domain.LessonMessageContextMode) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	course, lesson, path, item := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, lesson, now)
	dialogItem.TeacherContextType = domain.TeacherContextLesson
	dialogs := &fakeDialogs{item: dialogItem}
	messages := &fakeMessages{items: map[uuid.UUID]domain.Message{}}
	outbox := &fakeOutbox{}
	service := Service{Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: dialogs,
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: messages, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now }, NewID: uuid.New}
	anchor := &domain.LessonMessageContext{Schema: domain.LessonMessageContextSchemaV2, Mode: domain.LessonMessageContextOverview,
		CourseID: &course, LessonID: &lesson, ContentRevision: uuid.NewString(), LearningPathID: &path, LearningPathItemID: &item, ContentDigest: "sha256:" + strings.Repeat("a", 64)}
	anchor.Mode = mode
	if mode == domain.LessonMessageContextSelection {
		selected := " \nExact frozen 界 selection\n "
		anchor.SelectedText = &selected
	}
	actor := domain.Actor{UserID: studentID, Role: "STUDENT"}
	input := CreateInput{DialogID: dialogID, Body: "Explain frozen lesson", IdempotencyKey: uuid.New(), LessonContext: anchor}
	foreign := *anchor
	foreignLesson := uuid.New()
	foreign.LessonID = &foreignLesson
	invalid := input
	invalid.LessonContext = &foreign
	if _, err := service.Create(context.Background(), actor, invalid); !errors.Is(err, domain.ErrValidation) || len(outbox.items) != 0 {
		t.Fatalf("foreign lesson produced effect: %v", err)
	}
	created, err := service.Create(context.Background(), actor, input)
	if err != nil {
		t.Fatal(err)
	}
	if !domain.LessonMessageContextsEqual(created.View.Message.LessonContext, anchor) {
		t.Fatal("source anchor changed")
	}
	replay, err := service.Create(context.Background(), actor, input)
	if err != nil || replay.Created || replay.View.Message.ID != created.View.Message.ID || len(outbox.items) != 2 {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	for name, mutate := range map[string]func(*domain.LessonMessageContext){
		"path":     func(c *domain.LessonMessageContext) { id := uuid.New(); c.LearningPathID = &id },
		"item":     func(c *domain.LessonMessageContext) { id := uuid.New(); c.LearningPathItemID = &id },
		"digest":   func(c *domain.LessonMessageContext) { c.ContentDigest = "sha256:" + strings.Repeat("b", 64) },
		"revision": func(c *domain.LessonMessageContext) { c.ContentRevision = uuid.NewString() },
		"course":   func(c *domain.LessonMessageContext) { id := uuid.New(); c.CourseID = &id },
	} {
		t.Run(name, func(t *testing.T) {
			changed := *anchor
			mutate(&changed)
			retry := input
			retry.LessonContext = &changed
			if _, err := service.Create(context.Background(), actor, retry); !errors.Is(err, domain.ErrIdempotencyConflict) {
				t.Fatalf("changed anchor replay: %v", err)
			}
			if len(outbox.items) != 2 {
				t.Fatal("conflict created event")
			}
		})
	}
	messages.windowItems = []domain.Message{created.View.Message}
	request, err := service.GetTeacherRequestContext(context.Background(), dialogID, teacherID, created.View.Message.ID, 0)
	if err != nil || !domain.LessonMessageContextsEqual(request.Source.LessonContext, anchor) {
		t.Fatalf("private context lost anchor: %v", err)
	}
	for _, event := range outbox.items {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if event.Subject == domain.EventDialogMessageCreated {
			var projected domain.LessonMessageContext
			if err := json.Unmarshal(payload["lesson_context"], &projected); err != nil || !domain.LessonMessageContextsEqual(&projected, anchor) {
				t.Fatalf("lifecycle projection lost anchor: %v", err)
			}
		}
		if event.Subject == domain.EventDialogTeacherRequested {
			if _, ok := payload["lesson_context"]; ok {
				t.Fatal("teacher trigger leaked anchor")
			}
		}
	}
	dialogs.item.TeacherContextType = domain.TeacherContextGeneralTeacher
	dialogs.item.ContextID = nil
	fresh := input
	fresh.IdempotencyKey = uuid.New()
	if _, err := service.Create(context.Background(), actor, fresh); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("v2 accepted outside lesson context: %v", err)
	}
}
