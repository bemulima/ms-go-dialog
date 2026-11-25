package message

import (
	"context"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	"github.com/google/uuid"
)

const mentorMCPBodyRunes = 800

type MentorMCPMessage struct {
	MessageID     uuid.UUID `json:"message_id"`
	SenderID      uuid.UUID `json:"sender_id"`
	Body          string    `json:"body"`
	BodyTruncated bool      `json:"body_truncated"`
	SentAt        time.Time `json:"sent_at"`
}

type MentorMCPMessagePage struct {
	Items              []MentorMCPMessage `json:"items"`
	NextBeforeSequence string             `json:"next_before_sequence,omitempty"`
}

type MentorMCPBindingVerifier interface {
	MentorMCPBinding(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
}

func (s Service) MentorMCPMessages(ctx context.Context, bindings MentorMCPBindingVerifier, mentor, dialog, binding uuid.UUID, before int64, limit int) (MentorMCPMessagePage, error) {
	if bindings == nil || s.Education == nil {
		return MentorMCPMessagePage{}, domain.ErrDependencyUnavailable
	}
	if before < 0 || limit < 1 || limit > dialoguc.MentorMCPPageLimit {
		return MentorMCPMessagePage{}, domain.ErrValidation
	}
	if err := bindings.MentorMCPBinding(ctx, mentor, dialog, binding); err != nil {
		return MentorMCPMessagePage{}, err
	}
	q := repository.MessageListQuery{DialogID: dialog, Limit: limit}
	if before != 0 {
		q.Before = &repository.MessageCursor{ID: binding, Sequence: before}
	}
	page, err := s.MentorPage(domain.WithMentorBinding(ctx, binding), domain.Actor{UserID: mentor, Role: "MENTOR"}, q)
	if err != nil {
		return MentorMCPMessagePage{}, err
	}
	out := MentorMCPMessagePage{Items: make([]MentorMCPMessage, 0, len(page.Items))}
	if page.Next != nil {
		out.NextBeforeSequence = strconv.FormatInt(page.Next.Sequence, 10)
	}
	for _, view := range page.Items {
		m := view.Message
		if m.Status != domain.MessageStatusActive {
			continue
		}
		if m.MentorBindingID == nil || *m.MentorBindingID != binding || m.DialogID != dialog || m.AuthorType != domain.MessageAuthorUser ||
			m.SenderID == uuid.Nil || m.ID == uuid.Nil || m.CreatedAt.IsZero() || !utf8.ValidString(m.Body) {
			return MentorMCPMessagePage{}, domain.ErrDependencyUnavailable
		}
		body, truncated := truncateMentorMCPBody(m.Body)
		out.Items = append(out.Items, MentorMCPMessage{MessageID: m.ID, SenderID: m.SenderID, Body: body, BodyTruncated: truncated, SentAt: m.CreatedAt.UTC()})
	}
	return out, nil
}

func truncateMentorMCPBody(body string) (string, bool) {
	runes := 0
	for i := range body {
		if runes == mentorMCPBodyRunes {
			return body[:i], true
		}
		runes++
	}
	return body, false
}
