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
			ID: uuid.New(), DialogID: dialogID, SenderID: senderID, IdempotencyKey: uuid.New(),
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
