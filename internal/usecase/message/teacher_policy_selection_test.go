package message

import (
	"context"
	"errors"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func policyMessageFixture() (Service, domain.Actor, CreateInput, *fakeDialogs, *fakeMessages, *fakeOutbox) {
	now := time.Now().UTC()
	dialogID, spaceID, studentID, teacherID, contextID, actionID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogs := &fakeDialogs{item: teacherDialog(dialogID, spaceID, studentID, teacherID, contextID, now)}
	messages := &fakeMessages{}
	outbox := &fakeOutbox{}
	s := Service{TeacherMentorPolicyEnabled: true, Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: dialogs, Members: &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}}, Messages: messages, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now.Add(time.Second) }, NewID: uuid.New}
	in := CreateInput{DialogID: dialogID, Body: "Explain this step", IdempotencyKey: uuid.New(), LearningActionID: &actionID, TeacherPolicySelection: &domain.TeacherPolicySelection{Kind: "OFFER", OfferingID: uuid.NewString(), AssignmentID: uuid.NewString()}}
	return s, domain.Actor{UserID: studentID, Role: "STUDENT"}, in, dialogs, messages, outbox
}
func TestTeacherPolicySelectionStoredAndReplayedAfterAdmissionRollback(t *testing.T) {
	s, actor, in, dialogs, messages, outbox := policyMessageFixture()
	ctx := context.Background()
	result, e := s.Create(ctx, actor, in)
	if e != nil || !result.Created || !domain.TeacherPolicySelectionsEqual(in.TeacherPolicySelection, result.View.Message.TeacherPolicySelection) {
		t.Fatalf("selection not saved: %v", e)
	}
	messages.windowItems = []domain.Message{result.View.Message}
	history, e := s.GetTeacherRequestContext(ctx, in.DialogID, dialogs.item.PersonalTeacherID, result.View.Message.ID, 0)
	if e != nil || !domain.TeacherPolicySelectionsEqual(history.Source.TeacherPolicySelection, in.TeacherPolicySelection) {
		t.Fatalf("private source selection missing: %v", e)
	}
	events := len(outbox.items)
	s.TeacherMentorPolicyEnabled = false
	replay, e := s.Create(ctx, actor, in)
	if e != nil || replay.Created || replay.View.Message.ID != result.View.Message.ID || len(outbox.items) != events {
		t.Fatalf("rollback lost exact receipt replay: %v", e)
	}
	changed := in
	selection := *in.TeacherPolicySelection
	selection.AssignmentID = uuid.NewString()
	changed.TeacherPolicySelection = &selection
	if _, e = s.Create(ctx, actor, changed); !errors.Is(e, domain.ErrIdempotencyConflict) {
		t.Fatalf("same key changed selector allowed %v", e)
	}
	changed = in
	changed.TeacherPolicySelection = nil
	if _, e = s.Create(ctx, actor, changed); !errors.Is(e, domain.ErrIdempotencyConflict) {
		t.Fatal("same key erased selector")
	}
	updated, e := s.Update(ctx, actor, UpdateInput{MessageID: result.View.Message.ID, Body: "edited", ExpectedVersion: 1})
	if e != nil || !domain.TeacherPolicySelectionsEqual(updated.Message.TeacherPolicySelection, in.TeacherPolicySelection) {
		t.Fatalf("edit changed selector %v", e)
	}
	if _, e = s.Create(ctx, actor, in); !errors.Is(e, domain.ErrIdempotencyConflict) {
		t.Fatal("edited message must not invent original snapshot receipt")
	}
	deleted, e := s.Delete(ctx, actor, DeleteInput{MessageID: result.View.Message.ID, ExpectedVersion: 2})
	if e != nil || deleted.Message.Status != domain.MessageStatusDeleted || !domain.TeacherPolicySelectionsEqual(deleted.Message.TeacherPolicySelection, in.TeacherPolicySelection) {
		t.Fatalf("delete lost immutable intent %v", e)
	}
	if _, e = s.GetTeacherRequestContext(ctx, in.DialogID, dialogs.item.PersonalTeacherID, result.View.Message.ID, 0); e == nil {
		t.Fatal("deleted source remains usable")
	}
}
func TestTeacherPolicySelectionDefaultOffAndUnsupportedContextsFailBeforeWrite(t *testing.T) {
	for _, kind := range []string{"disabled", "personal", "group", "general_teacher", "lesson_task", "lesson-v1"} {
		t.Run(kind, func(t *testing.T) {
			s, actor, in, dialogs, messages, outbox := policyMessageFixture()
			switch kind {
			case "disabled":
				s.TeacherMentorPolicyEnabled = false
			case "personal":
				dialogs.item.Type = domain.DialogTypePersonal
				in.LearningActionID = nil
			case "group":
				dialogs.item.Type = domain.DialogTypeGroup
				in.LearningActionID = nil
			case "general_teacher":
				dialogs.item.TeacherContextType = domain.TeacherContextGeneralTeacher
				dialogs.item.ContextID = nil
				in.LearningActionID = nil
			case "lesson_task":
				dialogs.item.TeacherContextType = domain.TeacherContextLessonTask
			case "lesson-v1":
				dialogs.item.TeacherContextType = domain.TeacherContextLesson
				in.LearningActionID = nil
				course := uuid.New()
				in.LessonContext = &domain.LessonMessageContext{Schema: domain.LessonMessageContextSchemaV1, Mode: domain.LessonMessageContextOverview, CourseID: &course, LessonID: dialogs.item.ContextID, ContentRevision: time.Now().UTC().Format(time.RFC3339Nano)}
			}
			_, e := s.Create(context.Background(), actor, in)
			if e == nil || len(messages.items) != 0 || len(outbox.items) != 0 {
				t.Fatalf("unsupported selector mutated source: %v", e)
			}
		})
	}
}
func TestTeacherPolicySelectionProjectAcceptsExplicitCohortOnlyAsIntent(t *testing.T) {
	s, actor, in, dialogs, _, _ := policyMessageFixture()
	dialogs.item.TeacherContextType = domain.TeacherContextProject
	in.TeacherPolicySelection = &domain.TeacherPolicySelection{Kind: "COHORT", CohortID: uuid.NewString(), EnrollmentID: uuid.NewString()}
	r, e := s.Create(context.Background(), actor, in)
	if e != nil || r.View.Message.MentorBindingID != nil || !domain.TeacherPolicySelectionsEqual(r.View.Message.TeacherPolicySelection, in.TeacherPolicySelection) {
		t.Fatalf("project intent changed to human binding %v", e)
	}
}
func TestTeacherPolicySelectionLessonRequiresActualV2Anchor(t *testing.T) {
	s, actor, in, dialogs, _, _ := policyMessageFixture()
	dialogs.item.TeacherContextType = domain.TeacherContextLesson
	in.LearningActionID = nil
	course, path, item := uuid.New(), uuid.New(), uuid.New()
	in.LessonContext = &domain.LessonMessageContext{Schema: domain.LessonMessageContextSchemaV2, Mode: domain.LessonMessageContextOverview, CourseID: &course, LessonID: dialogs.item.ContextID, ContentRevision: uuid.NewString(), LearningPathID: &path, LearningPathItemID: &item, ContentDigest: "sha256:" + strings.Repeat("a", 64)}
	r, e := s.Create(context.Background(), actor, in)
	if e != nil || r.View.Message.LessonContext.Schema != domain.LessonMessageContextSchemaV2 || !domain.TeacherPolicySelectionsEqual(r.View.Message.TeacherPolicySelection, in.TeacherPolicySelection) {
		t.Fatalf("frozen lesson intent not retained %v", e)
	}
}

