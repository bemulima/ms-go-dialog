package message

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
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

func TestNonmemberCannotReadOrWriteGroupMessages(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, memberID, outsiderID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := domain.Dialog{
		ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive,
		Title: "Group", CreatedBy: memberID, Version: 1, MemberCount: 2,
		MessageCount: 1, MaxMessageSequence: 1, MaxEventSequence: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	space := domain.Space{ID: spaceID, Key: "platform", Name: "Platform", Status: domain.SpaceStatusActive,
		Policy: domain.DefaultPolicy(), CreatedBy: memberID, CreatedAt: now, UpdatedAt: now}
	newService := func() (*Service, *fakeDialogs, *fakeMessages, *fakeOutbox) {
		dialogs := &fakeDialogs{item: dialogItem}
		messages := &fakeMessages{items: make(map[uuid.UUID]domain.Message)}
		outbox := &fakeOutbox{}
		service := &Service{
			Spaces: fakeSpaces{item: space}, Dialogs: dialogs,
			Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{memberID: activeMember(dialogID, memberID, 1, 0, now)}},
			Messages: messages, Outbox: outbox, Tx: fakeTx{},
		}
		return service, dialogs, messages, outbox
	}

	t.Run("read", func(t *testing.T) {
		service, _, _, _ := newService()
		_, err := service.Window(context.Background(), domain.Actor{UserID: outsiderID, Role: "USER"}, dialogID, 10, 10)
		if !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("nonmember read error=%v, want forbidden", err)
		}
	})

	t.Run("write", func(t *testing.T) {
		service, dialogs, messages, outbox := newService()
		_, err := service.Create(context.Background(), domain.Actor{UserID: outsiderID, Role: "USER"}, CreateInput{
			DialogID: dialogID, IdempotencyKey: uuid.New(), Body: "must not be stored",
		})
		if !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("nonmember write error=%v, want forbidden", err)
		}
		if len(messages.items) != 0 || len(outbox.items) != 0 || dialogs.item.MaxMessageSequence != 1 || dialogs.item.MessageCount != 1 {
			t.Fatalf("nonmember write had side effects: messages=%d events=%d dialog=%+v", len(messages.items), len(outbox.items), dialogs.item)
		}
	})
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
	if outbox.items[1].SchemaVersion != 1 || result.View.Message.TeacherTurnSequence != nil || dialogs.item.MaxTeacherTurnSequence != 0 ||
		containsJSONField(outbox.items[1].Payload, "teacher_turn_sequence") || containsJSONField(outbox.items[1].Payload, "canonical_student_message_id") {
		t.Fatalf("default V1 request changed: message=%+v dialog=%+v event=%+v", result.View.Message, dialogs.item, outbox.items[1])
	}
	if containsJSONField(outbox.items[0].Payload, "assistant_ui") {
		t.Fatalf("body-only v1 event unexpectedly contains assistant_ui: %s", outbox.items[0].Payload)
	}
}

func TestCreate_TeacherOrderingV2EmitsBodyFreeRequestAfterAtomicAllocation(t *testing.T) {
	now := time.Date(2026, 9, 9, 13, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, uuid.New(), now)
	dialogItem.TeacherContextType, dialogItem.ContextID = domain.TeacherContextGeneralTeacher, nil
	dialogs := &fakeDialogs{item: dialogItem}
	messages, outbox := &fakeMessages{items: map[uuid.UUID]domain.Message{}}, &fakeOutbox{}
	var observations []TeacherRequestV2Observation
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: dialogs,
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: messages, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now }, NewID: uuid.New,
		TeacherOrderingV2Enabled: true,
		OnTeacherRequestV2:       func(observation TeacherRequestV2Observation) { observations = append(observations, observation) },
	}

	result, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{
		DialogID: dialogID, Body: "Explain this", IdempotencyKey: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.View.Message.TeacherTurnSequence == nil || *result.View.Message.TeacherTurnSequence != 1 || dialogs.item.MaxTeacherTurnSequence != 1 {
		t.Fatalf("dense allocation mismatch: result=%+v dialog=%+v", result, dialogs.item)
	}
	if len(outbox.items) != 2 || outbox.items[1].SchemaVersion != domain.TeacherRequestedSchemaVersionV2 ||
		containsJSONField(outbox.items[0].Payload, "teacher_turn_sequence") {
		t.Fatalf("V2 event boundary mismatch: %+v", outbox.items)
	}
	var payload domain.TeacherRequestedV2Payload
	if err := json.Unmarshal(outbox.items[1].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.EventID != outbox.items[1].ID || payload.CanonicalStudentMessageID != result.View.Message.ID ||
		payload.SourceMessageID != result.View.Message.ID || payload.TeacherTurnSequence != 1 ||
		payload.CorrelationID != result.View.Message.ID || payload.CausationID != result.View.Message.ID ||
		payload.SourceMessageVersion == nil || *payload.SourceMessageVersion != result.View.Message.Version ||
		payload.ReplyToMessageID != nil ||
		containsJSONField(outbox.items[1].Payload, "body") || containsJSONField(outbox.items[1].Payload, "assistant_ui") ||
		containsJSONField(outbox.items[1].Payload, "lesson_context") {
		t.Fatalf("invalid V2 payload: %s", outbox.items[1].Payload)
	}
	if len(observations) != 1 || observations[0].SourceEventID != outbox.items[1].ID ||
		observations[0].CanonicalStudentMessageID != result.View.Message.ID || observations[0].TeacherTurnSequence != 1 {
		t.Fatalf("post-commit observation mismatch: %+v", observations)
	}
}

func TestCreate_TeacherOrderingV2CarriesNormalReplyToMessageID(t *testing.T) {
	now := time.Date(2026, 9, 9, 13, 30, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID, sourceID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, uuid.New(), now)
	dialogItem.TeacherContextType, dialogItem.ContextID = domain.TeacherContextGeneralTeacher, nil
	dialogItem.MessageCount, dialogItem.MaxMessageSequence, dialogItem.MaxEventSequence = 1, 1, 1
	dialogItem.LastMessageID, dialogItem.LastMessageAt = &sourceID, &now
	dialogs := &fakeDialogs{item: dialogItem}
	messages, outbox := &fakeMessages{items: map[uuid.UUID]domain.Message{
		sourceID: {
			ID: sourceID, DialogID: dialogID, AuthorType: domain.MessageAuthorPersonalTeacher, Channel: domain.MessageChannelWeb,
			PersonalTeacherID: teacherID, Body: "Choose an option", Status: domain.MessageStatusActive, Version: 2,
			MessageSequence: 1, LastEventSequence: 1, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now,
		},
	}}, &fakeOutbox{}
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: dialogs,
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: messages, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now }, NewID: uuid.New,
		TeacherOrderingV2Enabled: true,
	}

	input := CreateInput{
		DialogID: dialogID, ReplyToMessageID: &sourceID, Body: "I choose A", IdempotencyKey: uuid.New(),
	}
	result, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.View.Message.ReplyToMessageID == nil || *result.View.Message.ReplyToMessageID != sourceID || len(outbox.items) != 2 {
		t.Fatalf("normal reply source was not committed with one V2 event: result=%+v events=%+v", result, outbox.items)
	}
	var payload domain.TeacherRequestedV2Payload
	if err := json.Unmarshal(outbox.items[1].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SourceMessageVersion == nil || *payload.SourceMessageVersion != 1 ||
		payload.ReplyToMessageID == nil || *payload.ReplyToMessageID != sourceID {
		t.Fatalf("normal reply V2 source metadata mismatch: %s", outbox.items[1].Payload)
	}
	replay, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, input)
	if err != nil || replay.Created || replay.View.Message.ID != result.View.Message.ID || len(outbox.items) != 2 {
		t.Fatalf("normal reply replay emitted an additional V2 outbox event: replay=%+v events=%+v err=%v", replay, outbox.items, err)
	}
}

