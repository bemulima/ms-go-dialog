package domain

import (
	"errors"
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

func TestLessonMessageContextNormalizesRevisionAndBoundsSelection(t *testing.T) {
	context, err := NormalizeLessonMessageContext(&LessonMessageContext{
		ContentRevision: "2026-08-31T15:00:00+03:00",
		SelectedText:    "for i := 0; i < n; i++",
	})
	if err != nil {
		t.Fatalf("valid lesson context rejected: %v", err)
	}
	if context.ContentRevision != "2026-08-31T12:00:00Z" {
		t.Fatalf("revision was not canonicalized: %s", context.ContentRevision)
	}
	context.SelectedText = " "
	if _, err := NormalizeLessonMessageContext(context); !errors.Is(err, ErrValidation) {
		t.Fatalf("whitespace selection error = %v", err)
	}
}

func TestMessageRejectsLessonContextOnTeacherResponse(t *testing.T) {
	now := time.Now().UTC()
	item := Message{
		ID: uuid.New(), DialogID: uuid.New(), AuthorType: MessageAuthorPersonalTeacher, Channel: MessageChannelWeb,
		PersonalTeacherID: uuid.New(), Body: "response", LessonContext: &LessonMessageContext{ContentRevision: now.Format(time.RFC3339Nano)},
		Status: MessageStatusActive, Version: 1, MessageSequence: 1, LastEventSequence: 1,
		IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now,
	}
	if err := item.Validate(); !errors.Is(err, ErrValidation) {
		t.Fatalf("teacher lesson context error = %v", err)
	}
}
