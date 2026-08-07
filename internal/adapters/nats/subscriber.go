package nats

import (
	"fmt"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	natsgo "github.com/nats-io/nats.go"
)

type RealtimeSink interface {
	BroadcastLifecycle(domain.EventSubject, []byte)
	BroadcastEphemeral([]byte)
}
type Subscription struct{ items []*natsgo.Subscription }

func (c *Client) SubscribeRealtime(sink RealtimeSink) (*Subscription, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("NATS connection is not configured")
	}
	result := &Subscription{}
	for _, subject := range lifecycleSubjects {
		item, err := c.Conn.Subscribe(subject, func(message *natsgo.Msg) {
			sink.BroadcastLifecycle(domain.EventSubject(message.Subject), append([]byte(nil), message.Data...))
		})
		if err != nil {
			_ = result.Close()
			return nil, err
		}
		result.items = append(result.items, item)
	}
	typing, err := c.Conn.Subscribe("dialog.realtime.typing.*", func(message *natsgo.Msg) { sink.BroadcastEphemeral(append([]byte(nil), message.Data...)) })
	if err != nil {
		_ = result.Close()
		return nil, err
	}
	result.items = append(result.items, typing)
	if err = c.Conn.Flush(); err != nil {
		_ = result.Close()
		return nil, err
	}
	return result, nil
}
func (s *Subscription) Close() error {
	if s == nil {
		return nil
	}
	var first error
	for _, item := range s.items {
		if err := item.Unsubscribe(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
