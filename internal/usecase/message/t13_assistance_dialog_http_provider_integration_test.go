//go:build integration

package message_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
func TestT13ServeAssistanceDialogHTTP(t *testing.T) {
	if os.Getenv("T13_SERVE_DIALOG") != "true" {
		t.Skip("owned T13 Dialog fixture gate absent")
	}
	ready, stop, result := os.Getenv("T13_DIALOG_READY_FILE"), os.Getenv("T13_DIALOG_STOP_FILE"), os.Getenv("T13_DIALOG_RESULT_FILE")
	token, dbURL := os.Getenv("T13_DIALOG_TOKEN"), os.Getenv("DIALOG_TEST_DATABASE_URL")
	parsed, e := url.Parse(dbURL)
	if ready == "" || stop == "" || result == "" || token == "" || e != nil || !strings.HasSuffix(parsed.Path, "_test") {
		t.Fatal("missing owned Dialog fixture configuration")
	}
	if _, e = os.Stat(stop); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("stop marker must be absent")
	}
	var runtime struct {
		StudentID        string `json:"student_id"`
		TaskInstanceID   string `json:"task_instance_id"`
		LearningActionID string `json:"learning_action_id"`
	}
	raw, e := os.ReadFile(os.Getenv("T13_RUNTIME_READY_FILE"))
	if e != nil || json.Unmarshal(raw, &runtime) != nil {
		t.Fatal("safe Runtime metadata unavailable")
	}
	student, e := uuid.Parse(runtime.StudentID)
	if e != nil {
		t.Fatal("invalid synthetic owner")
	}
	instance, e := uuid.Parse(runtime.TaskInstanceID)
	if e != nil {
		t.Fatal("invalid instance")
	}
	action, e := uuid.Parse(runtime.LearningActionID)
	if e != nil {
		t.Fatal("invalid action")
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
		if e := spaces.Create(txCtx, domain.Space{ID: spaceID, Key: "t13-" + spaceID.String(), Name: "Synthetic T13 assistance", Status: domain.SpaceStatusActive, AllowedOrigins: []string{"https://client.example"}, Policy: domain.DefaultPolicy(), CreatedBy: student, CreatedAt: now, UpdatedAt: now}); e != nil {
			return e
		}
		if e := dialogs.Create(txCtx, domain.Dialog{ID: dialogID, SpaceID: spaceID, Type: domain.DialogTypeTeacher, Status: domain.DialogStatusActive, StudentID: student, PersonalTeacherID: teacherID, TeacherContextType: domain.TeacherContextPracticeTask, ContextID: &instance, CreatedBy: student, Version: 1, MemberCount: 1, CreatedAt: now, UpdatedAt: now}); e != nil {
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
	source, e := service.Create(ctx, domain.Actor{UserID: student, Role: "STUDENT"}, messageuc.CreateInput{DialogID: dialogID, Body: "Synthetic student assistance request", IdempotencyKey: uuid.New(), LearningActionID: &action})
	if e != nil {
		t.Fatal("actual source message transaction failed")
	}
	var envelope json.RawMessage
	if e = pool.QueryRow(ctx, `SELECT payload FROM dialog_outbox WHERE dialog_id=$1 AND subject='dialog.teacher.requested'`, dialogID).Scan(&envelope); e != nil {
		t.Fatal("actual source event absent")
	}
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
		return map[string]any{"space_id": spaceID, "dialog_id": dialogID, "source_message_id": source.View.Message.ID, "assistant_count": len(replies), "replies": replies}, nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /__t13/inspect", func(w http.ResponseWriter, r *http.Request) {
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
	mux.Handle("/", registered)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal("owned Dialog listener unavailable")
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer func() {
		if err := server.Close(); err != nil {
			t.Errorf("owned Dialog server close failed: %v", err)
		}
	}()
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- server.Serve(listener) }()
	t13DialogWrite(t, ready, map[string]any{"url": "http://" + listener.Addr().String(), "space_id": spaceID, "dialog_id": dialogID, "personal_teacher_id": teacherID, "student_id": student, "source_message_id": source.View.Message.ID, "event_payload": envelope, "classification": "actual registered Dialog private HTTP/usecase/PG; synthetic space/membership/student source; no NATS or LLM"})
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
				t13DialogWrite(t, result, safe)
				return
			} else if !errors.Is(e, os.ErrNotExist) {
				t.Fatal("stop marker read failed")
			}
		}
	}
}
func t13DialogWrite(t *testing.T, path string, value any) {
	t.Helper()
	raw, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		t.Fatal("safe metadata encoding failed")
	}
	if os.WriteFile(path+".tmp", append(raw, '\n'), 0600) != nil || os.Rename(path+".tmp", path) != nil {
		t.Fatal("safe metadata export failed")
	}
}
