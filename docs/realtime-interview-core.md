# realtime-interview-core Operations Guide

This document explains how to configure, run, and verify the `realtime-interview-core`
service that powers 面试宝典实时语音/AI 编排。It complements the approved
specs in `.spec-workflow/specs/realtime-interview-core/` and focuses on the current
Postgres + Redis MVP scope (Aliyun NLS as the default provider).

## Runtime topology
- Single Go binary at `cmd/realtime-interview-core/main.go`.
- Dependencies: PostgreSQL + Redis (no Kafka/ClickHouse in the MVP).
- Redis Streams deliver transcripts, orchestrator outputs, control events, and usage
  exports. WebSocket hub (`/ws/realtime`) fans those streams to Web UI subscribers.
- Auth relies on `cloud-access-core` JWT/PASETO via the shared `pkg/authn.Guard`.

## Configuration reference
All knobs live in `services/realtime-interview-core/configs/config.example.yaml`. Point
`REALTIME_INTERVIEW_CONFIG` to your customized copy or set env overrides listed in
`pkg/bootstrap/config.go`. Highlights:

| Block | Key settings |
| --- | --- |
| `database` | Set `dsn` to your Postgres instance. For the shared cloud host use `postgres://heanyang:2580heanyang@38.165.21.136:5432/realtime_interview_core?sslmode=disable`. The service migrates the DB automatically. |
| `redis` | `addr=38.165.21.136:6379`, `password=heanyang` for the shared Redis. |
| `speech_gateway` | `max_chunk_bytes`, `session_idle_timeout_seconds`, `buffer_prefix`, `circuit_breaker_*` guard Aliyun calls. |
| `speech_gateway.runtime_*` | `runtime_config_key` + `runtime_update_channel` define the Redis key + pub/sub channel used for hot provider overrides. Leave defaults unless you want isolated environments. |
| `providers.aliyun` | `api_url`, `domain`, `app_key`, `token` or `token_url`. `format` defaults to `pcm`, sample rate to 16000 Hz. |
| `orchestrator` | Prompt templates + `budget_window_seconds`, `max_tokens_per_window`, and the `providers.*` block that stores OpenAI/DeepSeek/GLM credentials. |
| `usage_export` | Controls Prometheus gauge updates and Badger disk buffer (`buffer_path`). |
| `streams` | Redis stream names consumed by WS hub + downstream services. |

Refer to `configs/.env.example` for ready-to-use environment variable overrides (cloud
credentials + Prometheus bindings).

### LLM provider credentials
- OpenAI-compatible endpoints: set `REALTIME_INTERVIEW_ORCH_OPENAI_API_KEY`, optional
  `..._BASE_URL` (defaults to `https://api.openai.com/v1/chat/completions`, the sample
  YAML uses the user-provided proxy `https://api.gptgod.online/v1/chat/completions`),
  `..._MODEL`, and `..._TIMEOUT`. Additional headers like organization/project can be
  injected via the JSON config's `providers.openai.extra` map.
- DeepSeek: set `REALTIME_INTERVIEW_ORCH_DEEPSEEK_API_KEY` plus optional base/model.
  The default target is `https://api.deepseek.com/chat/completions` with
  `deepseek-reasoner`.
- GLM (Zhipu): set `REALTIME_INTERVIEW_ORCH_GLM_API_KEY` and endpoint (defaults to the
  Anthropic-compatible entrypoint `https://open.bigmodel.cn/api/anthropic`). Models can
  be swapped (e.g., `GLM-4.6`).

Leave these secrets out of Git—export them in your shell or Docker secrets before
running `go run`/`docker compose`. The orchestrator automatically loads the matching
provider block based on `orchestrator.provider`.

## Running the service

```bash
cd services/realtime-interview-core
export REALTIME_INTERVIEW_CONFIG=./configs/config.example.yaml
REALTIME_INTERVIEW_DB_DSN=postgres://heanyang:2580heanyang@38.165.21.136:5432/realtime_interview_core?sslmode=disable \
REALTIME_INTERVIEW_REDIS_ADDR=38.165.21.136:6379 \
REALTIME_INTERVIEW_REDIS_PASSWORD=heanyang \
GOPROXY=https://proxy.golang.org,direct \
go run ./cmd/realtime-interview-core
```

Health: `GET http://localhost:8084/healthz`
Ready: `GET http://localhost:8084/readyz`
Metrics: `GET http://localhost:8084/metrics`

## Aliyun NLS setup
1. Retrieve the AppKey + AccessKey in the Aliyun console.
2. Generate short-lived tokens via the Aliyun REST endpoint listed in
   `providers.aliyun.token_url` or manually paste a token into
   `REALTIME_INTERVIEW_PROVIDER_ALIYUN_TOKEN`.
3. Confirm the token scope matches the `api_url` (default Shanghai region).
4. Optional toggles (`enable_itn`, `enable_punctuation`, etc.) mirror the official Go
   SDK fields documented via Context7.

## Demo workflow
Use the bundled script + sample PCM to exercise both gRPC streaming and the WebSocket
hub:

```bash
export RTC_BEARER="<JWT from cloud-access-core login>"
export RTC_TENANT_ID="<tenant uuid>"
./scripts/demo-stream.sh
```

The script wraps `go run ./services/realtime-interview-core/tests/tools/demo_stream_client`,
streams `samples/demo-silence-16k.pcm`, and tails `/ws/realtime` for transcript +
orchestrator events associated with the generated `session_id`. Override addresses via
`RTC_GRPC_ADDR` and `RTC_HTTP_ADDR` env vars.

## Testing & QA
- `make rtc-test` → unit tests (speech gateway, orchestrator, usage, handlers).
- `RTC_RUN_INTEGRATION=1 make rtc-test-integration` → runs Postgres-backed lifecycle +
  usage buffering tests (requires Docker/Testcontainers).
- `scripts/checks.sh` runs both specs' unit tests; set `RTC_RUN_INTEGRATION=1` and
  `CLOUD_ACCESS_TEST_*` env vars to include their integration suites.

For manual QA against the shared cloud services, export the `REALTIME_TEST_*` variables
from `configs/.env.example` before invoking targeted Go tests or demo scripts. The
service auto-migrates the Postgres schema, so no manual SQL is required.

## Observability
- Prometheus endpoint: `/metrics` (speech latency histograms, provider error counters,
  WebSocket backpressure gauge, usage buffer depth).
- Redis Streams of interest: `realtime-interview-core:session.events`,
  `realtime-interview-core:orchestrator.outputs`, `realtime-interview-core:usage`.
- Logs use structured `slog` with `component` fields (speech gateway, orchestrator,
  sessions_http, ws.hub, usage_exporter).

## API references
- REST: `api/openapi/realtime-interview-core.yaml` (sessions + provider status).
- Provider runtime overrides: `GET/PUT /api/realtime/providers/runtime` (admin/ops role required) read/write the Redis-backed override that feeds the speech gateway without restarting the binary.
- gRPC: `proto/realtime/v1/{streaming.proto,control.proto}`.
- WebSocket: `GET ws(s)://<host>/ws/realtime?session_id=<uuid>&tenant_id=<uuid>` with the
  same JWT used for REST.
