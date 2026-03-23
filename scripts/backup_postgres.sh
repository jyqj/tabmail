#!/usr/bin/env bash
set -euo pipefail

: "${TABMAIL_DB_DSN:?TABMAIL_DB_DSN is required}"

mkdir -p backups
outfile="${1:-backups/postgres-$(date -u +%Y%m%dT%H%M%SZ).dump}"

pg_dump --format=custom --file="$outfile" "$TABMAIL_DB_DSN"
echo "PostgreSQL backup written to: $outfile"
