package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	adminuc "github.com/bemulima/ms-go-dialog/internal/usecase/admin"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/google/uuid"
	"time"
)

type ReadableMessage interface {
	Get(context.Context, domain.Actor, uuid.UUID) (messageuc.View, error)
}
type Service struct {
	Enabled   bool
	Reports   repository.ReportRepository
	Messages  repository.MessageRepository
	Reader    ReadableMessage
	Moderator *adminuc.Service
	Tx        repository.TransactionManager
	Now       func() time.Time
	NewID     func() uuid.UUID
}
type CreateInput struct {
	RequestID uuid.UUID `json:"request_id"`
	MessageID uuid.UUID `json:"message_id"`
	Category  string    `json:"category"`
	Details   string    `json:"details"`
}
type ResolveInput struct {
	RequestID        uuid.UUID `json:"request_id"`
	ExpectedRevision int       `json:"expected_revision"`
	Action           string    `json:"action"`
	Reason           string    `json:"reason"`
}
type Receipt struct {
	Schema    string    `json:"schema"`
	ReportID  uuid.UUID `json:"report_id"`
	RequestID uuid.UUID `json:"request_id"`
	MessageID uuid.UUID `json:"message_id"`
	Revision  int       `json:"revision"`
	Status    string    `json:"status"`
}
type Page struct {
	Schema       string                 `json:"schema"`
	Items        []domain.MessageReport `json:"items"`
	NextBeforeID *uuid.UUID             `json:"next_before_id"`
}

