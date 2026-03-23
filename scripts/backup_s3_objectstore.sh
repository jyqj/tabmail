#!/usr/bin/env bash
set -euo pipefail

: "${TABMAIL_S3_BUCKET:?TABMAIL_S3_BUCKET is required}"
: "${TABMAIL_S3_ACCESS_KEY:?TABMAIL_S3_ACCESS_KEY is required}"
: "${TABMAIL_S3_SECRET_KEY:?TABMAIL_S3_SECRET_KEY is required}"

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

mkdir -p backups
outfile="${1:-backups/objectstore-s3-$(date -u +%Y%m%dT%H%M%SZ).tar.gz}"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

args=(s3 sync "s3://${TABMAIL_S3_BUCKET}/" "$tmpdir/data")
if [[ -n "$endpoint_url" ]]; then
  args+=(--endpoint-url "$endpoint_url")
fi
aws "${args[@]}"

tar -C "$tmpdir" -czf "$outfile" data
echo "S3 object store backup written to: $outfile"
