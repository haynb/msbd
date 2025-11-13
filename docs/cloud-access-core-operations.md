# cloud-access-core Operations Runbook

This guide explains how to stand up, observe, and maintain the `cloud-access-core` service in local/dev environments. The stack ships as a single Go binary with PostgreSQL + Redis dependencies and covers IAM, policy, gateway, config center, and telemetry modules.

## 1. Prerequisites

- Docker Engine 24+ with the `docker compose` plugin.
- Make (GNU) for helper targets.
- Access to the repository root of `msbd`.
- Optional: Go 1.22+ if you want to run the binary outside of containers.

## 2. Quickstart

1. Copy the sample environment file and adjust secrets/ports as needed:
   ```bash
   cp configs/.env.example configs/.env
   ```
2. Ensure `configs/config.example.yaml` points to suitable DSNs (Compose overrides them via env vars by default).
3. Start the full stack (PostgreSQL, Redis, and the Go service) using the Makefile helper:
   ```bash
   make compose-up
   ```
   This command wraps `docker compose -f docker/docker-compose.cloud-access-core.yml --env-file configs/.env up -d --build`.
4. Verify readiness:
   ```bash
   curl -f http://localhost:8080/healthz
   curl -f http://localhost:8080/readyz
   curl -f http://localhost:9090/metrics  # Prometheus exporter
   ```
5. Tail service logs when troubleshooting:
   ```bash
   make compose-logs
   ```
6. Stop the stack:
   ```bash
   make compose-down
   ```

## 3. Environment Variables

`configs/.env.example` captures the required knobs. Key entries (all prefixed with `CLOUD_ACCESS_` when they override the JSON config):

| Variable | Purpose |
| --- | --- |
| `CLOUD_ACCESS_DB_DSN` | PostgreSQL DSN (compose defaults to `postgres://cloudaccess:cloudaccess@postgres:5432/cloud_access?sslmode=disable`). |
| `CLOUD_ACCESS_REDIS_ADDR` | Redis hostname inside the compose network (`redis:6379`). |
| `CLOUD_ACCESS_AUTH_SIGNING_*` | Optional Ed25519 key material for deterministic JWT/PASETO signing. Leave blank to auto-generate per boot (dev only). |
| `CLOUD_ACCESS_DEVICE_PROFILE_KEY` | 32-byte base64 AES-GCM key for encrypting device bundles. |
| `CLOUD_ACCESS_CONFIG_SIGNING_KEY` | 32-byte base64 key for Config Center response signatures. |
| `CLOUD_ACCESS_GATEWAY_ADMIN_TOKEN` | Shared bearer token for `gatewayctl` admin operations. |
| `CLOUD_ACCESS_TELEM_BUFFER_PATH` | Filesystem location for the Badger audit buffer (defaults to `/app/data/telemetry-buffer`). |

Generate cryptographic material with any secure random source. Example:
```bash
python - <<'PY'
import os, base64
print(base64.b64encode(os.urandom(32)).decode())
PY
```
For JWT/PASETO keys, use `openssl ed25519` or `age-keygen`, then base64-encode the raw private/public bytes.

### Shared cloud QA environment

For Task 10 the integration suite pins to the cloud PostgreSQL/Redis the user provided:

```
PostgreSQL DSN: postgres://heanyang:2580heanyang@38.165.21.136:5432/cloud_access_core?sslmode=disable
Redis Addr:     38.165.21.136:6379 (password `heanyang`)
```

Set the following before running `make test-integration` or `./scripts/checks.sh`:

```bash
export CLOUD_ACCESS_TEST_POSTGRES_DSN="postgres://heanyang:2580heanyang@38.165.21.136:5432/cloud_access_core?sslmode=disable"
export CLOUD_ACCESS_TEST_REDIS_ADDR="38.165.21.136:6379"
export CLOUD_ACCESS_TEST_REDIS_PASSWORD="heanyang"
```

The helper will create the `cloud_access_core` DB if missing, apply migrations, seed the admin user, and flush Redis between test runs. Keep the credentials scoped to CI/secrets; do not commit them into other configs.

