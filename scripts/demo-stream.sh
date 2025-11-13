#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLIENT_PKG="./services/realtime-interview-core/tests/tools/demo_stream_client"
DEFAULT_AUDIO="${ROOT_DIR}/services/realtime-interview-core/samples/demo-silence-16k.pcm"

grpc_addr="${RTC_GRPC_ADDR:-localhost:9084}"
http_addr="${RTC_HTTP_ADDR:-http://localhost:8084}"
audio_path="${RTC_AUDIO:-$DEFAULT_AUDIO}"
provider="${RTC_PROVIDER:-aliyun}"
session_id="${RTC_SESSION_ID:-}"
if [[ -z "$session_id" ]]; then
  if command -v uuidgen >/dev/null 2>&1; then
    session_id="$(uuidgen)"
  else
    session_id="$(python - <<'PY'
import uuid
print(uuid.uuid4())
PY
)"
  fi
fi

tenant_id="${RTC_TENANT_ID:-}"
token="${RTC_BEARER:-}"
if [[ -z "$token" ]]; then
  echo "RTC_BEARER environment variable (JWT) is required" >&2
  exit 1
fi

if [[ ! -f "$audio_path" ]]; then
  echo "Audio file not found: $audio_path" >&2
  exit 1
fi

args=(
  "-grpc" "$grpc_addr"
  "-http" "$http_addr"
  "-session" "$session_id"
  "-audio" "$audio_path"
  "-provider" "$provider"
  "-token" "$token"
  "-ws"
)

if [[ -n "$tenant_id" ]]; then
  args+=("-tenant" "$tenant_id")
fi

if [[ -n "${RTC_SAMPLE_RATE:-}" ]]; then
  args+=("-sample-rate" "${RTC_SAMPLE_RATE}")
fi

if [[ -n "${RTC_FORMAT:-}" ]]; then
  args+=("-format" "${RTC_FORMAT}")
fi

if [[ -n "${RTC_METADATA:-}" ]]; then
  IFS=',' read -ra meta_pairs <<< "${RTC_METADATA}"
  for pair in "${meta_pairs[@]}"; do
    if [[ -n "$pair" ]]; then
      args+=("-meta" "$pair")
    fi
  done
fi

echo "Streaming ${audio_path} to ${grpc_addr} (session ${session_id})"
pushd "$ROOT_DIR" >/dev/null
GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" \
  go run "$CLIENT_PKG" "${args[@]}"
popd >/dev/null
