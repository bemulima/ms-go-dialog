package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

func TestMessageRepository_WindowAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("DIALOG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DIALOG_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	rollback := errors.New("rollback integration fixture")
	err = (TransactionManager{Pool: pool}).WithinTransaction(ctx, func(txCtx context.Context) error {
		now := time.Now().UTC()
		spaceID, dialogID, senderID := uuid.New(), uuid.New(), uuid.New()
		space := domain.Space{
			ID: spaceID, Key: "integration-" + uuid.NewString(), Name: "Integration", Status: domain.SpaceStatusActive,
			AllowedOrigins: []string{"https://client.example"}, Policy: domain.DefaultPolicy(),
			CreatedBy: senderID, CreatedAt: now, UpdatedAt: now,
		}
		if err := (SpaceRepository{Pool: pool}).Create(txCtx, space); err != nil {
			return err
		}
		dialog := domain.Dialog{
			ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive,
			Title: "Integration", CreatedBy: senderID, Version: 1, MemberCount: 2,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := (DialogRepository{Pool: pool}).Create(txCtx, dialog); err != nil {
			return err
		}
		message := domain.Message{
			ID: uuid.New(), DialogID: dialogID, AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelWeb, SenderID: senderID, IdempotencyKey: uuid.New(),
			Body: "integration message", Links: []domain.Link{}, Status: domain.MessageStatusActive, Version: 1,
			MessageSequence: 1, LastEventSequence: 1, CreatedAt: now, UpdatedAt: now,
		}
		repositoryAdapter := MessageRepository{Pool: pool}
		if err := repositoryAdapter.Create(txCtx, message); err != nil {
			return err
		}
		items, err := repositoryAdapter.Window(txCtx, repository.MessageWindowQuery{
			DialogID: dialogID, FromSequence: 0, AnchorSequence: 2, Before: 2, After: 1,
		})
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].ID != message.ID {
			return fmt.Errorf("message window mismatch: %+v", items)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}

func TestTeacherDialogRepositoriesAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("DIALOG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DIALOG_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	rollback := errors.New("rollback teacher integration fixture")
	err = (TransactionManager{Pool: pool}).WithinTransaction(ctx, func(txCtx context.Context) error {
		now := time.Now().UTC()
		spaceID, dialogID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		contextID, learningActionID := uuid.New(), uuid.New()
		space := domain.Space{
			ID: spaceID, Key: "teacher-integration-" + uuid.NewString(), Name: "Teacher Integration",
			Status: domain.SpaceStatusActive, AllowedOrigins: []string{"https://client.example"},
			Policy: domain.DefaultPolicy(), CreatedBy: studentID, CreatedAt: now, UpdatedAt: now,
		}
		if err := (SpaceRepository{Pool: pool}).Create(txCtx, space); err != nil {
			return err
		}
		dialog := domain.Dialog{
			ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeTeacher, Status: domain.DialogStatusActive,
			StudentID: studentID, PersonalTeacherID: teacherID, TeacherContextType: domain.TeacherContextPracticeTask,
			ContextID: &contextID, CreatedBy: studentID, Version: 1, MemberCount: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		dialogs := DialogRepository{Pool: pool}
		if err := dialogs.Create(txCtx, dialog); err != nil {
			return err
		}
		member := domain.Member{
			DialogID: dialogID, UserID: studentID, Role: domain.MemberRoleMember, Status: domain.MemberStatusActive,
			AddedBy: studentID, JoinedAt: now, UpdatedAt: now,
		}
		if err := (MemberRepository{Pool: pool}).Create(txCtx, member); err != nil {
			return err
		}
		loaded, err := dialogs.FindTeacherByContext(txCtx, spaceID, studentID, teacherID, domain.TeacherContextPracticeTask, &contextID)
		if err != nil || loaded.PersonalTeacherID != teacherID || loaded.ContextID == nil || *loaded.ContextID != contextID {
			return fmt.Errorf("teacher dialog lookup mismatch: dialog=%+v err=%w", loaded, err)
		}

		sourceID := uuid.New()
		messages := MessageRepository{Pool: pool}
		if err := messages.Create(txCtx, domain.Message{
			ID: sourceID, DialogID: dialogID, AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelTelegram, SenderID: studentID,
			LearningActionID: &learningActionID, Body: "source", Links: []domain.Link{}, Status: domain.MessageStatusActive,
			Version: 1, MessageSequence: 1, LastEventSequence: 1, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		teacherKey := uuid.New()
		if err := messages.Create(txCtx, domain.Message{
			ID: uuid.New(), DialogID: dialogID, AuthorType: domain.MessageAuthorPersonalTeacher, Channel: domain.MessageChannelTelegram, PersonalTeacherID: teacherID,
			LearningActionID: &learningActionID, ReplyToMessageID: &sourceID, Body: "response", Links: []domain.Link{},
			Status: domain.MessageStatusActive, Version: 1, MessageSequence: 2, LastEventSequence: 2,
			IdempotencyKey: teacherKey, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		teacherMessage, err := messages.GetByTeacherIdempotencyKey(txCtx, teacherID, teacherKey)
		if err != nil || teacherMessage.AuthorType != domain.MessageAuthorPersonalTeacher || teacherMessage.SenderID != uuid.Nil || teacherMessage.PersonalTeacherID != teacherID {
			return fmt.Errorf("teacher message lookup mismatch: message=%+v err=%w", teacherMessage, err)
		}

		outbox := OutboxRepository{Pool: pool}
		for _, subject := range []domain.EventSubject{domain.EventDialogMessageCreated, domain.EventDialogTeacherRequested} {
			eventID := uuid.New()
			if err := outbox.Add(txCtx, domain.OutboxEvent{
				ID: eventID, DialogID: dialogID, AggregateType: "message", AggregateID: sourceID,
				Subject: subject, EventSequence: 1, SchemaVersion: 1,
				Payload: []byte(fmt.Sprintf(`{"event_id":%q}`, eventID.String())), NextAttemptAt: now, CreatedAt: now,
			}); err != nil {
				return err
			}
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}
