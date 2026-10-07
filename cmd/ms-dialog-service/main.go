package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/config"
	"github.com/bemulima/ms-go-dialog/internal/infrastructure/filescan"
	"github.com/bemulima/ms-go-dialog/internal/infrastructure/filestorage"
	"github.com/bemulima/ms-go-dialog/internal/infrastructure/health"
	useradapter "github.com/bemulima/ms-go-dialog/internal/infrastructure/http/user"
	natsadapter "github.com/bemulima/ms-go-dialog/internal/infrastructure/messaging/nats"
	"github.com/bemulima/ms-go-dialog/internal/infrastructure/observability"
	"github.com/bemulima/ms-go-dialog/internal/infrastructure/persistence/postgres"
	httpadapter "github.com/bemulima/ms-go-dialog/internal/transport/http"
	httpmiddleware "github.com/bemulima/ms-go-dialog/internal/transport/http/common/middleware"
	messageadapter "github.com/bemulima/ms-go-dialog/internal/transport/message"
	websocketadapter "github.com/bemulima/ms-go-dialog/internal/transport/websocket"
	adminuc "github.com/bemulima/ms-go-dialog/internal/usecase/admin"
	attachmentuc "github.com/bemulima/ms-go-dialog/internal/usecase/attachment"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	realtimeuc "github.com/bemulima/ms-go-dialog/internal/usecase/realtime"
	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger, err := zap.NewProduction()
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	spaces := &postgres.SpaceRepository{Pool: pool}
	dialogs := &postgres.DialogRepository{Pool: pool}
	members := &postgres.MemberRepository{Pool: pool}
	messages := &postgres.MessageRepository{Pool: pool}
	canonicalStudentTurns := &postgres.CanonicalStudentTurnRepository{Pool: pool}
	attachments := &postgres.AttachmentRepository{Pool: pool}
	outbox := &postgres.OutboxRepository{Pool: pool}
	tickets := &postgres.RealtimeTicketRepository{Pool: pool}
	blocks := &postgres.BlockRepository{Pool: pool}
	tx := &postgres.TransactionManager{Pool: pool}

	dialogService := &dialoguc.Service{
		Spaces: spaces, Dialogs: dialogs, TeacherDialogs: dialogs, Members: members, Blocks: blocks, Outbox: outbox, Tx: tx,
		Participants: &useradapter.Client{
			BaseURL:       cfg.UserServiceBaseURL,
			InternalToken: cfg.UserServiceInternalToken,
			HTTPClient:    &http.Client{Timeout: time.Duration(cfg.UserServiceTimeoutSeconds) * time.Second},
		},
	}
	metrics := observability.NewMetrics()
	messageService := &messageuc.Service{
		Spaces: spaces, Dialogs: dialogs, Members: members, Messages: messages, TeacherMessages: messages, CanonicalStudentTurns: canonicalStudentTurns,
		Attachments: attachments, Outbox: outbox, Tx: tx,
		Blocks: blocks, TeacherOrderingV2Enabled: cfg.DialogTeacherOrderingV2Enabled, CanonicalStudentTurnEnabled: cfg.DialogCanonicalStudentTurnEnabled,
		OnTeacherRequestV2: func(observation messageuc.TeacherRequestV2Observation) {
			metrics.Increment(observability.TeacherRequestedV2Total, 1)
			logger.Info("teacher request v2 committed",
				zap.String("dialog_id", observation.DialogID.String()),
				zap.String("canonical_student_message_id", observation.CanonicalStudentMessageID.String()),
				zap.Int64("teacher_turn_sequence", observation.TeacherTurnSequence),
				zap.String("correlation_id", observation.CorrelationID.String()),
				zap.String("causation_id", observation.CausationID.String()),
				zap.String("source_event_id", observation.SourceEventID.String()),
			)
		},
		OnCanonicalStudentTurn: func(observation messageuc.CanonicalStudentTurnObservation) {
			metrics.Increment(observability.CanonicalStudentTurnMaterializedTotal, 1)
			logger.Info("canonical student turn committed",
				zap.String("action_receipt_id", observation.ActionReceiptID.String()),
				zap.String("correlation_id", observation.CorrelationID.String()),
				zap.String("causation_id", observation.CausationID.String()),
				zap.String("source_prompt_message_id", observation.SourcePromptMessageID.String()),
				zap.Int("source_prompt_message_version", observation.SourcePromptMessageVersion),
				zap.String("block_id", observation.BlockID),
				zap.String("action_id", observation.ActionID),
				zap.String("source_ui_digest", observation.SourceUIDigest),
				zap.String("canonical_student_message_id", observation.CanonicalStudentMessageID.String()),
				zap.Int64("teacher_turn_sequence", observation.TeacherTurnSequence),
			)
		},
	}
	attachmentService := &attachmentuc.Service{Spaces: spaces, Dialogs: dialogs, Members: members, Messages: messages, Attachments: attachments, Outbox: outbox, Tx: tx,
		Files: &filestorage.Client{BaseURL: cfg.FileStorageServiceBaseURL, InternalToken: cfg.FileStorageInternalToken}, Scanner: &filescan.ClamAV{Address: cfg.ClamAVAddress, Timeout: time.Duration(cfg.ClamAVTimeoutSeconds) * time.Second}, TTLMinutes: cfg.AttachmentTTLMinutes, SignedURLMinutes: cfg.AttachmentSignedURLMinutes, ActivationMaxAttempts: cfg.AttachmentActivationAttempts, WorkerLease: time.Duration(cfg.AttachmentWorkerLeaseSeconds) * time.Second}
	adminService := &adminuc.Service{Spaces: spaces, Dialogs: dialogs, Members: members, Messages: messages, Attachments: attachments, Outbox: outbox, Tx: tx}
	realtimeService := &realtimeuc.TicketService{Spaces: spaces, Members: members, Tickets: tickets, TTL: time.Duration(cfg.RealtimeTicketTTLSeconds) * time.Second}
	dispatcher := &realtimeuc.Dispatcher{Outbox: outbox, Lease: time.Duration(cfg.OutboxLeaseSeconds) * time.Second}
	rateLimiter, err := httpmiddleware.NewActorRateLimiter(cfg.HTTPUserRateLimitRPS, cfg.HTTPUserRateLimitBurst, cfg.HTTPUserRateLimitMaxActors, time.Duration(cfg.HTTPUserRateLimitIdleSeconds)*time.Second)
	if err != nil {
		return fmt.Errorf("configure HTTP rate limiter: %w", err)
	}

	var natsConnection interface{ Drain() error }
	var natsClient *natsadapter.Client
	if modeHasRealtime(cfg.ServiceMode) || modeHasWorkers(cfg.ServiceMode) {
		connection, err := natsadapter.Connect(cfg.NATSURL)
		if err != nil {
			return fmt.Errorf("connect NATS: %w", err)
		}
		natsConnection = connection
		natsClient = &natsadapter.Client{Conn: connection}
		dispatcher.Publisher = natsClient
		if modeHasWorkers(cfg.ServiceMode) {
			if err := natsClient.EnsureLifecycleStream(ctx); err != nil {
				return fmt.Errorf("validate infrastructure-provisioned NATS stream: %w", err)
			}
		}
	}
	var hub *websocketadapter.Hub
	var subscription *messageadapter.Subscription
	var websocketHandler http.Handler
	if modeHasRealtime(cfg.ServiceMode) {
		hub = websocketadapter.NewHub(cfg.WSMaxConnectionsPerUser, cfg.WSQueueSize)
		websocketHandler = websocketadapter.Handler{Tickets: realtimeService, Hub: hub, Typing: natsClient, Metrics: metrics, MaxFrameBytes: cfg.WSMaxFrameBytes}
		subscription, err = messageadapter.SubscribeRealtime(natsClient.Conn, hub)
		if err != nil {
			return fmt.Errorf("subscribe realtime: %w", err)
		}
	}
	readinessChecks := map[string]health.Check{"postgres": pool.Ping}
	if natsClient != nil {
		readinessChecks["nats"] = natsClient.Ping
	}
	dependencies := httpadapter.RouterDependencies{
		Readiness: health.NewChecker(time.Duration(cfg.ReadinessTimeoutSeconds)*time.Second, readinessChecks),
		Metrics:   metrics,
	}
	if modeHasAPI(cfg.ServiceMode) {
		dependencies.DialogService = dialogService
		dependencies.MessageService = messageService
		dependencies.AttachmentService = attachmentService
		dependencies.RealtimeService = realtimeService
		dependencies.AdminService = adminService
		dependencies.UserRateLimiter = rateLimiter
		dependencies.InternalToken = cfg.InternalAPIToken
	}
	if modeHasRealtime(cfg.ServiceMode) {
		dependencies.WebSocketHandler = websocketHandler
	}
	router := httpadapter.NewRouter(dependencies)
	server := &http.Server{
		Addr:              cfg.HTTPListenAddress(),
		Handler:           router,
		ReadHeaderTimeout: time.Duration(cfg.HTTPReadHeaderTimeoutSeconds) * time.Second,
		ReadTimeout:       time.Duration(cfg.HTTPReadTimeoutSeconds) * time.Second,
		WriteTimeout:      time.Duration(cfg.HTTPWriteTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(cfg.HTTPIdleTimeoutSeconds) * time.Second,
		MaxHeaderBytes:    cfg.HTTPMaxHeaderBytes,
	}

	var workers sync.WaitGroup
	if modeHasWorkers(cfg.ServiceMode) {
		workers.Add(3)
		go func() {
			defer workers.Done()
			runAttachmentWorker(ctx, logger, metrics, attachmentService, time.Duration(cfg.AttachmentWorkerInterval)*time.Second, cfg.AttachmentWorkerBatch)
		}()
		go func() {
			defer workers.Done()
			runOutboxWorker(ctx, logger, metrics, dispatcher, time.Duration(cfg.OutboxWorkerIntervalMS)*time.Millisecond, cfg.OutboxWorkerBatch)
		}()
		go func() {
			defer workers.Done()
			runTicketCleanupWorker(ctx, logger, metrics, realtimeService, time.Duration(cfg.RealtimeTicketCleanupSeconds)*time.Second, cfg.OutboxWorkerBatch)
		}()
	}
	serverError := make(chan error, 1)
	go func() {
		logger.Info("dialog HTTP server started", zap.String("address", server.Addr), zap.String("mode", cfg.ServiceMode))
		serverError <- server.ListenAndServe()
	}()
	var serveErr error
	select {
	case <-ctx.Done():
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = err
		}
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ShutdownTimeoutSeconds)*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	if subscription != nil {
		_ = subscription.Close()
	}
	if hub != nil {
		hub.Close()
	}
	workers.Wait()
	if natsConnection != nil {
		_ = natsConnection.Drain()
	}
	return serveErr
}

