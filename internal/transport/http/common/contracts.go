package common

import (
	"encoding/json"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	"github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/google/uuid"
)

type MemberResponse struct {
	UserID                     uuid.UUID           `json:"user_id"`
	Role                       domain.MemberRole   `json:"role"`
	Status                     domain.MemberStatus `json:"status"`
	HistoryFromMessageSequence int64               `json:"history_from_message_sequence"`
	LastReadMessageSequence    int64               `json:"last_read_message_sequence"`
	UnreadCount                int64               `json:"unread_count"`
	LastReadAt                 *time.Time          `json:"last_read_at"`
	MutedUntil                 *time.Time          `json:"muted_until"`
	ArchivedAt                 *time.Time          `json:"archived_at"`
	JoinedAt                   time.Time           `json:"joined_at"`
}

type DialogResponse struct {
	ID                 uuid.UUID                 `json:"id"`
	SpaceID            uuid.UUID                 `json:"space_id"`
	Type               domain.DialogType         `json:"type"`
	Status             domain.DialogStatus       `json:"status"`
	Title              string                    `json:"title,omitempty"`
	StudentID          *uuid.UUID                `json:"student_id,omitempty"`
	PersonalTeacherID  *uuid.UUID                `json:"personal_teacher_id,omitempty"`
	ContextType        domain.TeacherContextType `json:"context_type,omitempty"`
	ContextID          *uuid.UUID                `json:"context_id,omitempty"`
	Version            int                       `json:"version"`
	MemberCount        int                       `json:"member_count"`
	MessageCount       int64                     `json:"message_count"`
	MaxMessageSequence int64                     `json:"max_message_sequence"`
	MaxEventSequence   int64                     `json:"max_event_sequence"`
	LastMessageID      *uuid.UUID                `json:"last_message_id"`
	LastMessageAt      *time.Time                `json:"last_message_at"`
	CurrentMember      MemberResponse            `json:"current_member"`
	Members            []MemberResponse          `json:"members,omitempty"`
	CreatedAt          time.Time                 `json:"created_at"`
	UpdatedAt          time.Time                 `json:"updated_at"`
}

func NewDialogResponse(view dialog.View, includeMembers bool) DialogResponse {
	result := DialogResponse{
		ID: view.Dialog.ID, SpaceID: view.Dialog.SpaceID, Type: view.Dialog.Type,
		Status: view.Dialog.Status, Title: view.Dialog.Title, Version: view.Dialog.Version,
		MemberCount: view.Dialog.MemberCount, MessageCount: view.Dialog.MessageCount,
		MaxMessageSequence: view.Dialog.MaxMessageSequence, MaxEventSequence: view.Dialog.MaxEventSequence,
		LastMessageID: view.Dialog.LastMessageID, LastMessageAt: view.Dialog.LastMessageAt,
		CurrentMember: NewMemberResponse(view.CurrentMember), CreatedAt: view.Dialog.CreatedAt, UpdatedAt: view.Dialog.UpdatedAt,
	}
	if view.Dialog.Type == domain.DialogTypeTeacher {
		result.StudentID = UUIDPointer(view.Dialog.StudentID)
		result.PersonalTeacherID = UUIDPointer(view.Dialog.PersonalTeacherID)
		result.ContextType = view.Dialog.TeacherContextType
		result.ContextID = view.Dialog.ContextID
	}
	if includeMembers {
		result.Members = make([]MemberResponse, 0, len(view.Members))
		for _, member := range view.Members {
			result.Members = append(result.Members, NewMemberResponse(member))
		}
	}
	return result
}

func NewMemberResponse(item domain.Member) MemberResponse {
	return MemberResponse{
		UserID: item.UserID, Role: item.Role, Status: item.Status,
		HistoryFromMessageSequence: item.HistoryFromMessageSequence,
		LastReadMessageSequence:    item.LastReadMessageSequence, UnreadCount: item.UnreadCount,
		LastReadAt: item.LastReadAt, MutedUntil: item.MutedUntil, ArchivedAt: item.ArchivedAt, JoinedAt: item.JoinedAt,
	}
}

