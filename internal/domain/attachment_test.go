package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAttachment_ValidatesImageAndGenericFileSeparately(t *testing.T) {
	now := time.Now().UTC()
	width, height := 100, 50
	image := Attachment{
		ID: uuid.New(), DialogID: uuid.New(), UploaderID: uuid.New(), FileStorageID: uuid.New(),
		Kind: AttachmentKindImage, Status: AttachmentStatusPending, MIMEType: "image/png", SizeBytes: 100,
		Width: &width, Height: &height, OriginalFilename: "image.png", CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := image.Validate(DefaultPolicy()); err != nil {
		t.Fatalf("valid image rejected: %v", err)
	}
	file := Attachment{
		ID: uuid.New(), DialogID: uuid.New(), UploaderID: uuid.New(), FileStorageID: uuid.New(),
		Kind: AttachmentKindFile, Status: AttachmentStatusPending, MIMEType: "application/pdf", SizeBytes: 100,
		OriginalFilename: "document.pdf", CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := file.Validate(DefaultPolicy()); err != nil {
		t.Fatalf("valid file rejected: %v", err)
	}
	file.MIMEType = "application/x-executable"
	if err := file.Validate(DefaultPolicy()); !errors.Is(err, ErrFilesDisabled) {
		t.Fatalf("unsafe file accepted: %v", err)
	}
}
