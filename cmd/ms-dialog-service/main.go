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

	"github.com/bemulima/ms-go-dialog/internal/adapters/filescan"
	"github.com/bemulima/ms-go-dialog/internal/adapters/filestorage"
	httpadapter "github.com/bemulima/ms-go-dialog/internal/adapters/http"
	natsadapter "github.com/bemulima/ms-go-dialog/internal/adapters/nats"
	"github.com/bemulima/ms-go-dialog/internal/adapters/postgres"
	websocketadapter "github.com/bemulima/ms-go-dialog/internal/adapters/websocket"
	"github.com/bemulima/ms-go-dialog/internal/config"
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
	attachments := &postgres.AttachmentRepository{Pool: pool}
	outbox := &postgres.OutboxRepository{Pool: pool}
	tickets := &postgres.RealtimeTicketRepository{Pool: pool}
	blocks := &postgres.BlockRepository{Pool: pool}
	tx := &postgres.TransactionManager{Pool: pool}

	dialogService := &dialoguc.Service{
		Spaces: spaces, Dialogs: dialogs, Members: members, Blocks: blocks, Outbox: outbox, Tx: tx,
	}
	messageService := &messageuc.Service{
		Spaces: spaces, Dialogs: dialogs, Members: members, Messages: messages,
		Attachments: attachments, Outbox: outbox, Tx: tx,
	}
	attachmentService := &attachmentuc.Service{Spaces: spaces, Dialogs: dialogs, Members: members, Messages: messages, Attachments: attachments, Outbox: outbox, Tx: tx,
		Files: &filestorage.Client{BaseURL: cfg.FileStorageServiceBaseURL}, Scanner: &filescan.ClamAV{Address: cfg.ClamAVAddress, Timeout: time.Duration(cfg.ClamAVTimeoutSeconds) * time.Second}, TTLMinutes: cfg.AttachmentTTLMinutes, SignedURLMinutes: cfg.AttachmentSignedURLMinutes, ActivationMaxAttempts: cfg.AttachmentActivationAttempts}
	realtimeService := &realtimeuc.TicketService{Spaces: spaces, Members: members, Tickets: tickets, TTL: time.Duration(cfg.RealtimeTicketTTLSeconds) * time.Second}
	dispatcher := &realtimeuc.Dispatcher{Outbox: outbox, Lease: time.Duration(cfg.OutboxLeaseSeconds) * time.Second}

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
				return fmt.Errorf("ensure NATS stream: %w", err)
			}
		}
	}
	var hub *websocketadapter.Hub
	var subscription *natsadapter.Subscription
	var websocketHandler http.Handler
	if modeHasRealtime(cfg.ServiceMode) {
		hub = websocketadapter.NewHub(cfg.WSMaxConnectionsPerUser, cfg.WSQueueSize)
		websocketHandler = websocketadapter.Handler{Tickets: realtimeService, Hub: hub, Typing: natsClient, MaxFrameBytes: cfg.WSMaxFrameBytes}
		subscription, err = natsClient.SubscribeRealtime(hub)
		if err != nil {
			return fmt.Errorf("subscribe realtime: %w", err)
		}
	}
	dependencies := httpadapter.RouterDependencies{}
	if modeHasAPI(cfg.ServiceMode) {
		dependencies.DialogService = dialogService
		dependencies.MessageService = messageService
		dependencies.AttachmentService = attachmentService
		dependencies.RealtimeService = realtimeService
	}
	if modeHasRealtime(cfg.ServiceMode) {
		dependencies.WebSocketHandler = websocketHandler
	}
	router := httpadapter.NewRouter(dependencies)
	server := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
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
			runAttachmentWorker(ctx, logger, attachmentService, time.Duration(cfg.AttachmentWorkerInterval)*time.Second, cfg.AttachmentWorkerBatch)
		}()
		go func() {
			defer workers.Done()
			runOutboxWorker(ctx, logger, dispatcher, time.Duration(cfg.OutboxWorkerIntervalMS)*time.Millisecond, cfg.OutboxWorkerBatch)
		}()
		go func() {
			defer workers.Done()
			runTicketCleanupWorker(ctx, logger, realtimeService, time.Duration(cfg.RealtimeTicketCleanupSeconds)*time.Second, cfg.OutboxWorkerBatch)
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

func runAttachmentWorker(ctx context.Context, logger *zap.Logger, service *attachmentuc.Service, interval time.Duration, batch int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := service.Process(ctx, batch)
			if err != nil {
				logger.Error("attachment worker failed", zap.Error(err))
			} else if result.Activated+result.Failed+result.Deleted > 0 {
				logger.Info("attachment worker completed", zap.Int("activated", result.Activated), zap.Int("failed", result.Failed), zap.Int("deleted", result.Deleted))
			}
		}
	}
}
func runOutboxWorker(ctx context.Context, logger *zap.Logger, dispatcher *realtimeuc.Dispatcher, interval time.Duration, batch int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := dispatcher.Process(ctx, batch)
			if err != nil {
				logger.Error("outbox worker failed", zap.Error(err))
			} else if result.Published+result.Failed > 0 {
				logger.Info("outbox worker completed", zap.Int("published", result.Published), zap.Int("failed", result.Failed))
			}
		}
	}
}
func runTicketCleanupWorker(ctx context.Context, logger *zap.Logger, service *realtimeuc.TicketService, interval time.Duration, batch int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := service.DeleteExpired(ctx, batch)
			if err != nil {
				logger.Error("ticket cleanup failed", zap.Error(err))
			} else if deleted > 0 {
				logger.Info("ticket cleanup completed", zap.Int("deleted", deleted))
			}
		}
	}
}
func modeHasAPI(mode string) bool      { return mode == "all" || mode == "api" }
func modeHasRealtime(mode string) bool { return mode == "all" || mode == "realtime" }
func modeHasWorkers(mode string) bool  { return mode == "all" || mode == "worker" }
