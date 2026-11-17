package postgres

import (
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTeacherPolicySelectionScanRetainsExactIntentAndRejectsNull(t *testing.T) {
	now := time.Now().UTC()
	sender, action := uuid.New(), uuid.New()
	a, b := uuid.NewString(), uuid.NewString()
	row := staticRow{uuid.New(), uuid.New(), domain.MessageAuthorUser, domain.MessageChannelWeb, &sender, (*uuid.UUID)(nil), &action, []byte(nil), []byte(nil), (*uuid.UUID)(nil), "question", []byte(`[]`), domain.MessageStatusActive, 1, int64(2), int64(3), (*int64)(nil), uuid.New(), (*time.Time)(nil), (*time.Time)(nil), now, now, (*uuid.UUID)(nil), int64(0), (*uuid.UUID)(nil), []byte(`{"kind":"OFFER","offering_id":"` + a + `","assignment_id":"` + b + `"}`)}
	got, e := scanMessage(row)
	if e != nil || got.TeacherPolicySelection == nil || got.TeacherPolicySelection.OfferingID != a || got.TeacherPolicySelection.AssignmentID != b {
		t.Fatalf("scanner lost selection %v", e)
	}
	row[len(row)-1] = []byte(`null`)
	if _, e = scanMessage(row); e == nil {
		t.Fatal("stored JSON null silently treated as no selection")
	}
	row[len(row)-1] = []byte(nil)
	got, e = scanMessage(row)
	if e != nil || got.TeacherPolicySelection != nil {
		t.Fatal("legacy SQL NULL changed")
	}
}
func TestTeacherPolicySelectionMigrationSourceGuardsHistoryAndWrapperTransaction(t *testing.T) {
	up, e := os.ReadFile("../../../../db/migrations/015_teacher_policy_selection.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	down, e := os.ReadFile("../../../../db/migrations/015_teacher_policy_selection.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, line := range strings.Split(string(up), "\n") {
		line = strings.TrimSpace(line)
		if line == "BEGIN;" || line == "COMMIT;" {
			t.Fatal("up must retain script-owned DDL+ledger transaction")
		}
	}
	for _, needle := range []string{"d.type=3", "d.student_id=NEW.sender_id", "lesson-message-context.v2", "NEW.teacher_policy_selection IS DISTINCT FROM OLD.teacher_policy_selection", "OLD.teacher_policy_selection IS NOT NULL", "mentor_binding_id IS NULL", "octet_length(teacher_policy_selection::text)<=512"} {
		if !strings.Contains(string(up), needle) {
			t.Fatalf("missing source invariant %s", needle)
		}
	}
	text := strings.TrimSpace(string(down))
	lock, guard, drop := strings.Index(text, "LOCK TABLE"), strings.Index(text, "EXISTS"), strings.Index(text, "DROP TRIGGER")
	if !strings.HasPrefix(text, "BEGIN;") || !strings.HasSuffix(text, "COMMIT;") || lock < 0 || guard < lock || drop < guard {
		t.Fatal("down not atomic retained-data refusal")
	}
}
