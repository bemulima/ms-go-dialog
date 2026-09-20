package message

import (
	"fmt"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	natsgo "github.com/nats-io/nats.go"
)

var lifecycleSubjects = []string{
	string(domain.EventDialogCreated),
	string(domain.EventDialogUpdated),
	string(domain.EventDialogClosed),
	string(domain.EventDialogMemberAdded),
	string(domain.EventDialogMemberRemoved),
	string(domain.EventDialogMemberRoleUpdated),
	string(domain.EventDialogMessageCreated),
	string(domain.EventDialogMessageUpdated),
	string(domain.EventDialogMessageDeleted),
	string(domain.EventDialogMessageHidden),
	string(domain.EventDialogMessageRestored),
	string(domain.EventDialogAttachmentReady),
	string(domain.EventDialogAttachmentFailed),
	string(domain.EventDialogReadUpdated),
}

type RealtimeSink interface {
	BroadcastLifecycle(domain.EventSubject, []byte)
	BroadcastEphemeral([]byte)
}
type Subscription struct{ items []*natsgo.Subscription }

func SubscribeRealtime(conn *natsgo.Conn, sink RealtimeSink) (*Subscription, error) {
	if conn == nil {
		return nil, fmt.Errorf("NATS connection is not configured")
	}
	result := &Subscription{}
	for _, subject := range lifecycleSubjects {
		item, err := conn.Subscribe(subject, func(message *natsgo.Msg) {
			sink.BroadcastLifecycle(domain.EventSubject(message.Subject), append([]byte(nil), message.Data...))
		})
		if err != nil {
			_ = result.Close()
			return nil, err
		}
		result.items = append(result.items, item)
	}
	typing, err := conn.Subscribe("dialog.realtime.typing.*", func(message *natsgo.Msg) { sink.BroadcastEphemeral(append([]byte(nil), message.Data...)) })
	if err != nil {
		_ = result.Close()
		return nil, err
	}
	result.items = append(result.items, typing)
	if err = conn.Flush(); err != nil {
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
