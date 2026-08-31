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

type MessageAuthorType string

const (
	MessageAuthorUser            MessageAuthorType = "user"
	MessageAuthorPersonalTeacher MessageAuthorType = "personal_teacher"
)

func (t MessageAuthorType) Valid() bool {
	return t == MessageAuthorUser || t == MessageAuthorPersonalTeacher
}

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

const MaxLessonSelectedTextRunes = 12000

// LessonMessageContext anchors a user question to the exact current Course
// revision visible when it was sent. SelectedText is always treated as
// untrusted until Course verifies it.
type LessonMessageContext struct {
	ContentRevision string `json:"content_revision"`
	SelectedText    string `json:"selected_text,omitempty"`
}

func NormalizeLessonMessageContext(input *LessonMessageContext) (*LessonMessageContext, error) {
	if input == nil {
		return nil, nil
	}
	revision, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.ContentRevision))
	if err != nil || revision.IsZero() || !utf8.ValidString(input.SelectedText) ||
		utf8.RuneCountInString(input.SelectedText) > MaxLessonSelectedTextRunes ||
		strings.TrimSpace(input.SelectedText) == "" {
		return nil, fmt.Errorf("%w: invalid lesson message context", ErrValidation)
	}
	return &LessonMessageContext{
		ContentRevision: revision.UTC().Format(time.RFC3339Nano),
		SelectedText:    input.SelectedText,
	}, nil
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
	AuthorType        MessageAuthorType
	SenderID          uuid.UUID
	PersonalTeacherID uuid.UUID
	LearningActionID  *uuid.UUID
	LessonContext     *LessonMessageContext
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
	if m.ID == uuid.Nil || m.DialogID == uuid.Nil || !m.AuthorType.Valid() || m.IdempotencyKey == uuid.Nil ||
		m.Version < 1 || m.MessageSequence < 1 || m.LastEventSequence < m.MessageSequence ||
		m.Status < MessageStatusActive || m.Status > MessageStatusHidden {
		return fmt.Errorf("%w: invalid message identity or state", ErrValidation)
	}
	if (m.AuthorType == MessageAuthorUser && (m.SenderID == uuid.Nil || m.PersonalTeacherID != uuid.Nil)) ||
		(m.AuthorType == MessageAuthorPersonalTeacher && (m.SenderID != uuid.Nil || m.PersonalTeacherID == uuid.Nil)) {
		return fmt.Errorf("%w: invalid message author", ErrValidation)
	}
	if m.LearningActionID != nil && *m.LearningActionID == uuid.Nil {
		return fmt.Errorf("%w: invalid learning action", ErrValidation)
	}
	if m.LessonContext != nil {
		normalized, err := NormalizeLessonMessageContext(m.LessonContext)
		if err != nil || m.AuthorType != MessageAuthorUser || normalized.ContentRevision != m.LessonContext.ContentRevision ||
			normalized.SelectedText != m.LessonContext.SelectedText {
			return fmt.Errorf("%w: invalid lesson message context", ErrValidation)
		}
	}
	if m.ReplyToMessageID != nil && *m.ReplyToMessageID == m.ID {
		return fmt.Errorf("%w: a message cannot reply to itself", ErrValidation)
	}
	if m.Status == MessageStatusDeleted && m.DeletedAt == nil {
		return fmt.Errorf("%w: deleted message requires deletion time", ErrValidation)
	}
	return nil
}
