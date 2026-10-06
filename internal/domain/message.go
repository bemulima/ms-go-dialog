package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

type MessageChannel string

const (
	MessageChannelWeb      MessageChannel = "web"
	MessageChannelTelegram MessageChannel = "telegram"
)

func (channel MessageChannel) Valid() bool {
	return channel == MessageChannelWeb || channel == MessageChannelTelegram
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

const LessonMessageContextSchemaV1 = "lesson-message-context.v1"
const LessonMessageContextSchemaV2 = "lesson-message-context.v2"

var lessonContentDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type LessonMessageContextMode string

const (
	LessonMessageContextOverview  LessonMessageContextMode = "lesson_overview"
	LessonMessageContextSelection LessonMessageContextMode = "selection"
)

const (
	AssistantUISchemaV1  = "assistant-ui.v1"
	MaxAssistantUIBlocks = 32
	MaxAssistantUIBytes  = 64 * 1024
)

// NormalizeAssistantUI validates only the Dialog-owned transport envelope and
// returns a stable JSON representation. Block discriminators and data remain
// opaque and are validated by their pedagogical owner and consuming frontend.
func NormalizeAssistantUI(input json.RawMessage) (json.RawMessage, error) {
	if len(input) == 0 {
		return nil, nil
	}
	if len(input) > MaxAssistantUIBytes || !json.Valid(input) {
		return nil, fmt.Errorf("%w: invalid assistant UI envelope", ErrValidation)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(input, &root); err != nil || root == nil || len(root) != 2 {
		return nil, fmt.Errorf("%w: invalid assistant UI envelope", ErrValidation)
	}
	var schema string
	if err := json.Unmarshal(root["schema"], &schema); err != nil || schema != AssistantUISchemaV1 {
		return nil, fmt.Errorf("%w: unsupported assistant UI schema", ErrValidation)
	}
	var blocks []json.RawMessage
	if len(root["blocks"]) == 0 || bytes.Equal(bytes.TrimSpace(root["blocks"]), []byte("null")) {
		return nil, fmt.Errorf("%w: invalid assistant UI blocks", ErrValidation)
	}
	if err := json.Unmarshal(root["blocks"], &blocks); err != nil || len(blocks) > MaxAssistantUIBlocks {
		return nil, fmt.Errorf("%w: invalid assistant UI blocks", ErrValidation)
	}
	for _, block := range blocks {
		trimmed := bytes.TrimSpace(block)
		if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
			return nil, fmt.Errorf("%w: assistant UI blocks must be objects", ErrValidation)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: invalid assistant UI envelope", ErrValidation)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("%w: invalid assistant UI envelope", ErrValidation)
	}
	canonical, err := json.Marshal(value)
	if err != nil || len(canonical) > MaxAssistantUIBytes {
		return nil, fmt.Errorf("%w: assistant UI envelope is too large", ErrValidation)
	}
	return canonical, nil
}

// LessonMessageContext preserves the displayed revision anchor. V2 identifies a
// frozen Student assignment; Teacher verifies its authoritative content and any
// selection. Dialog validates transport shape and the contextual lesson binding.
type LessonMessageContext struct {
	Schema             string                   `json:"schema,omitempty"`
	Mode               LessonMessageContextMode `json:"mode,omitempty"`
	CourseID           *uuid.UUID               `json:"course_id,omitempty"`
	LessonID           *uuid.UUID               `json:"lesson_id,omitempty"`
	ContentRevision    string                   `json:"content_revision"`
	SelectedText       *string                  `json:"selected_text,omitempty"`
	LearningPathID     *uuid.UUID               `json:"learning_path_id,omitempty"`
	LearningPathItemID *uuid.UUID               `json:"learning_path_item_id,omitempty"`
	ContentDigest      string                   `json:"content_digest,omitempty"`

	selectedTextPresent bool
}

func (context *LessonMessageContext) UnmarshalJSON(input []byte) error {
	// Decode keys separately: encoding/json's struct decoder otherwise accepts
	// case-insensitive aliases and silently overwrites duplicate fields.
	keyDecoder := json.NewDecoder(bytes.NewReader(input))
	opening, err := keyDecoder.Token()
	if err != nil || opening != json.Delim('{') {
		return fmt.Errorf("%w: invalid lesson message context", ErrValidation)
	}
	fields := make(map[string]json.RawMessage)
	for keyDecoder.More() {
		token, err := keyDecoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return fmt.Errorf("%w: invalid lesson message context field", ErrValidation)
		}
		if _, duplicate := fields[key]; duplicate {
			return fmt.Errorf("%w: duplicate lesson message context field", ErrValidation)
		}
		var value json.RawMessage
		if err := keyDecoder.Decode(&value); err != nil {
			return fmt.Errorf("%w: invalid lesson message context field", ErrValidation)
		}
		fields[key] = value
	}
	if closing, err := keyDecoder.Token(); err != nil || closing != json.Delim('}') {
		return fmt.Errorf("%w: invalid lesson message context", ErrValidation)
	}
	if err := keyDecoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("%w: invalid lesson message context", ErrValidation)
	}
	type wireContext struct {
		Schema             string                   `json:"schema"`
		Mode               LessonMessageContextMode `json:"mode"`
		CourseID           *uuid.UUID               `json:"course_id"`
		LessonID           *uuid.UUID               `json:"lesson_id"`
		ContentRevision    string                   `json:"content_revision"`
		SelectedText       *string                  `json:"selected_text"`
		LearningPathID     *uuid.UUID               `json:"learning_path_id"`
		LearningPathItemID *uuid.UUID               `json:"learning_path_item_id"`
		ContentDigest      string                   `json:"content_digest"`
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	var decoded wireContext
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("%w: invalid lesson message context", ErrValidation)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("%w: invalid lesson message context", ErrValidation)
	}
	if decoded.Schema == LessonMessageContextSchemaV2 {
		requiredKeys := []string{"schema", "mode", "course_id", "lesson_id", "content_revision", "learning_path_id", "learning_path_item_id", "content_digest"}
		expectedCount := len(requiredKeys)
		if decoded.Mode == LessonMessageContextSelection {
			requiredKeys = append(requiredKeys, "selected_text")
			expectedCount++
		}
		if len(fields) != expectedCount || !utf8.Valid(input) {
			return fmt.Errorf("%w: invalid frozen lesson message context fields", ErrValidation)
		}
		for _, key := range requiredKeys {
			if _, present := fields[key]; !present {
				return fmt.Errorf("%w: missing frozen lesson message context field", ErrValidation)
			}
		}
	}
	for key, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("%w: null lesson message context field", ErrValidation)
		}
		if decoded.Schema != LessonMessageContextSchemaV2 &&
			(key == "learning_path_id" || key == "learning_path_item_id" || key == "content_digest") {
			return fmt.Errorf("%w: cross-schema lesson message context field", ErrValidation)
		}
	}
	_, selectedTextPresent := fields["selected_text"]
	*context = LessonMessageContext{
		Schema: decoded.Schema, Mode: decoded.Mode, CourseID: decoded.CourseID, LessonID: decoded.LessonID,
		ContentRevision: decoded.ContentRevision, SelectedText: decoded.SelectedText,
		LearningPathID: decoded.LearningPathID, LearningPathItemID: decoded.LearningPathItemID, ContentDigest: decoded.ContentDigest,
		selectedTextPresent: selectedTextPresent,
	}
	_, err = NormalizeLessonMessageContext(context)
	return err
}

