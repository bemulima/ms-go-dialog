package dialog

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

type contactDialogStore struct {
	dialogStore
	onGet func()
}

func (s *contactDialogStore) FindPersonalByKey(_ context.Context, spaceID uuid.UUID, key []byte) (domain.Dialog, error) {
	if s.item.ID == uuid.Nil || s.item.SpaceID != spaceID || !bytes.Equal(s.item.PersonalKey, key) {
		return domain.Dialog{}, domain.ErrNotFound
	}
	return s.item, nil
}
func (*contactDialogStore) LockPersonalKey(context.Context, uuid.UUID, []byte) error { return nil }
func (s *contactDialogStore) GetByID(ctx context.Context, id uuid.UUID) (domain.Dialog, error) {
	if s.onGet != nil {
		s.onGet()
	}
	return s.dialogStore.GetByID(ctx, id)
}

type contactMemberStore struct{ dialogMemberStore }

func (s *contactMemberStore) CreateMany(_ context.Context, items []domain.Member) error {
	for _, item := range items {
		s.items[item.UserID] = item
	}
	return nil
}

type contactBlocks struct {
	repository.BlockRepository
	calls  int
	denyAt int
	err    error
}

func (s *contactBlocks) ExistsEitherDirection(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	s.calls++
	return s.denyAt > 0 && s.calls >= s.denyAt, s.err
}

type contactParticipants struct {
	calls    [][]uuid.UUID
	callback func(int) error
}

func (s *contactParticipants) RequireActiveUsers(_ context.Context, ids []uuid.UUID) error {
	s.calls = append(s.calls, append([]uuid.UUID(nil), ids...))
	if s.callback != nil {
		return s.callback(len(s.calls))
	}
	return nil
}

type contactFixture struct {
	service      MentorContactService
	in           MentorContactInput
	dialogs      *contactDialogStore
	members      *contactMemberStore
	blocks       *contactBlocks
	participants *contactParticipants
	outbox       *dialogOutboxStore
}

func newContactFixture() contactFixture {
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	in := MentorContactInput{StudentID: uuid.New(), MentorID: uuid.New()}
	dialogs := &contactDialogStore{}
	members := &contactMemberStore{dialogMemberStore{items: map[uuid.UUID]domain.Member{}}}
	blocks, participants, outbox := &contactBlocks{}, &contactParticipants{}, &dialogOutboxStore{}
	d := &Service{Dialogs: dialogs, Members: members, Blocks: blocks, Participants: participants, Outbox: outbox, Tx: &dialogTx{}, Now: func() time.Time { return now }, NewID: uuid.New,
		Spaces: &dialogSpaceStore{item: domain.Space{ID: uuid.New(), Key: "platform", Name: "Platform", Status: domain.SpaceStatusActive, Policy: domain.DefaultPolicy(), CreatedBy: in.StudentID, CreatedAt: now, UpdatedAt: now}}}
	return contactFixture{MentorContactService{Dialogs: d, Enabled: true, SpaceKey: "platform"}, in, dialogs, members, blocks, participants, outbox}
}

