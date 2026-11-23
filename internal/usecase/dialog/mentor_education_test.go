package dialog

import (
	"context"
	"errors"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"testing"
)

type educationBindings struct {
	repository.MentorRepository
	items map[uuid.UUID]domain.MentorBinding
}

func (b *educationBindings) Lock(context.Context, domain.MentorContextKind, uuid.UUID) error {
	return nil
}
func (b *educationBindings) Get(_ context.Context, id uuid.UUID) (domain.MentorBinding, error) {
	v, ok := b.items[id]
	if !ok {
		return v, domain.ErrNotFound
	}
	return v, nil
}
func (b *educationBindings) Find(_ context.Context, k domain.MentorContextKind, id uuid.UUID) (domain.MentorBinding, error) {
	for _, v := range b.items {
		if v.Kind == k && v.SourceID == id {
			return v, nil
		}
	}
	return domain.MentorBinding{}, domain.ErrNotFound
}
func (b *educationBindings) Put(_ context.Context, v domain.MentorBinding) error {
	b.items[v.ID] = v
	return nil
}
func (b *educationBindings) Protected(_ context.Context, id uuid.UUID) (bool, error) {
	for _, v := range b.items {
		if v.DialogID == id && v.Kind == domain.MentorCohort {
			return true, nil
		}
	}
	return false, nil
}

type educationOwner struct {
	p      MentorOwnerProof
	calls  int
	denyAt int
	hook   func(int)
}

