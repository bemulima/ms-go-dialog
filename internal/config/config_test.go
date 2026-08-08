package config

import "testing"

func TestConfig_ValidateModesAndTicketTTL(t *testing.T) {
	valid := Config{
		HTTPPort: "8080", DatabaseURL: "postgres://example", ServiceMode: "all",
		HTTPReadHeaderTimeoutSeconds: 5, HTTPReadTimeoutSeconds: 30, HTTPWriteTimeoutSeconds: 120,
		HTTPIdleTimeoutSeconds: 60, HTTPMaxHeaderBytes: 32768,
		HTTPUserRateLimitRPS: 20, HTTPUserRateLimitBurst: 40, HTTPUserRateLimitMaxActors: 10000, HTTPUserRateLimitIdleSeconds: 300,
		RealtimeTicketTTLSeconds: 30, RealtimeTicketCleanupSeconds: 30,
		WSMaxConnectionsPerUser: 5, WSQueueSize: 64, WSMaxFrameBytes: 16384,
		AttachmentTTLMinutes: 60, AttachmentSignedURLMinutes: 5, AttachmentWorkerInterval: 5,
		AttachmentWorkerBatch: 50, AttachmentWorkerLeaseSeconds: 120, AttachmentActivationAttempts: 5,
		OutboxWorkerIntervalMS: 500, OutboxWorkerBatch: 100, OutboxLeaseSeconds: 30,
		ReadinessTimeoutSeconds: 2,
		ShutdownTimeoutSeconds:  10,
		ClamAVTimeoutSeconds:    30,
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
	invalid = valid
	invalid.ReadinessTimeoutSeconds = 31
	if err := invalid.Validate(); err == nil {
		t.Fatal("readiness timeout above hard maximum accepted")
	}
	invalid = valid
	invalid.AttachmentWorkerLeaseSeconds = 60
	if err := invalid.Validate(); err == nil {
		t.Fatal("attachment worker lease below safe minimum accepted")
	}
}
