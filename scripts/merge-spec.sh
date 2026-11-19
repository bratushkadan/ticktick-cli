#!/usr/bin/env bash
# Assemble api/openapi.yaml from the header + path fragments + schema fragment.
# Fragments were authored against docs/openapi.md (official docs snapshot).
set -euo pipefail
cd "$(dirname "$0")/.."
API=api
OUT="$API/openapi.yaml"

{
  cat <<'EOF'
openapi: 3.0.3
info:
  title: TickTick Open API
  description: |
    Machine-readable OpenAPI spec compiled from the official TickTick Open API
    documentation (https://developer.ticktick.com/docs#/openapi); snapshot in
    docs/openapi.md. Focus: business entities and successful responses; 4xx
    error codes are intentionally not described.
    All date-time fields use TickTick format yyyy-MM-dd'T'HH:mm:ssZ (e.g.
    2019-11-13T03:00:00+0000) and are typed as plain strings because the
    colon-less offset is not RFC3339.
  version: 1.0.0
servers:
  - url: https://api.ticktick.com
security:
  - BearerAuth: []
EOF
  cat "$API/frag-paths-task.yaml"
  tail -n +2 "$API/frag-paths-rest.yaml"
  echo "components:"
  cat <<'EOF'
  securitySchemes:
    BearerAuth:
      type: http
      scheme: bearer
      bearerFormat: API token
EOF
  tail -n +2 "$API/frag-schemas.yaml"
} > "$OUT"

echo "wrote $OUT ($(wc -l < "$OUT") lines)"