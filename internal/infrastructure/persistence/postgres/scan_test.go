package postgres

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type staticRow []any

func (row staticRow) Scan(dest ...any) error {
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count %d, want %d", len(dest), len(row))
	}
	for index := range dest {
		reflect.ValueOf(dest[index]).Elem().Set(reflect.ValueOf(row[index]))
	}
	return nil
}

func TestScanMessageCarriesCanonicalAssistantUI(t *testing.T) {
	now := time.Now().UTC()
	messageID, dialogID, teacherID, actionID, replyID, key := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	assistantUI := []byte(`{"schema":"assistant-ui.v1","blocks":[{"type":"future","data":{"b":2,"a":1}}]}`)
	item, err := scanMessage(staticRow{
		messageID, dialogID, domain.MessageAuthorPersonalTeacher, domain.MessageChannelWeb,
		(*uuid.UUID)(nil), &teacherID, &actionID, []byte(nil), assistantUI, &replyID,
		"plain fallback", []byte(`[]`), domain.MessageStatusActive, 1, int64(2), int64(2), (*int64)(nil), key,
		(*time.Time)(nil), (*time.Time)(nil), now, now,
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := domain.NormalizeAssistantUI(json.RawMessage(assistantUI))
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != messageID || item.PersonalTeacherID != teacherID || item.TeacherTurnSequence != nil || string(item.AssistantUI) != string(want) {
		t.Fatalf("assistant UI scan mismatch: %+v", item)
	}
}

func TestScanMessageCarriesPrivateTeacherTurnSequence(t *testing.T) {
	now := time.Now().UTC()
	messageID, dialogID, senderID, key := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	teacherTurnSequence := int64(4)
	item, err := scanMessage(staticRow{
		messageID, dialogID, domain.MessageAuthorUser, domain.MessageChannelWeb,
		&senderID, (*uuid.UUID)(nil), (*uuid.UUID)(nil), []byte(nil), []byte(nil), (*uuid.UUID)(nil),
		"source", []byte(`[]`), domain.MessageStatusActive, 1, int64(9), int64(12), &teacherTurnSequence, key,
		(*time.Time)(nil), (*time.Time)(nil), now, now,
	})
	if err != nil || item.TeacherTurnSequence == nil || *item.TeacherTurnSequence != teacherTurnSequence {
		t.Fatalf("teacher turn sequence scan mismatch: item=%+v err=%v", item, err)
	}
}

func TestScanMessageReplaysLegacyAndVersionedLessonContexts(t *testing.T) {
	now := time.Now().UTC()
	messageID, dialogID, senderID, key := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	base := func(lessonContext []byte) staticRow {
		return staticRow{
			messageID, dialogID, domain.MessageAuthorUser, domain.MessageChannelWeb,
			&senderID, (*uuid.UUID)(nil), (*uuid.UUID)(nil), lessonContext, []byte(nil), (*uuid.UUID)(nil),
			"question", []byte(`[]`), domain.MessageStatusActive, 1, int64(1), int64(1), (*int64)(nil), key,
			(*time.Time)(nil), (*time.Time)(nil), now, now,
		}
	}

	legacySelection := "  legacy selection\n"
	legacy, err := scanMessage(base([]byte(`{"content_revision":"2026-09-04T13:00:00+05:00","selected_text":"  legacy selection\n"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if legacy.LessonContext == nil || legacy.LessonContext.Schema != "" || legacy.LessonContext.SelectedText == nil ||
		*legacy.LessonContext.SelectedText != legacySelection || legacy.LessonContext.ContentRevision != "2026-09-04T08:00:00Z" {
		t.Fatalf("legacy lesson context scan mismatch: %+v", legacy.LessonContext)
	}

	courseID, lessonID := uuid.New(), uuid.New()
	versioned, err := scanMessage(base([]byte(`{"schema":"lesson-message-context.v1","mode":"lesson_overview","course_id":"` +
		courseID.String() + `","lesson_id":"` + lessonID.String() + `","content_revision":"2026-09-04T08:00:00Z"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if versioned.LessonContext == nil || versioned.LessonContext.Mode != domain.LessonMessageContextOverview ||
		versioned.LessonContext.CourseID == nil || *versioned.LessonContext.CourseID != courseID ||
		versioned.LessonContext.LessonID == nil || *versioned.LessonContext.LessonID != lessonID || versioned.LessonContext.SelectedText != nil {
		t.Fatalf("versioned lesson context scan mismatch: %+v", versioned.LessonContext)
	}

	if _, err := scanMessage(base([]byte(`{"content_revision":"2026-09-04T08:00:00Z","selected_text":"x","unknown":true}`))); err == nil {
		t.Fatal("corrupt lesson context row was accepted")
	}
}
