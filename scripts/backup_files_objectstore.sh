#!/usr/bin/env bash
set -euo pipefail

: "${TABMAIL_DATADIR:?TABMAIL_DATADIR is required}"

mkdir -p backups
outfile="${1:-backups/objectstore-$(date -u +%Y%m%dT%H%M%SZ).tar.gz}"

tar -C "$(dirname "$TABMAIL_DATADIR")" -czf "$outfile" "$(basename "$TABMAIL_DATADIR")"
echo "Object store backup written to: $outfile"
