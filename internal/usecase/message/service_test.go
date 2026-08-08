package message

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

func TestReadThrough_UpdatesOnlyAuthenticatedMember(t *testing.T) {
	now := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	dialogID, spaceID := uuid.New(), uuid.New()
	readerID, otherID, senderID := uuid.New(), uuid.New(), uuid.New()
	dialogs := &fakeDialogs{item: domain.Dialog{
		ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive,
		Title: "Group", CreatedBy: senderID, Version: 1, MemberCount: 3,
		MessageCount: 120, MaxMessageSequence: 120, MaxEventSequence: 200,
		LastMessageID: uuidPointer(uuid.New()), LastMessageAt: &now, CreatedAt: now, UpdatedAt: now,
	}}
	members := &fakeMembers{items: map[uuid.UUID]domain.Member{
		readerID: activeMember(dialogID, readerID, 100, 20, now),
		otherID:  activeMember(dialogID, otherID, 100, 20, now),
		senderID: activeMember(dialogID, senderID, 120, 0, now),
	}}
	messages := &fakeMessages{}
	outbox := &fakeOutbox{}
	service := Service{
		Spaces:  fakeSpaces{item: domain.Space{ID: spaceID, Key: "platform", Name: "Platform", Status: domain.SpaceStatusActive, Policy: domain.DefaultPolicy(), CreatedBy: senderID, CreatedAt: now, UpdatedAt: now}},
		Dialogs: dialogs, Members: members, Messages: messages, Outbox: outbox, Tx: fakeTx{},
		Now: func() time.Time { return now.Add(time.Minute) }, NewID: uuid.New,
	}

	state, err := service.ReadThrough(context.Background(), domain.Actor{UserID: readerID, Role: "STUDENT"}, dialogID, 105)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastReadMessageSequence != 105 || state.UnreadCount != 15 {
		t.Fatalf("reader state mismatch: %+v", state)
	}
	if got := members.items[readerID]; got.LastReadMessageSequence != 105 || got.UnreadCount != 15 {
		t.Fatalf("persisted reader state mismatch: %+v", got)
	}
	if got := members.items[otherID]; got.LastReadMessageSequence != 100 || got.UnreadCount != 20 {
		t.Fatalf("another member was changed: %+v", got)
	}
	if len(outbox.items) != 1 || outbox.items[0].Subject != domain.EventDialogReadUpdated {
		t.Fatalf("read event mismatch: %+v", outbox.items)
	}
}

func TestReadAll_UsesCurrentDialogMaximumForOneMember(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, readerID, otherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogs := &fakeDialogs{item: domain.Dialog{
		ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive,
		Title: "Group", CreatedBy: readerID, Version: 1, MemberCount: 2,
		MessageCount: 50, MaxMessageSequence: 50, MaxEventSequence: 70,
		LastMessageID: uuidPointer(uuid.New()), LastMessageAt: &now, CreatedAt: now, UpdatedAt: now,
	}}
	members := &fakeMembers{items: map[uuid.UUID]domain.Member{
		readerID: activeMember(dialogID, readerID, 10, 40, now),
		otherID:  activeMember(dialogID, otherID, 20, 30, now),
	}}
	service := Service{
		Spaces:  fakeSpaces{item: domain.Space{ID: spaceID, Key: "platform", Name: "Platform", Status: domain.SpaceStatusActive, Policy: domain.DefaultPolicy(), CreatedBy: readerID, CreatedAt: now, UpdatedAt: now}},
		Dialogs: dialogs, Members: members, Messages: &fakeMessages{}, Outbox: &fakeOutbox{}, Tx: fakeTx{},
		Now: func() time.Time { return now.Add(time.Second) }, NewID: uuid.New,
	}
	state, err := service.ReadAll(context.Background(), domain.Actor{UserID: readerID, Role: "STUDENT"}, dialogID)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastReadMessageSequence != 50 || state.UnreadCount != 0 {
		t.Fatalf("read-all mismatch: %+v", state)
	}
	if got := members.items[otherID]; got.LastReadMessageSequence != 20 || got.UnreadCount != 30 {
		t.Fatalf("read-all changed another member: %+v", got)
	}
}

