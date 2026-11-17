package postgres

import (
	"context"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type mentorQueryTx struct {
	pgx.Tx
	t     *testing.T
	query string
	args  []any
}

func TestMentorEducationReportRollbackGuardsHistoryWithinTransaction(t *testing.T) {
	// Source qualification only: no driver, database, or migration is executed.
	data, err := os.ReadFile("../../../../db/migrations/014_message_reports.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := regexp.MustCompile(`(?m)--[^\n]*`).ReplaceAllString(string(data), "")
	pattern := `(?s)^\s*BEGIN;\s*LOCK TABLE dialog_message_report IN ACCESS EXCLUSIVE MODE;\s*DO \$\$ BEGIN IF EXISTS \(SELECT 1 FROM dialog_message_report\) THEN\s*RAISE EXCEPTION '[^']+';\s*END IF; END \$\$;\s*DROP TABLE dialog_message_report;\s*COMMIT;\s*$`
	if !regexp.MustCompile(pattern).MatchString(source) {
		t.Fatal("standalone rollback must lock, guard, and drop in one explicit transaction")
	}
}

func TestMentorEducationUpMigrationsKeepLedgerTransactionOwnership(t *testing.T) {
	// Inspect source only. The real runner owns DDL + ledger in one transaction.
	data, err := os.ReadFile("../../../../scripts/migrate.sh")
	if err != nil {
		t.Fatal(err)
	}
	remaining := string(data)
	for _, fragment := range []string{"for migration in db/migrations/[0-9]*.up.sql; do", "printf 'BEGIN;", "sed -n 'p' \"$migration\"", "INSERT INTO dialog_schema_migration (version) VALUES", "printf 'COMMIT;"} {
		at := strings.Index(remaining, fragment)
		if at < 0 {
			t.Fatal("migration runner no longer keeps DDL and ledger in the expected transaction")
		}
		remaining = remaining[at+len(fragment):]
	}
	for _, filename := range []string{"013_mentor_education.up.sql", "014_message_reports.up.sql"} {
		data, err := os.ReadFile("../../../../db/migrations/" + filename)
		if err != nil {
			t.Fatal(err)
		}
		source := regexp.MustCompile(`(?m)--[^\n]*`).ReplaceAllString(string(data), "")
		if regexp.MustCompile(`(?im)^\s*(BEGIN|START\s+TRANSACTION|COMMIT|ROLLBACK|END)\b`).MatchString(source) {
			t.Errorf("%s would split the runner-owned schema/ledger transaction", filename)
		}
	}
}

type mentorEmptyRows struct{ pgx.Rows }

func (mentorEmptyRows) Next() bool { return false }
func (mentorEmptyRows) Close()     {}
func (mentorEmptyRows) Err() error { return nil }
func (t *mentorQueryTx) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	t.query, t.args = q, args
	for _, p := range regexp.MustCompile(`\$([0-9]+)`).FindAllStringSubmatch(q, -1) {
		n, _ := strconv.Atoi(p[1])
		if n > len(args) {
			t.t.Errorf("unbound SQL parameter %s with %d args", p[0], len(args))
		}
	}
	return mentorEmptyRows{}, nil
}
func TestMentorEducationQueriesFenceEveryBranch(t *testing.T) {
	b := domain.MentorBinding{ID: uuid.New(), DialogID: uuid.New()}
	proof := &domain.MentorAccess{Binding: b, HistoryFromRevision: 7}
	for _, selection := range []bool{false, true} {
		t.Run(strconv.FormatBool(selection), func(t *testing.T) {
			tx := &mentorQueryTx{t: t}
			ctx := context.WithValue(context.Background(), transactionKey{}, pgx.Tx(tx))
			if selection {
				ctx = domain.WithMentorAccess(domain.WithMentorBinding(ctx, b.ID), proof)
			}
			r := MessageRepository{}
			for _, q := range []repository.MessageListQuery{{DialogID: b.DialogID, Limit: 20}, {DialogID: b.DialogID, Before: &repository.MessageCursor{Sequence: 20, ID: b.ID}, Limit: 20}, {DialogID: b.DialogID, After: &repository.MessageCursor{Sequence: 20, ID: b.ID}, Limit: 20}} {
				if _, err := r.List(ctx, q); err != nil {
					t.Fatal(err)
				}
				if strings.Count(tx.query, "mentor_binding_id IS NOT DISTINCT FROM") != 1 {
					t.Error("List must apply exactly one context predicate")
				}
			}
			if _, err := r.Window(ctx, repository.MessageWindowQuery{DialogID: b.DialogID, Before: 10, After: 10}); err != nil {
				t.Fatal(err)
			}
			branches := strings.Split(tx.query, "UNION ALL")
			if len(branches) != 2 {
				t.Fatal("expected two window branches")
			}
			for _, q := range branches {
				if !strings.Contains(q, "mentor_binding_id IS NOT DISTINCT FROM") || !strings.Contains(q, "mentor_context_revision >=") {
					t.Error("window branch lacks exact binding/history predicate")
				}
			}
		})
	}
}