func (o *educationOwner) ReadAccess(_ context.Context, k domain.MentorContextKind, id, actor uuid.UUID, authors []uuid.UUID) (MentorOwnerProof, error) {
	o.calls++
	if o.hook != nil {
		o.hook(o.calls)
	}
	if o.denyAt > 0 && o.calls >= o.denyAt {
		return MentorOwnerProof{}, domain.ErrForbidden
	}
	p := o.p
	p.Kind = k
	p.SourceID = id
	p.ActorID = actor
	p.Authors = map[uuid.UUID]uuid.UUID{}
	for _, a := range authors {
		p.Authors[a] = p.MembershipID
	}
	return p, nil
}
func TestMentorEducationUsesExistingPersonalPairAndCurrentOwner(t *testing.T) {
	f := newContactFixture()
	pair, err := f.service.Ensure(context.Background(), f.in)
	if err != nil {
		t.Fatal(err)
	}
	b := &educationBindings{items: map[uuid.UUID]domain.MentorBinding{}}
	owner := &educationOwner{p: MentorOwnerProof{MentorID: f.in.MentorID, StudentID: f.in.StudentID, MembershipID: uuid.New(), Revision: 3, HistoryFromRevision: 3}}
	s := MentorEducationService{Enabled: true, SpaceKey: "platform", Dialogs: f.service.Dialogs, Bindings: b, Owner: owner}
	in := MentorEnsureInput{Kind: domain.MentorAssignment, ID: uuid.New()}
	actor := domain.Actor{UserID: f.in.StudentID, Role: "STUDENT"}
	got, err := s.Ensure(context.Background(), actor, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.DialogID != pair.DialogID || f.dialogs.createCalls != 1 || len(b.items) != 1 {
		t.Fatalf("duplicate chat: %+v", got)
	}
	if _, err = s.Ensure(context.Background(), actor, in); err != nil || len(b.items) != 1 {
		t.Fatalf("retry=%v", err)
	}
	owner.denyAt = owner.calls + 1
	if _, err = s.Check(domain.WithMentorBinding(context.Background(), got.BindingID), actor.UserID, got.DialogID, nil); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("revoke=%v", err)
	}
	if _, err = s.Check(context.Background(), actor.UserID, got.DialogID, nil); err != nil {
		t.Fatalf("personal history destroyed: %v", err)
	}
}
func TestMentorEducationCohortHasNoShadowRosterOrGenericAccess(t *testing.T) {
	f := newContactFixture()
	owner := &educationOwner{p: MentorOwnerProof{MentorID: f.in.MentorID, MembershipID: uuid.New(), Revision: 9, HistoryFromRevision: 9}}
	b := &educationBindings{items: map[uuid.UUID]domain.MentorBinding{}}
	s := &MentorEducationService{Enabled: true, SpaceKey: "platform", Dialogs: f.service.Dialogs, Bindings: b, Owner: owner}
	f.service.Dialogs.Education = s
	in := MentorEnsureInput{Kind: domain.MentorCohort, ID: uuid.New()}
	actor := domain.Actor{UserID: f.in.StudentID, Role: "STUDENT"}
	got, err := s.Ensure(context.Background(), actor, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.members.items) != 1 || f.members.items[f.in.MentorID].Role != domain.MemberRoleOwner || len(f.outbox.items) != 0 {
		t.Fatal("Cohort roster or unguarded event was copied")
	}
	if _, err = s.Check(context.Background(), actor.UserID, got.DialogID, nil); !errors.Is(err, domain.ErrDialogNotFound) {
		t.Fatalf("generic read=%v", err)
	}
	ctx := domain.WithMentorBinding(context.Background(), got.BindingID)
	view, err := f.service.Dialogs.Get(ctx, actor, got.DialogID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Members) != 2 || view.CurrentMember.UserID != actor.UserID || view.Dialog.LastMessageID != nil || view.Dialog.MessageCount != 0 {
		t.Fatalf("private projection=%+v", view)
	}
	if _, err = f.service.Dialogs.Get(context.Background(), actor, got.DialogID); !errors.Is(err, domain.ErrDialogNotFound) {
		t.Fatalf("generic=%v", err)
	}
	owner.denyAt = owner.calls + 1
	if _, err = f.service.Dialogs.Get(ctx, actor, got.DialogID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("leave=%v", err)
	}
}
func TestMentorEducationBlocksAndChangedOwnerFence(t *testing.T) {
	for _, blockAt := range []int{1, 2} {
		t.Run(string(rune('0'+blockAt)), func(t *testing.T) {
			f := newContactFixture()
			f.blocks.denyAt = blockAt
			owner := &educationOwner{p: MentorOwnerProof{MentorID: f.in.MentorID, MembershipID: uuid.New(), Revision: 4, HistoryFromRevision: 2}}
			s := MentorEducationService{Enabled: true, SpaceKey: "platform", Dialogs: f.service.Dialogs, Bindings: &educationBindings{items: map[uuid.UUID]domain.MentorBinding{}}, Owner: owner}
			_, err := s.Ensure(context.Background(), domain.Actor{UserID: f.in.StudentID, Role: "ADMIN"}, MentorEnsureInput{Kind: domain.MentorCohort, ID: uuid.New()})
			if !errors.Is(err, domain.ErrBlocked) {
				t.Fatalf("block fence=%v", err)
			}
		})
	}
	f := newContactFixture()
	owner := &educationOwner{p: MentorOwnerProof{MentorID: f.in.MentorID, MembershipID: uuid.New(), Revision: 4, HistoryFromRevision: 2}}
	owner.hook = func(n int) {
		if n == 2 {
			owner.p.Revision++
		}
	}
	s := MentorEducationService{Enabled: true, SpaceKey: "platform", Dialogs: f.service.Dialogs, Bindings: &educationBindings{items: map[uuid.UUID]domain.MentorBinding{}}, Owner: owner}
	if _, err := s.Ensure(context.Background(), domain.Actor{UserID: f.in.StudentID, Role: "STUDENT"}, MentorEnsureInput{Kind: domain.MentorCohort, ID: uuid.New()}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("changed snapshot=%v", err)
	}
	s.Enabled = false
	if _, err := s.Ensure(context.Background(), domain.Actor{UserID: f.in.StudentID, Role: "STUDENT"}, MentorEnsureInput{}); !errors.Is(err, domain.ErrFeatureDisabled) {
		t.Fatal(err)
	}
}
