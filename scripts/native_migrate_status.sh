#!/bin/sh
set -eu

for name in PGHOST PGPORT PGUSER PGDATABASE PGSSLMODE; do
    case "$name" in
        PGHOST) value=${PGHOST:-} ;;
        PGPORT) value=${PGPORT:-} ;;
        PGUSER) value=${PGUSER:-} ;;
        PGDATABASE) value=${PGDATABASE:-} ;;
        PGSSLMODE) value=${PGSSLMODE:-} ;;
    esac
    if [ -z "$value" ]; then
        printf 'native migration status: %s is required\n' "$name" >&2
        exit 2
    fi
done

if ! command -v psql >/dev/null 2>&1; then
    printf 'native migration status: psql is required\n' >&2
    exit 127
fi

{
    printf 'BEGIN TRANSACTION READ ONLY;\n'
    printf "SELECT to_regclass('public.dialog_schema_migration') IS NOT NULL AS has_ledger \\gset\n"
    for migration in db/migrations/[0-9]*.up.sql; do
        filename=${migration##*/}
        version=${filename%.up.sql}
        printf '\\if :has_ledger\n'
        printf "SELECT EXISTS (SELECT 1 FROM dialog_schema_migration WHERE version = '%s') AS migration_applied \\gset\n" "$version"
        printf '\\if :migration_applied\n\\echo %s applied\n\\else\n\\echo %s pending\n\\endif\n' "$version" "$version"
        printf '\\else\n\\echo %s status-unknown-ledger-absent\n\\endif\n' "$version"
    done
    printf 'ROLLBACK;\n'
} | psql -X -v ON_ERROR_STOP=1 --quiet --no-align --tuples-only
