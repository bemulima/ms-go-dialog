package dialog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

type dialogTx struct{ calls int }

func (f *dialogTx) WithinTransaction(ctx context.Context, run func(context.Context) error) error {
	f.calls++
	return run(ctx)
}

type dialogStore struct {
	repository.DialogRepository
	item domain.Dialog
}

func (f *dialogStore) GetByID(_ context.Context, id uuid.UUID) (domain.Dialog, error) {
	if id != f.item.ID {
		return domain.Dialog{}, domain.ErrNotFound
	}
	return f.item, nil
}
func (f *dialogStore) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (domain.Dialog, error) {
	return f.GetByID(ctx, id)
}
func (f *dialogStore) UpdateState(_ context.Context, item domain.Dialog, expected int) error {
	if f.item.Version != expected {
		return domain.ErrMessageConflict
	}
	f.item = item
	return nil
}

type dialogSpaceStore struct {
	repository.SpaceRepository
	item domain.Space
}

func (f *dialogSpaceStore) GetByID(_ context.Context, id uuid.UUID) (domain.Space, error) {
	if id != f.item.ID {
		return domain.Space{}, domain.ErrNotFound
	}
	return f.item, nil
}

type dialogMemberStore struct {
	repository.MemberRepository
	items map[uuid.UUID]domain.Member
}

func (f *dialogMemberStore) Get(_ context.Context, dialogID, userID uuid.UUID) (domain.Member, error) {
	item, exists := f.items[userID]
	if !exists || item.DialogID != dialogID {
		return domain.Member{}, domain.ErrNotFound
	}
	return item, nil
}
func (f *dialogMemberStore) GetForUpdate(ctx context.Context, dialogID, userID uuid.UUID) (domain.Member, error) {
	return f.Get(ctx, dialogID, userID)
}
func (f *dialogMemberStore) Create(_ context.Context, item domain.Member) error {
	f.items[item.UserID] = item
	return nil
}
func (f *dialogMemberStore) Update(_ context.Context, item domain.Member) error {
	f.items[item.UserID] = item
	return nil
}
func (f *dialogMemberStore) ListActive(_ context.Context, dialogID uuid.UUID) ([]domain.Member, error) {
	items := make([]domain.Member, 0, len(f.items))
	for _, item := range f.items {
		if item.DialogID == dialogID && item.Status == domain.MemberStatusActive {
			items = append(items, item)
		}
	}
	return items, nil
}
func (f *dialogMemberStore) CountActiveOwners(context.Context, uuid.UUID) (int, error) {
	count := 0
	for _, item := range f.items {
		if item.Status == domain.MemberStatusActive && item.Role == domain.MemberRoleOwner {
			count++
		}
	}
	return count, nil
}

type dialogOutboxStore struct {
	repository.OutboxRepository
	items []domain.OutboxEvent
}

func (f *dialogOutboxStore) Add(_ context.Context, item domain.OutboxEvent) error {
	f.items = append(f.items, item)
	return nil
}

type participantResolverStub struct{ calls int }

func (f *participantResolverStub) RequireActiveUsers(context.Context, []uuid.UUID) error {
	f.calls++
	return nil
}

