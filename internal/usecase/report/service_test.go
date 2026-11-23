package report

import (
	"context"
	"errors"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	adminuc "github.com/bemulima/ms-go-dialog/internal/usecase/admin"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/google/uuid"
	"testing"
	"time"
)

type reportMemory struct {
	rows        map[uuid.UUID]domain.MessageReport
	failResolve bool
}

func (r *reportMemory) LockCommand(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *reportMemory) FindCommand(_ context.Context, actor, key uuid.UUID) (domain.MessageReport, error) {
	for _, v := range r.rows {
		if v.ReporterID == actor && v.RequestID == key {
			return v, nil
		}
	}
	return domain.MessageReport{}, domain.ErrNotFound
}
func (r *reportMemory) GetForUpdate(_ context.Context, id uuid.UUID) (domain.MessageReport, error) {
	v, ok := r.rows[id]
	if !ok {
		return v, domain.ErrNotFound
	}
	return v, nil
}
func (r *reportMemory) Create(_ context.Context, v domain.MessageReport) error {
	r.rows[v.ID] = v
	return nil
}
func (r *reportMemory) Resolve(_ context.Context, v domain.MessageReport) error {
	if r.failResolve {
		return errors.New("receipt write failed")
	}
	r.rows[v.ID] = v
	return nil
}
func (r *reportMemory) List(context.Context, *uuid.UUID, int) ([]domain.MessageReport, error) {
	out := []domain.MessageReport{}
	for _, v := range r.rows {
		out = append(out, v)
	}
	return out, nil
}

type reportMessages struct {
	repository.MessageRepository
	item domain.Message
}

func (r *reportMessages) GetByIDForUpdate(context.Context, uuid.UUID) (domain.Message, error) {
	return r.item, nil
}
func (r *reportMessages) UpdateModerationStatus(_ context.Context, v domain.Message, _ domain.MessageStatus, _ int) error {
	r.item = v
	return nil
}

type reportReader struct {
	messages *reportMessages
	deny     bool
}

func (r *reportReader) Get(context.Context, domain.Actor, uuid.UUID) (messageuc.View, error) {
	if r.deny {
		return messageuc.View{}, domain.ErrForbidden
	}
	return messageuc.View{Message: r.messages.item}, nil
}

type reportDialogs struct {
	repository.DialogRepository
	item domain.Dialog
}

func (r *reportDialogs) GetByIDForUpdate(context.Context, uuid.UUID) (domain.Dialog, error) {
	return r.item, nil
}
func (r *reportDialogs) UpdateState(_ context.Context, v domain.Dialog, _ int) error {
	r.item = v
	return nil
}

type reportAttachments struct {
	repository.AttachmentRepository
}

func (reportAttachments) ListByMessage(context.Context, uuid.UUID, uuid.UUID) ([]domain.Attachment, error) {
	return []domain.Attachment{}, nil
}

type reportAtomicTx struct {
	depth    int
	reports  *reportMemory
	messages *reportMessages
	dialogs  *reportDialogs
}

