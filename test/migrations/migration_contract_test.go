package migrations_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrations_AreReversibleAndKeepReadStatePerMember(t *testing.T) {
	root := filepath.Join("..", "..", "db", "migrations")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	up, down := map[string]bool{}, map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			up[strings.TrimSuffix(name, ".up.sql")] = true
		case strings.HasSuffix(name, ".down.sql"):
			down[strings.TrimSuffix(name, ".down.sql")] = true
		}
	}
	for version := range up {
		if !down[version] {
			t.Fatalf("migration %s has no down pair", version)
		}
	}

	schema, err := os.ReadFile(filepath.Join(root, "001_init.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(schema)
	for _, required := range []string{
		"CREATE TABLE dialog_member", "last_read_message_sequence", "unread_count",
		"PRIMARY KEY (dialog_id, user_id)", "max_message_sequence", "max_event_sequence",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("schema is missing per-member read contract %q", required)
		}
	}
	if strings.Contains(text, "REFERENCES user") || strings.Contains(text, "REFERENCES users") {
		t.Fatal("dialog schema must not create cross-service user foreign keys")
	}

	groupEvolution, err := os.ReadFile(filepath.Join(root, "003_group_single_owner.up.sql"))
	if err != nil {
		t.Fatal(err)
	}

	teacherEvolution, err := os.ReadFile(filepath.Join(root, "004_teacher_dialogs.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	teacherText := string(teacherEvolution)
	for _, required := range []string{
		"type IN (1, 2, 3)", "personal_teacher_id", "teacher_context_type",
		"learning_action_id", "author_type", "dialog.teacher.requested",
		"uq_dialog_outbox_sequence_subject",
	} {
		if !strings.Contains(teacherText, required) {
			t.Fatalf("teacher dialog migration is missing %q", required)
		}
	}
	if !strings.Contains(string(groupEvolution), "member_count BETWEEN 1 AND 1000") || !strings.Contains(string(groupEvolution), "DROP CONSTRAINT chk_dialog_personal_shape") {
		t.Fatal("group singleton evolution must be explicit and upgrade existing databases")
	}

	runner, err := os.ReadFile(filepath.Join("..", "..", "scripts", "migrate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	runnerText := string(runner)
	for _, required := range []string{"dialog_schema_migration", "BEGIN;", "COMMIT;", "ON_ERROR_STOP=1"} {
		if !strings.Contains(runnerText, required) {
			t.Fatalf("migration runner is missing atomic ledger contract %q", required)
		}
	}
}
