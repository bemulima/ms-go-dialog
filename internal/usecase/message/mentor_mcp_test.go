package message

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type mentorMCPBindingFunc func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error

func (f mentorMCPBindingFunc) MentorMCPBinding(ctx context.Context, mentor, dialog, binding uuid.UUID) error {
	return f(ctx, mentor, dialog, binding)
}

func TestMentorEducationMCPMessageReadPreservesCurrentAuthorHistory(t *testing.T) {
	s, student, guard, messages, outbox, _ := educationMessageFixture()
	ctx := context.Background()
	b := guard.access.Binding
	bindings := mentorMCPBindingFunc(func(_ context.Context, mentor, dialog, binding uuid.UUID) error {
		if mentor != b.MentorID || dialog != b.DialogID || binding != b.ID {
			return domain.ErrDialogNotFound
		}
		return nil
	})
	for n := int64(1); n <= 4; n++ {
		m := messageAt(b.DialogID, student.UserID, n, s.now())
		m.MentorBindingID, m.MentorContextRevision, m.MentorAuthorMembershipID = &b.ID, 7, guard.access.MembershipID
		m.Body = strings.Repeat("🙂", 801)
		switch n {
		case 3:
			m.Status = domain.MessageStatusHidden
		case 4:
			m.MentorAuthorMembershipID = uuid.New()
		}
		messages.items[m.ID] = m
	}
	page, err := s.MentorMCPMessages(ctx, bindings, b.MentorID, b.DialogID, b.ID, 0, 2)
	if err != nil || len(page.Items) != 0 || page.Items == nil || page.NextBeforeSequence != "3" {
		t.Fatalf("hidden or old membership data escaped: %+v %v", page, err)
	}
	page, err = s.MentorMCPMessages(ctx, bindings, b.MentorID, b.DialogID, b.ID, 3, 2)
	if err != nil || len(page.Items) != 2 || page.Items[0].SenderID != student.UserID || !page.Items[0].BodyTruncated ||
		utf8.RuneCountInString(page.Items[0].Body) != 800 || page.NextBeforeSequence != "" {
		t.Fatalf("current messages lost or malformed: %+v %v", page, err)
	}
	guard.denyAt = guard.calls + 1
	if page, err = s.MentorMCPMessages(ctx, bindings, b.MentorID, b.DialogID, b.ID, 3, 2); !errors.Is(err, domain.ErrForbidden) || page.Items != nil {
		t.Fatalf("revoked access returned page: %+v %v", page, err)
	}
	if len(outbox.items) != 0 {
		t.Fatal("read emitted an event")
	}
}
