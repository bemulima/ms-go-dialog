package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bemulima/ms-go-dialog/internal/adapters/observability"
	attachmentuc "github.com/bemulima/ms-go-dialog/internal/usecase/attachment"
	realtimeuc "github.com/bemulima/ms-go-dialog/internal/usecase/realtime"
	"go.uber.org/zap"
)

type attachmentProcessorStub struct {
	result attachmentuc.WorkResult
	err    error
}

func (s attachmentProcessorStub) Process(context.Context, int) (attachmentuc.WorkResult, error) {
	return s.result, s.err
}

type outboxProcessorStub struct {
	result realtimeuc.DispatchResult
	err    error
}

func (s outboxProcessorStub) Process(context.Context, int) (realtimeuc.DispatchResult, error) {
	return s.result, s.err
}

type ticketCleanerStub struct {
	deleted int
	err     error
}

func (s ticketCleanerStub) DeleteExpired(context.Context, int) (int, error) {
	return s.deleted, s.err
}

func TestWorkerBatchMetricsIncludePartialResultsAndErrors(t *testing.T) {
	metrics := observability.NewMetrics()
	workerError := errors.New("partial failure")
	processAttachmentBatch(context.Background(), zap.NewNop(), metrics, attachmentProcessorStub{
		result: attachmentuc.WorkResult{Activated: 2, Failed: 1, Deleted: 3}, err: workerError,
	}, 10)
	processOutboxBatch(context.Background(), zap.NewNop(), metrics, outboxProcessorStub{
		result: realtimeuc.DispatchResult{Published: 4, Failed: 2}, err: workerError,
	}, 10)
	processTicketCleanupBatch(context.Background(), zap.NewNop(), metrics, ticketCleanerStub{deleted: 5, err: workerError}, 10)

	response := httptest.NewRecorder()
	metrics.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := response.Body.String()
	for _, expected := range []string{
		observability.AttachmentWorkerActivatedTotal + " 2",
		observability.AttachmentWorkerFailedTotal + " 1",
		observability.AttachmentWorkerDeletedTotal + " 3",
		observability.AttachmentWorkerErrorsTotal + " 1",
		observability.OutboxPublishedTotal + " 4",
		observability.OutboxFailedTotal + " 2",
		observability.OutboxWorkerErrorsTotal + " 1",
		observability.TicketCleanupDeletedTotal + " 5",
		observability.TicketCleanupErrorsTotal + " 1",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in metrics:\n%s", expected, body)
		}
	}
}

func TestRuntimeModeCapabilities(t *testing.T) {
	tests := []struct {
		mode                   string
		api, realtime, workers bool
	}{
		{mode: "api", api: true},
		{mode: "realtime", realtime: true},
		{mode: "worker", workers: true},
		{mode: "all", api: true, realtime: true, workers: true},
		{mode: "unknown"},
	}
	for _, test := range tests {
		if modeHasAPI(test.mode) != test.api || modeHasRealtime(test.mode) != test.realtime || modeHasWorkers(test.mode) != test.workers {
			t.Fatalf("unexpected capabilities for mode %q", test.mode)
		}
	}
}
