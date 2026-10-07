package nats

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	realtimeuc "github.com/bemulima/ms-go-dialog/internal/usecase/realtime"
	natsgo "github.com/nats-io/nats.go"
)

const LifecycleStream = "DIALOG_EVENTS"

var lifecycleSubjects = []string{string(domain.EventDialogCreated), string(domain.EventDialogUpdated), string(domain.EventDialogClosed), string(domain.EventDialogMemberAdded), string(domain.EventDialogMemberRemoved), string(domain.EventDialogMemberRoleUpdated), string(domain.EventDialogMessageCreated), string(domain.EventDialogMessageUpdated), string(domain.EventDialogMessageDeleted), string(domain.EventDialogMessageHidden), string(domain.EventDialogMessageRestored), string(domain.EventDialogAttachmentReady), string(domain.EventDialogAttachmentFailed), string(domain.EventDialogReadUpdated)}
var durableSubjects = append(append([]string(nil), lifecycleSubjects...), string(domain.EventDialogTeacherRequested), string(domain.EventDialogTeacherContextMutated))

type Client struct {
	Conn *natsgo.Conn
	mu   sync.Mutex
	js   natsgo.JetStreamContext
}

func Connect(url string) (*natsgo.Conn, error) {
	return natsgo.Connect(url, natsgo.Name("ms-go-dialog"), natsgo.Timeout(5*time.Second), natsgo.MaxReconnects(-1), natsgo.ReconnectWait(time.Second))
}

// EnsureLifecycleStream validates the platform-owned stream. Only infrastructure
// provisions or maintains shared streams; a Dialog restart must not change them.
func (c *Client) EnsureLifecycleStream(ctx context.Context) error {
	js, err := c.jetStream()
	if err != nil {
		return err
	}
	info, err := js.StreamInfo(LifecycleStream, natsgo.Context(ctx))
	if err != nil {
		return fmt.Errorf("validate infrastructure-provisioned %s: %w", LifecycleStream, err)
	}
	if info == nil {
		return fmt.Errorf("infrastructure-provisioned %s returned no configuration", LifecycleStream)
	}
	return validateLifecycleStream(info.Config)
}

// This is the publisher's compatibility check, not a second stream manifest.
// Infrastructure separately validates every critical setting against its
// canonical manifest and is the only owner allowed to repair drift.
func validateLifecycleStream(config natsgo.StreamConfig) error {
	if config.Name != LifecycleStream {
		return fmt.Errorf("expected lifecycle stream %s, got %q", LifecycleStream, config.Name)
	}
	if config.Retention != natsgo.LimitsPolicy || config.Storage != natsgo.FileStorage {
		return fmt.Errorf("%s requires limits retention and file storage; repair through infrastructure", LifecycleStream)
	}
	if (config.MaxAge != 0 && config.MaxAge < 7*24*time.Hour) || config.Duplicates < 10*time.Minute {
		return fmt.Errorf("%s requires at least seven days retention and ten minutes deduplication; repair through infrastructure", LifecycleStream)
	}
	subjects := make(map[string]bool, len(config.Subjects))
	for _, subject := range config.Subjects {
		// Exact subjects keep Core NATS realtime traffic out of retained storage.
		if strings.ContainsAny(subject, "*>") || strings.HasPrefix(subject, "dialog.realtime.") {
			return fmt.Errorf("%s contains broad or ephemeral subject %q; repair through infrastructure", LifecycleStream, subject)
		}
		subjects[subject] = true
	}
	for _, required := range durableSubjects {
		if !subjects[required] {
			return fmt.Errorf("%s is missing durable subject %q; provision through infrastructure", LifecycleStream, required)
		}
	}
	return nil
}

func (c *Client) PublishLifecycle(ctx context.Context, event domain.OutboxEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	js, err := c.jetStream()
	if err != nil {
		return err
	}
	message := natsgo.NewMsg(string(event.Subject))
	message.Data = append([]byte(nil), event.Payload...)
	message.Header.Set(natsgo.MsgIdHdr, event.ID.String())
	if _, err = js.PublishMsg(message, natsgo.Context(ctx)); err != nil {
		return fmt.Errorf("publish lifecycle: %w", err)
	}
	return nil
}
func (c *Client) PublishTyping(ctx context.Context, dialogID string, payload []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if c == nil || c.Conn == nil {
		return errors.New("NATS connection is not configured")
	}
	return c.Conn.Publish("dialog.realtime.typing."+dialogID, payload)
}

func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.Conn == nil || !c.Conn.IsConnected() {
		return errors.New("NATS connection is unavailable")
	}
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("NATS readiness context must have a deadline")
	}
	return c.Conn.FlushWithContext(ctx)
}

func (c *Client) jetStream() (natsgo.JetStreamContext, error) {
	if c == nil || c.Conn == nil {
		return nil, errors.New("NATS connection is not configured")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.js != nil {
		return c.js, nil
	}
	js, err := c.Conn.JetStream()
	if err == nil {
		c.js = js
	}
	return js, err
}

var _ realtimeuc.LifecyclePublisher = (*Client)(nil)
