package nats

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
)

// Embedding the unimplemented API intentionally panics if startup ever calls a
// mutation such as AddStream or UpdateStream instead of the only allowed read.
type streamInfoOnly struct {
	natsgo.JetStreamContext
	info  *natsgo.StreamInfo
	err   error
	reads int
}

func (s *streamInfoOnly) StreamInfo(name string, _ ...natsgo.JSOpt) (*natsgo.StreamInfo, error) {
	if name != LifecycleStream {
		panic("unexpected lifecycle stream")
	}
	s.reads++
	return s.info, s.err
}

func lifecycleFixtureConfig() natsgo.StreamConfig {
	return natsgo.StreamConfig{Name: LifecycleStream, Subjects: append([]string(nil), durableSubjects...), Retention: natsgo.LimitsPolicy, Storage: natsgo.FileStorage, MaxAge: 7 * 24 * time.Hour, Duplicates: 10 * time.Minute}
}

func TestEnsureLifecycleStreamOnlyReadsProvisionedStream(t *testing.T) {
	for _, test := range []struct {
		name    string
		info    *natsgo.StreamInfo
		err     error
		wantErr string
	}{
		{name: "compatible", info: &natsgo.StreamInfo{Config: lifecycleFixtureConfig()}},
		{name: "missing", err: natsgo.ErrStreamNotFound, wantErr: "infrastructure-provisioned"},
		{name: "unavailable", err: context.DeadlineExceeded, wantErr: "infrastructure-provisioned"},
		{name: "empty response", wantErr: "returned no configuration"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &streamInfoOnly{info: test.info, err: test.err}
			client := &Client{Conn: &natsgo.Conn{}, js: reader}
			err := client.EnsureLifecycleStream(context.Background())
			if test.wantErr == "" && err != nil || test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("validation err=%v want=%q", err, test.wantErr)
			}
			if test.err != nil && !errors.Is(err, test.err) {
				t.Fatalf("validation did not retain broker error: %v", err)
			}
			if reader.reads != 1 {
				t.Fatalf("StreamInfo calls=%d want=1", reader.reads)
			}
		})
	}
}

func TestEnsureLifecycleStreamRejectsDriftWithoutMutations(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*natsgo.StreamConfig)
	}{
		{name: "wrong stream", mutate: func(c *natsgo.StreamConfig) { c.Name = "OTHER" }},
		{name: "missing teacher request", mutate: func(c *natsgo.StreamConfig) { c.Subjects = c.Subjects[:14] }},
		{name: "missing teacher context mutation", mutate: func(c *natsgo.StreamConfig) { c.Subjects = c.Subjects[:15] }},
		{name: "workqueue retention", mutate: func(c *natsgo.StreamConfig) { c.Retention = natsgo.WorkQueuePolicy }},
		{name: "memory storage", mutate: func(c *natsgo.StreamConfig) { c.Storage = natsgo.MemoryStorage }},
		{name: "short expiry", mutate: func(c *natsgo.StreamConfig) { c.MaxAge = time.Hour }},
		{name: "short deduplication", mutate: func(c *natsgo.StreamConfig) { c.Duplicates = time.Minute }},
		{name: "broad subjects", mutate: func(c *natsgo.StreamConfig) { c.Subjects = append(c.Subjects, "dialog.>") }},
		{name: "ephemeral typing", mutate: func(c *natsgo.StreamConfig) { c.Subjects = append(c.Subjects, "dialog.realtime.typing.fixture") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := lifecycleFixtureConfig()
			test.mutate(&config)
			reader := &streamInfoOnly{info: &natsgo.StreamInfo{Config: config}}
			client := &Client{Conn: &natsgo.Conn{}, js: reader}
			if err := client.EnsureLifecycleStream(context.Background()); err == nil {
				t.Fatal("incompatible infrastructure stream accepted")
			}
			if reader.reads != 1 {
				t.Fatalf("StreamInfo calls=%d want=1", reader.reads)
			}
		})
	}
}

func TestValidateLifecycleStreamPreservesCompatibleAdditions(t *testing.T) {
	config := lifecycleFixtureConfig()
	config.Subjects = append(config.Subjects, "dialog.future")
	config.MaxAge = 0 // Infrastructure may choose unlimited retention.
	if err := validateLifecycleStream(config); err != nil {
		t.Fatalf("compatible infrastructure addition rejected: %v", err)
	}
}

func TestDurableSubjectsRetainTeacherAndExcludeEphemeralTraffic(t *testing.T) {
	config := lifecycleFixtureConfig()
	if len(config.Subjects) != 16 {
		t.Fatalf("durable subject count=%d want=16", len(config.Subjects))
	}
	for _, required := range []string{"dialog.teacher.requested", "dialog.teacher.context-mutated"} {
		found := false
		for _, subject := range config.Subjects {
			if subject == required {
				found = true
			}
			if strings.ContainsAny(subject, "*>") || strings.HasPrefix(subject, "dialog.realtime.") {
				t.Fatalf("ephemeral/broad subject is retained: %q", subject)
			}
		}
		if !found {
			t.Fatalf("teacher durable subject missing: %q", required)
		}
	}
}
