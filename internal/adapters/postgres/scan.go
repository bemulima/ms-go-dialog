package postgres

import (
	"encoding/json"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const spaceColumns = `id, key, name, status, allowed_origins,
allow_personal, allow_groups, allow_images, allow_files, allow_links,
max_group_members, max_body_length, max_attachments, max_image_bytes, max_file_bytes,
allowed_file_mime_types, edit_window_seconds, created_by, created_at, updated_at`

func scanSpace(row pgx.Row) (domain.Space, error) {
	var item domain.Space
	err := row.Scan(
		&item.ID, &item.Key, &item.Name, &item.Status, &item.AllowedOrigins,
		&item.Policy.AllowPersonal, &item.Policy.AllowGroups, &item.Policy.AllowImages, &item.Policy.AllowFiles, &item.Policy.AllowLinks,
		&item.Policy.MaxGroupMembers, &item.Policy.MaxBodyLength, &item.Policy.MaxAttachments,
		&item.Policy.MaxImageBytes, &item.Policy.MaxFileBytes, &item.Policy.AllowedFileMIMETypes,
		&item.Policy.EditWindowSeconds, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, mapError(err)
}

const dialogColumns = `id, space_id, type, status, personal_key, title, created_by, version,
member_count, student_id, personal_teacher_id, teacher_context_type, context_id,
	message_count, max_message_sequence, max_event_sequence, max_teacher_turn_sequence,
last_message_id, last_message_at, created_at, updated_at`

func scanDialog(row pgx.Row) (domain.Dialog, error) {
	var item domain.Dialog
	var title *string
	var studentID, personalTeacherID *uuid.UUID
	var teacherContextType *string
	err := row.Scan(
		&item.ID, &item.SpaceID, &item.Type, &item.Status, &item.PersonalKey, &title,
		&item.CreatedBy, &item.Version, &item.MemberCount, &studentID, &personalTeacherID, &teacherContextType, &item.ContextID, &item.MessageCount,
		&item.MaxMessageSequence, &item.MaxEventSequence, &item.MaxTeacherTurnSequence, &item.LastMessageID,
		&item.LastMessageAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if title != nil {
		item.Title = *title
	}
	if studentID != nil {
		item.StudentID = *studentID
	}
	if personalTeacherID != nil {
		item.PersonalTeacherID = *personalTeacherID
	}
	if teacherContextType != nil {
		item.TeacherContextType = domain.TeacherContextType(*teacherContextType)
	}
	return item, mapError(err)
}

const memberColumns = `dialog_id, user_id, role, status, history_from_message_sequence,
last_read_message_sequence, unread_count, last_event_sequence, last_read_at,
muted_until, archived_at, added_by, joined_at, left_at, updated_at`

func scanMember(row pgx.Row) (domain.Member, error) {
	var item domain.Member
	err := row.Scan(
		&item.DialogID, &item.UserID, &item.Role, &item.Status,
		&item.HistoryFromMessageSequence, &item.LastReadMessageSequence, &item.UnreadCount,
		&item.LastEventSequence, &item.LastReadAt, &item.MutedUntil, &item.ArchivedAt,
		&item.AddedBy, &item.JoinedAt, &item.LeftAt, &item.UpdatedAt,
	)
	return item, mapError(err)
}

const messageColumns = `id, dialog_id, author_type, channel, sender_id, personal_teacher_id, learning_action_id, lesson_context, assistant_ui, reply_to_message_id, body, links,
	status, version, message_sequence, last_event_sequence, teacher_turn_sequence, idempotency_key,
edited_at, deleted_at, created_at, updated_at`

func scanMessage(row pgx.Row) (domain.Message, error) {
	var item domain.Message
	var links []byte
	var lessonContext, assistantUI []byte
	var senderID, personalTeacherID *uuid.UUID
	err := row.Scan(
		&item.ID, &item.DialogID, &item.AuthorType, &item.Channel, &senderID, &personalTeacherID, &item.LearningActionID, &lessonContext, &assistantUI, &item.ReplyToMessageID, &item.Body, &links,
		&item.Status, &item.Version, &item.MessageSequence, &item.LastEventSequence, &item.TeacherTurnSequence,
		&item.IdempotencyKey, &item.EditedAt, &item.DeletedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return domain.Message{}, mapError(err)
	}
	if senderID != nil {
		item.SenderID = *senderID
	}
	if personalTeacherID != nil {
		item.PersonalTeacherID = *personalTeacherID
	}
	if err := json.Unmarshal(links, &item.Links); err != nil {
		return domain.Message{}, err
	}
	if len(lessonContext) > 0 {
		if err := json.Unmarshal(lessonContext, &item.LessonContext); err != nil {
			return domain.Message{}, err
		}
		normalized, err := domain.NormalizeLessonMessageContext(item.LessonContext)
		if err != nil {
			return domain.Message{}, err
		}
		item.LessonContext = normalized
	}
	if len(assistantUI) > 0 {
		normalized, err := domain.NormalizeAssistantUI(assistantUI)
		if err != nil {
			return domain.Message{}, err
		}
		item.AssistantUI = normalized
	}
	return item, nil
}

func scanMessages(rows pgx.Rows) ([]domain.Message, error) {
	result := make([]domain.Message, 0)
	for rows.Next() {
		item, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

const attachmentColumns = `id, dialog_id, message_id, uploader_id, filestorage_id,
kind, status, mime_type, size_bytes, width, height, checksum_sha256, original_filename,
expires_at, activated_at, deleted_at, activation_attempts, activation_next_attempt_at,
delete_attempts, delete_next_attempt_at, last_error, storage_deleted_at, created_at, updated_at`

func scanAttachment(row pgx.Row) (domain.Attachment, error) {
	var item domain.Attachment
	var lastError *string
	err := row.Scan(
		&item.ID, &item.DialogID, &item.MessageID, &item.UploaderID, &item.FileStorageID,
		&item.Kind, &item.Status, &item.MIMEType, &item.SizeBytes, &item.Width, &item.Height,
		&item.ChecksumSHA256, &item.OriginalFilename, &item.ExpiresAt, &item.ActivatedAt,
		&item.DeletedAt, &item.ActivationAttempts, &item.ActivationNextAttemptAt,
		&item.DeleteAttempts, &item.DeleteNextAttemptAt, &lastError,
		&item.StorageDeletedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if lastError != nil {
		item.LastError = *lastError
	}
	return item, mapError(err)
}

const outboxColumns = `id, dialog_id, aggregate_type, aggregate_id, subject,
event_sequence, schema_version, payload, attempts, next_attempt_at,
published_at, last_error, created_at`

func scanOutbox(row pgx.Row) (domain.OutboxEvent, error) {
	var item domain.OutboxEvent
	var lastError *string
	err := row.Scan(
		&item.ID, &item.DialogID, &item.AggregateType, &item.AggregateID, &item.Subject,
		&item.EventSequence, &item.SchemaVersion, &item.Payload, &item.Attempts,
		&item.NextAttemptAt, &item.PublishedAt, &lastError, &item.CreatedAt,
	)
	if lastError != nil {
		item.LastError = *lastError
	}
	return item, mapError(err)
}