type teacherPolicyReplayAttachments struct {
	repository.AttachmentRepository
	items []domain.Attachment
}

func (a teacherPolicyReplayAttachments) ListByMessage(context.Context, uuid.UUID, uuid.UUID) ([]domain.Attachment, error) {
	return a.items, nil
}
func TestTeacherPolicySelectionExactAttachmentOnlyTombstoneReplay(t *testing.T) {
	dialogID, messageID, attachmentID, key, actionID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	selection := &domain.TeacherPolicySelection{Kind: "OFFER", OfferingID: uuid.NewString(), AssignmentID: uuid.NewString()}
	existing := domain.Message{ID: messageID, DialogID: dialogID, AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelWeb, LearningActionID: &actionID, IdempotencyKey: key, TeacherPolicySelection: selection, Status: domain.MessageStatusDeleted, Version: 2}
	s := Service{Attachments: teacherPolicyReplayAttachments{items: []domain.Attachment{{ID: attachmentID, DialogID: dialogID, MessageID: &messageID, Status: domain.AttachmentStatusDeleted}}}}
	in := CreateInput{DialogID: dialogID, Body: "", AttachmentIDs: []uuid.UUID{attachmentID}, IdempotencyKey: key, LearningActionID: &actionID, TeacherPolicySelection: selection}
	var result CreateResult
	if e := s.resolveReplay(context.Background(), existing, in, &result); e != nil || result.Created || result.View.Message.Status != domain.MessageStatusDeleted || result.View.Message.Version != 2 {
		t.Fatalf("exact current-state tombstone replay changed: %v", e)
	}
	in.Body = "original nonempty text"
	if e := s.resolveReplay(context.Background(), existing, in, &result); !errors.Is(e, domain.ErrIdempotencyConflict) {
		t.Fatal("tombstone invented original body receipt")
	}
}