func (x *reportAtomicTx) WithinTransaction(ctx context.Context, run func(context.Context) error) error {
	if x.depth > 0 {
		return run(ctx)
	}
	rows := map[uuid.UUID]domain.MessageReport{}
	for k, v := range x.reports.rows {
		rows[k] = v
	}
	m, d := x.messages.item, x.dialogs.item
	x.depth++
	err := run(ctx)
	x.depth--
	if err != nil {
		x.reports.rows = rows
		x.messages.item = m
		x.dialogs.item = d
	}
	return err
}
func reportFixture() (Service, domain.Actor, CreateInput, *reportMemory, *reportMessages, *reportReader) {
	b := uuid.New()
	actor := domain.Actor{UserID: uuid.New(), Role: "STUDENT"}
	now := time.Now().UTC()
	m := &reportMessages{item: domain.Message{ID: uuid.New(), DialogID: uuid.New(), SenderID: uuid.New(), Version: 1, Status: domain.MessageStatusActive, Body: "private message", MentorBindingID: &b}}
	r := &reportMemory{rows: map[uuid.UUID]domain.MessageReport{}}
	d := &reportDialogs{item: domain.Dialog{ID: m.item.DialogID, Version: 1}}
	reader := &reportReader{messages: m}
	tx := &reportAtomicTx{reports: r, messages: m, dialogs: d}
	admin := &adminuc.Service{Messages: m, Dialogs: d, Attachments: reportAttachments{}, Tx: tx}
	return Service{Enabled: true, Reports: r, Messages: m, Reader: reader, Moderator: admin, Tx: tx, Now: func() time.Time { return now }}, actor, CreateInput{RequestID: uuid.New(), MessageID: m.item.ID, Category: "HARASSMENT", Details: "A message targeted me"}, r, m, reader
}
func TestMentorEducationReportOwnReceiptRetryAndDenial(t *testing.T) {
	s, a, in, rows, _, reader := reportFixture()
	receipt, err := s.Create(context.Background(), a, in)
	if err != nil || len(rows.rows) != 1 {
		t.Fatal(receipt, err)
	}
	reader.deny = true
	replay, err := s.Create(context.Background(), a, in)
	if err != nil || replay != receipt || len(rows.rows) != 1 {
		t.Fatal("own immutable receipt lost", replay, err)
	}
	changed := in
	changed.Details = "changed"
	if _, err = s.Create(context.Background(), a, changed); !errors.Is(err, domain.ErrMessageConflict) {
		t.Fatal("changed bytes accepted", err)
	}
	changed = in
	changed.RequestID = uuid.New()
	a.Role = "ADMIN"
	if _, err = s.Create(context.Background(), a, changed); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("role bypassed current message read", err)
	}
	s.Enabled = false
	if _, err = s.Create(context.Background(), a, in); !errors.Is(err, domain.ErrFeatureDisabled) {
		t.Fatal("enabled by default")
	}
}
func TestMentorEducationReportHideAndAuditAreAtomic(t *testing.T) {
	s, a, in, rows, m, _ := reportFixture()
	receipt, err := s.Create(context.Background(), a, in)
	if err != nil {
		t.Fatal(err)
	}
	moderator := domain.Actor{UserID: uuid.New(), Role: "MODERATOR"}
	command := ResolveInput{RequestID: uuid.New(), ExpectedRevision: 1, Action: "HIDE", Reason: "Confirmed abuse"}
	if _, err = s.Resolve(context.Background(), a, receipt.ReportID, command); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("student moderation")
	}
	rows.failResolve = true
	if _, err = s.Resolve(context.Background(), moderator, receipt.ReportID, command); err == nil || m.item.Status != domain.MessageStatusActive || rows.rows[receipt.ReportID].Revision != 1 {
		t.Fatal("partial moderation committed", err)
	}
	rows.failResolve = false
	got, err := s.Resolve(context.Background(), moderator, receipt.ReportID, command)
	if err != nil || got.Revision != 2 || m.item.Status != domain.MessageStatusHidden || got.Resolution.ActorID != moderator.UserID {
		t.Fatal(got, err)
	}
	again, err := s.Resolve(context.Background(), moderator, receipt.ReportID, command)
	if err != nil || again.ResolutionDigest != got.ResolutionDigest || m.item.Version != 2 {
		t.Fatal("retry mutated again", again, err)
	}
	command.Reason = "new bytes"
	if _, err = s.Resolve(context.Background(), moderator, receipt.ReportID, command); !errors.Is(err, domain.ErrModerationConflict) {
		t.Fatal(err)
	}
	page, err := s.List(context.Background(), moderator, nil, 20)
	if err != nil || len(page.Items) != 1 || page.Items[0].Resolution == nil {
		t.Fatal("history missing", err)
	}
}
func TestMentorEducationReportNeverHidesChangedMessage(t *testing.T) {
	s, a, in, rows, m, _ := reportFixture()
	receipt, err := s.Create(context.Background(), a, in)
	if err != nil {
		t.Fatal(err)
	}
	m.item.Version++
	moderator := domain.Actor{UserID: uuid.New(), Role: "ADMIN"}
	command := ResolveInput{RequestID: uuid.New(), ExpectedRevision: 1, Action: "HIDE", Reason: "Reported revision"}
	if _, err = s.Resolve(context.Background(), moderator, receipt.ReportID, command); !errors.Is(err, domain.ErrModerationConflict) || m.item.Status != domain.MessageStatusActive || rows.rows[receipt.ReportID].Revision != 1 {
		t.Fatal("changed message hidden", err)
	}
	command.Action = "DISMISS"
	if _, err = s.Resolve(context.Background(), moderator, receipt.ReportID, command); err != nil || m.item.Status != domain.MessageStatusActive {
		t.Fatal("dismiss unavailable", err)
	}
}
