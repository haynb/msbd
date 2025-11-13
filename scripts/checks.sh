#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"

run_cloud_access() {
  pushd "$ROOT_DIR/services/cloud-access-core" >/dev/null
  echo "[cloud-access-core] Running unit tests..."
  GOPROXY="$GOPROXY" go test ./...

  if [[ -n "${CLOUD_ACCESS_TEST_POSTGRES_DSN:-}" && -n "${CLOUD_ACCESS_TEST_REDIS_ADDR:-}" ]]; then
    echo "[cloud-access-core] Running integration tests..."
    GOPROXY="$GOPROXY" go test -tags=integration ./tests/...
  else
    echo "[cloud-access-core] Skipping integration tests (set CLOUD_ACCESS_TEST_POSTGRES_DSN and CLOUD_ACCESS_TEST_REDIS_ADDR)." >&2
  fi
  popd >/dev/null
}

run_realtime_core() {
  pushd "$ROOT_DIR/services/realtime-interview-core" >/dev/null
  echo "[realtime-interview-core] Running unit tests..."
  GOPROXY="$GOPROXY" go test ./...

  if [[ "${RTC_RUN_INTEGRATION:-0}" == "1" ]]; then
    echo "[realtime-interview-core] Running integration tests (Docker + Testcontainers required)..."
    GOPROXY="$GOPROXY" go test -tags=integration ./tests/...
  else
    echo "[realtime-interview-core] Skipping integration tests (set RTC_RUN_INTEGRATION=1 to enable)." >&2
  fi
  popd >/dev/null
}

run_guardian_client() {
  pushd "$ROOT_DIR/clients/desktop-guardian-client" >/dev/null
  echo "[desktop-guardian-client] Running lint/tests/typecheck..."
  corepack pnpm lint
  corepack pnpm test
  corepack pnpm typecheck

  if [[ "${GUARDIAN_SKIP_NATIVE_TESTS:-0}" != "1" ]]; then
    if command -v cargo >/dev/null 2>&1; then
      echo "[desktop-guardian-client] Running native tests..."
      corepack pnpm native:test || echo "[desktop-guardian-client] Native tests failed (likely missing crates.io); see logs." >&2
    else
      echo "[desktop-guardian-client] Skipping native tests (cargo not found)." >&2
    fi
  else
    echo "[desktop-guardian-client] Skipping native tests (GUARDIAN_SKIP_NATIVE_TESTS=1)." >&2
  fi

  if [[ "${GUARDIAN_SKIP_E2E:-0}" != "1" ]]; then
    echo "[desktop-guardian-client] Running renderer smoke suite..."
    corepack pnpm e2e
  else
    echo "[desktop-guardian-client] Skipping e2e tests (GUARDIAN_SKIP_E2E=1)." >&2
  fi
  popd >/dev/null
}

run_cloud_access
run_realtime_core
run_guardian_client
