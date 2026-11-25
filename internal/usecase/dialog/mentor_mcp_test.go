package dialog

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type mcpEducationBindings struct {
	*educationBindings
	owner     uuid.UUID
	rows      []domain.MentorBinding
	lastLimit int
}

func (r *mcpEducationBindings) ListOwnedMentorBindings(_ context.Context, mentor, after uuid.UUID, limit int) ([]domain.MentorBinding, error) {
	r.lastLimit = limit
	if r.owner != mentor {
		return nil, nil
	}
	rows := slices.DeleteFunc(slices.Clone(r.rows), func(b domain.MentorBinding) bool { return b.ID.String() <= after.String() })
	return rows[:min(limit, len(rows))], nil
}

func TestMentorEducationMCPListUsesExistingCurrentBindings(t *testing.T) {
	ctx := context.Background()
	f := newContactFixture()
	pair, err := f.service.Ensure(ctx, f.in)
	if err != nil {
		t.Fatal(err)
	}
	owner := &educationOwner{p: MentorOwnerProof{MentorID: f.in.MentorID, StudentID: f.in.StudentID, MembershipID: uuid.New(), Revision: 3, HistoryFromRevision: 1}}
	first := domain.MentorBinding{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), DialogID: pair.DialogID, Kind: domain.MentorAssignment, SourceID: uuid.New(), MentorID: f.in.MentorID, StudentID: f.in.StudentID}
	second := first
	second.ID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	second.SourceID = uuid.New()
	store := &mcpEducationBindings{educationBindings: &educationBindings{items: map[uuid.UUID]domain.MentorBinding{first.ID: first, second.ID: second}}, owner: f.in.MentorID, rows: []domain.MentorBinding{first, second}}
	service := &MentorEducationService{Enabled: true, SpaceKey: "platform", Dialogs: f.service.Dialogs, Bindings: store, Owner: owner}
	f.service.Dialogs.Education = service
	owner.denyAt = 1
	items, after, err := service.MentorMCPDialogs(ctx, f.in.MentorID, uuid.Nil, 1)
	if err != nil || items == nil || len(items) != 0 || after != first.ID || store.lastLimit != 2 {
		t.Fatalf("denied binding blocked forward paging: %v %v %v", items, after, err)
	}
	owner.denyAt = 0
	items, after, err = service.MentorMCPDialogs(ctx, f.in.MentorID, after, 1)
	if err != nil || len(items) != 1 || items[0].BindingID != second.ID || items[0].SourceID != second.SourceID || after != uuid.Nil {
		t.Fatalf("current binding missing on next page: %v %v %v", items, after, err)
	}
	if err = service.MentorMCPBinding(ctx, f.in.StudentID, pair.DialogID, second.ID); !errors.Is(err, domain.ErrDialogNotFound) {
		t.Fatalf("MCP accepted a Student-side binding: %v", err)
	}
	if f.dialogs.createCalls != 1 || len(store.items) != 2 {
		t.Fatal("read created a dialog or binding")
	}
}
