package nats

import (
	"context"
	"errors"
	"fmt"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	realtimeuc "github.com/bemulima/ms-go-dialog/internal/usecase/realtime"
	natsgo "github.com/nats-io/nats.go"
	"sync"
	"time"
)

const LifecycleStream = "DIALOG_EVENTS"

var lifecycleSubjects = []string{string(domain.EventDialogCreated), string(domain.EventDialogUpdated), string(domain.EventDialogClosed), string(domain.EventDialogMemberAdded), string(domain.EventDialogMemberRemoved), string(domain.EventDialogMemberRoleUpdated), string(domain.EventDialogMessageCreated), string(domain.EventDialogMessageUpdated), string(domain.EventDialogMessageDeleted), string(domain.EventDialogMessageHidden), string(domain.EventDialogMessageRestored), string(domain.EventDialogAttachmentReady), string(domain.EventDialogAttachmentFailed), string(domain.EventDialogReadUpdated)}

type Client struct {
	Conn *natsgo.Conn
	mu   sync.Mutex
	js   natsgo.JetStreamContext
}

func Connect(url string) (*natsgo.Conn, error) {
	return natsgo.Connect(url, natsgo.Name("ms-go-dialog"), natsgo.Timeout(5*time.Second), natsgo.MaxReconnects(-1), natsgo.ReconnectWait(time.Second))
}
func (c *Client) EnsureLifecycleStream(ctx context.Context) error {
	js, err := c.jetStream()
	if err != nil {
		return err
	}
	if _, err = js.StreamInfo(LifecycleStream, natsgo.Context(ctx)); err == nil {
		return nil
	} else if !errors.Is(err, natsgo.ErrStreamNotFound) {
		return err
	}
	_, err = js.AddStream(&natsgo.StreamConfig{Name: LifecycleStream, Subjects: append([]string(nil), lifecycleSubjects...), Retention: natsgo.LimitsPolicy, Storage: natsgo.FileStorage, MaxAge: 7 * 24 * time.Hour, Duplicates: 10 * time.Minute}, natsgo.Context(ctx))
	return err
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