func TestReadThrough_OlderConcurrentRequestIsIdempotent(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, readerID := uuid.New(), uuid.New(), uuid.New()
	dialogs := &fakeDialogs{item: domain.Dialog{
		ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive,
		Title: "Group", CreatedBy: readerID, Version: 3, MemberCount: 2,
		MessageCount: 50, MaxMessageSequence: 50, MaxEventSequence: 60,
		LastMessageID: uuidPointer(uuid.New()), LastMessageAt: &now, CreatedAt: now, UpdatedAt: now,
	}}
	members := &fakeMembers{items: map[uuid.UUID]domain.Member{
		readerID: activeMember(dialogID, readerID, 30, 20, now),
	}}
	messages := &fakeMessages{}
	outbox := &fakeOutbox{}
	service := Service{
		Spaces:  fakeSpaces{item: domain.Space{ID: spaceID, Key: "platform", Name: "Platform", Status: domain.SpaceStatusActive, Policy: domain.DefaultPolicy(), CreatedBy: readerID, CreatedAt: now, UpdatedAt: now}},
		Dialogs: dialogs, Members: members, Messages: messages, Outbox: outbox, Tx: fakeTx{},
	}

	state, err := service.ReadThrough(context.Background(), domain.Actor{UserID: readerID, Role: "USER"}, dialogID, 25)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastReadMessageSequence != 30 || state.UnreadCount != 20 {
		t.Fatalf("older request changed state: %+v", state)
	}
	if messages.countUnreadCalls != 0 || len(outbox.items) != 0 {
		t.Fatalf("older request performed a mutation: count_calls=%d events=%d", messages.countUnreadCalls, len(outbox.items))
	}
}

func TestReadThrough_DisabledSpaceDoesNotMutateMember(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, readerID := uuid.New(), uuid.New(), uuid.New()
	dialogs := &fakeDialogs{item: domain.Dialog{
		ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive,
		Title: "Group", CreatedBy: readerID, Version: 1, MemberCount: 2,
		MessageCount: 10, MaxMessageSequence: 10, MaxEventSequence: 10,
		LastMessageID: uuidPointer(uuid.New()), LastMessageAt: &now, CreatedAt: now, UpdatedAt: now,
	}}
	members := &fakeMembers{items: map[uuid.UUID]domain.Member{readerID: activeMember(dialogID, readerID, 5, 5, now)}}
	service := Service{
		Spaces:  fakeSpaces{item: domain.Space{ID: spaceID, Key: "platform", Name: "Platform", Status: domain.SpaceStatusDisabled, Policy: domain.DefaultPolicy(), CreatedBy: readerID, CreatedAt: now, UpdatedAt: now}},
		Dialogs: dialogs, Members: members, Messages: &fakeMessages{}, Outbox: &fakeOutbox{}, Tx: fakeTx{},
	}

	_, err := service.ReadThrough(context.Background(), domain.Actor{UserID: readerID, Role: "USER"}, dialogID, 10)
	if !errors.Is(err, domain.ErrDialogNotFound) {
		t.Fatalf("disabled space error=%v", err)
	}
	if got := members.items[readerID]; got.LastReadMessageSequence != 5 || got.UnreadCount != 5 {
		t.Fatalf("disabled space changed member: %+v", got)
	}
}

func TestWindow_AppliesMemberHistoryBoundary(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, userID := uuid.New(), uuid.New(), uuid.New()
	messages := &fakeMessages{}
	member := activeMember(dialogID, userID, 50, 0, now)
	member.HistoryFromMessageSequence = 51
	service := Service{Spaces: fakeSpaces{item: domain.Space{ID: spaceID, Key: "platform", Name: "Platform", Status: domain.SpaceStatusActive, Policy: domain.DefaultPolicy(), CreatedBy: userID, CreatedAt: now, UpdatedAt: now}}, Dialogs: &fakeDialogs{item: domain.Dialog{ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive, Title: "Group", CreatedBy: userID, Version: 1, MemberCount: 1, MessageCount: 50, MaxMessageSequence: 50, MaxEventSequence: 50, CreatedAt: now, UpdatedAt: now}}, Members: &fakeMembers{items: map[uuid.UUID]domain.Member{userID: member}}, Messages: messages}
	if _, err := service.Window(context.Background(), domain.Actor{UserID: userID, Role: "USER"}, dialogID, 10, 10); err != nil {
		t.Fatal(err)
	}
	if messages.lastWindow.FromSequence != 51 {
		t.Fatalf("history boundary=%d", messages.lastWindow.FromSequence)
	}
}

