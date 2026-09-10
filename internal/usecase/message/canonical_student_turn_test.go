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

func TestMaterializeCanonicalStudentTurn_CommitsOneNormalTurnAndBodyFreeV2Receipt(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	service, identity, source, dialogs, messages, ledger, outbox, observed := canonicalStudentTurnFixture(t, now)

	result, err := service.MaterializeCanonicalStudentTurn(context.Background(), MaterializeCanonicalStudentTurnInput{Identity: identity, CanonicalBody: "Start the exercise"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.View.Message.AuthorType != domain.MessageAuthorUser || result.View.Message.SenderID != identity.StudentID ||
		result.View.Message.ReplyToMessageID == nil || *result.View.Message.ReplyToMessageID != source.ID ||
		result.View.Message.TeacherTurnSequence == nil || *result.View.Message.TeacherTurnSequence != 1 ||
		result.View.Message.IdempotencyKey != identity.CanonicalMessageCommandID {
		t.Fatalf("canonical message shape mismatch: %+v", result.View.Message)
	}
	if dialogs.item.MaxTeacherTurnSequence != 1 || dialogs.item.MaxMessageSequence != 2 || len(ledger.items) != 1 {
		t.Fatalf("private ledger/ordering was not atomically recorded: dialog=%+v ledger=%+v", dialogs.item, ledger.items)
	}
	if len(outbox.items) != 2 || outbox.items[0].Subject != domain.EventDialogMessageCreated || outbox.items[1].Subject != domain.EventDialogTeacherRequested {
		t.Fatalf("expected normal lifecycle evidence plus one Teacher trigger, got %+v", outbox.items)
	}
	var payload domain.TeacherRequestedV2Payload
	if err := json.Unmarshal(outbox.items[1].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ActionReceiptID == nil || *payload.ActionReceiptID != identity.ActionReceiptID || payload.CorrelationID != identity.ActionReceiptID ||
		payload.CausationID != result.View.Message.ID || payload.CanonicalStudentMessageID != result.View.Message.ID ||
		payload.TeacherTurnSequence != 1 || payload.SourceMessageVersion == nil || *payload.SourceMessageVersion != result.View.Message.Version ||
		payload.ReplyToMessageID == nil || *payload.ReplyToMessageID != source.ID || containsJSONField(outbox.items[1].Payload, "body") ||
		containsJSONField(outbox.items[1].Payload, "block_id") || containsJSONField(outbox.items[1].Payload, "action_id") ||
		containsJSONField(outbox.items[1].Payload, "source_ui_digest") || containsJSONField(outbox.items[1].Payload, "student_command_id") {
		t.Fatalf("invalid body-free action V2 payload: %s", outbox.items[1].Payload)
	}
	if len(*observed) != 1 || (*observed)[0].ActionReceiptID != identity.ActionReceiptID || (*observed)[0].CanonicalStudentMessageID != result.View.Message.ID {
		t.Fatalf("post-commit action observation mismatch: %+v", *observed)
	}
	if item := ledger.items[identity.ActionReceiptID]; item.CanonicalStudentMessageID != result.View.Message.ID || item.Identity.Interaction.SourceUIDigest != identity.Interaction.SourceUIDigest {
		t.Fatalf("ledger identity mismatch: %+v", item)
	}
	if _, exists := messages.items[result.View.Message.ID]; !exists {
		t.Fatal("normal message was not persisted")
	}
}

func TestMaterializeCanonicalStudentTurn_ExactReplaySurvivesKillSwitchAndConflictsOnReuse(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	service, identity, _, dialogs, _, _, outbox, _ := canonicalStudentTurnFixture(t, now)
	created, err := service.MaterializeCanonicalStudentTurn(context.Background(), MaterializeCanonicalStudentTurnInput{Identity: identity, CanonicalBody: "Choice A"})
	if err != nil || !created.Created {
		t.Fatalf("initial action materialization: result=%+v err=%v", created, err)
	}
	service.CanonicalStudentTurnEnabled = false
	service.TeacherOrderingV2Enabled = false
	replay, err := service.MaterializeCanonicalStudentTurn(context.Background(), MaterializeCanonicalStudentTurnInput{Identity: identity, CanonicalBody: "Choice A"})
	if err != nil || replay.Created || replay.View.Message.ID != created.View.Message.ID || dialogs.item.MaxTeacherTurnSequence != 1 || len(outbox.items) != 2 {
		t.Fatalf("committed receipt was not recoverable without effect: replay=%+v dialog=%+v events=%d err=%v", replay, dialogs.item, len(outbox.items), err)
	}
	if _, err := service.MaterializeCanonicalStudentTurn(context.Background(), MaterializeCanonicalStudentTurnInput{Identity: identity, CanonicalBody: "Changed body"}); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("changed body reused receipt: %v", err)
	}
	reusedSource := identity
	reusedSource.ActionReceiptID, reusedSource.CorrelationID, reusedSource.CausationID = uuid.New(), uuid.New(), uuid.New()
	reusedSource.CorrelationID, reusedSource.CausationID = reusedSource.ActionReceiptID, reusedSource.ActionReceiptID
	reusedSource.Interaction.ActionReceiptID = reusedSource.ActionReceiptID
	if _, err := service.MaterializeCanonicalStudentTurn(context.Background(), MaterializeCanonicalStudentTurnInput{Identity: reusedSource, CanonicalBody: "Choice A"}); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same source action with a different receipt was accepted: %v", err)
	}
}

func TestMaterializeCanonicalStudentTurn_RejectsNewWhenOrderingOrFeatureIsDisabled(t *testing.T) {
	now := time.Date(2026, 9, 10, 11, 0, 0, 0, time.UTC)
	service, identity, _, dialogs, messages, ledger, outbox, _ := canonicalStudentTurnFixture(t, now)
	service.CanonicalStudentTurnEnabled = false
	if _, err := service.MaterializeCanonicalStudentTurn(context.Background(), MaterializeCanonicalStudentTurnInput{Identity: identity, CanonicalBody: "Choice"}); !errors.Is(err, domain.ErrFeatureDisabled) {
		t.Fatalf("disabled materialization error=%v", err)
	}
	if dialogs.item.MaxTeacherTurnSequence != 0 || len(messages.items) != 1 || len(ledger.items) != 0 || len(outbox.items) != 0 {
		t.Fatalf("disabled receipt had an effect: dialog=%+v messages=%+d ledger=%+v events=%+v", dialogs.item, len(messages.items), ledger.items, outbox.items)
	}
}

func TestMaterializeCanonicalStudentTurn_RejectsStaleSourceWithoutEffect(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	for _, mutate := range []struct {
		name string
		fn   func(*domain.Message)
	}{
		{name: "version", fn: func(source *domain.Message) { source.Version++ }},
		{name: "digest", fn: func(source *domain.Message) {
			source.AssistantUI = json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[{"opaque":"changed"}]}`)
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			service, identity, source, dialogs, messages, ledger, outbox, _ := canonicalStudentTurnFixture(t, now)
			mutate.fn(&source)
			messages.items[source.ID] = source
			if _, err := service.MaterializeCanonicalStudentTurn(context.Background(), MaterializeCanonicalStudentTurnInput{Identity: identity, CanonicalBody: "Choice"}); !errors.Is(err, domain.ErrMessageConflict) {
				t.Fatalf("stale source %s error=%v", mutate.name, err)
			}
			if dialogs.item.MaxTeacherTurnSequence != 0 || len(messages.items) != 1 || len(ledger.items) != 0 || len(outbox.items) != 0 {
				t.Fatalf("stale source %s had an effect: dialog=%+v messages=%d ledger=%+v outbox=%+v", mutate.name, dialogs.item, len(messages.items), ledger.items, outbox.items)
			}
		})
	}
}

func TestCanonicalStudentTurn_IsImmutableToPublicUpdateAndDelete(t *testing.T) {
	now := time.Date(2026, 9, 10, 13, 0, 0, 0, time.UTC)
	service, identity, _, _, _, _, _, _ := canonicalStudentTurnFixture(t, now)
	created, err := service.MaterializeCanonicalStudentTurn(context.Background(), MaterializeCanonicalStudentTurnInput{Identity: identity, CanonicalBody: "Choice"})
	if err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{UserID: identity.StudentID, Role: "STUDENT"}
	if _, err := service.Update(context.Background(), actor, UpdateInput{MessageID: created.View.Message.ID, Body: "Changed", ExpectedVersion: 1}); !errors.Is(err, domain.ErrMessageConflict) {
		t.Fatalf("canonical public update error=%v", err)
	}
	if _, err := service.Delete(context.Background(), actor, DeleteInput{MessageID: created.View.Message.ID, ExpectedVersion: 1}); !errors.Is(err, domain.ErrMessageConflict) {
		t.Fatalf("canonical public delete error=%v", err)
	}
}

func canonicalStudentTurnFixture(t *testing.T, now time.Time) (Service, domain.CanonicalStudentTurnIdentity, domain.Message, *fakeDialogs, *fakeMessages, *fakeCanonicalStudentTurns, *fakeOutbox, *[]CanonicalStudentTurnObservation) {
	t.Helper()
	dialogID, spaceID, studentID, teacherID, sourceID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ui := json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[{"opaque":"button"}]}`)
	digest, err := domain.CanonicalAssistantUIDigest(ui)
	if err != nil {
		t.Fatal(err)
	}
	dialogItem := teacherDialog(dialogID, spaceID, studentID, teacherID, uuid.New(), now)
	dialogItem.TeacherContextType, dialogItem.ContextID = domain.TeacherContextGeneralTeacher, nil
	dialogItem.MessageCount, dialogItem.MaxMessageSequence, dialogItem.MaxEventSequence = 1, 1, 1
	dialogItem.LastMessageID, dialogItem.LastMessageAt = &sourceID, &now
	source := domain.Message{ID: sourceID, DialogID: dialogID, AuthorType: domain.MessageAuthorPersonalTeacher, Channel: domain.MessageChannelWeb,
		PersonalTeacherID: teacherID, AssistantUI: ui, Body: "Choose", Status: domain.MessageStatusActive, Version: 3,
		MessageSequence: 1, LastEventSequence: 1, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now}
	dialogs := &fakeDialogs{item: dialogItem}
	messages := &fakeMessages{items: map[uuid.UUID]domain.Message{sourceID: source}}
	ledger := &fakeCanonicalStudentTurns{items: map[uuid.UUID]domain.CanonicalStudentTurn{}}
	outbox := &fakeOutbox{}
	observed := []CanonicalStudentTurnObservation{}
	_, err = domain.NewCanonicalStudentTurnIdentity(domain.CanonicalStudentTurnIdentity{
		Schema: domain.CanonicalStudentTurnIdentitySchemaV1, DialogID: dialogID, StudentID: studentID, PersonalTeacherID: teacherID,
		ActionReceiptID: uuid.New(), CorrelationID: uuid.Nil, CausationID: uuid.Nil, CanonicalMessageCommandID: uuid.New(),
		Interaction: domain.TrustedCanonicalStudentTurnInteraction{Schema: domain.CanonicalStudentTurnInteractionSchemaV1, Kind: domain.CanonicalStudentTurnInteractionActionReceipt,
			SourcePromptMessageID: sourceID, SourcePromptMessageVersion: source.Version, BlockID: "choice-1", ActionID: "accept", SourceUIDigest: digest},
	})
	if err == nil { // Set causal UUIDs through the constructor's strict boundary.
		t.Fatal("identity without receipt correlation unexpectedly validated")
	}
	receiptID := uuid.New()
	identity, err := domain.NewCanonicalStudentTurnIdentity(domain.CanonicalStudentTurnIdentity{
		Schema: domain.CanonicalStudentTurnIdentitySchemaV1, DialogID: dialogID, StudentID: studentID, PersonalTeacherID: teacherID,
		ActionReceiptID: receiptID, CorrelationID: receiptID, CausationID: receiptID, CanonicalMessageCommandID: uuid.New(),
		Interaction: domain.TrustedCanonicalStudentTurnInteraction{Schema: domain.CanonicalStudentTurnInteractionSchemaV1, Kind: domain.CanonicalStudentTurnInteractionActionReceipt,
			ActionReceiptID: receiptID, SourcePromptMessageID: sourceID, SourcePromptMessageVersion: source.Version, BlockID: "choice-1", ActionID: "accept", SourceUIDigest: digest},
	})
	if err != nil {
		t.Fatal(err)
	}
	service := Service{Spaces: fakeSpaces{item: activeTestSpace(spaceID, studentID, now)}, Dialogs: dialogs,
		Members:  &fakeMembers{items: map[uuid.UUID]domain.Member{studentID: activeMember(dialogID, studentID, 0, 0, now)}},
		Messages: messages, CanonicalStudentTurns: ledger, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now }, NewID: uuid.New,
		TeacherOrderingV2Enabled: true, CanonicalStudentTurnEnabled: true,
		OnCanonicalStudentTurn: func(value CanonicalStudentTurnObservation) { observed = append(observed, value) },
	}
	return service, identity, source, dialogs, messages, ledger, outbox, &observed
}

type fakeCanonicalStudentTurns struct {
	items map[uuid.UUID]domain.CanonicalStudentTurn
}

func (*fakeCanonicalStudentTurns) LockIdentity(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (*fakeCanonicalStudentTurns) LockSourceAction(context.Context, uuid.UUID, uuid.UUID, string, string) error {
	return nil
}
func (f *fakeCanonicalStudentTurns) GetByActionReceiptID(_ context.Context, id uuid.UUID) (domain.CanonicalStudentTurn, error) {
	item, ok := f.items[id]
	if !ok {
		return domain.CanonicalStudentTurn{}, domain.ErrNotFound
	}
	return item, nil
}
func (f *fakeCanonicalStudentTurns) GetByCanonicalMessageCommandID(_ context.Context, id uuid.UUID) (domain.CanonicalStudentTurn, error) {
	for _, item := range f.items {
		if item.Identity.CanonicalMessageCommandID == id {
			return item, nil
		}
	}
	return domain.CanonicalStudentTurn{}, domain.ErrNotFound
}
func (f *fakeCanonicalStudentTurns) GetBySourceAction(_ context.Context, dialogID, sourceID uuid.UUID, blockID, actionID string) (domain.CanonicalStudentTurn, error) {
	for _, item := range f.items {
		interaction := item.Identity.Interaction
		if item.Identity.DialogID == dialogID && interaction.SourcePromptMessageID == sourceID && interaction.BlockID == blockID && interaction.ActionID == actionID {
			return item, nil
		}
	}
	return domain.CanonicalStudentTurn{}, domain.ErrNotFound
}
func (f *fakeCanonicalStudentTurns) GetByCanonicalMessageID(_ context.Context, messageID uuid.UUID) (domain.CanonicalStudentTurn, error) {
	for _, item := range f.items {
		if item.CanonicalStudentMessageID == messageID {
			return item, nil
		}
	}
	return domain.CanonicalStudentTurn{}, domain.ErrNotFound
}
func (f *fakeCanonicalStudentTurns) Create(_ context.Context, item domain.CanonicalStudentTurn) error {
	if _, exists := f.items[item.Identity.ActionReceiptID]; exists {
		return domain.ErrAlreadyExists
	}
	f.items[item.Identity.ActionReceiptID] = item
	return nil
}

var _ repository.CanonicalStudentTurnRepository = (*fakeCanonicalStudentTurns)(nil)
