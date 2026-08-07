package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AttachmentKind int16

const (
	AttachmentKindImage AttachmentKind = iota + 1
	AttachmentKindFile
)

type AttachmentStatus int16

const (
	AttachmentStatusPending AttachmentStatus = iota + 1
	AttachmentStatusScanning
	AttachmentStatusProcessing
	AttachmentStatusReady
	AttachmentStatusFailed
	AttachmentStatusDeleted
)

type Attachment struct {
	ID                      uuid.UUID
	DialogID                uuid.UUID
	MessageID               *uuid.UUID
	UploaderID              uuid.UUID
	FileStorageID           uuid.UUID
	Kind                    AttachmentKind
	Status                  AttachmentStatus
	MIMEType                string
	SizeBytes               int64
	Width                   *int
	Height                  *int
	ChecksumSHA256          []byte
	OriginalFilename        string
	ExpiresAt               time.Time
	ActivatedAt             *time.Time
	DeletedAt               *time.Time
	ActivationAttempts      int
	ActivationNextAttemptAt *time.Time
	DeleteAttempts          int
	DeleteNextAttemptAt     *time.Time
	LastError               string
	StorageDeletedAt        *time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (a Attachment) Validate(policy Policy) error {
	if a.ID == uuid.Nil || a.DialogID == uuid.Nil || a.UploaderID == uuid.Nil || a.FileStorageID == uuid.Nil ||
		a.Status < AttachmentStatusPending || a.Status > AttachmentStatusDeleted || strings.TrimSpace(a.OriginalFilename) == "" ||
		a.SizeBytes < 1 || !a.ExpiresAt.After(a.CreatedAt) || a.ActivationAttempts < 0 || a.DeleteAttempts < 0 {
		return fmt.Errorf("%w: invalid identity, lifecycle, or metadata", ErrInvalidAttachment)
	}
	if len(a.ChecksumSHA256) != 0 && len(a.ChecksumSHA256) != 32 {
		return fmt.Errorf("%w: checksum must be SHA-256", ErrInvalidAttachment)
	}
	switch a.Kind {
	case AttachmentKindImage:
		if !policy.AllowImages || a.SizeBytes > policy.MaxImageBytes || (a.MIMEType != "image/jpeg" && a.MIMEType != "image/png" && a.MIMEType != "image/webp") || a.Width == nil || a.Height == nil || *a.Width < 1 || *a.Height < 1 || *a.Width > 32768 || *a.Height > 32768 {
			return ErrImagesDisabled
		}
	case AttachmentKindFile:
		if !policy.AllowFiles || a.SizeBytes > policy.MaxFileBytes || !policy.AllowsFileMIME(a.MIMEType) || a.Width != nil || a.Height != nil {
			return ErrFilesDisabled
		}
	default:
		return fmt.Errorf("%w: unsupported attachment kind", ErrInvalidAttachment)
	}
	if a.Status == AttachmentStatusReady && (a.MessageID == nil || a.ActivatedAt == nil) {
		return fmt.Errorf("%w: ready attachment must be bound and activated", ErrInvalidAttachment)
	}
	if a.Status == AttachmentStatusDeleted && a.DeletedAt == nil {
		return fmt.Errorf("%w: deleted attachment requires deletion time", ErrInvalidAttachment)
	}
	return nil
}
