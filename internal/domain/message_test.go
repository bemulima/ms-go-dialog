package domain

import (
	"errors"
	"testing"
)

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