type fakeTx struct{}

func (fakeTx) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type fakeSpaces struct{ item domain.Space }

func (f fakeSpaces) Create(context.Context, domain.Space) error { return nil }
func (f fakeSpaces) GetByID(_ context.Context, id uuid.UUID) (domain.Space, error) {
	if id != f.item.ID {
		return domain.Space{}, domain.ErrNotFound
	}
	return f.item, nil
}
func (f fakeSpaces) GetByKey(_ context.Context, key string) (domain.Space, error) {
	if key != f.item.Key {
		return domain.Space{}, domain.ErrNotFound
	}
	return f.item, nil
}
func (f fakeSpaces) Update(context.Context, domain.Space) error             { return nil }
func (f fakeSpaces) List(context.Context, int, int) ([]domain.Space, error) { return nil, nil }

type fakeDialogs struct{ item domain.Dialog }

func (f *fakeDialogs) Create(_ context.Context, item domain.Dialog) error { f.item = item; return nil }
func (f *fakeDialogs) GetByID(_ context.Context, id uuid.UUID) (domain.Dialog, error) {
	if id != f.item.ID {
		return domain.Dialog{}, domain.ErrNotFound
	}
	return f.item, nil
}
func (f *fakeDialogs) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (domain.Dialog, error) {
	return f.GetByID(ctx, id)
}
func (f *fakeDialogs) FindPersonalByKey(context.Context, uuid.UUID, []byte) (domain.Dialog, error) {
	return domain.Dialog{}, domain.ErrNotFound
}
func (f *fakeDialogs) LockPersonalKey(context.Context, uuid.UUID, []byte) error { return nil }
func (f *fakeDialogs) ListForUser(context.Context, repository.DialogListQuery) ([]repository.DialogListItem, error) {
	return nil, nil
}
func (f *fakeDialogs) ListAdmin(context.Context, repository.AdminDialogListQuery) ([]domain.Dialog, error) {
	return nil, nil
}
func (f *fakeDialogs) UpdateState(_ context.Context, item domain.Dialog, expected int) error {
	if f.item.Version != expected {
		return domain.ErrMessageConflict
	}
	f.item = item
	return nil
}

type fakeMembers struct{ items map[uuid.UUID]domain.Member }

func (f *fakeMembers) CreateMany(_ context.Context, items []domain.Member) error {
	for _, item := range items {
		f.items[item.UserID] = item
	}
	return nil
}
func (f *fakeMembers) Create(_ context.Context, item domain.Member) error {
	f.items[item.UserID] = item
	return nil
}
func (f *fakeMembers) Get(_ context.Context, dialogID, userID uuid.UUID) (domain.Member, error) {
	item, ok := f.items[userID]
	if !ok || item.DialogID != dialogID {
		return domain.Member{}, domain.ErrNotFound
	}
	return item, nil
}
func (f *fakeMembers) GetForUpdate(ctx context.Context, dialogID, userID uuid.UUID) (domain.Member, error) {
	return f.Get(ctx, dialogID, userID)
}
func (f *fakeMembers) ListActive(context.Context, uuid.UUID) ([]domain.Member, error) {
	return nil, nil
}
func (f *fakeMembers) Update(_ context.Context, item domain.Member) error {
	f.items[item.UserID] = item
	return nil
}
func (f *fakeMembers) IncrementUnreadRecipients(context.Context, uuid.UUID, uuid.UUID, int64) error {
	return nil
}
func (f *fakeMembers) DecrementUnreadForDeletedMessage(context.Context, uuid.UUID, uuid.UUID, int64, int64) error {
	return nil
}
func (f *fakeMembers) IncrementUnreadForRestoredMessage(context.Context, uuid.UUID, uuid.UUID, int64, int64) error {
	return nil
}
func (f *fakeMembers) CountActiveOwners(context.Context, uuid.UUID) (int, error) { return 1, nil }
func (f *fakeMembers) ListActiveDialogSequencesForUser(context.Context, uuid.UUID, uuid.UUID) (map[uuid.UUID]int64, error) {
	return nil, nil
}

