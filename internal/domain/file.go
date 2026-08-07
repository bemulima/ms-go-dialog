package domain

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"path"
	"strings"

	_ "golang.org/x/image/webp"
)

type FileMetadata struct {
	Kind             AttachmentKind
	MIMEType         string
	SizeBytes        int64
	Width            *int
	Height           *int
	ChecksumSHA256   []byte
	OriginalFilename string
}

func InspectAttachment(data []byte, originalFilename string, policy Policy) (FileMetadata, error) {
	if err := policy.Validate(); err != nil {
		return FileMetadata{}, err
	}
	filename := path.Base(strings.ReplaceAll(strings.TrimSpace(originalFilename), `\`, "/"))
	if filename == "" || filename == "." || strings.ContainsRune(filename, '\x00') || len(filename) > 255 {
		return FileMetadata{}, fmt.Errorf("%w: original filename is invalid", ErrInvalidAttachment)
	}
	if len(data) == 0 || int64(len(data)) > HardMaxFileBytes {
		return FileMetadata{}, fmt.Errorf("%w: attachment size is outside hard limits", ErrInvalidAttachment)
	}
	digest := sha256.Sum256(data)
	config, format, imageErr := image.DecodeConfig(bytes.NewReader(data))
	if imageErr == nil {
		mimeType := map[string]string{"jpeg": "image/jpeg", "png": "image/png", "webp": "image/webp"}[format]
		if mimeType == "" || !policy.AllowImages || policy.MaxAttachments == 0 || int64(len(data)) > policy.MaxImageBytes {
			return FileMetadata{}, ErrImagesDisabled
		}
		if config.Width < 1 || config.Width > 32768 || config.Height < 1 || config.Height > 32768 {
			return FileMetadata{}, fmt.Errorf("%w: image dimensions are outside limits", ErrInvalidAttachment)
		}
		width, height := config.Width, config.Height
		return FileMetadata{Kind: AttachmentKindImage, MIMEType: mimeType, SizeBytes: int64(len(data)), Width: &width, Height: &height, ChecksumSHA256: digest[:], OriginalFilename: filename}, nil
	}
	if !policy.AllowFiles || policy.MaxAttachments == 0 || int64(len(data)) > policy.MaxFileBytes {
		return FileMetadata{}, ErrFilesDisabled
	}
	mimeType := strings.Split(http.DetectContentType(data[:min(len(data), 512)]), ";")[0]
	if strings.EqualFold(path.Ext(filename), ".csv") && mimeType == "text/plain" {
		mimeType = "text/csv"
	}
	if !policy.AllowsFileMIME(mimeType) {
		return FileMetadata{}, fmt.Errorf("%w: MIME type %s is not allowed", ErrInvalidAttachment, mimeType)
	}
	return FileMetadata{Kind: AttachmentKindFile, MIMEType: mimeType, SizeBytes: int64(len(data)), ChecksumSHA256: digest[:], OriginalFilename: filename}, nil
}
