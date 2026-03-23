#!/usr/bin/env bash
set -euo pipefail

: "${TABMAIL_S3_BUCKET:?TABMAIL_S3_BUCKET is required}"
: "${TABMAIL_S3_ACCESS_KEY:?TABMAIL_S3_ACCESS_KEY is required}"
: "${TABMAIL_S3_SECRET_KEY:?TABMAIL_S3_SECRET_KEY is required}"

archive="${1:-}"
if [[ -z "$archive" ]]; then
  echo "usage: TABMAIL_S3_BUCKET=... TABMAIL_S3_ACCESS_KEY=... TABMAIL_S3_SECRET_KEY=... $0 <archive-file>" >&2
  exit 1
fi
if [[ ! -f "$archive" ]]; then
  echo "archive file not found: $archive" >&2
  exit 1
fi
command -v aws >/dev/null 2>&1 || { echo 'aws CLI is required' >&2; exit 1; }

export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-$TABMAIL_S3_ACCESS_KEY}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-$TABMAIL_S3_SECRET_KEY}"
export AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-${TABMAIL_S3_REGION:-us-east-1}}"

scheme="https"
if [[ "${TABMAIL_S3_USE_TLS:-true}" == "false" ]]; then
  scheme="http"
fi
endpoint="${TABMAIL_S3_ENDPOINT:-}"
endpoint_url=""
if [[ -n "$endpoint" ]]; then
  if [[ "$endpoint" == http://* || "$endpoint" == https://* ]]; then
    endpoint_url="$endpoint"
  else
    endpoint_url="$scheme://$endpoint"
  fi
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

tar -C "$tmpdir" -xzf "$archive"
args=(s3 sync "$tmpdir/data/" "s3://${TABMAIL_S3_BUCKET}/")
if [[ -n "$endpoint_url" ]]; then
  args+=(--endpoint-url "$endpoint_url")
fi
if [[ "${TABMAIL_S3_RESTORE_DELETE:-false}" == "true" ]]; then
  args+=(--delete)
fi
aws "${args[@]}"
echo "S3 object store restore completed from: $archive"