func TestCreate_TeacherOrderingV2StaysDenseAcrossTeacherReplyGapAndReplay(t *testing.T) {
	now := time.Date(2026, 9, 9, 14, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, uuid.New(), now)
	dialogItem.TeacherContextType, dialogItem.ContextID = domain.TeacherContextGeneralTeacher, nil
	dialogs := &fakeDialogs{item: dialogItem}
	messages, outbox := &fakeMessages{items: map[uuid.UUID]domain.Message{}}, &fakeOutbox{}
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: dialogs,
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: messages, TeacherMessages: messages, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now }, NewID: uuid.New,
		TeacherOrderingV2Enabled: true,
	}
	firstInput := CreateInput{DialogID: dialogID, Body: "first", IdempotencyKey: uuid.New()}
	first, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, firstInput)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, firstInput)
	if err != nil || replay.Created || dialogs.item.MaxTeacherTurnSequence != 1 || len(outbox.items) != 2 {
		t.Fatalf("replay allocated or emitted again: replay=%+v dialog=%+v events=%+v err=%v", replay, dialogs.item, outbox.items, err)
	}
	response, err := service.AppendTeacherResponse(context.Background(), AppendTeacherResponseInput{
		DialogID: dialogID, PersonalTeacherID: teacherID, SourceMessageID: first.View.Message.ID,
		IdempotencyKey: uuid.New(), Body: "reply",
	})
	if err != nil || response.View.Message.TeacherTurnSequence != nil {
		t.Fatalf("teacher reply unexpectedly allocated a turn: response=%+v err=%v", response, err)
	}
	second, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{
		DialogID: dialogID, Body: "second", IdempotencyKey: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.View.Message.MessageSequence != 1 || response.View.Message.MessageSequence != 2 || second.View.Message.MessageSequence != 3 ||
		first.View.Message.TeacherTurnSequence == nil || *first.View.Message.TeacherTurnSequence != 1 ||
		second.View.Message.TeacherTurnSequence == nil || *second.View.Message.TeacherTurnSequence != 2 || dialogs.item.MaxTeacherTurnSequence != 2 {
		t.Fatalf("teacher ordering is not dense across reply gap: first=%+v response=%+v second=%+v dialog=%+v", first, response, second, dialogs.item)
	}
}

func TestCreate_TeacherOrderingV2RollsBackAllocationAndDoesNotObserve(t *testing.T) {
	now := time.Date(2026, 9, 9, 15, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, uuid.New(), now)
	dialogItem.TeacherContextType, dialogItem.ContextID = domain.TeacherContextGeneralTeacher, nil
	dialogs := &fakeDialogs{item: dialogItem}
	messages := &fakeMessages{items: map[uuid.UUID]domain.Message{}}
	outbox := &fakeOutbox{failSubject: domain.EventDialogTeacherRequested}
	tx := &rollbackFakeTx{dialogs: dialogs, messages: messages, outbox: outbox}
	observed := false
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: dialogs,
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: messages, Outbox: outbox, Tx: tx, Now: func() time.Time { return now }, NewID: uuid.New,
		TeacherOrderingV2Enabled: true, OnTeacherRequestV2: func(TeacherRequestV2Observation) { observed = true },
	}
	if _, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{DialogID: dialogID, Body: "rollback", IdempotencyKey: uuid.New()}); err == nil {
		t.Fatal("teacher request outbox failure did not fail the transaction")
	}
	if dialogs.item.MaxTeacherTurnSequence != 0 || len(messages.items) != 0 || len(outbox.items) != 0 || !tx.rolledBack || observed {
		t.Fatalf("V2 allocation escaped rollback: dialog=%+v messages=%+v events=%+v observed=%t", dialogs.item, messages.items, outbox.items, observed)
	}
}