type AttachmentResponse struct {
	ID               uuid.UUID               `json:"id"`
	Kind             domain.AttachmentKind   `json:"kind"`
	Status           domain.AttachmentStatus `json:"status"`
	MIMEType         string                  `json:"mime_type"`
	SizeBytes        int64                   `json:"size_bytes"`
	Width            *int                    `json:"width,omitempty"`
	Height           *int                    `json:"height,omitempty"`
	OriginalFilename string                  `json:"original_filename"`
}

func NewAttachmentResponse(item domain.Attachment) AttachmentResponse {
	return AttachmentResponse{ID: item.ID, Kind: item.Kind, Status: item.Status, MIMEType: item.MIMEType, SizeBytes: item.SizeBytes, Width: item.Width, Height: item.Height, OriginalFilename: item.OriginalFilename}
}

type MessageResponse struct {
	ID                uuid.UUID                    `json:"id"`
	DialogID          uuid.UUID                    `json:"dialog_id"`
	AuthorType        domain.MessageAuthorType     `json:"author_type"`
	Channel           domain.MessageChannel        `json:"channel"`
	SenderID          *uuid.UUID                   `json:"sender_id"`
	PersonalTeacherID *uuid.UUID                   `json:"personal_teacher_id,omitempty"`
	LearningActionID  *uuid.UUID                   `json:"learning_action_id,omitempty"`
	LessonContext     *domain.LessonMessageContext `json:"lesson_context,omitempty"`
	AssistantUI       json.RawMessage              `json:"assistant_ui,omitempty"`
	ReplyToMessageID  *uuid.UUID                   `json:"reply_to_message_id"`
	Body              string                       `json:"body"`
	Links             []domain.Link                `json:"links"`
	Status            domain.MessageStatus         `json:"status"`
	Version           int                          `json:"version"`
	MessageSequence   int64                        `json:"message_sequence"`
	LastEventSequence int64                        `json:"last_event_sequence"`
	Attachments       []AttachmentResponse         `json:"attachments"`
	EditedAt          *time.Time                   `json:"edited_at"`
	DeletedAt         *time.Time                   `json:"deleted_at"`
	CreatedAt         time.Time                    `json:"created_at"`
	UpdatedAt         time.Time                    `json:"updated_at"`
}

func NewMessageResponse(view message.View) MessageResponse {
	attachments := make([]AttachmentResponse, 0, len(view.Attachments))
	for _, item := range view.Attachments {
		attachments = append(attachments, NewAttachmentResponse(item))
	}
	return MessageResponse{
		ID: view.Message.ID, DialogID: view.Message.DialogID, AuthorType: view.Message.AuthorType, Channel: view.Message.Channel,
		SenderID: UUIDPointer(view.Message.SenderID), PersonalTeacherID: UUIDPointer(view.Message.PersonalTeacherID), LearningActionID: view.Message.LearningActionID,
		LessonContext:    view.Message.LessonContext,
		AssistantUI:      view.Message.AssistantUI,
		ReplyToMessageID: view.Message.ReplyToMessageID, Body: view.Message.Body, Links: view.Message.Links,
		Status: view.Message.Status, Version: view.Message.Version, MessageSequence: view.Message.MessageSequence,
		LastEventSequence: view.Message.LastEventSequence, Attachments: attachments,
		EditedAt: view.Message.EditedAt, DeletedAt: view.Message.DeletedAt,
		CreatedAt: view.Message.CreatedAt, UpdatedAt: view.Message.UpdatedAt,
	}
}

func UUIDPointer(value uuid.UUID) *uuid.UUID {
	if value == uuid.Nil {
		return nil
	}
	copy := value
	return &copy
}
