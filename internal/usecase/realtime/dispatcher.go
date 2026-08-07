package realtime

import (
	"context"
	"errors"
	"fmt"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"time"
)

type LifecyclePublisher interface {
	PublishLifecycle(context.Context, domain.OutboxEvent) error
}
type Dispatcher struct {
	Outbox    repository.OutboxRepository
	Publisher LifecyclePublisher
	Now       func() time.Time
	Lease     time.Duration
}
type DispatchResult struct {
	Published int
	Failed    int
}

func (d Dispatcher) Process(ctx context.Context, limit int) (DispatchResult, error) {
	if limit < 1 {
		return DispatchResult{}, fmt.Errorf("%w: dispatcher limit", domain.ErrValidation)
	}
	if d.Publisher == nil {
		return DispatchResult{}, errors.New("lifecycle publisher is not configured")
	}
	now := d.now()
	events, err := d.Outbox.ClaimPending(ctx, now, now.Add(d.lease()), limit)
	if err != nil {
		return DispatchResult{}, err
	}
	result := DispatchResult{}
	var failures []error
	for _, event := range events {
		if err := d.Publisher.PublishLifecycle(ctx, event); err != nil {
			next := d.now().Add(retryDelay(event.Attempts + 1))
			if markErr := d.Outbox.MarkFailed(ctx, event.ID, next, bounded(err)); markErr != nil {
				failures = append(failures, markErr)
			}
			result.Failed++
			continue
		}
		if err := d.Outbox.MarkPublished(ctx, event.ID, d.now()); err != nil && !errors.Is(err, domain.ErrNotFound) {
			failures = append(failures, err)
			continue
		}
		result.Published++
	}
	return result, errors.Join(failures...)
}
func (d Dispatcher) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}
func (d Dispatcher) lease() time.Duration {
	if d.Lease > 0 {
		return d.Lease
	}
	return 30 * time.Second
}
func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	return min(5*time.Second*time.Duration(1<<min(attempt-1, 6)), 5*time.Minute)
}
func bounded(err error) string {
	value := err.Error()
	if len(value) > 2048 {
		return value[:2048]
	}
	return value
}