func TestCreate_TeacherOrderingV2ConcurrentCreatesAndDifferentDialogs(t *testing.T) {
	now := time.Date(2026, 9, 9, 16, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, uuid.New(), now)
	dialogItem.TeacherContextType, dialogItem.ContextID = domain.TeacherContextGeneralTeacher, nil
	dialogs := &fakeDialogs{item: dialogItem}
	messages := &fakeMessages{items: map[uuid.UUID]domain.Message{}}
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: dialogs,
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: messages, Outbox: &fakeOutbox{}, Tx: &serializingFakeTx{}, Now: func() time.Time { return now }, NewID: uuid.New,
		TeacherOrderingV2Enabled: true,
	}
	errorsByCall := make(chan error, 2)
	for _, body := range []string{"one", "two"} {
		body := body
		go func() {
			_, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{DialogID: dialogID, Body: body, IdempotencyKey: uuid.New()})
			errorsByCall <- err
		}()
	}
	for range 2 {
		if err := <-errorsByCall; err != nil {
			t.Fatal(err)
		}
	}
	turns := map[int64]bool{}
	for _, message := range messages.items {
		if message.TeacherTurnSequence != nil {
			turns[*message.TeacherTurnSequence] = true
		}
	}
	if dialogs.item.MaxTeacherTurnSequence != 2 || !turns[1] || !turns[2] || len(turns) != 2 {
		t.Fatalf("concurrent turn allocation is not dense: dialog=%+v turns=%v", dialogs.item, turns)
	}

	otherDialogID := uuid.New()
	otherDialog := teacherDialog(otherDialogID, spaceID, studentID, teacherID, uuid.New(), now)
	otherDialog.TeacherContextType, otherDialog.ContextID = domain.TeacherContextGeneralTeacher, nil
	otherService := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: &fakeDialogs{item: otherDialog},
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(otherDialogID, studentID, 0, 0, now)}},
		Messages: &fakeMessages{items: map[uuid.UUID]domain.Message{}}, Outbox: &fakeOutbox{}, Tx: fakeTx{}, Now: func() time.Time { return now }, NewID: uuid.New,
		TeacherOrderingV2Enabled: true,
	}
	other, err := otherService.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{DialogID: otherDialogID, Body: "independent", IdempotencyKey: uuid.New()})
	if err != nil || other.View.Message.TeacherTurnSequence == nil || *other.View.Message.TeacherTurnSequence != 1 {
		t.Fatalf("different dialog did not receive independent sequence: result=%+v err=%v", other, err)
	}
}

func TestCreate_LessonTeacherDialogRequiresRevisionAnchor(t *testing.T) {
	now := time.Date(2026, 8, 31, 15, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID, courseID, lessonID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, lessonID, now)
	dialogItem.TeacherContextType = domain.TeacherContextLesson
	outbox := &fakeOutbox{}
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: &fakeDialogs{item: dialogItem},
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: &fakeMessages{items: map[uuid.UUID]domain.Message{}}, Outbox: outbox, Tx: fakeTx{},
		Now: func() time.Time { return now }, NewID: uuid.New,
	}

	_, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{
		DialogID: dialogID, Body: "Объясни этот фрагмент", IdempotencyKey: uuid.New(),
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("missing lesson context error = %v", err)
	}
	selection := "  for i := 0; i < n; i++\n"
	idempotencyKey := uuid.New()
	result, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{
		DialogID: dialogID, Body: "Объясни этот фрагмент", IdempotencyKey: idempotencyKey,
		LessonContext: &domain.LessonMessageContext{
			Schema: domain.LessonMessageContextSchemaV1, Mode: domain.LessonMessageContextSelection,
			CourseID: &courseID, LessonID: &lessonID, ContentRevision: now.Format(time.RFC3339Nano), SelectedText: &selection,
		},
	})
	if err != nil {
		t.Fatalf("create anchored lesson message: %v", err)
	}
	if !result.Created || result.View.Message.LessonContext == nil || result.View.Message.LessonContext.SelectedText == nil ||
		*result.View.Message.LessonContext.SelectedText != selection {
		t.Fatalf("lesson anchor was not stored: %+v", result)
	}
	if len(outbox.items) != 2 || !containsJSONField(outbox.items[0].Payload, "lesson_context") ||
		containsJSONField(outbox.items[1].Payload, "lesson_context") || containsJSONField(outbox.items[1].Payload, "body") {
		t.Fatalf("lesson event visibility mismatch: %+v", outbox.items)
	}

	replay, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{
		DialogID: dialogID, Body: "Объясни этот фрагмент", IdempotencyKey: idempotencyKey,
		LessonContext: &domain.LessonMessageContext{
			Schema: domain.LessonMessageContextSchemaV1, Mode: domain.LessonMessageContextSelection,
			CourseID: &courseID, LessonID: &lessonID, ContentRevision: now.Format(time.RFC3339Nano), SelectedText: &selection,
		},
	})
	if err != nil || replay.Created || replay.View.Message.ID != result.View.Message.ID || len(outbox.items) != 2 {
		t.Fatalf("anchored replay mismatch: replay=%+v err=%v events=%d", replay, err, len(outbox.items))
	}
	changed := selection + "changed"
	_, err = service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{
		DialogID: dialogID, Body: "Объясни этот фрагмент", IdempotencyKey: idempotencyKey,
		LessonContext: &domain.LessonMessageContext{
			Schema: domain.LessonMessageContextSchemaV1, Mode: domain.LessonMessageContextSelection,
			CourseID: &courseID, LessonID: &lessonID, ContentRevision: now.Format(time.RFC3339Nano), SelectedText: &changed,
		},
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("changed anchor replay error = %v", err)
	}
}