func TestAddMember_RejoinStartsNewHistoryAndMembershipInterval(t *testing.T) {
	oldJoined := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	now := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	dialogID, spaceID, ownerID, targetID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	leftAt := now.Add(-time.Hour)
	dialogs := &dialogStore{item: domain.Dialog{
		ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive,
		Title: "group", CreatedBy: ownerID, Version: 4, MemberCount: 2, MessageCount: 40,
		MaxMessageSequence: 40, MaxEventSequence: 50, CreatedAt: oldJoined, UpdatedAt: now,
	}}
	space := domain.Space{ID: spaceID, Key: "platform", Name: "Platform", Status: domain.SpaceStatusActive, Policy: domain.DefaultPolicy(), CreatedBy: ownerID, CreatedAt: oldJoined, UpdatedAt: now}
	members := &dialogMemberStore{items: map[uuid.UUID]domain.Member{
		ownerID:  {DialogID: dialogID, UserID: ownerID, Role: domain.MemberRoleOwner, Status: domain.MemberStatusActive, AddedBy: ownerID, JoinedAt: oldJoined, UpdatedAt: now},
		targetID: {DialogID: dialogID, UserID: targetID, Role: domain.MemberRoleMember, Status: domain.MemberStatusRemoved, HistoryFromMessageSequence: 1, LastReadMessageSequence: 10, UnreadCount: 5, AddedBy: ownerID, JoinedAt: oldJoined, LeftAt: &leftAt, UpdatedAt: leftAt},
	}}
	outbox := &dialogOutboxStore{}
	tx := &dialogTx{}
	service := Service{Spaces: &dialogSpaceStore{item: space}, Dialogs: dialogs, Members: members, Outbox: outbox, Tx: tx, Now: func() time.Time { return now }, NewID: uuid.New}

	view, err := service.AddMember(context.Background(), domain.Actor{UserID: ownerID, Role: "USER"}, AddMemberInput{DialogID: dialogID, UserID: targetID})
	if err != nil {
		t.Fatal(err)
	}
	rejoined := members.items[targetID]
	if rejoined.Status != domain.MemberStatusActive || rejoined.JoinedAt != now || rejoined.LeftAt != nil {
		t.Fatalf("membership interval mismatch: %+v", rejoined)
	}
	if rejoined.HistoryFromMessageSequence != 41 || rejoined.LastReadMessageSequence != 40 || rejoined.UnreadCount != 0 {
		t.Fatalf("history/read boundary mismatch: %+v", rejoined)
	}
	if view.Dialog.MemberCount != 3 || view.Dialog.MaxEventSequence != 51 || len(outbox.items) != 1 || outbox.items[0].Subject != domain.EventDialogMemberAdded {
		t.Fatalf("dialog/event mismatch: dialog=%+v events=%+v", view.Dialog, outbox.items)
	}
}

func TestAddMember_ExistingMemberWinsOverFullGroup(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, ownerID, targetID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	policy := domain.DefaultPolicy()
	policy.MaxGroupMembers = 2
	dialogs := &dialogStore{item: domain.Dialog{ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive, Title: "group", CreatedBy: ownerID, Version: 1, MemberCount: 2, CreatedAt: now, UpdatedAt: now}}
	members := &dialogMemberStore{items: map[uuid.UUID]domain.Member{
		ownerID:  {DialogID: dialogID, UserID: ownerID, Role: domain.MemberRoleOwner, Status: domain.MemberStatusActive, AddedBy: ownerID, JoinedAt: now, UpdatedAt: now},
		targetID: {DialogID: dialogID, UserID: targetID, Role: domain.MemberRoleMember, Status: domain.MemberStatusActive, AddedBy: ownerID, JoinedAt: now, UpdatedAt: now},
	}}
	service := Service{Spaces: &dialogSpaceStore{item: domain.Space{ID: spaceID, Key: "platform", Name: "Platform", Status: domain.SpaceStatusActive, Policy: policy, CreatedBy: ownerID, CreatedAt: now, UpdatedAt: now}}, Dialogs: dialogs, Members: members, Outbox: &dialogOutboxStore{}, Tx: &dialogTx{}}

	_, err := service.AddMember(context.Background(), domain.Actor{UserID: ownerID, Role: "USER"}, AddMemberInput{DialogID: dialogID, UserID: targetID})
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("full group existing-member error=%v", err)
	}
}

func TestAddMember_DoesNotResolveTargetBeforeManagerAuthorization(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, actorID := uuid.New(), uuid.New(), uuid.New()
	dialogs := &dialogStore{item: domain.Dialog{ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive, Title: "group", CreatedBy: uuid.New(), Version: 1, MemberCount: 2, CreatedAt: now, UpdatedAt: now}}
	members := &dialogMemberStore{items: map[uuid.UUID]domain.Member{
		actorID: {DialogID: dialogID, UserID: actorID, Role: domain.MemberRoleMember, Status: domain.MemberStatusActive, AddedBy: actorID, JoinedAt: now, UpdatedAt: now},
	}}
	resolver := &participantResolverStub{}
	tx := &dialogTx{}
	service := Service{Spaces: &dialogSpaceStore{item: domain.Space{ID: spaceID, Key: "platform", Name: "Platform", Status: domain.SpaceStatusActive, Policy: domain.DefaultPolicy(), CreatedBy: actorID, CreatedAt: now, UpdatedAt: now}}, Dialogs: dialogs, Members: members, Participants: resolver, Tx: tx}

	_, err := service.AddMember(context.Background(), domain.Actor{UserID: actorID, Role: "USER"}, AddMemberInput{DialogID: dialogID, UserID: uuid.New()})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("authorization error=%v", err)
	}
	if resolver.calls != 0 || tx.calls != 0 {
		t.Fatalf("unauthorized request reached resolver/transaction: resolver=%d tx=%d", resolver.calls, tx.calls)
	}
}

var _ repository.TransactionManager = (*dialogTx)(nil)
