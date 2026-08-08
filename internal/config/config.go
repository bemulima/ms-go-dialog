package config

import (
	"fmt"
	"strings"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	HTTPPort                     string `envconfig:"HTTP_PORT" default:"8080"`
	HTTPReadHeaderTimeoutSeconds int    `envconfig:"HTTP_READ_HEADER_TIMEOUT_SECONDS" default:"5"`
	HTTPReadTimeoutSeconds       int    `envconfig:"HTTP_READ_TIMEOUT_SECONDS" default:"30"`
	HTTPWriteTimeoutSeconds      int    `envconfig:"HTTP_WRITE_TIMEOUT_SECONDS" default:"120"`
	HTTPIdleTimeoutSeconds       int    `envconfig:"HTTP_IDLE_TIMEOUT_SECONDS" default:"60"`
	HTTPMaxHeaderBytes           int    `envconfig:"HTTP_MAX_HEADER_BYTES" default:"32768"`
	HTTPUserRateLimitRPS         int    `envconfig:"HTTP_USER_RATE_LIMIT_RPS" default:"20"`
	HTTPUserRateLimitBurst       int    `envconfig:"HTTP_USER_RATE_LIMIT_BURST" default:"40"`
	HTTPUserRateLimitMaxActors   int    `envconfig:"HTTP_USER_RATE_LIMIT_MAX_ACTORS" default:"10000"`
	HTTPUserRateLimitIdleSeconds int    `envconfig:"HTTP_USER_RATE_LIMIT_IDLE_SECONDS" default:"300"`
	DatabaseURL                  string `envconfig:"DATABASE_URL" default:"postgres://postgres:postgres@localhost:5444/ms_dialog?sslmode=disable"`
	NATSURL                      string `envconfig:"NATS_URL" default:"nats://localhost:4222"`
	FileStorageServiceBaseURL    string `envconfig:"FILESTORAGE_SERVICE_BASE_URL" default:"http://localhost:8088"`
	ClamAVAddress                string `envconfig:"CLAMAV_ADDRESS" default:"localhost:3310"`
	ClamAVTimeoutSeconds         int    `envconfig:"CLAMAV_TIMEOUT_SECONDS" default:"30"`
	UserServiceBaseURL           string `envconfig:"USER_SERVICE_BASE_URL" default:"http://localhost:8082"`
	InternalAPIToken             string `envconfig:"INTERNAL_API_TOKEN" default:"change-me"`
	ServiceMode                  string `envconfig:"SERVICE_MODE" default:"all"`
	ReadinessTimeoutSeconds      int    `envconfig:"READINESS_TIMEOUT_SECONDS" default:"2"`
	ShutdownTimeoutSeconds       int    `envconfig:"SHUTDOWN_TIMEOUT_SECONDS" default:"10"`
	AttachmentTTLMinutes         int    `envconfig:"ATTACHMENT_TTL_MINUTES" default:"60"`
	AttachmentSignedURLMinutes   int    `envconfig:"ATTACHMENT_SIGNED_URL_MINUTES" default:"5"`
	AttachmentWorkerInterval     int    `envconfig:"ATTACHMENT_WORKER_INTERVAL_SECONDS" default:"5"`
	AttachmentWorkerBatch        int    `envconfig:"ATTACHMENT_WORKER_BATCH" default:"50"`
	AttachmentWorkerLeaseSeconds int    `envconfig:"ATTACHMENT_WORKER_LEASE_SECONDS" default:"120"`
	AttachmentActivationAttempts int    `envconfig:"ATTACHMENT_ACTIVATION_MAX_ATTEMPTS" default:"5"`
	OutboxWorkerIntervalMS       int    `envconfig:"OUTBOX_WORKER_INTERVAL_MS" default:"500"`
	OutboxWorkerBatch            int    `envconfig:"OUTBOX_WORKER_BATCH" default:"100"`
	OutboxLeaseSeconds           int    `envconfig:"OUTBOX_LEASE_SECONDS" default:"30"`
	RealtimeTicketTTLSeconds     int    `envconfig:"REALTIME_TICKET_TTL_SECONDS" default:"30"`
	RealtimeTicketCleanupSeconds int    `envconfig:"REALTIME_TICKET_CLEANUP_SECONDS" default:"30"`
	WSMaxConnectionsPerUser      int    `envconfig:"WS_MAX_CONNECTIONS_PER_USER" default:"5"`
	WSQueueSize                  int    `envconfig:"WS_QUEUE_SIZE" default:"64"`
	WSMaxFrameBytes              int64  `envconfig:"WS_MAX_FRAME_BYTES" default:"16384"`
}

func Load() (Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return Config{}, err
	}
	cfg.ServiceMode = strings.ToLower(strings.TrimSpace(cfg.ServiceMode))
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	switch c.ServiceMode {
	case "all", "api", "realtime", "worker":
	default:
		return fmt.Errorf("SERVICE_MODE must be all, api, realtime, or worker")
	}
	if c.HTTPPort == "" || c.DatabaseURL == "" || c.HTTPReadHeaderTimeoutSeconds < 1 ||
		c.HTTPReadTimeoutSeconds < 1 || c.HTTPWriteTimeoutSeconds < 1 || c.HTTPIdleTimeoutSeconds < 1 ||
		c.HTTPMaxHeaderBytes < 4096 || c.HTTPMaxHeaderBytes > 1<<20 {
		return fmt.Errorf("HTTP and database configuration is invalid")
	}
	if c.HTTPUserRateLimitRPS < 1 || c.HTTPUserRateLimitBurst < 1 || c.HTTPUserRateLimitMaxActors < 1 || c.HTTPUserRateLimitIdleSeconds < 1 {
		return fmt.Errorf("HTTP rate limit configuration is invalid")
	}
	if c.RealtimeTicketTTLSeconds < 1 || c.RealtimeTicketTTLSeconds > 30 ||
		c.RealtimeTicketCleanupSeconds < 1 || c.WSMaxConnectionsPerUser < 1 || c.WSQueueSize < 1 || c.WSMaxFrameBytes < 1024 {
		return fmt.Errorf("realtime configuration is invalid")
	}
	if c.AttachmentTTLMinutes < 1 || c.AttachmentSignedURLMinutes < 1 || c.AttachmentWorkerInterval < 1 ||
		c.AttachmentWorkerBatch < 1 || c.AttachmentWorkerLeaseSeconds <= 60 || c.AttachmentWorkerLeaseSeconds > 3600 || c.AttachmentActivationAttempts < 1 ||
		c.OutboxWorkerIntervalMS < 1 || c.OutboxWorkerBatch < 1 || c.OutboxLeaseSeconds < 1 || c.ShutdownTimeoutSeconds < 1 {
		return fmt.Errorf("worker configuration is invalid")
	}
	if c.ReadinessTimeoutSeconds < 1 || c.ReadinessTimeoutSeconds > 30 {
		return fmt.Errorf("readiness configuration is invalid")
	}
	if c.ClamAVTimeoutSeconds < 1 || c.ClamAVTimeoutSeconds > 300 {
		return fmt.Errorf("file scan configuration is invalid")
	}
	return nil
}
