package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	rawHTMLPattern  = regexp.MustCompile(`(?i)<\s*/?\s*[a-z!][^>]*>`)
	httpLinkPattern = regexp.MustCompile(`(?i)https?://[^\s<>()]+`)
)

type MessageStatus int16

const (
	MessageStatusActive MessageStatus = iota + 1
	MessageStatusDeleted
	MessageStatusHidden
)

type Link struct {
	URL string `json:"url"`
}

type MessageContent struct {
	Body            string
	Links           []Link
	AttachmentCount int
	ImageCount      int
	FileCount       int
	ContainsRawHTML bool
}

func AnalyzeMessageContent(body string, imageCount, fileCount int) MessageContent {
	matches := httpLinkPattern.FindAllString(body, -1)
	links := make([]Link, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		candidate := strings.TrimRight(match, `.,;:!?)]}`)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		links = append(links, Link{URL: candidate})
	}
	return MessageContent{
		Body: body, Links: links, ImageCount: imageCount, FileCount: fileCount,
		AttachmentCount: imageCount + fileCount, ContainsRawHTML: rawHTMLPattern.MatchString(body),
	}
}

func (c MessageContent) Validate(policy Policy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if c.ContainsRawHTML || utf8.RuneCountInString(c.Body) > policy.MaxBodyLength || c.ImageCount < 0 || c.FileCount < 0 || c.AttachmentCount != c.ImageCount+c.FileCount || c.AttachmentCount > int(policy.MaxAttachments) {
		return fmt.Errorf("%w: body or attachment limits are invalid", ErrInvalidContent)
	}
	if c.ImageCount > 0 && !policy.AllowImages {
		return ErrImagesDisabled
	}
	if c.FileCount > 0 && !policy.AllowFiles {
		return ErrFilesDisabled
	}
	if strings.TrimSpace(c.Body) == "" && c.AttachmentCount == 0 {
		return fmt.Errorf("%w: body or attachment is required", ErrInvalidContent)
	}
	if len(c.Links) > 0 && !policy.AllowLinks {
		return ErrLinksDisabled
	}
	for _, link := range c.Links {
		parsed, err := url.ParseRequestURI(link.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("%w: links must be absolute HTTP(S) URLs", ErrInvalidContent)
		}
	}
	return nil
}

type Message struct {
	ID                uuid.UUID
	DialogID          uuid.UUID
	SenderID          uuid.UUID
	ReplyToMessageID  *uuid.UUID
	Body              string
	Links             []Link
	Status            MessageStatus
	Version           int
	MessageSequence   int64
	LastEventSequence int64
	IdempotencyKey    uuid.UUID
	EditedAt          *time.Time
	DeletedAt         *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (m Message) Validate() error {
	if m.ID == uuid.Nil || m.DialogID == uuid.Nil || m.SenderID == uuid.Nil || m.IdempotencyKey == uuid.Nil ||
		m.Version < 1 || m.MessageSequence < 1 || m.LastEventSequence < m.MessageSequence ||
		m.Status < MessageStatusActive || m.Status > MessageStatusHidden {
		return fmt.Errorf("%w: invalid message identity or state", ErrValidation)
	}
	if m.ReplyToMessageID != nil && *m.ReplyToMessageID == m.ID {
		return fmt.Errorf("%w: a message cannot reply to itself", ErrValidation)
	}
	if m.Status == MessageStatusDeleted && m.DeletedAt == nil {
		return fmt.Errorf("%w: deleted message requires deletion time", ErrValidation)
	}
	return nil
}
