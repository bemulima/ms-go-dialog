package message

import (
	"context"
	"encoding/json"
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

func TestWindow_NoUnreadUsesFullBudgetForLatestMessages(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, userID := uuid.New(), uuid.New(), uuid.New()
	messages := &fakeMessages{}
	service := Service{
		Spaces:   fakeSpaces{item: domain.Space{ID: spaceID, Key: "platform", Name: "Platform", Status: domain.SpaceStatusActive, Policy: domain.DefaultPolicy(), CreatedBy: userID, CreatedAt: now, UpdatedAt: now}},
		Dialogs:  &fakeDialogs{item: domain.Dialog{ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive, Title: "Group", CreatedBy: userID, Version: 1, MemberCount: 1, MessageCount: 50, MaxMessageSequence: 50, MaxEventSequence: 50, CreatedAt: now, UpdatedAt: now}},
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{userID: activeMember(dialogID, userID, 50, 0, now)}},
		Messages: messages,
	}

	if _, err := service.Window(context.Background(), domain.Actor{UserID: userID, Role: "USER"}, dialogID, 10, 20); err != nil {
		t.Fatal(err)
	}
	if messages.lastWindow.AnchorSequence != 51 || messages.lastWindow.Before != 31 || messages.lastWindow.After != 1 {
		t.Fatalf("latest window budget mismatch: %+v", messages.lastWindow)
	}
}

func TestWindow_TrimsProbesAndReportsAvailableDirections(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, userID := uuid.New(), uuid.New(), uuid.New()
	messages := &fakeMessages{windowItems: []domain.Message{
		messageAt(dialogID, userID, 7, now),
		messageAt(dialogID, userID, 8, now),
		messageAt(dialogID, uuid.New(), 9, now),
		messageAt(dialogID, uuid.New(), 10, now),
		messageAt(dialogID, uuid.New(), 11, now),
	}}
	service := Service{
		Spaces:   fakeSpaces{item: domain.Space{ID: spaceID, Key: "platform", Name: "Platform", Status: domain.SpaceStatusActive, Policy: domain.DefaultPolicy(), CreatedBy: userID, CreatedAt: now, UpdatedAt: now}},
		Dialogs:  &fakeDialogs{item: domain.Dialog{ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive, Title: "Group", CreatedBy: userID, Version: 1, MemberCount: 2, MessageCount: 20, MaxMessageSequence: 20, MaxEventSequence: 20, CreatedAt: now, UpdatedAt: now}},
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{userID: activeMember(dialogID, userID, 8, 12, now)}},
		Messages: messages,
	}

	window, err := service.Window(context.Background(), domain.Actor{UserID: userID, Role: "USER"}, dialogID, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !window.HasOlder || !window.HasNewer || len(window.Items) != 3 {
		t.Fatalf("window directions/items mismatch: %+v", window)
	}
	if window.Items[0].Message.MessageSequence != 8 || window.Items[2].Message.MessageSequence != 10 {
		t.Fatalf("probe rows leaked into window: %+v", window.Items)
	}
}

func TestCreate_TeacherDialogPublishesDedicatedRequest(t *testing.T) {
	now := time.Date(2026, 8, 30, 13, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	contextID, learningActionID := uuid.New(), uuid.New()
	dialogs := &fakeDialogs{item: teacherDialog(dialogID, spaceID, studentID, teacherID, contextID, now)}
	members := &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}}
	messages := &fakeMessages{items: map[uuid.UUID]domain.Message{}}
	outbox := &fakeOutbox{}
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: dialogs,
		Members: members, Messages: messages, Outbox: outbox, Tx: fakeTx{},
		Now: func() time.Time { return now }, NewID: uuid.New,
	}

	result, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{
		DialogID: dialogID, Body: "Помоги разобраться", IdempotencyKey: uuid.New(), LearningActionID: &learningActionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.View.Message.AuthorType != domain.MessageAuthorUser || result.View.Message.LearningActionID == nil || *result.View.Message.LearningActionID != learningActionID {
		t.Fatalf("created teacher request mismatch: %+v", result)
	}
	if len(outbox.items) != 2 || outbox.items[0].Subject != domain.EventDialogMessageCreated || outbox.items[1].Subject != domain.EventDialogTeacherRequested {
		t.Fatalf("teacher request events mismatch: %+v", outbox.items)
	}
	if outbox.items[0].EventSequence != outbox.items[1].EventSequence {
		t.Fatalf("one message mutation must share one event sequence: %+v", outbox.items)
	}
	if string(outbox.items[1].Payload) == "" || containsJSONField(outbox.items[1].Payload, "body") {
		t.Fatalf("teacher trigger leaked message body: %s", outbox.items[1].Payload)
	}
}

