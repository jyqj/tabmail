#!/usr/bin/env bash
set -euo pipefail

[[ "${TABMAIL_ALLOW_DESTRUCTIVE_RESTORE:-}" == "yes" ]] || { echo "Use company_snapshot.py for an empty-target company restore. Legacy destructive restore requires TABMAIL_ALLOW_DESTRUCTIVE_RESTORE=yes." >&2; exit 1; }

: "${TABMAIL_DB_DSN:?TABMAIL_DB_DSN is required}"

dump_file="${1:-}"
if [[ -z "$dump_file" ]]; then
  echo "usage: TABMAIL_DB_DSN=... $0 <dump-file>" >&2
  exit 1
fi

if [[ ! -f "$dump_file" ]]; then
  echo "dump file not found: $dump_file" >&2
  exit 1
fi

pg_restore --clean --if-exists --no-owner --dbname="$TABMAIL_DB_DSN" "$dump_file"
echo "PostgreSQL restore completed from: $dump_file"
