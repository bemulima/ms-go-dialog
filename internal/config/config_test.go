package config

import "testing"

func TestHTTPListenAddressPreservesContainerDefaultAndSupportsLoopback(t *testing.T) {
	if got := (Config{HTTPPort: "8080"}).HTTPListenAddress(); got != ":8080" {
		t.Fatalf("container listen address=%q want=%q", got, ":8080")
	}
	if got := (Config{HTTPHost: "127.0.0.1", HTTPPort: "18091"}).HTTPListenAddress(); got != "127.0.0.1:18091" {
		t.Fatalf("native listen address=%q want=%q", got, "127.0.0.1:18091")
	}
}

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
		DialogTeacherOrderingV2Enabled: false,
		ReadinessTimeoutSeconds:        2,
		ShutdownTimeoutSeconds:         10,
		ClamAVTimeoutSeconds:           30,
		UserServiceBaseURL:             "http://ms-user-service:8080",
		UserServiceInternalToken:       "user-secret",
		UserServiceTimeoutSeconds:      3,
		InternalAPIToken:               "dialog-secret",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	enabled := valid
	enabled.DialogTeacherOrderingV2Enabled = true
	if err := enabled.Validate(); err != nil {
		t.Fatalf("enabled teacher ordering v2 config rejected: %v", err)
	}
	enabled.DialogCanonicalStudentTurnEnabled = true
	if err := enabled.Validate(); err != nil {
		t.Fatalf("canonical materialization with ordering config rejected: %v", err)
	}
	invalid := valid
	invalid.DialogCanonicalStudentTurnEnabled = true
	if err := invalid.Validate(); err == nil {
		t.Fatal("canonical materialization without ordering v2 was accepted")
	}
	invalid = valid
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
	invalid = valid
	invalid.UserServiceTimeoutSeconds = 31
	if err := invalid.Validate(); err == nil {
		t.Fatal("user service timeout above hard maximum accepted")
	}
	invalid = valid
	invalid.UserServiceInternalToken = ""
	if err := invalid.Validate(); err == nil {
		t.Fatal("empty user service internal token accepted")
	}
}

func TestLoad_TeacherOrderingV2FlagDefaultsFalseAndCanEnable(t *testing.T) {
	t.Setenv("DIALOG_TEACHER_ORDERING_V2_ENABLED", "false")
	t.Setenv("DIALOG_CANONICAL_STUDENT_TURN_ENABLED", "false")
	cfg, err := Load()
	if err != nil || cfg.DialogTeacherOrderingV2Enabled {
		t.Fatalf("default-disabled teacher ordering config: cfg=%+v err=%v", cfg, err)
	}
	t.Setenv("DIALOG_TEACHER_ORDERING_V2_ENABLED", "true")
	cfg, err = Load()
	if err != nil || !cfg.DialogTeacherOrderingV2Enabled {
		t.Fatalf("enabled teacher ordering config: cfg=%+v err=%v", cfg, err)
	}
}

func TestLoad_CanonicalStudentTurnRequiresAndDefaultsBehindOrderingV2(t *testing.T) {
	t.Setenv("DIALOG_TEACHER_ORDERING_V2_ENABLED", "false")
	t.Setenv("DIALOG_CANONICAL_STUDENT_TURN_ENABLED", "true")
	if _, err := Load(); err == nil {
		t.Fatal("canonical materialization enabled without ordering v2")
	}
	t.Setenv("DIALOG_TEACHER_ORDERING_V2_ENABLED", "true")
	cfg, err := Load()
	if err != nil || !cfg.DialogCanonicalStudentTurnEnabled || !cfg.DialogTeacherOrderingV2Enabled {
		t.Fatalf("canonical materialization config mismatch: cfg=%+v err=%v", cfg, err)
	}
}