func TestCreate_LessonTeacherDialogRequiresRevisionAnchor(t *testing.T) {
	now := time.Date(2026, 8, 31, 15, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID, lessonID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, lessonID, now)
	dialogItem.TeacherContextType = domain.TeacherContextLesson
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: &fakeDialogs{item: dialogItem},
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: &fakeMessages{items: map[uuid.UUID]domain.Message{}}, Outbox: &fakeOutbox{}, Tx: fakeTx{},
		Now: func() time.Time { return now }, NewID: uuid.New,
	}

	_, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{
		DialogID: dialogID, Body: "Объясни этот фрагмент", IdempotencyKey: uuid.New(),
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("missing lesson context error = %v", err)
	}
	result, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{
		DialogID: dialogID, Body: "Объясни этот фрагмент", IdempotencyKey: uuid.New(),
		LessonContext: &domain.LessonMessageContext{
			ContentRevision: now.Format(time.RFC3339Nano), SelectedText: "for i := 0; i < n; i++",
		},
	})
	if err != nil {
		t.Fatalf("create anchored lesson message: %v", err)
	}
	if !result.Created || result.View.Message.LessonContext == nil || result.View.Message.LessonContext.SelectedText == "" {
		t.Fatalf("lesson anchor was not stored: %+v", result)
	}
}

