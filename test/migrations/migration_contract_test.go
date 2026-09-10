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
	lessonContextEvolution, err := os.ReadFile(filepath.Join(root, "005_lesson_message_context.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"lesson_context JSONB", "content_revision", "selected_text", "author_type = 'user'"} {
		if !strings.Contains(string(lessonContextEvolution), required) {
			t.Fatalf("lesson message context migration is missing %q", required)
		}
	}
	channelEvolution, err := os.ReadFile(filepath.Join(root, "006_message_channel.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"channel VARCHAR(32)", "'web'", "'telegram'"} {
		if !strings.Contains(string(channelEvolution), required) {
			t.Fatalf("message channel migration is missing %q", required)
		}
	}
	assistantUIEvolution, err := os.ReadFile(filepath.Join(root, "007_assistant_ui.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"assistant_ui JSONB", "assistant-ui.v1", "jsonb_array_length", "jsonb_path_exists", "personal_teacher", "<= 32"} {
		if !strings.Contains(string(assistantUIEvolution), required) {
			t.Fatalf("assistant UI migration is missing %q", required)
		}
	}
	assistantUIDown, err := os.ReadFile(filepath.Join(root, "007_assistant_ui.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(assistantUIDown), "cannot roll back assistant UI while structured messages exist") {
		t.Fatal("assistant UI rollback must fail closed while structured messages exist")
	}
	lessonContextV1, err := os.ReadFile(filepath.Join(root, "008_lesson_message_context_v1.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"lesson-message-context.v1", "lesson_overview", "selection", "course_id", "lesson_id",
		"selected_text", "char_length(lesson_context->>'selected_text') <= 12000",
	} {
		if !strings.Contains(string(lessonContextV1), required) {
			t.Fatalf("lesson context v1 migration is missing %q", required)
		}
	}
	lessonContextV1Down, err := os.ReadFile(filepath.Join(root, "008_lesson_message_context_v1.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lessonContextV1Down), "cannot roll back lesson message context v1 while versioned anchors exist") {
		t.Fatal("lesson context v1 rollback must fail closed while versioned anchors exist")
	}
	teacherContextContracts, err := os.ReadFile(filepath.Join(root, "009_teacher_context_contracts.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(teacherContextContracts), "dialog.teacher.context-mutated") {
		t.Fatal("teacher context contract migration must allowlist the mutation subject")
	}
	teacherContextContractsDown, err := os.ReadFile(filepath.Join(root, "009_teacher_context_contracts.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(teacherContextContractsDown), "cannot roll back teacher context contracts while mutation events exist") {
		t.Fatal("teacher context contract rollback must fail closed while mutation events exist")
	}
	teacherTurnOrdering, err := os.ReadFile(filepath.Join(root, "010_teacher_turn_ordering_v2.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"max_teacher_turn_sequence BIGINT NOT NULL DEFAULT 0", "teacher_turn_sequence BIGINT",
		"chk_dialog_max_teacher_turn_sequence", "chk_dialog_message_teacher_turn_sequence",
		"uq_dialog_message_teacher_turn_sequence", "WHERE teacher_turn_sequence IS NOT NULL",
	} {
		if !strings.Contains(string(teacherTurnOrdering), required) {
			t.Fatalf("teacher turn ordering migration is missing %q", required)
		}
	}
	teacherTurnOrderingDown, err := os.ReadFile(filepath.Join(root, "010_teacher_turn_ordering_v2.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"max_teacher_turn_sequence <> 0", "teacher_turn_sequence IS NOT NULL", "schema_version >= 2",
		"cannot roll back teacher turn ordering v2 while ordered turns or v2 requests exist",
	} {
		if !strings.Contains(string(teacherTurnOrderingDown), required) {
			t.Fatalf("teacher turn ordering rollback is not fail closed for %q", required)
		}
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
