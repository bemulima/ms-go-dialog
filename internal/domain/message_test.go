package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMessage_ValidatesFirstClassTeacherAuthor(t *testing.T) {
	now := time.Now().UTC()
	sourceID := uuid.New()
	item := Message{
		ID: uuid.New(), DialogID: uuid.New(), AuthorType: MessageAuthorPersonalTeacher, Channel: MessageChannelWeb,
		PersonalTeacherID: uuid.New(), ReplyToMessageID: &sourceID, Body: "response",
		Status: MessageStatusActive, Version: 1, MessageSequence: 2, LastEventSequence: 2,
		IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now,
	}
	if err := item.Validate(); err != nil {
		t.Fatalf("valid teacher author rejected: %v", err)
	}
	item.SenderID = uuid.New()
	if err := item.Validate(); err == nil {
		t.Fatal("teacher message with user sender was accepted")
	}
}

func TestMessageContent_ValidatesLinksAndAttachments(t *testing.T) {
	policy := DefaultPolicy()
	content := AnalyzeMessageContent("See https://example.com/docs.", 1, 1)
	if err := content.Validate(policy); err != nil {
		t.Fatalf("valid content rejected: %v", err)
	}
	if len(content.Links) != 1 || content.Links[0].URL != "https://example.com/docs" {
		t.Fatalf("link extraction mismatch: %+v", content.Links)
	}

	policy.AllowLinks = false
	if err := content.Validate(policy); !errors.Is(err, ErrLinksDisabled) {
		t.Fatalf("expected links disabled, got %v", err)
	}
}

func TestMessageContent_RejectsRawHTMLAndEmptyContent(t *testing.T) {
	policy := DefaultPolicy()
	if err := AnalyzeMessageContent("<script>alert(1)</script>", 0, 0).Validate(policy); !errors.Is(err, ErrInvalidContent) {
		t.Fatalf("raw HTML accepted: %v", err)
	}
	if err := AnalyzeMessageContent("   ", 0, 0).Validate(policy); !errors.Is(err, ErrInvalidContent) {
		t.Fatalf("empty content accepted: %v", err)
	}
}

func TestMessageContent_SeparatesImageAndFilePolicy(t *testing.T) {
	policy := DefaultPolicy()
	policy.AllowImages = false
	if err := AnalyzeMessageContent("", 1, 0).Validate(policy); !errors.Is(err, ErrImagesDisabled) {
		t.Fatalf("expected images disabled, got %v", err)
	}
	policy.AllowImages = true
	policy.AllowFiles = false
	if err := AnalyzeMessageContent("", 0, 1).Validate(policy); !errors.Is(err, ErrFilesDisabled) {
		t.Fatalf("expected files disabled, got %v", err)
	}
}

func TestLessonMessageContextNormalizesLegacyRevisionAndPreservesSelection(t *testing.T) {
	selection := "  for i := 0; i < n; i++\n"
	context, err := NormalizeLessonMessageContext(&LessonMessageContext{
		ContentRevision: "2026-08-31T15:00:00+03:00",
		SelectedText:    &selection,
	})
	if err != nil {
		t.Fatalf("valid lesson context rejected: %v", err)
	}
	if context.ContentRevision != "2026-08-31T12:00:00Z" {
		t.Fatalf("revision was not canonicalized: %s", context.ContentRevision)
	}
	if context.SelectedText == nil || *context.SelectedText != selection {
		t.Fatalf("selected text was reformatted: %#v", context.SelectedText)
	}
	blank := " "
	context.SelectedText = &blank
	if _, err := NormalizeLessonMessageContext(context); !errors.Is(err, ErrValidation) {
		t.Fatalf("whitespace selection error = %v", err)
	}
}

func TestLessonMessageContextV1ModesAndUnicodeBound(t *testing.T) {
	courseID, lessonID := uuid.New(), uuid.New()
	overview, err := NormalizeLessonMessageContext(&LessonMessageContext{
		Schema: LessonMessageContextSchemaV1, Mode: LessonMessageContextOverview,
		CourseID: &courseID, LessonID: &lessonID, ContentRevision: "2026-08-31T15:00:00.123456789+03:00",
	})
	if err != nil {
		t.Fatalf("valid overview rejected: %v", err)
	}
	if overview.SelectedText != nil || overview.ContentRevision != "2026-08-31T12:00:00.123456789Z" {
		t.Fatalf("overview was not normalized: %+v", overview)
	}

	selection := "  точный фрагмент\n"
	selected, err := NormalizeLessonMessageContext(&LessonMessageContext{
		Schema: LessonMessageContextSchemaV1, Mode: LessonMessageContextSelection,
		CourseID: &courseID, LessonID: &lessonID, ContentRevision: "2026-08-31T12:00:00Z", SelectedText: &selection,
	})
	if err != nil || selected.SelectedText == nil || *selected.SelectedText != selection {
		t.Fatalf("selection was rejected or reformatted: context=%+v err=%v", selected, err)
	}

	oversized := strings.Repeat("界", MaxLessonSelectedTextRunes+1)
	selected.SelectedText = &oversized
	if _, err := NormalizeLessonMessageContext(selected); !errors.Is(err, ErrValidation) {
		t.Fatalf("oversized Unicode selection error = %v", err)
	}
}