func TestTeacherRequestContextAndAppendResponse(t *testing.T) {
	now := time.Date(2026, 8, 30, 14, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	contextID, learningActionID, sourceID := uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, contextID, now)
	dialogItem.MessageCount = 1
	dialogItem.MaxMessageSequence = 1
	dialogItem.MaxEventSequence = 2
	dialogItem.LastMessageID = &sourceID
	dialogItem.LastMessageAt = &now
	source := domain.Message{
		ID: sourceID, DialogID: dialogID, AuthorType: domain.MessageAuthorUser, SenderID: studentID,
		LearningActionID: &learningActionID, Body: "Почему тест падает?", Status: domain.MessageStatusActive,
		Version: 1, MessageSequence: 1, LastEventSequence: 2, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now,
	}
	messages := &fakeMessages{items: map[uuid.UUID]domain.Message{sourceID: source}, windowItems: []domain.Message{source}}
	outbox := &fakeOutbox{}
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: &fakeDialogs{item: dialogItem},
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: messages, TeacherMessages: messages, Outbox: outbox, Tx: fakeTx{},
		Now: func() time.Time { return now.Add(time.Second) }, NewID: uuid.New,
	}

	requestContext, err := service.GetTeacherRequestContext(context.Background(), dialogID, teacherID, sourceID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if requestContext.Source.ID != sourceID || len(requestContext.Messages) != 1 || requestContext.Dialog.PersonalTeacherID != teacherID {
		t.Fatalf("bounded request context mismatch: %+v", requestContext)
	}

	idempotencyKey := uuid.New()
	result, err := service.AppendTeacherResponse(context.Background(), AppendTeacherResponseInput{
		DialogID: dialogID, PersonalTeacherID: teacherID, SourceMessageID: sourceID,
		IdempotencyKey: idempotencyKey, Body: "Проверь условие перед циклом.",
	})
	if err != nil {
		t.Fatal(err)
	}
	message := result.View.Message
	if !result.Created || message.AuthorType != domain.MessageAuthorPersonalTeacher || message.PersonalTeacherID != teacherID || message.SenderID != uuid.Nil ||
		message.ReplyToMessageID == nil || *message.ReplyToMessageID != sourceID || message.LearningActionID == nil || *message.LearningActionID != learningActionID {
		t.Fatalf("teacher response mismatch: %+v", result)
	}
	if len(outbox.items) != 1 || outbox.items[0].Subject != domain.EventDialogMessageCreated {
		t.Fatalf("teacher response outbox mismatch: %+v", outbox.items)
	}
	replay, err := service.AppendTeacherResponse(context.Background(), AppendTeacherResponseInput{
		DialogID: dialogID, PersonalTeacherID: teacherID, SourceMessageID: sourceID,
		IdempotencyKey: idempotencyKey, Body: "Проверь условие перед циклом.",
	})
	if err != nil || replay.Created || replay.View.Message.ID != message.ID || len(outbox.items) != 1 {
		t.Fatalf("teacher response replay mismatch: replay=%+v err=%v events=%d", replay, err, len(outbox.items))
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
	windowItems      []domain.Message
	countUnreadCalls int
	items            map[uuid.UUID]domain.Message
}

func (f *fakeMessages) Create(_ context.Context, item domain.Message) error {
	if f.items == nil {
		f.items = make(map[uuid.UUID]domain.Message)
	}
	f.items[item.ID] = item
	return nil
}
func (f *fakeMessages) GetByID(_ context.Context, id uuid.UUID) (domain.Message, error) {
	item, ok := f.items[id]
	if !ok {
		return domain.Message{}, domain.ErrNotFound
	}
	return item, nil
}
func (*fakeMessages) GetByIDForUpdate(context.Context, uuid.UUID) (domain.Message, error) {
	return domain.Message{}, domain.ErrNotFound
}
func (*fakeMessages) GetByIdempotencyKey(context.Context, uuid.UUID, uuid.UUID) (domain.Message, error) {
	return domain.Message{}, domain.ErrNotFound
}
func (*fakeMessages) LockIdempotencyKey(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakeMessages) GetByTeacherIdempotencyKey(_ context.Context, personalTeacherID, key uuid.UUID) (domain.Message, error) {
	for _, item := range f.items {
		if item.AuthorType == domain.MessageAuthorPersonalTeacher && item.PersonalTeacherID == personalTeacherID && item.IdempotencyKey == key {
			return item, nil
		}
	}
	return domain.Message{}, domain.ErrNotFound
}
func (*fakeMessages) LockTeacherIdempotencyKey(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (f *fakeMessages) List(_ context.Context, q repository.MessageListQuery) ([]domain.Message, error) {
	f.lastList = q
	return nil, nil
}
func (f *fakeMessages) Window(_ context.Context, q repository.MessageWindowQuery) ([]domain.Message, error) {
	f.lastWindow = q
	return f.windowItems, nil
}

func messageAt(dialogID, senderID uuid.UUID, sequence int64, now time.Time) domain.Message {
	return domain.Message{
		ID: uuid.New(), DialogID: dialogID, AuthorType: domain.MessageAuthorUser, SenderID: senderID, IdempotencyKey: uuid.New(),
		Body: "message", Status: domain.MessageStatusActive, Version: 1,
		MessageSequence: sequence, LastEventSequence: sequence, CreatedAt: now, UpdatedAt: now,
	}
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
var _ repository.TeacherMessageRepository = (*fakeMessages)(nil)
var _ repository.OutboxRepository = (*fakeOutbox)(nil)
var _ repository.TransactionManager = fakeTx{}

func teacherDialog(dialogID, spaceID, studentID, teacherID, contextID uuid.UUID, now time.Time) domain.Dialog {
	return domain.Dialog{
		ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeTeacher, Status: domain.DialogStatusActive,
		StudentID: studentID, PersonalTeacherID: teacherID, TeacherContextType: domain.TeacherContextPracticeTask, ContextID: &contextID,
		CreatedBy: studentID, Version: 1, MemberCount: 1, CreatedAt: now, UpdatedAt: now,
	}
}

func activeTestSpace(spaceID, createdBy uuid.UUID, now time.Time) domain.Space {
	return domain.Space{ID: spaceID, Key: "platform", Name: "Platform", Status: domain.SpaceStatusActive, Policy: domain.DefaultPolicy(), CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now}
}

func containsJSONField(payload []byte, field string) bool {
	var value map[string]any
	if json.Unmarshal(payload, &value) != nil {
		return false
	}
	_, ok := value[field]
	return ok
}