func runAttachmentWorker(ctx context.Context, logger *zap.Logger, metrics *observability.Metrics, service *attachmentuc.Service, interval time.Duration, batch int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			processAttachmentBatch(ctx, logger, metrics, service, batch)
		}
	}
}

type attachmentProcessor interface {
	Process(context.Context, int) (attachmentuc.WorkResult, error)
}

func processAttachmentBatch(ctx context.Context, logger *zap.Logger, metrics *observability.Metrics, service attachmentProcessor, batch int) {
	result, err := service.Process(ctx, batch)
	metrics.Increment(observability.AttachmentWorkerActivatedTotal, result.Activated)
	metrics.Increment(observability.AttachmentWorkerFailedTotal, result.Failed)
	metrics.Increment(observability.AttachmentWorkerDeletedTotal, result.Deleted)
	if err != nil {
		metrics.Increment(observability.AttachmentWorkerErrorsTotal, 1)
		logger.Error("attachment worker failed", zap.Error(err), zap.Int("activated", result.Activated), zap.Int("failed", result.Failed), zap.Int("deleted", result.Deleted))
	} else if result.Activated+result.Failed+result.Deleted > 0 {
		logger.Info("attachment worker completed", zap.Int("activated", result.Activated), zap.Int("failed", result.Failed), zap.Int("deleted", result.Deleted))
	}
}

