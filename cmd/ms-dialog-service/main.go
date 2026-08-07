package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpadapter "github.com/bemulima/ms-go-dialog/internal/adapters/http"
	"github.com/bemulima/ms-go-dialog/internal/adapters/postgres"
	"github.com/bemulima/ms-go-dialog/internal/config"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
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
	blocks := &postgres.BlockRepository{Pool: pool}
	tx := &postgres.TransactionManager{Pool: pool}

	dialogService := &dialoguc.Service{
		Spaces: spaces, Dialogs: dialogs, Members: members, Blocks: blocks, Outbox: outbox, Tx: tx,
	}
	messageService := &messageuc.Service{
		Spaces: spaces, Dialogs: dialogs, Members: members, Messages: messages,
		Attachments: attachments, Outbox: outbox, Tx: tx,
	}
	router := httpadapter.NewRouter(httpadapter.RouterDependencies{DialogService: dialogService, MessageService: messageService})
	server := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           router,
		ReadHeaderTimeout: time.Duration(cfg.HTTPReadHeaderTimeoutSeconds) * time.Second,
		ReadTimeout:       time.Duration(cfg.HTTPReadTimeoutSeconds) * time.Second,
		WriteTimeout:      time.Duration(cfg.HTTPWriteTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(cfg.HTTPIdleTimeoutSeconds) * time.Second,
		MaxHeaderBytes:    cfg.HTTPMaxHeaderBytes,
	}

	serverError := make(chan error, 1)
	go func() {
		logger.Info("dialog HTTP server started", zap.String("address", server.Addr), zap.String("mode", cfg.ServiceMode))
		serverError <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ShutdownTimeoutSeconds)*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	return nil
}
