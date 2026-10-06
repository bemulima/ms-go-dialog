//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Root runs this only against an owned disposable _test database. All DDL and
// fixtures are scoped to one random schema; no provider or model is contacted.
func TestT14LessonContextMigrationAgainstPostgres(t *testing.T) {
	if os.Getenv("T14_DIALOG_MIGRATION_TEST") != "true" {
		t.Skip("owned migration fixture gate absent")
	}
	databaseURL := os.Getenv("DIALOG_TEST_DATABASE_URL")
	parsed, err := url.Parse(databaseURL)
	if err != nil || !strings.HasSuffix(parsed.Path, "_test") {
		t.Fatal("requires owned disposable _test database")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	schema := "t14_context_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(ctx, "ROLLBACK"); _, _ = conn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") }()
	if _, err = conn.Exec(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("../../../../db/migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 12 {
		t.Fatalf("expected migrations 001–012, got %d", len(files))
	}
	read := func(name string) string {
		t.Helper()
		raw, e := os.ReadFile(filepath.Join("../../../../db/migrations", name))
		if e != nil {
			t.Fatal(e)
		}
		return string(raw)
	}
	constraint := func() string {
		t.Helper()
		var definition string
		if e := conn.QueryRow(ctx, "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='dialog_message'::regclass AND conname='chk_dialog_message_lesson_context'").Scan(&definition); e != nil {
			t.Fatal(e)
		}
		return definition
	}
	for _, file := range files[:11] {
		raw, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = conn.Exec(ctx, string(raw)); e != nil {
			t.Fatalf("migration %s: %v", filepath.Base(file), e)
		}
	}
	original := constraint()
	up, down := read("012_lesson_message_context_v2.up.sql"), read("012_lesson_message_context_v2.down.sql")
	apply := func(sql string) {
		t.Helper()
		if _, e := conn.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	space, dialog, sender := uuid.New(), uuid.New(), uuid.New()
	if _, err = conn.Exec(ctx, "INSERT INTO dialog_space (id,key,name,created_by) VALUES ($1,$2,'T14 migration',$3)", space, "t14-"+uuid.NewString(), sender); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, "INSERT INTO dialog (id,space_id,type,title,created_by,member_count) VALUES ($1,$2,2,'T14 migration',$3,2)", dialog, space, sender); err != nil {
		t.Fatal(err)
	}
	sequence := int64(0)
	insert := func(value any) (uuid.UUID, error) {
		raw, e := json.Marshal(value)
		if e != nil {
			return uuid.Nil, e
		}
		sequence++
		id := uuid.New()
		_, e = conn.Exec(ctx, "INSERT INTO dialog_message (id,dialog_id,sender_id,body,message_sequence,last_event_sequence,idempotency_key,lesson_context) VALUES ($1,$2,$3,'synthetic',$4,$4,$5,$6::jsonb)", id, dialog, sender, sequence, uuid.New(), string(raw))
		return id, e
	}
	anchor := map[string]any{"schema": "lesson-message-context.v2", "mode": "lesson_overview", "course_id": uuid.NewString(), "lesson_id": uuid.NewString(), "content_revision": uuid.NewString(), "learning_path_id": uuid.NewString(), "learning_path_item_id": uuid.NewString(), "content_digest": "sha256:" + strings.Repeat("a", 64)}
	// The current 008 constraint rejects a valid frozen anchor before 012.
	if _, e := insert(anchor); e == nil {
		t.Fatal("008 unexpectedly accepted v2 before migration")
	} else if pgerr, ok := e.(*pgconn.PgError); !ok || pgerr.Code != "23514" || pgerr.ConstraintName != "chk_dialog_message_lesson_context" {
		t.Fatalf("008 rejection was not lesson constraint: %v", e)
	}
	apply(up)
	v2Definition := constraint()
	apply(down)
	if constraint() != original {
		t.Fatal("down did not restore exact 008 definition")
	}
	apply(up)
	if constraint() != v2Definition {
		t.Fatal("up/down/up constraint drift")
	}

	for _, value := range []any{map[string]any{"content_revision": "2026-09-04T08:00:00Z", "selected_text": "legacy"}, map[string]any{"schema": "lesson-message-context.v1", "mode": "lesson_overview", "course_id": uuid.NewString(), "lesson_id": uuid.NewString(), "content_revision": "2026-09-04T08:00:00Z"}, anchor} {
		if _, e := insert(value); e != nil {
			t.Fatalf("positive anchor rejected: %v", e)
		}
	}
	reject := func(value any) {
		t.Helper()
		_, e := insert(value)
		pgerr, ok := e.(*pgconn.PgError)
		if !ok || pgerr.Code != "23514" || pgerr.ConstraintName != "chk_dialog_message_lesson_context" {
			t.Fatalf("expected lesson CHECK rejection, got %v", e)
		}
	}
	clone := func() map[string]any {
		v := map[string]any{}
		for k, x := range anchor {
			v[k] = x
		}
		return v
	}
	for key := range anchor {
		for _, kind := range []string{"missing", "null", "type"} {
			t.Run(key+"_"+kind, func(t *testing.T) {
				v := clone()
				switch kind {
				case "missing":
					delete(v, key)
				case "null":
					v[key] = nil
				case "type":
					v[key] = 7
				}
				reject(v)
			})
		}
	}
	for _, key := range []string{"course_id", "lesson_id", "content_revision", "learning_path_id", "learning_path_item_id"} {
		v := clone()
		v[key] = uuid.Nil.String()
		reject(v)
		v = clone()
		v[key] = "2026-09-04T08:00:00Z"
		reject(v)
	}
	for _, mutation := range []map[string]any{{"unknown": "x"}, {"mode": "invalid"}, {"selected_text": nil}, {"selected_text": "x"}, {"content_digest": "sha256:" + strings.Repeat("A", 64)}, {"mode": "selection"}, {"mode": "selection", "selected_text": " \n\t "}, {"mode": "selection", "selected_text": strings.Repeat("界", 12001)}, {"mode": "selection", "selected_text": false}} {
		v := clone()
		for k, x := range mutation {
			v[k] = x
		}
		reject(v)
	}
	selection := clone()
	selection["mode"] = "selection"
	selection["selected_text"] = " \n" + strings.Repeat("界", 11996) + "\n "
	retained, e := insert(selection)
	if e != nil {
		t.Fatalf("12000-rune exact selection rejected: %v", e)
	}
	var before string
	if e = conn.QueryRow(ctx, "SELECT lesson_context::text FROM dialog_message WHERE id=$1", retained).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(ctx, down); e == nil {
		t.Fatal("down discarded frozen anchors")
	}
	// Failed multi-statement down must leave an aborted transaction; rollback
	// returns the unchanged V2 constraint and exact retained JSON bytes.
	if _, e = conn.Exec(ctx, "ROLLBACK"); e != nil {
		t.Fatal(e)
	}
	if constraint() != v2Definition {
		t.Fatal("failed rollback changed constraint")
	}
	var after string
	if e = conn.QueryRow(ctx, "SELECT lesson_context::text FROM dialog_message WHERE id=$1", retained).Scan(&after); e != nil || after != before {
		t.Fatalf("failed rollback changed retained anchor: %v", e)
	}
	if _, e = conn.Exec(ctx, "DELETE FROM dialog_message WHERE lesson_context->>'schema'='lesson-message-context.v2'"); e != nil {
		t.Fatal(e)
	}
	apply(down)
	if constraint() != original {
		t.Fatal("final down constraint drift")
	}
	var remaining int
	if e = conn.QueryRow(ctx, "SELECT count(*) FROM dialog_message").Scan(&remaining); e != nil || remaining != 2 {
		t.Fatalf("legacy/v1 rows changed: %d %v", remaining, e)
	}
}
