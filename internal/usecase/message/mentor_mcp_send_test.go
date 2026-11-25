package message

import (
	"context"
	"errors"
	"testing"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type memoryMentorMCPSendReceipts map[[2]uuid.UUID]domain.MentorMCPMessageReceipt

func (r memoryMentorMCPSendReceipts) GetMentorMCPMessageReceipt(_ context.Context, mentor, request uuid.UUID) (domain.MentorMCPMessageReceipt, error) {
	v, ok := r[[2]uuid.UUID{mentor, request}]
	if !ok {
		return v, domain.ErrNotFound
	}
	return v, nil
}

func (r memoryMentorMCPSendReceipts) PutMentorMCPMessageReceipt(_ context.Context, receipt domain.MentorMCPMessageReceipt) error {
	key := [2]uuid.UUID{receipt.MentorID, receipt.RequestID}
	if _, ok := r[key]; ok {
		return domain.ErrAlreadyExists
	}
	r[key] = receipt
	return nil
}

func TestMentorEducationMCPSendCommitsOnceAndRecoversReceiptAfterUnknownAck(t *testing.T) {
	s, _, g, store, outbox, _ := educationMessageFixture()
	binding := g.access.Binding
	g.authors[binding.MentorID] = g.access.MembershipID
	receipts := memoryMentorMCPSendReceipts{}
	s.MentorMCPReceipts = receipts
	ctx := context.Background()
	command := MentorMCPSendCommand{MentorMCPSendInput: MentorMCPSendInput{MentorID: binding.MentorID, RequestID: uuid.New(),
		Kind: binding.Kind, SourceID: binding.SourceID, DialogID: binding.DialogID, BindingID: binding.ID, ExpectedRevision: g.access.Revision, Body: "\t Проверим задачу.\n"},
		ConfirmationID: uuid.New()}
	if err := s.PreviewMentorMCPSend(ctx, command.MentorMCPSendInput); err != nil || len(receipts) != 0 || len(store.items) != 0 {
		t.Fatal("preview was not read-only", err)
	}
	stale := command
	stale.ExpectedRevision--
	if _, err := s.SendMentorMCP(ctx, stale); !errors.Is(err, domain.ErrMessageConflict) || len(receipts) != 0 || len(store.items) != 0 {
		t.Fatal("outdated Mentor context accepted", err)
	}
	committed, err := s.SendMentorMCP(ctx, command)
	if err != nil || committed.MessageID == uuid.Nil || len(receipts) != 1 || len(store.items) != 1 || len(outbox.items) != 0 ||
		store.items[committed.MessageID].Body != command.Body {
		t.Fatalf("exact educational message missing %#v %v", committed, err)
	}
	// The first response can be lost, or the message edited before retry.
	item := store.items[committed.MessageID]
	item.Body = "edited by the Mentor"
	store.items[item.ID] = item
	g.denyAt = g.calls + 1
	before := g.calls
	replay, err := s.SendMentorMCP(ctx, command)
	if err != nil || replay != committed || len(store.items) != 1 || g.calls != before {
		t.Fatalf("exact retry recreated or read old message: %+v %v", replay, err)
	}
	changed := command
	changed.Body += "..."
	if _, err = s.SendMentorMCP(ctx, changed); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatal("changed command read existing receipt", err)
	}
	changed = command
	changed.RequestID = uuid.New()
	if _, err = s.SendMentorMCP(ctx, changed); !errors.Is(err, domain.ErrForbidden) || len(store.items) != 1 {
		t.Fatal("revoked Mentor created a new message", err)
	}
}
