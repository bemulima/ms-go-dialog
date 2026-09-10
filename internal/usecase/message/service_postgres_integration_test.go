package message

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/adapters/postgres"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestTeacherOrderingV2AgainstPostgres exercises the production repositories
// and transaction manager. It is intentionally opt-in because it requires an
// isolated, fully migrated PostgreSQL database.
func TestTeacherOrderingV2AgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("DIALOG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DIALOG_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	t.Run("v1 default is byte-compatible and emits once", func(t *testing.T) {
		fixture := newTeacherOrderingPostgresFixture(t, ctx, pool, false)
		result, err := fixture.service.Create(ctx, fixture.actor, CreateInput{
			DialogID: fixture.dialogID, Body: "v1 source", IdempotencyKey: uuid.New(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.View.Message.TeacherTurnSequence != nil {
			t.Fatalf("V1 source unexpectedly has teacher turn: %+v", result.View.Message)
		}
		dialogItem := fixture.dialog(t)
		if dialogItem.MaxTeacherTurnSequence != 0 {
			t.Fatalf("V1 dialog high-water=%d, want 0", dialogItem.MaxTeacherTurnSequence)
		}
		events := fixture.teacherEvents(t)
		if len(events) != 1 || events[0].SchemaVersion != 1 {
			t.Fatalf("V1 teacher events=%+v", events)
		}
		assertPayloadHasNoFields(t, events[0].Payload, "body", "canonical_student_message_id", "teacher_turn_sequence", "correlation_id", "causation_id")
	})

	t.Run("v2 is dense across reply gaps and idempotency replay", func(t *testing.T) {
		fixture := newTeacherOrderingPostgresFixture(t, ctx, pool, true)
		firstKey := uuid.New()
		first, err := fixture.service.Create(ctx, fixture.actor, CreateInput{
			DialogID: fixture.dialogID, Body: "first source", IdempotencyKey: firstKey,
		})
		if err != nil {
			t.Fatal(err)
		}
		if first.View.Message.TeacherTurnSequence == nil || *first.View.Message.TeacherTurnSequence != 1 {
			t.Fatalf("first teacher turn=%v, want 1", first.View.Message.TeacherTurnSequence)
		}
		replayed, err := fixture.service.Create(ctx, fixture.actor, CreateInput{
			DialogID: fixture.dialogID, Body: "first source", IdempotencyKey: firstKey,
		})
		if err != nil {
			t.Fatal(err)
		}
		if replayed.Created || replayed.View.Message.ID != first.View.Message.ID {
			t.Fatalf("idempotency replay result=%+v first=%+v", replayed, first)
		}
		if _, err := fixture.service.AppendTeacherResponse(ctx, AppendTeacherResponseInput{
			DialogID: fixture.dialogID, PersonalTeacherID: fixture.teacherID, SourceMessageID: first.View.Message.ID,
			IdempotencyKey: uuid.New(), Body: "teacher reply",
		}); err != nil {
			t.Fatal(err)
		}
		second, err := fixture.service.Create(ctx, fixture.actor, CreateInput{
			DialogID: fixture.dialogID, Body: "second source", IdempotencyKey: uuid.New(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if second.View.Message.TeacherTurnSequence == nil || *second.View.Message.TeacherTurnSequence != 2 {
			t.Fatalf("second teacher turn=%v, want 2", second.View.Message.TeacherTurnSequence)
		}
		dialogItem := fixture.dialog(t)
		if dialogItem.MaxTeacherTurnSequence != 2 {
			t.Fatalf("V2 dialog high-water=%d, want 2", dialogItem.MaxTeacherTurnSequence)
		}
		events := fixture.teacherEvents(t)
		if len(events) != 2 {
			t.Fatalf("V2 teacher event count=%d, want 2", len(events))
		}
		for index, event := range events {
			if event.SchemaVersion != domain.TeacherRequestedSchemaVersionV2 {
				t.Fatalf("event %d schema=%d, want V2", index, event.SchemaVersion)
			}
			payload := decodePayload(t, event.Payload)
			assertPayloadHasNoFields(t, event.Payload, "body", "links", "assistant_ui", "history")
			assertPayloadUUID(t, payload, "canonical_student_message_id")
			assertPayloadUUID(t, payload, "correlation_id")
			assertPayloadUUID(t, payload, "causation_id")
			if payload["canonical_student_message_id"] != payload["source_message_id"] ||
				payload["correlation_id"] != payload["source_message_id"] ||
				payload["causation_id"] != payload["source_message_id"] {
				t.Fatalf("direct-source V2 causal identities mismatch: %v", payload)
			}
			if turn, ok := payload["teacher_turn_sequence"].(float64); !ok || int64(turn) != int64(index+1) {
				t.Fatalf("event %d payload turn=%v, want %d", index, payload["teacher_turn_sequence"], index+1)
			}
			if version, ok := payload["source_message_version"].(float64); !ok || int(version) != 1 {
				t.Fatalf("event %d source message version=%v, want 1", index, payload["source_message_version"])
			}
			if reply, exists := payload["reply_to_message_id"]; !exists || reply != nil {
				t.Fatalf("event %d direct source reply metadata=%v, want explicit null", index, reply)
			}
		}
	})

	t.Run("v2 carries normal text reply metadata", func(t *testing.T) {
		fixture := newTeacherOrderingPostgresFixture(t, ctx, pool, true)
		prompt, err := fixture.service.AppendTeacherProactive(ctx, AppendTeacherProactiveInput{
			DialogID: fixture.dialogID, PersonalTeacherID: fixture.teacherID, IdempotencyKey: uuid.New(), Body: "Choose one",
		})
		if err != nil {
			t.Fatal(err)
		}
		created, err := fixture.service.Create(ctx, fixture.actor, CreateInput{
			DialogID: fixture.dialogID, ReplyToMessageID: &prompt.View.Message.ID, Body: "I choose A", IdempotencyKey: uuid.New(),
		})
		if err != nil {
			t.Fatal(err)
		}
		events := fixture.teacherEvents(t)
		if len(events) != 1 || events[0].SchemaVersion != domain.TeacherRequestedSchemaVersionV2 {
			t.Fatalf("normal reply teacher events=%+v", events)
		}
		payload := decodePayload(t, events[0].Payload)
		if payload["source_message_id"] != created.View.Message.ID.String() || payload["source_message_version"] != float64(1) ||
			payload["reply_to_message_id"] != prompt.View.Message.ID.String() {
			t.Fatalf("normal reply V2 source metadata mismatch: %v", payload)
		}
		assertPayloadHasNoFields(t, events[0].Payload, "body", "assistant_ui", "block_id", "action_id", "source_ui_digest", "student_command_id")
	})

	t.Run("two concurrent sources receive one dense order each", func(t *testing.T) {
		fixture := newTeacherOrderingPostgresFixture(t, ctx, pool, true)
		otherFixture := newTeacherOrderingPostgresFixture(t, ctx, pool, true)
		start := make(chan struct{})
		results := make(chan CreateResult, 2)
		errs := make(chan error, 2)
		var workers sync.WaitGroup
		for _, body := range []string{"concurrent one", "concurrent two"} {
			body := body
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				result, createErr := fixture.service.Create(ctx, fixture.actor, CreateInput{
					DialogID: fixture.dialogID, Body: body, IdempotencyKey: uuid.New(),
				})
				if createErr != nil {
					errs <- createErr
					return
				}
				results <- result
			}()
		}
		close(start)
		workers.Wait()
		close(errs)
		close(results)
		for createErr := range errs {
			t.Fatal(createErr)
		}
		turns := make([]int64, 0, 2)
		for result := range results {
			if result.View.Message.TeacherTurnSequence == nil {
				t.Fatalf("concurrent source is unsequenced: %+v", result.View.Message)
			}
			turns = append(turns, *result.View.Message.TeacherTurnSequence)
		}
		sort.Slice(turns, func(i, j int) bool { return turns[i] < turns[j] })
		if len(turns) != 2 || turns[0] != 1 || turns[1] != 2 {
			t.Fatalf("concurrent turns=%v, want [1 2]", turns)
		}
		if highWater := fixture.dialog(t).MaxTeacherTurnSequence; highWater != 2 {
			t.Fatalf("concurrent high-water=%d, want 2", highWater)
		}
		if events := fixture.teacherEvents(t); len(events) != 2 {
			t.Fatalf("concurrent teacher event count=%d, want 2", len(events))
		}
		other, err := otherFixture.service.Create(ctx, otherFixture.actor, CreateInput{
			DialogID: otherFixture.dialogID, Body: "independent dialog", IdempotencyKey: uuid.New(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if other.View.Message.TeacherTurnSequence == nil || *other.View.Message.TeacherTurnSequence != 1 ||
			otherFixture.dialog(t).MaxTeacherTurnSequence != 1 {
			t.Fatalf("independent dialog did not start at one: message=%+v dialog=%+v", other.View.Message, otherFixture.dialog(t))
		}
	})

	t.Run("outbox failure rolls back the allocated turn", func(t *testing.T) {
		fixture := newTeacherOrderingPostgresFixture(t, ctx, pool, true)
		fixture.service.Outbox = failingOutbox{OutboxRepository: fixture.outbox}
		_, err := fixture.service.Create(ctx, fixture.actor, CreateInput{
			DialogID: fixture.dialogID, Body: "must roll back", IdempotencyKey: uuid.New(),
		})
		if !errors.Is(err, errTeacherOrderingIntegrationOutbox) {
			t.Fatalf("rollback error=%v", err)
		}
		if highWater := fixture.dialog(t).MaxTeacherTurnSequence; highWater != 0 {
			t.Fatalf("rollback high-water=%d, want 0", highWater)
		}
		var messages, events int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM dialog_message WHERE dialog_id=$1`, fixture.dialogID).Scan(&messages); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM dialog_outbox WHERE dialog_id=$1`, fixture.dialogID).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if messages != 0 || events != 0 {
			t.Fatalf("rollback left messages=%d events=%d", messages, events)
		}
	})
}

// TestCanonicalStudentTurnAgainstPostgres verifies the S2 transaction against
// production repositories. It runs only against the caller's isolated database
// after migration 011 has been applied.
func TestCanonicalStudentTurnAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("DIALOG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DIALOG_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	fixture := newTeacherOrderingPostgresFixture(t, ctx, pool, true)
	fixture.service.CanonicalStudentTurnEnabled = true

	source, err := fixture.service.AppendTeacherProactive(ctx, AppendTeacherProactiveInput{
		DialogID: fixture.dialogID, PersonalTeacherID: fixture.teacherID, IdempotencyKey: uuid.New(), Body: "Choose one",
		AssistantUI: json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[{"opaque":"choice"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := domain.CanonicalAssistantUIDigest(source.View.Message.AssistantUI)
	if err != nil {
		t.Fatal(err)
	}
	receiptID := uuid.New()
	identity, err := domain.NewCanonicalStudentTurnIdentity(domain.CanonicalStudentTurnIdentity{
		Schema: domain.CanonicalStudentTurnIdentitySchemaV1, DialogID: fixture.dialogID, StudentID: fixture.studentID, PersonalTeacherID: fixture.teacherID,
		ActionReceiptID: receiptID, CorrelationID: receiptID, CausationID: receiptID, CanonicalMessageCommandID: uuid.New(),
		Interaction: domain.TrustedCanonicalStudentTurnInteraction{Schema: domain.CanonicalStudentTurnInteractionSchemaV1, Kind: domain.CanonicalStudentTurnInteractionActionReceipt,
			ActionReceiptID: receiptID, SourcePromptMessageID: source.View.Message.ID, SourcePromptMessageVersion: source.View.Message.Version,
			BlockID: "choice-1", ActionID: "accept", SourceUIDigest: digest},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := fixture.service.MaterializeCanonicalStudentTurn(ctx, MaterializeCanonicalStudentTurnInput{Identity: identity, CanonicalBody: "I choose A"})
	if err != nil || !created.Created || created.View.Message.TeacherTurnSequence == nil || *created.View.Message.TeacherTurnSequence != 1 {
		t.Fatalf("canonical turn=%+v err=%v", created, err)
	}
	var receipts int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM dialog_canonical_student_turn WHERE action_receipt_id=$1 AND canonical_student_message_id=$2`, receiptID, created.View.Message.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("private receipt ledger count=%d err=%v", receipts, err)
	}
	events := fixture.teacherEvents(t)
	if len(events) != 1 || events[0].SchemaVersion != domain.TeacherRequestedSchemaVersionV2 {
		t.Fatalf("teacher events=%+v", events)
	}
	payload := decodePayload(t, events[0].Payload)
	if payload["action_receipt_id"] != receiptID.String() || payload["correlation_id"] != receiptID.String() || payload["causation_id"] != created.View.Message.ID.String() {
		t.Fatalf("canonical action V2 ledger mismatch: %v", payload)
	}
	if payload["source_message_version"] != float64(1) || payload["reply_to_message_id"] != source.View.Message.ID.String() {
		t.Fatalf("canonical action source metadata mismatch: %v", payload)
	}
	assertPayloadHasNoFields(t, events[0].Payload, "body", "assistant_ui", "block_id", "action_id", "source_ui_digest", "student_command_id")

	rollbackReceiptID := uuid.New()
	rollbackIdentity := identity
	rollbackIdentity.ActionReceiptID, rollbackIdentity.CorrelationID, rollbackIdentity.CausationID = rollbackReceiptID, rollbackReceiptID, rollbackReceiptID
	rollbackIdentity.CanonicalMessageCommandID = uuid.New()
	rollbackIdentity.Interaction.ActionReceiptID = rollbackReceiptID
	rollbackIdentity.Interaction.BlockID = "choice-2"
	fixture.service.Outbox = failingOutbox{OutboxRepository: fixture.outbox}
	if _, err := fixture.service.MaterializeCanonicalStudentTurn(ctx, MaterializeCanonicalStudentTurnInput{Identity: rollbackIdentity, CanonicalBody: "I choose B"}); !errors.Is(err, errTeacherOrderingIntegrationOutbox) {
		t.Fatalf("canonical rollback error=%v", err)
	}
	var persistedReceipts, persistedMessages int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM dialog_canonical_student_turn WHERE dialog_id=$1`, fixture.dialogID).Scan(&persistedReceipts); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM dialog_message WHERE dialog_id=$1`, fixture.dialogID).Scan(&persistedMessages); err != nil {
		t.Fatal(err)
	}
	if persistedReceipts != 1 || persistedMessages != 2 || fixture.dialog(t).MaxTeacherTurnSequence != 1 {
		t.Fatalf("canonical rollback escaped: receipts=%d messages=%d dialog=%+v", persistedReceipts, persistedMessages, fixture.dialog(t))
	}
	fixture.service.Outbox = fixture.outbox

	fixture.service.CanonicalStudentTurnEnabled = false
	fixture.service.TeacherOrderingV2Enabled = false
	replayed, err := fixture.service.MaterializeCanonicalStudentTurn(ctx, MaterializeCanonicalStudentTurnInput{Identity: identity, CanonicalBody: "I choose A"})
	if err != nil || replayed.Created || replayed.View.Message.ID != created.View.Message.ID || len(fixture.teacherEvents(t)) != 1 {
		t.Fatalf("disabled exact replay=%+v err=%v events=%+v", replayed, err, fixture.teacherEvents(t))
	}
}

func TestCanonicalStudentTurnConcurrentAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("DIALOG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DIALOG_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	fixture := newTeacherOrderingPostgresFixture(t, ctx, pool, true)
	fixture.service.CanonicalStudentTurnEnabled = true
	source, err := fixture.service.AppendTeacherProactive(ctx, AppendTeacherProactiveInput{
		DialogID: fixture.dialogID, PersonalTeacherID: fixture.teacherID, IdempotencyKey: uuid.New(), Body: "Choose",
		AssistantUI: json.RawMessage(`{"schema":"assistant-ui.v1","blocks":[{"opaque":"choice"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := domain.CanonicalAssistantUIDigest(source.View.Message.AssistantUI)
	if err != nil {
		t.Fatal(err)
	}
	identity := func(receiptID uuid.UUID, blockID string) domain.CanonicalStudentTurnIdentity {
		value, identityErr := domain.NewCanonicalStudentTurnIdentity(domain.CanonicalStudentTurnIdentity{
			Schema: domain.CanonicalStudentTurnIdentitySchemaV1, DialogID: fixture.dialogID, StudentID: fixture.studentID, PersonalTeacherID: fixture.teacherID,
			ActionReceiptID: receiptID, CorrelationID: receiptID, CausationID: receiptID, CanonicalMessageCommandID: uuid.New(),
			Interaction: domain.TrustedCanonicalStudentTurnInteraction{Schema: domain.CanonicalStudentTurnInteractionSchemaV1, Kind: domain.CanonicalStudentTurnInteractionActionReceipt,
				ActionReceiptID: receiptID, SourcePromptMessageID: source.View.Message.ID, SourcePromptMessageVersion: source.View.Message.Version,
				BlockID: blockID, ActionID: "accept", SourceUIDigest: digest},
		})
		if identityErr != nil {
			t.Fatal(identityErr)
		}
		return value
	}

	start := make(chan struct{})
	results := make(chan CreateResult, 2)
	errorsByCall := make(chan error, 2)
	var workers sync.WaitGroup
	for index, blockID := range []string{"choice-1", "choice-2"} {
		index, blockID := index, blockID
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, materializeErr := fixture.service.MaterializeCanonicalStudentTurn(ctx, MaterializeCanonicalStudentTurnInput{Identity: identity(uuid.New(), blockID), CanonicalBody: "Choice " + string(rune('A'+index))})
			if materializeErr != nil {
				errorsByCall <- materializeErr
				return
			}
			results <- result
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(errorsByCall)
	for materializeErr := range errorsByCall {
		t.Fatal(materializeErr)
	}
	turns := make([]int64, 0, 2)
	for result := range results {
		if !result.Created || result.View.Message.TeacherTurnSequence == nil {
			t.Fatalf("concurrent canonical result=%+v", result)
		}
		turns = append(turns, *result.View.Message.TeacherTurnSequence)
	}
	sort.Slice(turns, func(i, j int) bool { return turns[i] < turns[j] })
	if len(turns) != 2 || turns[0] != 1 || turns[1] != 2 || fixture.dialog(t).MaxTeacherTurnSequence != 2 {
		t.Fatalf("concurrent canonical turns=%v dialog=%+v", turns, fixture.dialog(t))
	}

	start = make(chan struct{})
	results = make(chan CreateResult, 2)
	errorsByCall = make(chan error, 2)
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, materializeErr := fixture.service.MaterializeCanonicalStudentTurn(ctx, MaterializeCanonicalStudentTurnInput{Identity: identity(uuid.New(), "choice-race"), CanonicalBody: "Race choice"})
			if materializeErr != nil {
				errorsByCall <- materializeErr
				return
			}
			results <- result
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(errorsByCall)
	created, conflicts := 0, 0
	for result := range results {
		if result.Created {
			created++
		}
	}
	for materializeErr := range errorsByCall {
		if errors.Is(materializeErr, domain.ErrIdempotencyConflict) {
			conflicts++
			continue
		}
		t.Fatal(materializeErr)
	}
	if created != 1 || conflicts != 1 || fixture.dialog(t).MaxTeacherTurnSequence != 3 || len(fixture.teacherEvents(t)) != 3 {
		t.Fatalf("same source action was not exactly-once: created=%d conflicts=%d dialog=%+v events=%+v", created, conflicts, fixture.dialog(t), fixture.teacherEvents(t))
	}
}

var errTeacherOrderingIntegrationOutbox = errors.New("teacher ordering integration outbox failure")

type failingOutbox struct{ repository.OutboxRepository }

func (failingOutbox) Add(context.Context, domain.OutboxEvent) error {
	return errTeacherOrderingIntegrationOutbox
}

type teacherOrderingPostgresFixture struct {
	pool      *pgxpool.Pool
	service   Service
	outbox    repository.OutboxRepository
	dialogID  uuid.UUID
	studentID uuid.UUID
	teacherID uuid.UUID
	actor     domain.Actor
}

func newTeacherOrderingPostgresFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, enabled bool) teacherOrderingPostgresFixture {
	t.Helper()
	now := time.Now().UTC()
	spaceID, dialogID, studentID, teacherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	spaces := &postgres.SpaceRepository{Pool: pool}
	dialogs := &postgres.DialogRepository{Pool: pool}
	members := &postgres.MemberRepository{Pool: pool}
	messages := &postgres.MessageRepository{Pool: pool}
	canonicalStudentTurns := &postgres.CanonicalStudentTurnRepository{Pool: pool}
	outbox := &postgres.OutboxRepository{Pool: pool}
	tx := postgres.TransactionManager{Pool: pool}
	if err := tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		space := domain.Space{
			ID: spaceID, Key: "teacher-ordering-" + uuid.NewString()[:16], Name: "Teacher ordering integration",
			Status: domain.SpaceStatusActive, AllowedOrigins: []string{"https://client.example"},
			Policy: domain.DefaultPolicy(), CreatedBy: studentID, CreatedAt: now, UpdatedAt: now,
		}
		if err := spaces.Create(txCtx, space); err != nil {
			return err
		}
		dialogItem := domain.Dialog{
			ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeTeacher, Status: domain.DialogStatusActive,
			StudentID: studentID, PersonalTeacherID: teacherID, TeacherContextType: domain.TeacherContextGeneralTeacher,
			CreatedBy: studentID, Version: 1, MemberCount: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := dialogs.Create(txCtx, dialogItem); err != nil {
			return err
		}
		return members.Create(txCtx, domain.Member{
			DialogID: dialogID, UserID: studentID, Role: domain.MemberRoleMember, Status: domain.MemberStatusActive,
			AddedBy: studentID, JoinedAt: now, UpdatedAt: now,
		})
	}); err != nil {
		t.Fatal(err)
	}
	return teacherOrderingPostgresFixture{
		pool: pool, outbox: outbox, dialogID: dialogID, studentID: studentID, teacherID: teacherID,
		actor: domain.Actor{UserID: studentID, Role: "STUDENT"},
		service: Service{
			Spaces: spaces, Dialogs: dialogs, Members: members, Messages: messages, TeacherMessages: messages, CanonicalStudentTurns: canonicalStudentTurns,
			Outbox: outbox, Tx: tx, Now: func() time.Time { return time.Now().UTC() }, NewID: uuid.New,
			TeacherOrderingV2Enabled: enabled,
		},
	}
}

func (f teacherOrderingPostgresFixture) dialog(t *testing.T) domain.Dialog {
	t.Helper()
	dialogItem, err := (&postgres.DialogRepository{Pool: f.pool}).GetByID(context.Background(), f.dialogID)
	if err != nil {
		t.Fatal(err)
	}
	return dialogItem
}

type teacherOrderingEvent struct {
	SchemaVersion int16
	Payload       []byte
}

func (f teacherOrderingPostgresFixture) teacherEvents(t *testing.T) []teacherOrderingEvent {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT schema_version,payload
FROM dialog_outbox WHERE dialog_id=$1 AND subject='dialog.teacher.requested'
ORDER BY CASE WHEN schema_version >= 2 THEN (payload->>'teacher_turn_sequence')::BIGINT END,id`, f.dialogID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var events []teacherOrderingEvent
	for rows.Next() {
		var event teacherOrderingEvent
		if err := rows.Scan(&event.SchemaVersion, &event.Payload); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

func decodePayload(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertPayloadHasNoFields(t *testing.T, payload []byte, fields ...string) {
	t.Helper()
	decoded := decodePayload(t, payload)
	for _, field := range fields {
		if _, exists := decoded[field]; exists {
			t.Fatalf("payload unexpectedly contains %q: %s", field, payload)
		}
	}
}

func assertPayloadUUID(t *testing.T, payload map[string]any, field string) {
	t.Helper()
	value, ok := payload[field].(string)
	if !ok {
		t.Fatalf("payload field %q is not a UUID string: %v", field, payload[field])
	}
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil {
		t.Fatalf("payload field %q is not a non-zero UUID: %v", field, value)
	}
}