func runOutboxWorker(ctx context.Context, logger *zap.Logger, metrics *observability.Metrics, dispatcher *realtimeuc.Dispatcher, interval time.Duration, batch int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			processOutboxBatch(ctx, logger, metrics, dispatcher, batch)
		}
	}
}

type outboxProcessor interface {
	Process(context.Context, int) (realtimeuc.DispatchResult, error)
}

func processOutboxBatch(ctx context.Context, logger *zap.Logger, metrics *observability.Metrics, dispatcher outboxProcessor, batch int) {
	result, err := dispatcher.Process(ctx, batch)
	metrics.Increment(observability.OutboxPublishedTotal, result.Published)
	metrics.Increment(observability.OutboxFailedTotal, result.Failed)
	if err != nil {
		metrics.Increment(observability.OutboxWorkerErrorsTotal, 1)
		logger.Error("outbox worker failed", zap.Error(err), zap.Int("published", result.Published), zap.Int("failed", result.Failed))
	} else if result.Published+result.Failed > 0 {
		logger.Info("outbox worker completed", zap.Int("published", result.Published), zap.Int("failed", result.Failed))
	}
}

func runTicketCleanupWorker(ctx context.Context, logger *zap.Logger, metrics *observability.Metrics, service *realtimeuc.TicketService, interval time.Duration, batch int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			processTicketCleanupBatch(ctx, logger, metrics, service, batch)
		}
	}
}

type ticketCleaner interface {
	DeleteExpired(context.Context, int) (int, error)
}

func processTicketCleanupBatch(ctx context.Context, logger *zap.Logger, metrics *observability.Metrics, service ticketCleaner, batch int) {
	deleted, err := service.DeleteExpired(ctx, batch)
	metrics.Increment(observability.TicketCleanupDeletedTotal, deleted)
	if err != nil {
		metrics.Increment(observability.TicketCleanupErrorsTotal, 1)
		logger.Error("ticket cleanup failed", zap.Error(err), zap.Int("deleted", deleted))
	} else if deleted > 0 {
		logger.Info("ticket cleanup completed", zap.Int("deleted", deleted))
	}
}

func modeHasAPI(mode string) bool      { return mode == "all" || mode == "api" }
func modeHasRealtime(mode string) bool { return mode == "all" || mode == "realtime" }
func modeHasWorkers(mode string) bool  { return mode == "all" || mode == "worker" }
