//go:build integration

package message_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	dialognats "github.com/bemulima/ms-go-dialog/internal/infrastructure/messaging/nats"
	"github.com/bemulima/ms-go-dialog/internal/infrastructure/persistence/postgres"
	dialoghttp "github.com/bemulima/ms-go-dialog/internal/transport/http"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	realtimeuc "github.com/bemulima/ms-go-dialog/internal/usecase/realtime"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type t15DialogLane struct {
	URL               string    `json:"url"`
	StudentID         uuid.UUID `json:"student_id"`
	PersonalTeacherID uuid.UUID `json:"personal_teacher_id"`
	DialogID          uuid.UUID `json:"dialog_id"`
	SpaceID           uuid.UUID `json:"space_id"`
	server            *httptest.Server
}

// This named fault loses only the reply AFTER the real JetStream client has
// received PubAck. Dispatcher then records its ordinary durable pending retry.
// It never substitutes an ACK or claims publication without the network call.
type t15LostPublishReply struct {
	client      *dialognats.Client
	armed       atomic.Bool
	lost        atomic.Int64
	lostEventID uuid.UUID
}

func (p *t15LostPublishReply) PublishLifecycle(ctx context.Context, event domain.OutboxEvent) error {
	if err := p.client.PublishLifecycle(ctx, event); err != nil {
		return err
	}
	if event.Subject == domain.EventDialogTeacherRequested && p.armed.CompareAndSwap(true, false) {
		p.lost.Add(1)
		p.lostEventID = event.ID
		return errors.New("t15_lost_publish_reply_after_real_jetstream_puback")
	}
	return nil
}

