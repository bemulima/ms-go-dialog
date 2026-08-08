#!/bin/sh
set -eu

db_service=${POSTGRES_SERVICE:-postgres}
db_user=${POSTGRES_USER:-postgres}
db_name=${POSTGRES_DB:-ms_dialog}

run_psql() {
    docker compose exec -T "$db_service" psql -v ON_ERROR_STOP=1 -U "$db_user" -d "$db_name" "$@"
}

run_psql <<'SQL'
CREATE TABLE IF NOT EXISTS dialog_schema_migration (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Bootstrap databases created before the migration ledger existed. These
-- checks describe schema evidence, not an assumed deployment version.
INSERT INTO dialog_schema_migration (version)
SELECT '001_init'
WHERE to_regclass('public.dialog') IS NOT NULL
  AND to_regclass('public.idx_dialog_user_block_target') IS NOT NULL
ON CONFLICT DO NOTHING;

INSERT INTO dialog_schema_migration (version)
SELECT '002_attachments'
WHERE to_regclass('public.dialog_attachment') IS NOT NULL
  AND to_regclass('public.idx_dialog_attachment_pending_expiry') IS NOT NULL
ON CONFLICT DO NOTHING;

INSERT INTO dialog_schema_migration (version)
SELECT '003_group_single_owner'
WHERE EXISTS (
    SELECT 1
    FROM pg_constraint c
    JOIN pg_class t ON t.oid = c.conrelid
    JOIN pg_namespace n ON n.oid = t.relnamespace
    WHERE n.nspname = 'public'
      AND t.relname = 'dialog'
      AND c.conname = 'chk_dialog_personal_shape'
      AND pg_get_constraintdef(c.oid) LIKE '%member_count >= 1%'
)
ON CONFLICT DO NOTHING;
SQL

for migration in db/migrations/*.up.sql; do
    filename=$(basename "$migration")
    version=${filename%.up.sql}
    applied=$(run_psql -Atc "SELECT COUNT(*) FROM dialog_schema_migration WHERE version='$version';")
    if [ "$applied" = "1" ]; then
        continue
    fi
    (
        printf 'BEGIN;\n'
        sed -n 'p' "$migration"
        printf "INSERT INTO dialog_schema_migration (version) VALUES ('%s');\n" "$version"
        printf 'COMMIT;\n'
    ) | run_psql
done
