package config

import "testing"

func TestConfig_ValidateModesAndTicketTTL(t *testing.T) {
	valid := Config{
		HTTPPort: "8080", DatabaseURL: "postgres://example", ServiceMode: "all",
		HTTPReadHeaderTimeoutSeconds: 5, HTTPReadTimeoutSeconds: 30, HTTPWriteTimeoutSeconds: 120,
		HTTPIdleTimeoutSeconds: 60, HTTPMaxHeaderBytes: 32768,
		RealtimeTicketTTLSeconds: 30, RealtimeTicketCleanupSeconds: 30,
		WSMaxConnectionsPerUser: 5, WSQueueSize: 64, WSMaxFrameBytes: 16384,
		AttachmentTTLMinutes: 60, AttachmentSignedURLMinutes: 5, AttachmentWorkerInterval: 5,
		AttachmentWorkerBatch: 50, AttachmentActivationAttempts: 5,
		OutboxWorkerIntervalMS: 500, OutboxWorkerBatch: 100, OutboxLeaseSeconds: 30,
		ShutdownTimeoutSeconds: 10,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	invalid := valid
	invalid.RealtimeTicketTTLSeconds = 31
	if err := invalid.Validate(); err == nil {
		t.Fatal("ticket TTL above hard maximum accepted")
	}
	invalid = valid
	invalid.ServiceMode = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown service mode accepted")
	}
}