// ROOT alone starts this provider against a freshly migrated disposable DB and
// dedicated loopback NATS. Identity/space/member fixtures are synthetic; public
// creates, private context/appends, PG transactions/outbox and publication are
// production implementations. No JWT, LLM, main-service or full-E2E claim.
func TestT15ServeDenseDialogHTTP(t *testing.T) {
	if os.Getenv("T15_SERVE_DIALOG") != "true" {
		t.Skip("owned T15 Dialog fixture gate absent")
	}
	ready, stop, result := os.Getenv("T15_DIALOG_READY_FILE"), os.Getenv("T15_DIALOG_STOP_FILE"), os.Getenv("T15_DIALOG_RESULT_FILE")
	token, dbURL := os.Getenv("T15_DIALOG_TOKEN"), os.Getenv("DIALOG_TEST_DATABASE_URL")
	parsed, err := url.Parse(dbURL)
	natsURL, natsErr := url.Parse(os.Getenv("NATS_URL"))
	if ready == "" || stop == "" || result == "" || token == "" || err != nil || !strings.HasSuffix(parsed.Path, "_test") || natsErr != nil || natsURL.Scheme != "nats" || natsURL.User != nil || !t15Loopback(natsURL.Hostname()) {
		t.Fatal("missing isolated T15 Dialog provider configuration")
	}
	if _, err := os.Stat(stop); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stop marker must be absent")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal("owned Dialog database unavailable")
	}
	defer pool.Close()
	// A suffix alone does not authorize global Dispatcher claims. Refuse an
	// existing populated DB before constructing any owned fixture or dispatcher.
	var occupied bool
	if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM dialog) OR EXISTS(SELECT 1 FROM dialog_outbox) OR EXISTS(SELECT 1 FROM dialog_space)`).Scan(&occupied); err != nil || occupied {
		t.Fatal("T15 requires fresh empty isolated Dialog database")
	}
	conn, err := dialognats.Connect(natsURL.String())
	if err != nil {
		t.Fatal("owned NATS connection unavailable")
	}
	defer conn.Close()
	client := &dialognats.Client{Conn: conn}
	setupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = client.EnsureLifecycleStream(setupCtx)
	cancel()
	if err != nil {
		t.Fatal("owned lifecycle stream setup failed")
	}
	spaces := &postgres.SpaceRepository{Pool: pool}
	dialogs := &postgres.DialogRepository{Pool: pool}
	members := &postgres.MemberRepository{Pool: pool}
	messages := &postgres.MessageRepository{Pool: pool}
	outbox := &postgres.OutboxRepository{Pool: pool}
	tx := postgres.TransactionManager{Pool: pool}
	routers := map[bool]http.Handler{}
	for _, dense := range []bool{false, true} {
		service := &messageuc.Service{Spaces: spaces, Dialogs: dialogs, Members: members, Messages: messages, TeacherMessages: messages, Attachments: &postgres.AttachmentRepository{Pool: pool}, Outbox: outbox, Tx: tx, Now: func() time.Time { return time.Now().UTC() }, NewID: uuid.New, TeacherOrderingV2Enabled: dense}
		routers[dense] = dialoghttp.NewRouter(dialoghttp.RouterDependencies{InternalToken: token, DialogService: &dialoguc.Service{Spaces: spaces, Dialogs: dialogs, TeacherDialogs: dialogs, Members: members, Outbox: outbox, Tx: tx}, MessageService: service})
	}
	publisher := &t15LostPublishReply{client: client}
	dispatcher := realtimeuc.Dispatcher{Outbox: outbox, Publisher: publisher}
	var mu sync.Mutex
	lanes := map[uuid.UUID]*t15DialogLane{}
	var cut atomic.Bool
	var cutCount atomic.Int64
	defer func() {
		// All listeners are stopped before owned-row cleanup. Existing streams are
		// retained; ROOT removes only its dedicated NATS process/storage directory.
		for _, lane := range lanes {
			lane.server.Close()
		}
		for _, lane := range lanes {
			if err := t15CleanupDialog(pool, lane); err != nil {
				t.Error("owned T15 Dialog cleanup failed")
			}
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /__t15/lanes", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Dense bool `json:"dense"`
		}
		if !t15Decode(w, r, &input) {
			return
		}
		lane := &t15DialogLane{StudentID: uuid.New(), PersonalTeacherID: uuid.New(), DialogID: uuid.New(), SpaceID: uuid.New()}
		now := time.Now().UTC()
		err := tx.WithinTransaction(r.Context(), func(c context.Context) error {
			if err := spaces.Create(c, domain.Space{ID: lane.SpaceID, Key: "t15-" + lane.SpaceID.String(), Name: "Synthetic T15 teacher ordering", Status: domain.SpaceStatusActive, AllowedOrigins: []string{"https://client.example"}, Policy: domain.DefaultPolicy(), CreatedBy: lane.StudentID, CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
			if err := dialogs.Create(c, domain.Dialog{ID: lane.DialogID, SpaceID: lane.SpaceID, Type: domain.DialogTypeTeacher, Status: domain.DialogStatusActive, StudentID: lane.StudentID, PersonalTeacherID: lane.PersonalTeacherID, TeacherContextType: domain.TeacherContextGeneralTeacher, CreatedBy: lane.StudentID, Version: 1, MemberCount: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
			return members.Create(c, domain.Member{DialogID: lane.DialogID, UserID: lane.StudentID, Role: domain.MemberRoleMember, Status: domain.MemberStatusActive, AddedBy: lane.StudentID, JoinedAt: now, UpdatedAt: now})
		})
		if err != nil {
			w.WriteHeader(500)
			return
		}
		registered := routers[input.Dense]
		lane.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Retain the registered route's exact authentication and transaction. Only
			// this lane's successful response append may consume the once-only cut.
			if r.Method == http.MethodPost && r.URL.Path == "/internal/v1/teacher-dialog/"+lane.DialogID.String()+"/message" && cut.Load() {
				recorder := httptest.NewRecorder()
				registered.ServeHTTP(recorder, r)
				if recorder.Code >= 200 && recorder.Code < 300 && cut.CompareAndSwap(true, false) {
					cutCount.Add(1)
					w.WriteHeader(503)
					return
				}
				for key, values := range recorder.Header() {
					w.Header()[key] = values
				}
				w.WriteHeader(recorder.Code)
				_, _ = io.Copy(w, recorder.Body)
				return
			}
			registered.ServeHTTP(w, r)
		}))
		lane.URL = lane.server.URL
		lanes[lane.DialogID] = lane
		t15JSON(w, lane)
	})
	mux.HandleFunc("POST /__t15/publish", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			DialogID            uuid.UUID  `json:"dialog_id"`
			TeacherTurnSequence *int64     `json:"teacher_turn_sequence"`
			SourceMessageID     *uuid.UUID `json:"source_message_id"`
		}
		if !t15Decode(w, r, &input) {
			return
		}
		if lanes[input.DialogID] == nil || (input.TeacherTurnSequence == nil) == (input.SourceMessageID == nil) || (input.TeacherTurnSequence != nil && *input.TeacherTurnSequence < 1) || (input.SourceMessageID != nil && *input.SourceMessageID == uuid.Nil) {
			w.WriteHeader(400)
			return
		}
		// Strict selection of exactly one actual stored teacher-request trigger.
		rows, err := pool.Query(r.Context(), `SELECT id,dialog_id,aggregate_type,aggregate_id,subject,event_sequence,schema_version,payload,attempts,next_attempt_at,published_at,COALESCE(last_error,''),created_at FROM dialog_outbox WHERE dialog_id=$1 AND subject='dialog.teacher.requested' AND (($2::bigint IS NOT NULL AND payload->>'teacher_turn_sequence'=$2::text) OR ($3::uuid IS NOT NULL AND payload->>'source_message_id'=$3::text))`, input.DialogID, input.TeacherTurnSequence, input.SourceMessageID)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		events := []domain.OutboxEvent{}
		for rows.Next() {
			var event domain.OutboxEvent
			err = rows.Scan(&event.ID, &event.DialogID, &event.AggregateType, &event.AggregateID, &event.Subject, &event.EventSequence, &event.SchemaVersion, &event.Payload, &event.Attempts, &event.NextAttemptAt, &event.PublishedAt, &event.LastError, &event.CreatedAt)
			if err != nil {
				break
			}
			events = append(events, event)
		}
		rowErr := rows.Err()
		rows.Close()
		if err != nil || rowErr != nil {
			w.WriteHeader(500)
			return
		}
		if len(events) != 1 {
			w.WriteHeader(409)
			return
		}
		c, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err = client.PublishLifecycle(c, events[0]); err != nil {
			w.WriteHeader(503)
			return
		}
		if err = outbox.MarkPublished(c, events[0].ID, time.Now().UTC()); err != nil {
			w.WriteHeader(503)
			return
		}
		t15JSON(w, map[string]any{"event_id": events[0].ID, "event_payload": events[0].Payload})
	})
	mux.HandleFunc("POST /__t15/dispatch", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Lose bool `json:"lose_publish_reply_once"`
		}
		if !t15Decode(w, r, &input) {
			return
		}
		if input.Lose {
			if publisher.lost.Load() != 0 {
				w.WriteHeader(409)
				return
			}
			publisher.armed.Store(true)
		}
		c, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		result, err := dispatcher.Process(c, 1000)
		if err != nil {
			w.WriteHeader(503)
			return
		}
		t15JSON(w, map[string]any{"published": result.Published, "failed": result.Failed, "lost_publish_reply_count": publisher.lost.Load(), "lost_event_id": publisher.lostEventID})
	})
	mux.HandleFunc("POST /__t15/append-cut", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Enabled bool `json:"enabled"`
		}
		if !t15Decode(w, r, &input) {
			return
		}
		if input.Enabled && cutCount.Load() != 0 {
			w.WriteHeader(409)
			return
		}
		cut.Store(input.Enabled)
		t15JSON(w, map[string]any{"enabled": cut.Load(), "append_cut_count": cutCount.Load()})
	})
	mux.HandleFunc("GET /__t15/inspect", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.URL.Query().Get("dialog_id"))
		if err != nil || lanes[id] == nil {
			w.WriteHeader(404)
			return
		}
		safe, err := t15InspectDialog(r.Context(), pool, lanes[id])
		if err != nil {
			w.WriteHeader(500)
			return
		}
		safe["append_cut_count"] = cutCount.Load()
		t15JSON(w, safe)
	})
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != token {
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	defer control.Close()
	t15DialogWrite(t, ready, map[string]any{"control_url": control.URL, "classification": "actual registered Dialog public create/private context/append, PostgreSQL transactional outbox and real JetStream publication; synthetic teacher/learner/space/member fixtures; no JWT/LLM/full E2E"})
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(30 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("owned T15 provider deadline exceeded")
		case <-ticker.C:
			if _, err := os.Stat(stop); err == nil {
				control.Close()
				mu.Lock()
				results := []map[string]any{}
				for _, lane := range lanes {
					safe, err := t15InspectDialog(ctx, pool, lane)
					if err != nil {
						mu.Unlock()
						t.Fatal("safe T15 result read failed")
					}
					results = append(results, safe)
				}
				mu.Unlock()
				t15DialogWrite(t, result, map[string]any{"lanes": results, "append_cut_count": cutCount.Load(), "lost_publish_reply_count": publisher.lost.Load(), "lost_event_id": publisher.lostEventID})
				return
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal("stop marker read failed")
			}
		}
	}
}

func t15Loopback(host string) bool { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }
func t15Decode(w http.ResponseWriter, r *http.Request, out any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		w.WriteHeader(400)
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		w.WriteHeader(400)
		return false
	}
	return true
}
func t15JSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func t15DialogWrite(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal("safe T15 metadata encoding failed")
	}
	if os.WriteFile(path+".tmp", append(raw, '\n'), 0600) != nil || os.Rename(path+".tmp", path) != nil {
		t.Fatal("safe T15 metadata export failed")
	}
}
func t15InspectDialog(ctx context.Context, pool *pgxpool.Pool, lane *t15DialogLane) (map[string]any, error) {
	var highwater, turns, sourceCount int64
	if err := pool.QueryRow(ctx, `SELECT max_message_sequence,max_teacher_turn_sequence,(SELECT count(*) FROM dialog_message WHERE dialog_id=$1 AND author_type='user') FROM dialog WHERE id=$1 AND space_id=$2`, lane.DialogID, lane.SpaceID).Scan(&highwater, &turns, &sourceCount); err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT id::text,idempotency_key::text,body FROM dialog_message WHERE dialog_id=$1 AND author_type='personal_teacher' ORDER BY message_sequence`, lane.DialogID)
	if err != nil {
		return nil, err
	}
	replies := []map[string]any{}
	for rows.Next() {
		var id, key, body string
		if err = rows.Scan(&id, &key, &body); err != nil {
			rows.Close()
			return nil, err
		}
		digest := sha256.Sum256([]byte(body))
		replies = append(replies, map[string]any{"id": id, "key": key, "sha256": hex.EncodeToString(digest[:])})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = pool.Query(ctx, `SELECT id::text,subject,attempts,published_at IS NOT NULL FROM dialog_outbox WHERE dialog_id=$1 ORDER BY event_sequence,id`, lane.DialogID)
	if err != nil {
		return nil, err
	}
	statuses := []map[string]any{}
	for rows.Next() {
		var id, subject string
		var attempts int
		var published bool
		if err = rows.Scan(&id, &subject, &attempts, &published); err != nil {
			rows.Close()
			return nil, err
		}
		statuses = append(statuses, map[string]any{"id": id, "subject": subject, "attempts": attempts, "published": published})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return map[string]any{"dialog_id": lane.DialogID, "space_id": lane.SpaceID, "source_count": sourceCount, "assistant_count": len(replies), "highwater": highwater, "max_teacher_turn_sequence": turns, "replies": replies, "outbox": statuses}, nil
}

func t15CleanupDialog(pool *pgxpool.Pool, lane *t15DialogLane) error {
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Clear owned last-message FK, remove assistant references before sources,
	// then delete circular teacher-dialog/member pair in one statement (RESTRICT).
	for _, query := range []string{
		`UPDATE dialog SET last_message_id=NULL,last_message_at=NULL,message_count=0,max_message_sequence=0 WHERE id=$1`,
		`DELETE FROM dialog_outbox WHERE dialog_id=$1`,
		`DELETE FROM dialog_message WHERE dialog_id=$1 AND author_type='personal_teacher'`,
		`DELETE FROM dialog_message WHERE dialog_id=$1`,
	} {
		if _, err = tx.Exec(ctx, query, lane.DialogID); err != nil {
			return err
		}
	}
	command, err := tx.Exec(ctx, `WITH removed_members AS (DELETE FROM dialog_member WHERE dialog_id=$1 AND user_id=$2 RETURNING dialog_id) DELETE FROM dialog WHERE id=$1 AND student_id=$2 AND space_id=$3 AND personal_teacher_id=$4 AND EXISTS(SELECT 1 FROM removed_members WHERE dialog_id=$1)`, lane.DialogID, lane.StudentID, lane.SpaceID, lane.PersonalTeacherID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return errors.New("owned cleanup identity mismatch")
	}
	if _, err = tx.Exec(ctx, `DELETE FROM dialog_space WHERE id=$1`, lane.SpaceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