type fakeMessages struct {
	lastList         repository.MessageListQuery
	lastWindow       repository.MessageWindowQuery
	lastChanges      repository.MessageChangeQuery
	countUnreadCalls int
}

func (*fakeMessages) Create(context.Context, domain.Message) error { return nil }
func (*fakeMessages) GetByID(context.Context, uuid.UUID) (domain.Message, error) {
	return domain.Message{}, domain.ErrNotFound
}
func (*fakeMessages) GetByIDForUpdate(context.Context, uuid.UUID) (domain.Message, error) {
	return domain.Message{}, domain.ErrNotFound
}
func (*fakeMessages) GetByIdempotencyKey(context.Context, uuid.UUID, uuid.UUID) (domain.Message, error) {
	return domain.Message{}, domain.ErrNotFound
}
func (*fakeMessages) LockIdempotencyKey(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakeMessages) List(_ context.Context, q repository.MessageListQuery) ([]domain.Message, error) {
	f.lastList = q
	return nil, nil
}
func (f *fakeMessages) Window(_ context.Context, q repository.MessageWindowQuery) ([]domain.Message, error) {
	f.lastWindow = q
	return nil, nil
}
func (f *fakeMessages) ListChanges(_ context.Context, q repository.MessageChangeQuery) ([]domain.Message, error) {
	f.lastChanges = q
	return nil, nil
}
func (*fakeMessages) FirstUnreadIncoming(_ context.Context, _ uuid.UUID, _ uuid.UUID, after int64) (int64, error) {
	return after + 1, nil
}
func (f *fakeMessages) CountUnreadIncoming(_ context.Context, _ uuid.UUID, _ uuid.UUID, after, through int64) (int64, error) {
	f.countUnreadCalls++
	if through < after {
		return 0, domain.ErrInvalidReadSequence
	}
	return through - after, nil
}
func (*fakeMessages) UpdateContent(context.Context, domain.Message, int) error { return nil }
func (*fakeMessages) MarkDeleted(context.Context, domain.Message, int) error   { return nil }
func (*fakeMessages) AdvanceEvent(context.Context, uuid.UUID, int64) (domain.Message, error) {
	return domain.Message{}, domain.ErrNotFound
}
func (*fakeMessages) UpdateModerationStatus(context.Context, domain.Message, domain.MessageStatus, int) error {
	return nil
}

type fakeOutbox struct{ items []domain.OutboxEvent }

func (f *fakeOutbox) Add(_ context.Context, item domain.OutboxEvent) error {
	f.items = append(f.items, item)
	return nil
}
func (*fakeOutbox) ClaimPending(context.Context, time.Time, time.Time, int) ([]domain.OutboxEvent, error) {
	return nil, nil
}
func (*fakeOutbox) MarkPublished(context.Context, uuid.UUID, time.Time) error      { return nil }
func (*fakeOutbox) MarkFailed(context.Context, uuid.UUID, time.Time, string) error { return nil }

func activeMember(dialogID, userID uuid.UUID, read, unread int64, now time.Time) domain.Member {
	return domain.Member{
		DialogID: dialogID, UserID: userID, Role: domain.MemberRoleMember, Status: domain.MemberStatusActive,
		LastReadMessageSequence: read, UnreadCount: unread, AddedBy: userID, JoinedAt: now, UpdatedAt: now,
	}
}

func uuidPointer(id uuid.UUID) *uuid.UUID { return &id }

var _ repository.SpaceRepository = fakeSpaces{}
var _ repository.DialogRepository = (*fakeDialogs)(nil)
var _ repository.MemberRepository = (*fakeMembers)(nil)
var _ repository.MessageRepository = (*fakeMessages)(nil)
var _ repository.OutboxRepository = (*fakeOutbox)(nil)
var _ repository.TransactionManager = fakeTx{}