func TestCreate_LessonContextV1OverviewBindsExactLesson(t *testing.T) {
	now := time.Date(2026, 8, 31, 15, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	courseID, lessonID, otherLessonID := uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, lessonID, now)
	dialogItem.TeacherContextType = domain.TeacherContextLesson
	messages := &fakeMessages{items: map[uuid.UUID]domain.Message{}}
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: &fakeDialogs{item: dialogItem},
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: messages, Outbox: &fakeOutbox{}, Tx: fakeTx{},
		Now: func() time.Time { return now }, NewID: uuid.New,
	}

	_, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{
		DialogID: dialogID, Body: "Объясни урок", IdempotencyKey: uuid.New(),
		LessonContext: &domain.LessonMessageContext{
			Schema: domain.LessonMessageContextSchemaV1, Mode: domain.LessonMessageContextOverview,
			CourseID: &courseID, LessonID: &otherLessonID, ContentRevision: now.Format(time.RFC3339Nano),
		},
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("mismatched lesson error = %v", err)
	}
	result, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, CreateInput{
		DialogID: dialogID, Body: "Объясни урок", IdempotencyKey: uuid.New(),
		LessonContext: &domain.LessonMessageContext{
			Schema: domain.LessonMessageContextSchemaV1, Mode: domain.LessonMessageContextOverview,
			CourseID: &courseID, LessonID: &lessonID, ContentRevision: now.Format(time.RFC3339Nano),
		},
	})
	if err != nil || result.View.Message.LessonContext == nil || result.View.Message.LessonContext.SelectedText != nil {
		t.Fatalf("valid overview result=%+v err=%v", result, err)
	}
	messages.windowItems = []domain.Message{result.View.Message}
	requestContext, err := service.GetTeacherRequestContext(context.Background(), dialogID, teacherID, result.View.Message.ID, 0)
	if err != nil || requestContext.Source.LessonContext == nil || requestContext.Source.LessonContext.LessonID == nil ||
		*requestContext.Source.LessonContext.LessonID != lessonID {
		t.Fatalf("internal request context lost lesson anchor: context=%+v err=%v", requestContext, err)
	}
}

func TestCreate_LegacyLessonContextRemainsReplayable(t *testing.T) {
	now := time.Date(2026, 8, 31, 15, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID, lessonID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, lessonID, now)
	dialogItem.TeacherContextType = domain.TeacherContextLesson
	messages := &fakeMessages{items: map[uuid.UUID]domain.Message{}}
	outbox := &fakeOutbox{}
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: &fakeDialogs{item: dialogItem},
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: messages, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now }, NewID: uuid.New,
	}
	selection := "legacy exact text"
	input := CreateInput{
		DialogID: dialogID, Body: "legacy question", IdempotencyKey: uuid.New(),
		LessonContext: &domain.LessonMessageContext{ContentRevision: now.Format(time.RFC3339Nano), SelectedText: &selection},
	}
	created, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.Create(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, input)
	if err != nil || replay.Created || replay.View.Message.ID != created.View.Message.ID || len(outbox.items) != 2 {
		t.Fatalf("legacy replay mismatch: replay=%+v err=%v events=%d", replay, err, len(outbox.items))
	}
}

func TestUpdateRetainsLessonAnchorAndDeleteClearsIt(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	courseID, lessonID, messageID := uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, lessonID, now)
	dialogItem.TeacherContextType = domain.TeacherContextLesson
	dialogItem.MessageCount, dialogItem.MaxMessageSequence, dialogItem.MaxEventSequence = 1, 1, 1
	selection := "immutable selection"
	item := domain.Message{
		ID: messageID, DialogID: dialogID, AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelWeb,
		SenderID: studentID, Body: "question", Status: domain.MessageStatusActive, Version: 1,
		MessageSequence: 1, LastEventSequence: 1, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now,
		LessonContext: &domain.LessonMessageContext{
			Schema: domain.LessonMessageContextSchemaV1, Mode: domain.LessonMessageContextSelection,
			CourseID: &courseID, LessonID: &lessonID, ContentRevision: now.Format(time.RFC3339Nano), SelectedText: &selection,
		},
	}
	messages := &fakeMessages{items: map[uuid.UUID]domain.Message{messageID: item}}
	outbox := &fakeOutbox{}
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: &fakeDialogs{item: dialogItem},
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: messages, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now.Add(time.Second) }, NewID: uuid.New,
	}

	updated, err := service.Update(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, UpdateInput{
		MessageID: messageID, Body: "edited question", ExpectedVersion: 1,
	})
	if err != nil || updated.Message.LessonContext == nil || updated.Message.LessonContext.SelectedText == nil ||
		*updated.Message.LessonContext.SelectedText != selection {
		t.Fatalf("body update replaced lesson anchor: view=%+v err=%v", updated, err)
	}
	deleted, err := service.Delete(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, DeleteInput{
		MessageID: messageID, ExpectedVersion: 2,
	})
	if err != nil || deleted.Message.LessonContext != nil || messages.items[messageID].LessonContext != nil {
		t.Fatalf("delete retained lesson anchor: view=%+v stored=%+v err=%v", deleted, messages.items[messageID], err)
	}
	if len(outbox.items) != 4 || outbox.items[1].Subject != domain.EventDialogTeacherContextMutated ||
		outbox.items[3].Subject != domain.EventDialogTeacherContextMutated ||
		!containsJSONField(outbox.items[0].Payload, "lesson_context") ||
		containsJSONField(outbox.items[1].Payload, "lesson_context") || containsJSONField(outbox.items[3].Payload, "lesson_context") {
		t.Fatalf("update/delete lesson event projections mismatch: %+v", outbox.items)
	}
	var updatedMutation, deletedMutation domain.TeacherContextMutationPayload
	if json.Unmarshal(outbox.items[1].Payload, &updatedMutation) != nil || json.Unmarshal(outbox.items[3].Payload, &deletedMutation) != nil ||
		updatedMutation.Mutation != domain.TeacherContextMutationUpdated || deletedMutation.Mutation != domain.TeacherContextMutationDeleted ||
		updatedMutation.MessageSequence != 1 || deletedMutation.MessageSequence != 1 || updatedMutation.MessageVersion != 2 || deletedMutation.MessageVersion != 3 {
		t.Fatalf("mutation kinds/versions mismatch: update=%+v delete=%+v", updatedMutation, deletedMutation)
	}
	if _, err := service.Delete(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, DeleteInput{MessageID: messageID, ExpectedVersion: 2}); !errors.Is(err, domain.ErrMessageConflict) || len(outbox.items) != 4 {
		t.Fatalf("delete retry duplicated events: err=%v events=%+v", err, outbox.items)
	}
}