func TestMentorContactEnsuresOneOrdinaryPersonalPair(t *testing.T) {
	f := newContactFixture()
	first, err := f.service.Ensure(context.Background(), f.in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Ensure(context.Background(), f.in)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.DialogID == uuid.Nil || first.StudentID != f.in.StudentID || first.MentorID != f.in.MentorID || first.DialogVersion != 1 {
		t.Fatalf("exact pair replay mismatch: first=%+v second=%+v", first, second)
	}
	if f.dialogs.createCalls != 1 || len(f.outbox.items) != 1 || f.outbox.items[0].Subject != domain.EventDialogCreated || len(f.members.items) != 2 {
		t.Fatal("pair retry created additional conversation, memberships or events")
	}
	if f.dialogs.item.Type != domain.DialogTypePersonal || f.dialogs.item.StudentID != uuid.Nil || f.dialogs.item.PersonalTeacherID != uuid.Nil || f.dialogs.item.MessageCount != 0 {
		t.Fatal("contact became a Teacher binding or sent a message")
	}
	for _, at := range []int{0, 2, 3, 5} {
		ids := f.participants.calls[at]
		if len(ids) != 2 || ids[0] != f.in.StudentID || ids[1] != f.in.MentorID {
			t.Fatalf("pair availability not fenced: %v", f.participants.calls)
		}
	}
}

func TestMentorContactDeniesAtEveryPairBlockFence(t *testing.T) {
	for at := 1; at <= 6; at++ {
		t.Run(string(rune('0'+at)), func(t *testing.T) {
			f := newContactFixture()
			f.blocks.denyAt = at
			got, err := f.service.Ensure(context.Background(), f.in)
			if !errors.Is(err, domain.ErrBlocked) || got != (MentorContact{}) {
				t.Fatalf("block fence %d leaked reply: %+v %v", at, got, err)
			}
			if at <= 3 && (f.dialogs.createCalls != 0 || len(f.outbox.items) != 0) {
				t.Fatal("initial/locked denial mutated conversation")
			}
		})
	}
}

func TestMentorContactDeniesCurrentParticipantOrDependencyFailure(t *testing.T) {
	for _, at := range []int{1, 2, 3} {
		t.Run(string(rune('0'+at)), func(t *testing.T) {
			f := newContactFixture()
			f.participants.callback = func(call int) error {
				if call == at {
					return domain.ErrParticipantUnavailable
				}
				return nil
			}
			got, err := f.service.Ensure(context.Background(), f.in)
			if !errors.Is(err, domain.ErrParticipantUnavailable) || got != (MentorContact{}) {
				t.Fatalf("late participant failure leaked reply: %+v %v", got, err)
			}
		})
	}
	f := newContactFixture()
	f.blocks.err = domain.ErrDependencyUnavailable
	if got, err := f.service.Ensure(context.Background(), f.in); !errors.Is(err, domain.ErrDependencyUnavailable) || got != (MentorContact{}) {
		t.Fatalf("dependency fail open: %+v %v", got, err)
	}
}

func TestMentorContactDefaultOffAndMissingGuards(t *testing.T) {
	for _, name := range []string{"disabled", "nil service", "blocks", "participants", "space", "transaction"} {
		t.Run(name, func(t *testing.T) {
			f := newContactFixture()
			want := domain.ErrDependencyUnavailable
			switch name {
			case "disabled":
				f.service.Enabled = false
				want = domain.ErrFeatureDisabled
			case "nil service":
				f.service.Dialogs = nil
			case "blocks":
				f.service.Dialogs.Blocks = nil
			case "participants":
				f.service.Dialogs.Participants = nil
			case "space":
				f.service.SpaceKey = ""
			case "transaction":
				f.service.Dialogs.Tx = nil
			}
			got, err := f.service.Ensure(context.Background(), f.in)
			if !errors.Is(err, want) || got != (MentorContact{}) || f.dialogs.createCalls != 0 {
				t.Fatalf("guard %s failed: %+v %v", name, got, err)
			}
		})
	}
}

func TestMentorContactRejectsInvalidPairAndCancellation(t *testing.T) {
	for _, change := range []func(*MentorContactInput){func(i *MentorContactInput) { i.StudentID = uuid.Nil }, func(i *MentorContactInput) { i.MentorID = uuid.Nil }, func(i *MentorContactInput) { i.MentorID = i.StudentID }} {
		f := newContactFixture()
		change(&f.in)
		if _, err := f.service.Ensure(context.Background(), f.in); !errors.Is(err, domain.ErrValidation) || f.dialogs.createCalls != 0 {
			t.Fatalf("invalid pair accepted: %v", err)
		}
	}
	f := newContactFixture()
	ctx, cancel := context.WithCancel(context.Background())
	f.participants.callback = func(call int) error {
		if call == 3 {
			cancel()
		}
		return nil
	}
	if got, err := f.service.Ensure(ctx, f.in); !errors.Is(err, context.Canceled) || got != (MentorContact{}) {
		t.Fatalf("cancelled reply leaked: %+v %v", got, err)
	}
}

func TestMentorContactReadbackDeniesChangedContextAndMembers(t *testing.T) {
	for _, name := range []string{"hidden", "closed", "teacher", "foreign member", "left member", "disabled space", "personal forbidden"} {
		t.Run(name, func(t *testing.T) {
			f := newContactFixture()
			f.dialogs.onGet = func() {
				switch name {
				case "hidden":
					f.dialogs.item.Status = domain.DialogStatusHidden
				case "closed":
					f.dialogs.item.Status = domain.DialogStatusClosed
				case "teacher":
					f.dialogs.item.PersonalTeacherID = uuid.New()
				case "foreign member":
					delete(f.members.items, f.in.MentorID)
					foreign := uuid.New()
					f.members.items[foreign] = domain.Member{DialogID: f.dialogs.item.ID, UserID: foreign, Status: domain.MemberStatusActive}
				case "left member":
					m := f.members.items[f.in.MentorID]
					m.Status = domain.MemberStatusLeft
					f.members.items[f.in.MentorID] = m
				case "disabled space":
					f.service.Dialogs.Spaces.(*dialogSpaceStore).item.Status = domain.SpaceStatusDisabled
				case "personal forbidden":
					f.service.Dialogs.Spaces.(*dialogSpaceStore).item.Policy.AllowPersonal = false
				}
			}
			got, err := f.service.Ensure(context.Background(), f.in)
			if err == nil || got != (MentorContact{}) {
				t.Fatalf("%s leaked current pair: %+v %v", name, got, err)
			}
		})
	}
}
