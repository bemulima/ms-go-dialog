//go:build integration

package message_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/infrastructure/persistence/postgres"
	dialoghttp "github.com/bemulima/ms-go-dialog/internal/transport/http"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// The fixture is synthetic space/membership/source data, with real repository
// transactions, actual message use cases and registered private HTTP handlers.
// It runs in a root-migrated, disposable _test DB. No NATS publisher is started.
func TestT14ServeFrozenLessonHTTP(t *testing.T) {
	if os.Getenv("T14_SERVE_DIALOG") != "true" {
		t.Skip("owned T14 Dialog fixture gate absent")
	}
	ready, stop, result := os.Getenv("T14_DIALOG_READY_FILE"), os.Getenv("T14_DIALOG_STOP_FILE"), os.Getenv("T14_DIALOG_RESULT_FILE")
	token, dbURL := os.Getenv("T14_DIALOG_TOKEN"), os.Getenv("DIALOG_TEST_DATABASE_URL")
	parsed, e := url.Parse(dbURL)
	if ready == "" || stop == "" || result == "" || token == "" || e != nil || !strings.HasSuffix(parsed.Path, "_test") {
		t.Fatal("missing owned Dialog fixture configuration")
	}
	if _, e = os.Stat(stop); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("stop marker must be absent")
	}
	var studentMetadata struct {
		StudentID string `json:"student_id"`
		PathID    string `json:"learning_path_id"`
		ItemID    string `json:"learning_path_item_id"`
		CourseID  string `json:"course_id"`
		LessonID  string `json:"lesson_id"`
		Revision  string `json:"lesson_revision_id"`
		Digest    string `json:"content_digest"`
	}
	raw, e := os.ReadFile(os.Getenv("T14_STUDENT_READY_FILE"))
	if e != nil || json.Unmarshal(raw, &studentMetadata) != nil {
		t.Fatal("SETUP: Student metadata unavailable")
	}
	student, e := uuid.Parse(studentMetadata.StudentID)
	if e != nil {
		t.Fatal("SETUP: invalid owner")
	}
	lessonID, e := uuid.Parse(studentMetadata.LessonID)
	if e != nil {
		t.Fatal("SETUP: invalid lesson")
	}
	ctx := context.Background()
	pool, e := postgres.Connect(ctx, dbURL)
	if e != nil {
		t.Fatal("owned Dialog database unavailable")
	}
	defer pool.Close()
	spaceID, dialogID, teacherID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	spaces := &postgres.SpaceRepository{Pool: pool}
	dialogs := &postgres.DialogRepository{Pool: pool}
	members := &postgres.MemberRepository{Pool: pool}
	messages := &postgres.MessageRepository{Pool: pool}
	outbox := &postgres.OutboxRepository{Pool: pool}
	tx := postgres.TransactionManager{Pool: pool}
	e = tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if e := spaces.Create(txCtx, domain.Space{ID: spaceID, Key: "t14-" + spaceID.String(), Name: "Synthetic T14 frozen lesson", Status: domain.SpaceStatusActive, AllowedOrigins: []string{"https://client.example"}, Policy: domain.DefaultPolicy(), CreatedBy: student, CreatedAt: now, UpdatedAt: now}); e != nil {
			return e
		}
		if e := dialogs.Create(txCtx, domain.Dialog{ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeTeacher, Status: domain.DialogStatusActive, StudentID: student, PersonalTeacherID: teacherID, TeacherContextType: domain.TeacherContextLesson, ContextID: &lessonID, CreatedBy: student, Version: 1, MemberCount: 1, CreatedAt: now, UpdatedAt: now}); e != nil {
			return e
		}
		return members.Create(txCtx, domain.Member{DialogID: dialogID, UserID: student, Role: domain.MemberRoleMember, Status: domain.MemberStatusActive, AddedBy: student, JoinedAt: now, UpdatedAt: now})
	})
	if e != nil {
		t.Fatal("synthetic Dialog seed failed")
	}
	defer func() {
		// ON DELETE RESTRICT acts at statement boundaries even for the two
		// DEFERRABLE FKs. Clear this owned last-message reference first, then
		// delete the circular teacher-dialog/member pair in ONE SQL statement.
		// All changes occur only after the provider stops, inside one cleanup
		// transaction; neither constraints nor immutable fixture IDs change.
		stage := "begin"
		cleanupError := func(err error) {
			var pgError *pgconn.PgError
			if errors.As(err, &pgError) {
				t.Errorf("owned Dialog fixture cleanup failed stage=%s sqlstate=%s constraint=%s table=%s", stage, pgError.Code, pgError.ConstraintName, pgError.TableName)
			} else {
				t.Errorf("owned Dialog fixture cleanup failed stage=%s class=non_postgres", stage)
			}
		}
		cleanupTx, e := pool.Begin(context.Background())
		if e != nil {
			cleanupError(e)
			return
		}
		defer func() { _ = cleanupTx.Rollback(context.Background()) }()
		e = func() error {
			c := context.Background()
			stage = "clear_last_message"
			if _, e := cleanupTx.Exec(c, `UPDATE dialog SET last_message_id=NULL,last_message_at=NULL,message_count=0,max_message_sequence=0 WHERE id=$1 AND space_id=$2 AND student_id=$3 AND personal_teacher_id=$4`, dialogID, spaceID, student, teacherID); e != nil {
				return e
			}
			stage = "delete_outbox"
			if _, e := cleanupTx.Exec(c, `DELETE FROM dialog_outbox WHERE dialog_id=$1`, dialogID); e != nil {
				return e
			}
			stage = "delete_assistant"
			if _, e := cleanupTx.Exec(c, `DELETE FROM dialog_message WHERE dialog_id=$1 AND author_type='personal_teacher'`, dialogID); e != nil {
				return e
			}
			stage = "delete_source"
			if _, e := cleanupTx.Exec(c, `DELETE FROM dialog_message WHERE dialog_id=$1`, dialogID); e != nil {
				return e
			}
			stage = "delete_dialog_member_pair"
			command, e := cleanupTx.Exec(c, `WITH removed_members AS (
              DELETE FROM dialog_member WHERE dialog_id=$1 AND user_id=$2 RETURNING dialog_id
            ) DELETE FROM dialog WHERE id=$1 AND student_id=$2 AND space_id=$3 AND personal_teacher_id=$4
            AND EXISTS(SELECT 1 FROM removed_members WHERE dialog_id=$1)`, dialogID, student, spaceID, teacherID)
			if e != nil {
				return e
			}
			if command.RowsAffected() != 1 {
				return errors.New("owned cleanup identity count mismatch")
			}
			stage = "delete_space"
			_, e = cleanupTx.Exec(c, `DELETE FROM dialog_space WHERE id=$1`, spaceID)
			return e
		}()
		if e == nil {
			stage = "commit"
			e = cleanupTx.Commit(context.Background())
		}
		if e != nil {
			cleanupError(e)
		}
	}()
	service := &messageuc.Service{Spaces: spaces, Dialogs: dialogs, Members: members, Messages: messages, TeacherMessages: messages, Attachments: &postgres.AttachmentRepository{Pool: pool}, Outbox: outbox, Tx: tx, Now: func() time.Time { return time.Now().UTC() }, NewID: uuid.New}
	registered := dialoghttp.NewRouter(dialoghttp.RouterDependencies{InternalToken: token, DialogService: &dialoguc.Service{Spaces: spaces, Dialogs: dialogs, TeacherDialogs: dialogs, Members: members, Outbox: outbox, Tx: tx}, MessageService: service})
	inspect := func() (map[string]any, error) {
		rows, e := pool.Query(ctx, `SELECT id::text,idempotency_key::text,body FROM dialog_message WHERE dialog_id=$1 AND author_type='personal_teacher' ORDER BY message_sequence`, dialogID)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		replies := []map[string]any{}
		for rows.Next() {
			var id, key, body string
			if e = rows.Scan(&id, &key, &body); e != nil {
				return nil, e
			}
			digest := sha256.Sum256([]byte(body))
			replies = append(replies, map[string]any{"message_id": id, "append_key": key, "body_sha256": hex.EncodeToString(digest[:])})
		}
		if e = rows.Err(); e != nil {
			return nil, e
		}
		return map[string]any{"space_id": spaceID, "dialog_id": dialogID, "assistant_count": len(replies), "replies": replies}, nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /__t14/inspect", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		safe, e := inspect()
		if e != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(safe)
	})
	mux.HandleFunc("GET /__t14/latest-source", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var envelope json.RawMessage
		if pool.QueryRow(r.Context(), `SELECT payload FROM dialog_outbox WHERE dialog_id=$1 AND subject='dialog.teacher.requested' ORDER BY event_sequence DESC LIMIT 1`, dialogID).Scan(&envelope) != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"event_payload": envelope})
	})
	mux.Handle("/", registered)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal("owned Dialog listener unavailable")
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer func() { _ = server.Close() }()
	errorsCh := make(chan error, 1)
	baseURL := "http://" + listener.Addr().String()
	// Commands traverse the actual registered public HTTP handler, then PG message
	// transaction and transactional outbox. Control routes require exact root token.
	createSource := func(ctx context.Context, lessonContext any) (json.RawMessage, int, error) {
		requestBody, _ := json.Marshal(map[string]any{"dialog_id": dialogID, "body": "Explain the bounded frozen lesson fragment.", "idempotency_key": uuid.New(), "lesson_context": lessonContext})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/message/create", bytes.NewReader(requestBody))
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", student.String())
		req.Header.Set("X-User-Role", "STUDENT")
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2<<20))
		if resp.StatusCode != 201 {
			return nil, resp.StatusCode, nil
		}
		var event json.RawMessage
		err = pool.QueryRow(ctx, `SELECT payload FROM dialog_outbox WHERE dialog_id=$1 AND subject='dialog.teacher.requested' ORDER BY event_sequence DESC LIMIT 1`, dialogID).Scan(&event)
		return event, resp.StatusCode, err
	}
	sourceContext := map[string]string{"schema": "lesson-message-context.v2", "mode": "selection", "course_id": studentMetadata.CourseID, "lesson_id": studentMetadata.LessonID, "content_revision": studentMetadata.Revision, "learning_path_id": studentMetadata.PathID, "learning_path_item_id": studentMetadata.ItemID, "content_digest": studentMetadata.Digest, "selected_text": "Keep the exact selected fragment unchanged."}
	go func() { errorsCh <- server.Serve(listener) }()
	envelope, status, err := createSource(ctx, sourceContext)
	if err != nil || status != 201 {
		t.Fatalf("actual registered Dialog V2 source status=%d; no append attempted", status)
	}
	mux.HandleFunc("POST /__t14/source", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != token {
			w.WriteHeader(401)
			return
		}
		var input struct {
			Context json.RawMessage `json:"context"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<17))
		dec.DisallowUnknownFields()
		if dec.Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		event, status, err := createSource(r.Context(), input.Context)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		if status != 201 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"event_payload": event})
	})
	t14DialogWrite(t, ready, map[string]any{"url": baseURL, "space_id": spaceID, "dialog_id": dialogID, "personal_teacher_id": teacherID, "student_id": student, "event_payload": envelope, "classification": "actual registered public Dialog source HTTP/create/usecase/PG/outbox and private context/append routes; synthetic learner text/space/membership; no NATS or LLM"})
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(30 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case e := <-errorsCh:
			if !errors.Is(e, http.ErrServerClosed) {
				t.Fatal("owned Dialog listener failed")
			}
			return
		case <-deadline.C:
			t.Fatal("owned Dialog fixture deadline exceeded")
		case <-ticker.C:
			if _, e := os.Stat(stop); e == nil {
				safe, e := inspect()
				if e != nil {
					t.Fatal("safe Dialog result read failed")
				}
				t14DialogWrite(t, result, safe)
				return
			} else if !errors.Is(e, os.ErrNotExist) {
				t.Fatal("stop marker read failed")
			}
		}
	}
}
func t14DialogWrite(t *testing.T, path string, value any) {
	t.Helper()
	raw, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		t.Fatal("safe metadata encoding failed")
	}
	if os.WriteFile(path+".tmp", append(raw, '\n'), 0600) != nil || os.Rename(path+".tmp", path) != nil {
		t.Fatal("safe metadata export failed")
	}
}