func TestUpdateTeacherDialogEmitsBodyFreeMutationForEveryContext(t *testing.T) {
	contexts := []domain.TeacherContextType{
		domain.TeacherContextGeneralTeacher, domain.TeacherContextLesson, domain.TeacherContextLessonTask,
		domain.TeacherContextPracticeTask, domain.TeacherContextProject,
	}
	for _, contextType := range contexts {
		t.Run(string(contextType), func(t *testing.T) {
			now := time.Date(2026, 9, 4, 13, 0, 0, 0, time.UTC)
			dialogID, spaceID, studentID, teacherID, contextID, messageID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, contextID, now)
			dialogItem.TeacherContextType = contextType
			if contextType == domain.TeacherContextGeneralTeacher {
				dialogItem.ContextID = nil
			}
			dialogItem.MessageCount, dialogItem.MaxMessageSequence, dialogItem.MaxEventSequence = 1, 7, 9
			item := domain.Message{
				ID: messageID, DialogID: dialogID, AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelWeb,
				SenderID: studentID, Body: "before", Status: domain.MessageStatusActive, Version: 2,
				MessageSequence: 7, LastEventSequence: 7, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now,
			}
			messages := &fakeMessages{items: map[uuid.UUID]domain.Message{messageID: item}}
			outbox := &fakeOutbox{}
			service := Service{
				Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: &fakeDialogs{item: dialogItem},
				Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
				Messages: messages, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now.Add(time.Second) }, NewID: uuid.New,
			}
			if _, err := service.Update(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, UpdateInput{MessageID: messageID, Body: "after", ExpectedVersion: 2}); err != nil {
				t.Fatal(err)
			}
			if len(outbox.items) != 2 || outbox.items[0].Subject != domain.EventDialogMessageUpdated || outbox.items[1].Subject != domain.EventDialogTeacherContextMutated || outbox.items[0].EventSequence != outbox.items[1].EventSequence {
				t.Fatalf("outbox mismatch: %+v", outbox.items)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(outbox.items[1].Payload, &payload); err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"body", "links", "attachments", "assistant_ui", "lesson_context", "learning_action_id", "mastery", "code"} {
				if _, ok := payload[forbidden]; ok {
					t.Fatalf("mutation event leaked %q: %s", forbidden, outbox.items[1].Payload)
				}
			}
		})
	}
}

func TestUpdateGroupDoesNotEmitTeacherContextMutation(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, userID, messageID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := domain.Dialog{ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive, Title: "Group", CreatedBy: userID, Version: 1, MemberCount: 1, MessageCount: 1, MaxMessageSequence: 1, MaxEventSequence: 1, CreatedAt: now, UpdatedAt: now}
	item := domain.Message{ID: messageID, DialogID: dialogID, AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelWeb, SenderID: userID, Body: "before", Status: domain.MessageStatusActive, Version: 1, MessageSequence: 1, LastEventSequence: 1, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now}
	outbox := &fakeOutbox{}
	service := Service{Spaces: fakeSpaces{item: activeTestSpace(spaceID, userID, now)}, Dialogs: &fakeDialogs{item: dialogItem}, Members: &fakeMembers{items: map[uuid.UUID]domain.Member{userID: activeMember(dialogID, userID, 0, 0, now)}}, Messages: &fakeMessages{items: map[uuid.UUID]domain.Message{messageID: item}}, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now.Add(time.Second) }, NewID: uuid.New}
	if _, err := service.Update(context.Background(), domain.Actor{UserID: userID, Role: "USER"}, UpdateInput{MessageID: messageID, Body: "after", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if len(outbox.items) != 1 || outbox.items[0].Subject != domain.EventDialogMessageUpdated {
		t.Fatalf("group event mismatch: %+v", outbox.items)
	}
}

func TestUpdatePersonalDialogDoesNotEmitTeacherContextMutation(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, userID, messageID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := domain.Dialog{ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypePersonal, Status: domain.DialogStatusActive, PersonalKey: make([]byte, 32), CreatedBy: userID, Version: 1, MemberCount: 2, MessageCount: 1, MaxMessageSequence: 1, MaxEventSequence: 1, CreatedAt: now, UpdatedAt: now}
	item := domain.Message{ID: messageID, DialogID: dialogID, AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelWeb, SenderID: userID, Body: "before", Status: domain.MessageStatusActive, Version: 1, MessageSequence: 1, LastEventSequence: 1, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now}
	outbox := &fakeOutbox{}
	service := Service{Spaces: fakeSpaces{item: activeTestSpace(spaceID, userID, now)}, Dialogs: &fakeDialogs{item: dialogItem}, Members: &fakeMembers{items: map[uuid.UUID]domain.Member{userID: activeMember(dialogID, userID, 0, 0, now)}}, Messages: &fakeMessages{items: map[uuid.UUID]domain.Message{messageID: item}}, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now.Add(time.Second) }, NewID: uuid.New}
	if _, err := service.Update(context.Background(), domain.Actor{UserID: userID, Role: "USER"}, UpdateInput{MessageID: messageID, Body: "after", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if len(outbox.items) != 1 || outbox.items[0].Subject != domain.EventDialogMessageUpdated {
		t.Fatalf("personal dialog event mismatch: %+v", outbox.items)
	}
}

func TestPersonalTeacherMessageCannotEmitTeacherContextMutation(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, studentID, teacherID, contextID, messageID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, contextID, now)
	dialogItem.MessageCount, dialogItem.MaxMessageSequence, dialogItem.MaxEventSequence = 1, 1, 1
	item := domain.Message{ID: messageID, DialogID: dialogID, AuthorType: domain.MessageAuthorPersonalTeacher, Channel: domain.MessageChannelWeb, PersonalTeacherID: teacherID, Body: "teacher", Status: domain.MessageStatusActive, Version: 1, MessageSequence: 1, LastEventSequence: 1, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now}
	outbox := &fakeOutbox{}
	service := Service{Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: &fakeDialogs{item: dialogItem}, Members: &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}}, Messages: &fakeMessages{items: map[uuid.UUID]domain.Message{messageID: item}}, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now.Add(time.Second) }, NewID: uuid.New}
	if _, err := service.Update(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, UpdateInput{MessageID: messageID, Body: "attempt", ExpectedVersion: 1}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("error=%v", err)
	}
	if len(outbox.items) != 0 {
		t.Fatalf("teacher-authored mutation emitted events: %+v", outbox.items)
	}
}

