# realtime-interview-core

`realtime-interview-core` streams PCM audio from the desktop guardian, proxies it to
Aliyun NLS, orchestrates LLM prompts, and publishes transcripts/AI outputs to Redis
Streams + WebSocket subscribers. The service mirrors the bootstrap/observability stack
introduced in `cloud-access-core` and depends only on PostgreSQL + Redis for the MVP.

## Prerequisites
- Go 1.24+
- Access to PostgreSQL and Redis. For the shared cloud environment:
  - PostgreSQL: `postgres://heanyang:2580heanyang@38.165.21.136:5432/realtime_interview_core?sslmode=disable`
  - Redis: `38.165.21.136:6379` (password `heanyang`)
- (Optional) Aliyun NLS token + app key.
- (Optional) LLM API keys (OpenAI, DeepSeek, GLM) if you want the orchestrator to
  call real models instead of the echo fallback.

The service creates the `realtime_interview_core` database and runs migrations on
startup if the database is missing.

## Configuration
Copy `configs/config.example.yaml`, adjust credentials, and point the binary at it:

```bash
export REALTIME_INTERVIEW_CONFIG=./configs/config.example.yaml
```

Key sections:
- `database`: PostgreSQL DSN + pool settings.
- `redis`: Address/password for session cache + streams.
- `speech_gateway`: Chunk size, idle timeout, circuit breaker.
- `providers.aliyun`: WebSocket URL, token/app key, ASR toggles.
- `orchestrator`: Prompt templates + per-tenant budget window.
- `orchestrator.providers.*`: API keys/base URLs/models for OpenAI/DeepSeek/GLM. These
  map to the `REALTIME_INTERVIEW_ORCH_<PROVIDER>_*` env vars shown in
  `configs/.env.example`.
- `usage_export`: Batch size + Badger buffer dir.
- `streams`: Redis stream names for transcripts/usage/events.

Environment overrides are documented inside `pkg/bootstrap/config.go` (e.g.
`REALTIME_INTERVIEW_DB_DSN`). See `configs/.env.example` for ready-to-use values.

## Running locally

```bash
cd services/realtime-interview-core
REALTIME_INTERVIEW_CONFIG=./configs/config.example.yaml \
  go run ./cmd/realtime-interview-core
```

`make rtc-run` from the repo root performs the same action. Health endpoints are
available at `http://localhost:8084/healthz` and metrics at `/metrics`.

## Testing

```bash
# Unit tests
make rtc-test

# Integration tests (requires Docker)
RTC_RUN_INTEGRATION=1 make rtc-test-integration
```

`scripts/checks.sh` now runs both the cloud-access-core and realtime-interview-core test
suites; set `RTC_RUN_INTEGRATION=1` to include integration coverage.

## Demo client
Use `scripts/demo-stream.sh` plus the bundled PCM sample to open a gRPC stream and tail
WebSocket events:

```bash
export RTC_BEARER="<jwt-from-cloud-access-core>"
export RTC_TENANT_ID="<tenant-uuid>"
./scripts/demo-stream.sh
```

The script calls `tests/tools/demo_stream_client` under the hood, streams
`samples/demo-silence-16k.pcm`, and watches `/ws/realtime` for the session created by
the run.

For more operational detail see `docs/realtime-interview-core.md`.
