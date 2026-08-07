package realtime

import (
	"context"
	"errors"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
	"testing"
	"time"
)

type outboxStub struct {
	event     domain.OutboxEvent
	published bool
	failed    bool
}

func (s *outboxStub) Add(context.Context, domain.OutboxEvent) error { return nil }
func (s *outboxStub) ClaimPending(context.Context, time.Time, time.Time, int) ([]domain.OutboxEvent, error) {
	return []domain.OutboxEvent{s.event}, nil
}
func (s *outboxStub) MarkPublished(context.Context, uuid.UUID, time.Time) error {
	s.published = true
	return nil
}
func (s *outboxStub) MarkFailed(context.Context, uuid.UUID, time.Time, string) error {
	s.failed = true
	return nil
}

type publisherStub struct{ err error }

func (p publisherStub) PublishLifecycle(context.Context, domain.OutboxEvent) error { return p.err }
func TestDispatcherMarksPublishedAndFailed(t *testing.T) {
	event := domain.OutboxEvent{ID: uuid.New(), DialogID: uuid.New(), AggregateID: uuid.New(), AggregateType: "message", Subject: domain.EventDialogMessageCreated, EventSequence: 1, SchemaVersion: 1, Payload: []byte(`{"event_id":"x"}`), NextAttemptAt: time.Now(), CreatedAt: time.Now()}
	store := &outboxStub{event: event}
	result, err := (Dispatcher{Outbox: store, Publisher: publisherStub{}}).Process(context.Background(), 1)
	if err != nil || result.Published != 1 || !store.published {
		t.Fatalf("published result=%+v err=%v", result, err)
	}
	store = &outboxStub{event: event}
	result, err = (Dispatcher{Outbox: store, Publisher: publisherStub{err: errors.New("down")}}).Process(context.Background(), 1)
	if err != nil || result.Failed != 1 || !store.failed {
		t.Fatalf("failed result=%+v err=%v", result, err)
	}
}