func TestTeacherContextMutationOutboxFailureRollsBackMutation(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, studentID, teacherID, contextID, messageID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogs := &fakeDialogs{item: teacherDialog(dialogID, spaceID, studentID, teacherID, contextID, now)}
	dialogs.item.MessageCount, dialogs.item.MaxMessageSequence, dialogs.item.MaxEventSequence = 1, 1, 1
	original := domain.Message{ID: messageID, DialogID: dialogID, AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelWeb, SenderID: studentID, Body: "before", Status: domain.MessageStatusActive, Version: 1, MessageSequence: 1, LastEventSequence: 1, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now}
	messages := &fakeMessages{items: map[uuid.UUID]domain.Message{messageID: original}}
	outbox := &fakeOutbox{failSubject: domain.EventDialogTeacherContextMutated}
	tx := &rollbackFakeTx{dialogs: dialogs, messages: messages, outbox: outbox}
	service := Service{Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: dialogs, Members: &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}}, Messages: messages, Outbox: outbox, Tx: tx, Now: func() time.Time { return now.Add(time.Second) }, NewID: uuid.New}
	if _, err := service.Update(context.Background(), domain.Actor{UserID: studentID, Role: "STUDENT"}, UpdateInput{MessageID: messageID, Body: "after", ExpectedVersion: 1}); err == nil {
		t.Fatal("missing body-free outbox insert did not fail the transaction")
	}
	if messages.items[messageID].Body != original.Body || dialogs.item.MaxEventSequence != 1 || len(outbox.items) != 0 || !tx.rolledBack {
		t.Fatalf("mutation was not rolled back: message=%+v dialog=%+v events=%+v", messages.items[messageID], dialogs.item, outbox.items)
	}
}

func TestGetAssistantUISourceExactBindingAndNotFoundPrivacy(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, studentID, teacherID, contextID, messageID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, contextID, now)
	message := domain.Message{ID: messageID, DialogID: dialogID, AuthorType: domain.MessageAuthorPersonalTeacher, Channel: domain.MessageChannelWeb, PersonalTeacherID: teacherID, Body: "do not return", AssistantUI: json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[{"opaque":true}]}`), Status: domain.MessageStatusActive, Version: 2, MessageSequence: 3, LastEventSequence: 3, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now}
	service := Service{Dialogs: &fakeDialogs{item: dialogItem}, Messages: &fakeMessages{items: map[uuid.UUID]domain.Message{messageID: message}}}
	source, err := service.GetAssistantUISource(context.Background(), AssistantUISourceInput{DialogID: dialogID, StudentID: studentID, PersonalTeacherID: teacherID, MessageID: messageID})
	if err != nil || source.MessageID != messageID || source.MessageVersion != 2 || source.Schema != domain.DialogAssistantUISourceSchemaV1 {
		t.Fatalf("source=%+v err=%v", source, err)
	}
	for name, input := range map[string]AssistantUISourceInput{
		"wrong student": {DialogID: dialogID, StudentID: uuid.New(), PersonalTeacherID: teacherID, MessageID: messageID},
		"wrong teacher": {DialogID: dialogID, StudentID: studentID, PersonalTeacherID: uuid.New(), MessageID: messageID},
		"wrong message": {DialogID: dialogID, StudentID: studentID, PersonalTeacherID: teacherID, MessageID: uuid.New()},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.GetAssistantUISource(context.Background(), input); !errors.Is(err, domain.ErrMessageNotFound) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	message.AssistantUI = nil
	service.Messages = &fakeMessages{items: map[uuid.UUID]domain.Message{messageID: message}}
	if _, err := service.GetAssistantUISource(context.Background(), AssistantUISourceInput{DialogID: dialogID, StudentID: studentID, PersonalTeacherID: teacherID, MessageID: messageID}); !errors.Is(err, domain.ErrMessageNotFound) {
		t.Fatalf("missing assistant_ui error=%v", err)
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
		ID: sourceID, DialogID: dialogID, AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelWeb, SenderID: studentID,
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
	assistantUI := json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[{"id":"hint","type":"future_hint","data":{"level":1}}]}`)
	result, err := service.AppendTeacherResponse(context.Background(), AppendTeacherResponseInput{
		DialogID: dialogID, PersonalTeacherID: teacherID, SourceMessageID: sourceID,
		IdempotencyKey: idempotencyKey, Body: "Проверь условие перед циклом.", AssistantUI: assistantUI,
	})
	if err != nil {
		t.Fatal(err)
	}
	message := result.View.Message
	if !result.Created || message.AuthorType != domain.MessageAuthorPersonalTeacher || message.PersonalTeacherID != teacherID || message.SenderID != uuid.Nil ||
		message.ReplyToMessageID == nil || *message.ReplyToMessageID != sourceID || message.LearningActionID == nil || *message.LearningActionID != learningActionID || len(message.AssistantUI) == 0 {
		t.Fatalf("teacher response mismatch: %+v", result)
	}
	if len(outbox.items) != 1 || outbox.items[0].Subject != domain.EventDialogMessageCreated || outbox.items[0].SchemaVersion != 1 || !containsJSONField(outbox.items[0].Payload, "assistant_ui") {
		t.Fatalf("teacher response outbox mismatch: %+v", outbox.items)
	}
	replay, err := service.AppendTeacherResponse(context.Background(), AppendTeacherResponseInput{
		DialogID: dialogID, PersonalTeacherID: teacherID, SourceMessageID: sourceID,
		IdempotencyKey: idempotencyKey, Body: "Проверь условие перед циклом.",
		AssistantUI: json.RawMessage(`{"blocks":[{"data":{"level":1},"type":"future_hint","id":"hint"}],"schema":"assistant-ui.v1"}`),
	})
	if err != nil || replay.Created || replay.View.Message.ID != message.ID || len(outbox.items) != 1 {
		t.Fatalf("teacher response replay mismatch: replay=%+v err=%v events=%d", replay, err, len(outbox.items))
	}
	_, err = service.AppendTeacherResponse(context.Background(), AppendTeacherResponseInput{
		DialogID: dialogID, PersonalTeacherID: teacherID, SourceMessageID: sourceID,
		IdempotencyKey: idempotencyKey, Body: "Проверь условие перед циклом.",
		AssistantUI: json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[{"id":"hint","type":"future_hint","data":{"level":2}}]}`),
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("changed assistant UI replay error = %v", err)
	}
	_, err = service.AppendTeacherResponse(context.Background(), AppendTeacherResponseInput{
		DialogID: dialogID, PersonalTeacherID: teacherID, SourceMessageID: sourceID,
		IdempotencyKey: idempotencyKey, Body: "Проверь условие перед циклом.",
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("missing assistant UI replay error = %v", err)
	}
	_, err = service.AppendTeacherResponse(context.Background(), AppendTeacherResponseInput{
		DialogID: dialogID, PersonalTeacherID: teacherID, SourceMessageID: sourceID,
		IdempotencyKey: uuid.New(), Body: "   ",
		AssistantUI: json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[]}`),
	})
	if !errors.Is(err, domain.ErrInvalidContent) {
		t.Fatalf("assistant UI replaced mandatory body: %v", err)
	}
}