func TestLessonMessageContextV1RejectsUnknownAndCrossModeFields(t *testing.T) {
	courseID, lessonID := uuid.New(), uuid.New()
	inputs := []string{
		`{"schema":"lesson-message-context.v1","mode":"lesson_overview","course_id":"` + courseID.String() + `","lesson_id":"` + lessonID.String() + `","content_revision":"2026-08-31T12:00:00Z","selected_text":null}`,
		`{"schema":"lesson-message-context.v1","mode":"selection","course_id":"` + courseID.String() + `","lesson_id":"` + lessonID.String() + `","content_revision":"2026-08-31T12:00:00Z"}`,
		`{"schema":"lesson-message-context.v1","mode":"selection","course_id":"` + courseID.String() + `","lesson_id":"` + lessonID.String() + `","content_revision":"2026-08-31T12:00:00Z","selected_text":"x","unknown":true}`,
		`{"schema":"lesson-message-context.v1","mode":"lesson_overview","course_id":"` + courseID.String() + `","lesson_id":"` + lessonID.String() + `","content_revision":" 2026-08-31T12:00:00Z"}`,
	}
	for _, input := range inputs {
		var context LessonMessageContext
		if err := json.Unmarshal([]byte(input), &context); err == nil {
			if _, err := NormalizeLessonMessageContext(&context); !errors.Is(err, ErrValidation) {
				t.Fatalf("invalid context accepted: %s", input)
			}
		} else if !errors.Is(err, ErrValidation) {
			t.Fatalf("unexpected decode error for %s: %v", input, err)
		}
	}
}

func TestMessageRejectsLessonContextOnTeacherResponse(t *testing.T) {
	now := time.Now().UTC()
	courseID, lessonID := uuid.New(), uuid.New()
	item := Message{
		ID: uuid.New(), DialogID: uuid.New(), AuthorType: MessageAuthorPersonalTeacher, Channel: MessageChannelWeb,
		PersonalTeacherID: uuid.New(), Body: "response", LessonContext: &LessonMessageContext{
			Schema: LessonMessageContextSchemaV1, Mode: LessonMessageContextOverview, CourseID: &courseID, LessonID: &lessonID,
			ContentRevision: now.Format(time.RFC3339Nano),
		},
		Status: MessageStatusActive, Version: 1, MessageSequence: 1, LastEventSequence: 1,
		IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now,
	}
	if err := item.Validate(); !errors.Is(err, ErrValidation) {
		t.Fatalf("teacher lesson context error = %v", err)
	}
}

func TestNormalizeAssistantUIBoundsOpaqueVersionedEnvelope(t *testing.T) {
	valid := json.RawMessage(`{"blocks":[{"type":"future_pedagogy","data":{"nested":[1,true,"opaque"]}}],"schema":"assistant-ui.v1"}`)
	normalized, err := NormalizeAssistantUI(valid)
	if err != nil {
		t.Fatalf("valid opaque envelope rejected: %v", err)
	}
	if string(normalized) != `{"blocks":[{"data":{"nested":[1,true,"opaque"]},"type":"future_pedagogy"}],"schema":"assistant-ui.v1"}` {
		t.Fatalf("assistant UI was not canonicalized: %s", normalized)
	}

	invalid := []json.RawMessage{
		json.RawMessage(`{"schema":"assistant-ui.v2","blocks":[]}`),
		json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[1]}`),
		json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[],"extra":true}`),
		json.RawMessage(`{"schema":"assistant-ui.v1","blocks":null}`),
	}
	for _, candidate := range invalid {
		if _, err := NormalizeAssistantUI(candidate); !errors.Is(err, ErrValidation) {
			t.Fatalf("invalid assistant UI accepted: %s err=%v", candidate, err)
		}
	}

	blocks := make([]map[string]any, MaxAssistantUIBlocks+1)
	for index := range blocks {
		blocks[index] = map[string]any{"opaque": index}
	}
	tooMany, err := json.Marshal(map[string]any{"schema": AssistantUISchemaV1, "blocks": blocks})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeAssistantUI(tooMany); !errors.Is(err, ErrValidation) {
		t.Fatalf("too many assistant UI blocks accepted: %v", err)
	}

	oversized := json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[{"data":"` + strings.Repeat("x", MaxAssistantUIBytes) + `"}]}`)
	if _, err := NormalizeAssistantUI(oversized); !errors.Is(err, ErrValidation) {
		t.Fatalf("oversized assistant UI accepted: %v", err)
	}
}

func TestMessageRejectsAssistantUIOnUserMessage(t *testing.T) {
	now := time.Now().UTC()
	item := Message{
		ID: uuid.New(), DialogID: uuid.New(), AuthorType: MessageAuthorUser, Channel: MessageChannelWeb,
		SenderID: uuid.New(), Body: "fallback", AssistantUI: json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[]}`),
		Status: MessageStatusActive, Version: 1, MessageSequence: 1, LastEventSequence: 1,
		IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now,
	}
	if err := item.Validate(); !errors.Is(err, ErrValidation) {
		t.Fatalf("user-authored assistant UI error = %v", err)
	}
}
