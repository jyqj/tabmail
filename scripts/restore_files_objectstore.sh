#!/usr/bin/env bash
set -euo pipefail

[[ "${TABMAIL_ALLOW_DESTRUCTIVE_RESTORE:-}" == "yes" ]] || { echo "Use company_snapshot.py for an empty-target company restore. Legacy destructive restore requires TABMAIL_ALLOW_DESTRUCTIVE_RESTORE=yes." >&2; exit 1; }

: "${TABMAIL_DATADIR:?TABMAIL_DATADIR is required}"

archive="${1:-}"
if [[ -z "$archive" ]]; then
  echo "usage: TABMAIL_DATADIR=... $0 <archive-file>" >&2
  exit 1
fi

if [[ ! -f "$archive" ]]; then
  echo "archive file not found: $archive" >&2
  exit 1
fi

mkdir -p "$(dirname "$TABMAIL_DATADIR")"
rm -rf "$TABMAIL_DATADIR"
tar -C "$(dirname "$TABMAIL_DATADIR")" -xzf "$archive"
echo "Object store restore completed from: $archive"