func TestAppendStudentChannelMessageUsesGeneralDialogAndNormalTeacherRequest(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, uuid.New(), now)
	dialogItem.TeacherContextType, dialogItem.ContextID = domain.TeacherContextGeneralTeacher, nil
	messages := &fakeMessages{items: map[uuid.UUID]domain.Message{}}
	outbox := &fakeOutbox{}
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: &fakeDialogs{item: dialogItem},
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: messages, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now }, NewID: uuid.New,
	}
	key := uuid.New()
	input := AppendStudentChannelMessageInput{
		DialogID: dialogID, StudentID: studentID, PersonalTeacherID: teacherID,
		IdempotencyKey: key, Channel: domain.MessageChannelTelegram, Body: "Объясни интерфейсы",
	}
	result, err := service.AppendStudentChannelMessage(context.Background(), input)
	if err != nil {
		t.Fatalf("append Telegram message: %v", err)
	}
	if !result.Created || result.View.Message.Channel != domain.MessageChannelTelegram || result.View.Message.SenderID != studentID {
		t.Fatalf("unexpected channel message: %+v", result)
	}
	if len(outbox.items) != 2 || outbox.items[0].Subject != domain.EventDialogMessageCreated || outbox.items[1].Subject != domain.EventDialogTeacherRequested {
		t.Fatalf("unexpected channel outbox: %+v", outbox.items)
	}
	replay, err := service.AppendStudentChannelMessage(context.Background(), input)
	if err != nil || replay.Created || replay.View.Message.ID != result.View.Message.ID || len(outbox.items) != 2 {
		t.Fatalf("channel replay mismatch: replay=%+v err=%v events=%d", replay, err, len(outbox.items))
	}
}

func TestAppendStudentChannelMessageTeacherOrderingV2AllocatesAndEmitsOnce(t *testing.T) {
	now := time.Date(2026, 9, 9, 17, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, uuid.New(), now)
	dialogItem.TeacherContextType, dialogItem.ContextID = domain.TeacherContextGeneralTeacher, nil
	dialogs := &fakeDialogs{item: dialogItem}
	outbox := &fakeOutbox{}
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: dialogs,
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: &fakeMessages{items: map[uuid.UUID]domain.Message{}}, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now }, NewID: uuid.New,
		TeacherOrderingV2Enabled: true,
	}
	input := AppendStudentChannelMessageInput{
		DialogID: dialogID, StudentID: studentID, PersonalTeacherID: teacherID,
		IdempotencyKey: uuid.New(), Channel: domain.MessageChannelTelegram, Body: "Telegram source",
	}
	created, err := service.AppendStudentChannelMessage(context.Background(), input)
	if err != nil || !created.Created || created.View.Message.TeacherTurnSequence == nil ||
		*created.View.Message.TeacherTurnSequence != 1 || dialogs.item.MaxTeacherTurnSequence != 1 ||
		len(outbox.items) != 2 || outbox.items[1].SchemaVersion != domain.TeacherRequestedSchemaVersionV2 {
		t.Fatalf("V2 channel append mismatch: created=%+v dialog=%+v events=%+v err=%v", created, dialogs.item, outbox.items, err)
	}
	replay, err := service.AppendStudentChannelMessage(context.Background(), input)
	if err != nil || replay.Created || dialogs.item.MaxTeacherTurnSequence != 1 || len(outbox.items) != 2 {
		t.Fatalf("V2 channel replay allocated or emitted again: replay=%+v events=%+v err=%v", replay, outbox.items, err)
	}
}