func digest(v any) string {
	raw, _ := json.Marshal(v)
	d := sha256.Sum256(raw)
	return hex.EncodeToString(d[:])
}
func receipt(r domain.MessageReport) Receipt {
	return Receipt{"dialog.report-receipt.v1", r.ID, r.RequestID, r.MessageID, 1, "OPEN"}
}
func (s Service) ready() error {
	if !s.Enabled {
		return domain.ErrFeatureDisabled
	}
	if s.Reports == nil || s.Tx == nil {
		return domain.ErrDependencyUnavailable
	}
	return nil
}
func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s Service) newID() uuid.UUID {
	if s.NewID != nil {
		return s.NewID()
	}
	return uuid.New()
}
func moderator(a domain.Actor) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if a.Role != "ADMIN" && a.Role != "MODERATOR" {
		return domain.ErrForbidden
	}
	return nil
}
func (s Service) Create(ctx context.Context, a domain.Actor, in CreateInput) (Receipt, error) {
	if err := a.Validate(); err != nil {
		return Receipt{}, err
	}
	if err := s.ready(); err != nil {
		return Receipt{}, err
	}
	if in.RequestID == uuid.Nil || in.MessageID == uuid.Nil || !domain.ValidReportText(in.Details, 1000) || (in.Category != "SPAM" && in.Category != "HARASSMENT" && in.Category != "OTHER") {
		return Receipt{}, domain.ErrValidation
	}
	if s.Reader == nil {
		return Receipt{}, domain.ErrDependencyUnavailable
	}
	commandDigest := digest(struct {
		Actor   uuid.UUID
		Binding *uuid.UUID
		Command CreateInput
	}{a.UserID, domain.SelectedMentorBinding(ctx), in})
	var result Receipt
	err := s.Tx.WithinTransaction(ctx, func(tx context.Context) error {
		if err := s.Reports.LockCommand(tx, a.UserID, in.RequestID); err != nil {
			return err
		}
		prior, err := s.Reports.FindCommand(tx, a.UserID, in.RequestID)
		if err == nil {
			if prior.CommandDigest != commandDigest {
				return domain.ErrMessageConflict
			}
			result = receipt(prior)
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		view, err := s.Reader.Get(tx, a, in.MessageID)
		if err != nil {
			return err
		}
		if view.Message.Status != domain.MessageStatusActive {
			return domain.ErrMessageNotFound
		}
		item := domain.MessageReport{ID: s.newID(), RequestID: in.RequestID, ReporterID: a.UserID, MessageID: in.MessageID, DialogID: view.Message.DialogID, MessageVersion: view.Message.Version, Category: in.Category, Details: in.Details, Status: "OPEN", Revision: 1, CreatedAt: s.now(), CommandDigest: commandDigest, MentorBindingID: view.Message.MentorBindingID}
		if !item.Valid() {
			return domain.ErrValidation
		}
		if err = s.Reports.Create(tx, item); err != nil {
			return err
		}
		// Report submission is not a grant; changing access or message content during
		// persistence aborts this transaction. Existing own receipts remain readable.
		current, err := s.Reader.Get(tx, a, in.MessageID)
		if err != nil {
			return err
		}
		if current.Message.Version != item.MessageVersion {
			return domain.ErrMessageConflict
		}
		result = receipt(item)
		return nil
	})
	return result, err
}
func (s Service) List(ctx context.Context, a domain.Actor, before *uuid.UUID, limit int) (Page, error) {
	if err := moderator(a); err != nil {
		return Page{}, err
	}
	if err := s.ready(); err != nil {
		return Page{}, err
	}
	if limit < 1 || limit > 50 || before != nil && *before == uuid.Nil {
		return Page{}, domain.ErrValidation
	}
	rows, err := s.Reports.List(ctx, before, limit+1)
	if err != nil {
		return Page{}, err
	}
	p := Page{Schema: "dialog.report-list.v1", Items: []domain.MessageReport{}}
	for _, r := range rows {
		if !r.Valid() {
			return Page{}, domain.ErrDependencyUnavailable
		}
	}
	if len(rows) > limit {
		rows = rows[:limit]
		id := rows[len(rows)-1].ID
		p.NextBeforeID = &id
	}
	p.Items = append(p.Items, rows...)
	return p, nil
}
func (s Service) Resolve(ctx context.Context, a domain.Actor, id uuid.UUID, in ResolveInput) (domain.MessageReport, error) {
	if err := moderator(a); err != nil {
		return domain.MessageReport{}, err
	}
	if err := s.ready(); err != nil {
		return domain.MessageReport{}, err
	}
	if id == uuid.Nil || in.RequestID == uuid.Nil || in.ExpectedRevision != 1 || !domain.ValidReportText(in.Reason, 500) || (in.Action != "HIDE" && in.Action != "DISMISS") {
		return domain.MessageReport{}, domain.ErrValidation
	}
	commandDigest := digest(struct {
		Actor, Report uuid.UUID
		Command       ResolveInput
	}{a.UserID, id, in})
	var result domain.MessageReport
	err := s.Tx.WithinTransaction(ctx, func(tx context.Context) error {
		if err := s.Reports.LockCommand(tx, a.UserID, in.RequestID); err != nil {
			return err
		}
		item, err := s.Reports.GetForUpdate(tx, id)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrReportNotFound
		}
		if err != nil {
			return err
		}
		if item.Resolution != nil {
			if item.ResolutionDigest != commandDigest {
				return domain.ErrModerationConflict
			}
			result = item
			return nil
		}
		if item.Revision != in.ExpectedRevision {
			return domain.ErrModerationConflict
		}
		if in.Action == "HIDE" {
			if s.Messages == nil || s.Moderator == nil {
				return domain.ErrDependencyUnavailable
			}
			message, err := s.Messages.GetByIDForUpdate(tx, item.MessageID)
			if err != nil {
				return err
			}
			if message.Version != item.MessageVersion || message.Status != domain.MessageStatusActive {
				return domain.ErrModerationConflict
			}
			if _, err = s.Moderator.HideMessage(tx, a, item.MessageID); err != nil {
				return err
			}
		}
		item.Revision = 2
		item.Status = "RESOLVED"
		item.Resolution = &domain.ReportResolution{RequestID: in.RequestID, ActorID: a.UserID, Action: in.Action, Reason: in.Reason, ResolvedAt: s.now()}
		item.ResolutionDigest = commandDigest
		if !item.Valid() {
			return domain.ErrValidation
		}
		if err = s.Reports.Resolve(tx, item); err != nil {
			return err
		}
		result = item
		return nil
	})
	return result, err
}