func NormalizeLessonMessageContext(input *LessonMessageContext) (*LessonMessageContext, error) {
	if input == nil {
		return nil, nil
	}
	revisionText := strings.TrimSpace(input.ContentRevision)
	if input.Schema != "" && revisionText != input.ContentRevision {
		return nil, fmt.Errorf("%w: invalid lesson message context revision", ErrValidation)
	}
	if input.Schema == LessonMessageContextSchemaV2 {
		revision, err := uuid.Parse(revisionText)
		if err != nil || revision == uuid.Nil || revision.String() != revisionText ||
			input.LearningPathID == nil || *input.LearningPathID == uuid.Nil ||
			input.LearningPathItemID == nil || *input.LearningPathItemID == uuid.Nil ||
			!lessonContentDigestPattern.MatchString(input.ContentDigest) {
			return nil, fmt.Errorf("%w: invalid frozen lesson message context", ErrValidation)
		}
	} else {
		if input.LearningPathID != nil || input.LearningPathItemID != nil || input.ContentDigest != "" {
			return nil, fmt.Errorf("%w: cross-schema lesson message context", ErrValidation)
		}
		revision, err := time.Parse(time.RFC3339Nano, revisionText)
		if err != nil || revision.IsZero() {
			return nil, fmt.Errorf("%w: invalid lesson message context", ErrValidation)
		}
		revisionText = revision.UTC().Format(time.RFC3339Nano)
	}
	selectedTextPresent := input.selectedTextPresent || input.SelectedText != nil
	validSelection := input.SelectedText != nil && utf8.ValidString(*input.SelectedText) &&
		utf8.RuneCountInString(*input.SelectedText) <= MaxLessonSelectedTextRunes && strings.TrimSpace(*input.SelectedText) != ""
	if input.Schema == "" {
		if input.Mode != "" || input.CourseID != nil || input.LessonID != nil || !selectedTextPresent || !validSelection {
			return nil, fmt.Errorf("%w: invalid legacy lesson message context", ErrValidation)
		}
	} else if (input.Schema != LessonMessageContextSchemaV1 && input.Schema != LessonMessageContextSchemaV2) || input.CourseID == nil || *input.CourseID == uuid.Nil ||
		input.LessonID == nil || *input.LessonID == uuid.Nil ||
		(input.Mode == LessonMessageContextOverview && selectedTextPresent) ||
		(input.Mode == LessonMessageContextSelection && (!selectedTextPresent || !validSelection)) ||
		(input.Mode != LessonMessageContextOverview && input.Mode != LessonMessageContextSelection) {
		return nil, fmt.Errorf("%w: invalid versioned lesson message context", ErrValidation)
	}
	result := &LessonMessageContext{
		Schema: input.Schema, Mode: input.Mode, ContentRevision: revisionText, ContentDigest: input.ContentDigest,
		selectedTextPresent: selectedTextPresent,
	}
	if input.CourseID != nil {
		courseID := *input.CourseID
		result.CourseID = &courseID
	}
	if input.LessonID != nil {
		lessonID := *input.LessonID
		result.LessonID = &lessonID
	}
	if input.LearningPathID != nil {
		pathID := *input.LearningPathID
		result.LearningPathID = &pathID
	}
	if input.LearningPathItemID != nil {
		itemID := *input.LearningPathItemID
		result.LearningPathItemID = &itemID
	}
	if input.SelectedText != nil {
		selectedText := *input.SelectedText
		result.SelectedText = &selectedText
	}
	return result, nil
}