func TestAppendTeacherProactiveUsesGeneralDialogAndIsIdempotent(t *testing.T) {
	now := time.Date(2026, 8, 31, 18, 0, 0, 0, time.UTC)
	dialogID, spaceID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, uuid.New(), now)
	dialogItem.TeacherContextType, dialogItem.ContextID = domain.TeacherContextGeneralTeacher, nil
	messages := &fakeMessages{items: map[uuid.UUID]domain.Message{}}
	outbox := &fakeOutbox{}
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: &fakeDialogs{item: dialogItem},
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: messages, TeacherMessages: messages, Outbox: outbox, Tx: fakeTx{},
		Now: func() time.Time { return now }, NewID: uuid.New,
	}
	idempotencyKey := uuid.New()
	result, err := service.AppendTeacherProactive(context.Background(), AppendTeacherProactiveInput{
		DialogID: dialogID, PersonalTeacherID: teacherID, IdempotencyKey: idempotencyKey,
		Body:        "Похоже, ты зациклился. Хочешь небольшую подсказку?",
		AssistantUI: json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[]}`),
	})
	if err != nil {
		t.Fatalf("append proactive message: %v", err)
	}
	message := result.View.Message
	if !result.Created || message.AuthorType != domain.MessageAuthorPersonalTeacher || message.ReplyToMessageID != nil || message.LearningActionID != nil || len(message.AssistantUI) == 0 {
		t.Fatalf("unexpected proactive message: %+v", result)
	}
	if len(outbox.items) != 1 || outbox.items[0].Subject != domain.EventDialogMessageCreated {
		t.Fatalf("unexpected proactive outbox: %+v", outbox.items)
	}
	replay, err := service.AppendTeacherProactive(context.Background(), AppendTeacherProactiveInput{
		DialogID: dialogID, PersonalTeacherID: teacherID, IdempotencyKey: idempotencyKey,
		Body:        "Похоже, ты зациклился. Хочешь небольшую подсказку?",
		AssistantUI: json.RawMessage(`{"blocks":[],"schema":"assistant-ui.v1"}`),
	})
	if err != nil || replay.Created || replay.View.Message.ID != message.ID || len(outbox.items) != 1 {
		t.Fatalf("proactive replay mismatch: replay=%+v err=%v events=%d", replay, err, len(outbox.items))
	}
}

func TestAppendTeacherProactiveRejectsContextDialog(t *testing.T) {
	now := time.Now().UTC()
	dialogID, spaceID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, uuid.New(), now)
	service := Service{
		Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: &fakeDialogs{item: dialogItem},
		Messages: &fakeMessages{items: map[uuid.UUID]domain.Message{}}, TeacherMessages: &fakeMessages{items: map[uuid.UUID]domain.Message{}},
		Outbox: &fakeOutbox{}, Tx: fakeTx{}, Now: func() time.Time { return now }, NewID: uuid.New,
	}
	_, err := service.AppendTeacherProactive(context.Background(), AppendTeacherProactiveInput{
		DialogID: dialogID, PersonalTeacherID: teacherID, IdempotencyKey: uuid.New(), Body: "Offer",
	})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected contextual dialog rejection, got %v", err)
	}
}

type fakeTx struct{}

func (fakeTx) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type serializingFakeTx struct{ mutex sync.Mutex }

func (f *serializingFakeTx) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return fn(ctx)
}

type rollbackFakeTx struct {
	dialogs    *fakeDialogs
	messages   *fakeMessages
	outbox     *fakeOutbox
	rolledBack bool
}

func (f *rollbackFakeTx) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	dialogSnapshot := f.dialogs.item
	messageSnapshot := make(map[uuid.UUID]domain.Message, len(f.messages.items))
	for id, item := range f.messages.items {
		messageSnapshot[id] = item
	}
	outboxSnapshot := append([]domain.OutboxEvent(nil), f.outbox.items...)
	err := fn(ctx)
	if err != nil {
		f.dialogs.item, f.messages.items, f.outbox.items, f.rolledBack = dialogSnapshot, messageSnapshot, outboxSnapshot, true
	}
	return err
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
func (f *fakeMessages) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (domain.Message, error) {
	return f.GetByID(ctx, id)
}

func (f *fakeMessages) GetByIdempotencyKey(_ context.Context, senderID, key uuid.UUID) (domain.Message, error) {
	for _, item := range f.items {
		if item.AuthorType == domain.MessageAuthorUser && item.SenderID == senderID && item.IdempotencyKey == key {
			return item, nil
		}
	}
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
		ID: uuid.New(), DialogID: dialogID, AuthorType: domain.MessageAuthorUser, Channel: domain.MessageChannelWeb, SenderID: senderID, IdempotencyKey: uuid.New(),
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
func (f *fakeMessages) UpdateContent(_ context.Context, item domain.Message, expected int) error {
	stored, ok := f.items[item.ID]
	if !ok || stored.Version != expected {
		return domain.ErrMessageConflict
	}
	f.items[item.ID] = item
	return nil
}
func (f *fakeMessages) MarkDeleted(ctx context.Context, item domain.Message, expected int) error {
	return f.UpdateContent(ctx, item, expected)
}
func (*fakeMessages) AdvanceEvent(context.Context, uuid.UUID, int64) (domain.Message, error) {
	return domain.Message{}, domain.ErrNotFound
}
func (*fakeMessages) UpdateModerationStatus(context.Context, domain.Message, domain.MessageStatus, int) error {
	return nil
}

type fakeOutbox struct {
	items       []domain.OutboxEvent
	failSubject domain.EventSubject
}

func (f *fakeOutbox) Add(_ context.Context, item domain.OutboxEvent) error {
	if item.Subject == f.failSubject {
		return errors.New("outbox insert failed")
	}
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