## 4. Docker Compose Layout

File: `docker/docker-compose.cloud-access-core.yml`

- `postgres`: official PostgreSQL 16 image with health checks and persisted volume `postgres_data`.
- `redis`: Redis 7 with append-only persistence on `redis_data` volume.
- `cloud-access-core`: builds `services/cloud-access-core/Dockerfile`, mounts `configs/` read-only, and exposes `8080` (REST/gateway), `9090` (Prometheus), `9091` (gRPC IAM/Policy). Telemetry buffer lives on the `telemetry_buffer` named volume.

The compose file automatically waits for PostgreSQL readiness before starting the Go binary.

## 5. Gateway & Admin CLI

Routes live at `configs/gateway.routes.yaml`. Update the file, then push changes via:
```bash
cd services/cloud-access-core
go run ./cmd/gatewayctl routes push \
  --file ../../configs/gateway.routes.yaml \
  --token "$CLOUD_ACCESS_GATEWAY_ADMIN_TOKEN"
```
Certificates are rotated similarly:
```bash
go run ./cmd/gatewayctl certs rotate \
  --cert /path/to/fullchain.pem \
  --key /path/to/privkey.pem \
  --token "$CLOUD_ACCESS_GATEWAY_ADMIN_TOKEN"
```
The gateway returns deterministic error codes: `40101` (unauthorized), `40301` (policy deny), `40302` (pairing token mismatch), `40405` (device not found), `42901` (rate limited), `50000` (internal error). Monitor `/metrics` (`gateway_requests_total`) for spikes.

## 6. Database & Migrations

SQL migrations live in `services/cloud-access-core/db/migrations/`. Apply them before production deploys using your preferred tool (Goose/Atlas). In dev, the service runs `gormdb.EnsureSeedData` automatically, creating a root tenant and admin user (`admin@cloud-access.local`).

## 7. Observability & Audit

- Prometheus scrape target: `http://cloud-access-core:9090/metrics` (or localhost when port-forwarded).
- Audit events flow to PostgreSQL (`audit_logs` table) and Redis Streams (`cloud-access-core:audit` by default). When Redis/Kafka are unavailable, events buffer on disk (`telemetry_buffer` volume). The `/healthz` endpoint reports degraded state when backlog grows beyond 5k entries.
- Device heartbeats publish to Redis Stream `cloud-access-core:device-heartbeats`. Consume via `internal/consumers/heartbeat_consumer`. Stream offsets stored per consumer group `iam-heartbeat`.

## 8. Backup & Recovery

1. **PostgreSQL**: snapshot the `postgres_data` volume or schedule logical dumps.
2. **Redis**: append-only logs live on `redis_data`. Ensure host-level backups or configure upstream Redis with persistence.
3. **Telemetry Buffer**: stop the service (`make compose-down`), archive the `telemetry_buffer` volume, then restart. The Badger worker replays buffered events on boot.
4. **Key Rotation**: update `.env` with new signing/config keys, restart the stack (`make compose-restart`). For JWT rotation, publish the new public key to dependent services (gateway caches automatically on restart).

## 9. Troubleshooting

| Symptom | Checks |
| --- | --- |
| `cloud-access-core` container exits immediately | Inspect logs (`make compose-logs`). Common causes: missing `CLOUD_ACCESS_DB_DSN`, Postgres unreachable, invalid base64 keys. |
| HTTP 401 despite valid token | Verify gateway admin token and that JWT audience matches `auth.web_audience`. Tokens issued before key rotation are invalidated. |
| Config updates not propagating | Confirm Redis Pub/Sub channel `cloud-access-core:config-updates`. Use `redis-cli MONITOR` or the WebSocket stream (`/configs/stream`). |
| Device revocation slow | Check Redis latency and ensure `device.revocation_channel` matches env overrides. |

For deeper issues, rerun the binary locally (`make run`) with `CLOUD_ACCESS_LOG_LEVEL=debug` and reproduce outside of containers.
