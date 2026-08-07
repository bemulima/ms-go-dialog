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
}