func LessonMessageContextsEqual(first, second *LessonMessageContext) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return first.Schema == second.Schema && first.Mode == second.Mode && sameOptionalUUID(first.CourseID, second.CourseID) &&
		sameOptionalUUID(first.LessonID, second.LessonID) && first.ContentRevision == second.ContentRevision &&
		sameOptionalString(first.SelectedText, second.SelectedText) &&
		sameOptionalUUID(first.LearningPathID, second.LearningPathID) &&
		sameOptionalUUID(first.LearningPathItemID, second.LearningPathItemID) && first.ContentDigest == second.ContentDigest
}

func sameOptionalUUID(first, second *uuid.UUID) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return *first == *second
}

func sameOptionalString(first, second *string) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return *first == *second
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
	Channel           MessageChannel
	SenderID          uuid.UUID
	PersonalTeacherID uuid.UUID
	LearningActionID  *uuid.UUID
	LessonContext     *LessonMessageContext
	AssistantUI       json.RawMessage
	ReplyToMessageID  *uuid.UUID
	Body              string
	Links             []Link
	Status            MessageStatus
	Version           int
	MessageSequence   int64
	LastEventSequence int64
	// TeacherTurnSequence is private durable ordering evidence for a
	// student-authored teacher request. It is intentionally not a public REST,
	// WebSocket, or ordinary lifecycle-event field.
	TeacherTurnSequence *int64
	IdempotencyKey      uuid.UUID
	EditedAt            *time.Time
	DeletedAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (m Message) Validate() error {
	if m.ID == uuid.Nil || m.DialogID == uuid.Nil || !m.AuthorType.Valid() || !m.Channel.Valid() || m.IdempotencyKey == uuid.Nil ||
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
		if err != nil || m.AuthorType != MessageAuthorUser || !LessonMessageContextsEqual(normalized, m.LessonContext) {
			return fmt.Errorf("%w: invalid lesson message context", ErrValidation)
		}
	}
	if len(m.AssistantUI) > 0 {
		if _, err := NormalizeAssistantUI(m.AssistantUI); err != nil || m.AuthorType != MessageAuthorPersonalTeacher || m.Status == MessageStatusDeleted {
			return fmt.Errorf("%w: invalid assistant UI message binding", ErrValidation)
		}
	}
	if m.ReplyToMessageID != nil && *m.ReplyToMessageID == m.ID {
		return fmt.Errorf("%w: a message cannot reply to itself", ErrValidation)
	}
	if m.TeacherTurnSequence != nil && *m.TeacherTurnSequence < 1 {
		return fmt.Errorf("%w: invalid teacher turn sequence", ErrValidation)
	}
	if m.Status == MessageStatusDeleted && m.DeletedAt == nil {
		return fmt.Errorf("%w: deleted message requires deletion time", ErrValidation)
	}
	return nil
}
