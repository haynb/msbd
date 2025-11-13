# cloud-access-core

`cloud-access-core` is the identity, policy, and configuration control plane for 面试宝典 2.0. The service is a single Go binary that wires the IAM, Policy, Gateway, Config Center, and Telemetry modules using a lightweight bootstrap container.

## Layout

```
services/cloud-access-core/
├── cmd/cloud-access-core     # Binary entrypoint
├── pkg/bootstrap             # Config, logging, router, module wiring
├── db                        # (future) migrations
├── internal / pkg            # (future) IAM, policy, gateway, etc.
```

## Prerequisites

- Go 1.22+
- PostgreSQL + Redis (future tasks will consume them)

## Configuration

The service reads a JSON (YAML-compatible) config via `CLOUD_ACCESS_CONFIG`. If unset, sensible defaults apply. Copy the example and adjust as needed:

```bash
cp configs/config.example.yaml configs/config.yaml
export CLOUD_ACCESS_CONFIG=configs/config.yaml
```

Environment overrides:

- `CLOUD_ACCESS_ENV` – deployment environment label.
- `CLOUD_ACCESS_SERVER_ADDR` – HTTP listen address (default `:8080`).
- `CLOUD_ACCESS_LOG_LEVEL` – `debug|info|warn|error`.
- `CLOUD_ACCESS_LOG_PRETTY` – `true/false` console output toggle.
- `CLOUD_ACCESS_PROM_ADDR` – Prometheus exporter bind address.
- `CLOUD_ACCESS_POLICY_BUCKET_PREFIX`, `CLOUD_ACCESS_POLICY_UPDATE_CHANNEL`, `CLOUD_ACCESS_POLICY_QUOTA_WINDOW`, `CLOUD_ACCESS_POLICY_GRPC_ADDR` — tuning knobs for the policy/quotas module.
- `CLOUD_ACCESS_CONFIG_UPDATE_PREFIX`, `CLOUD_ACCESS_CONFIG_SIGNING_KEY` — Redis channel namespace + signing secret for the Config Center broadcasts.
- Telemetry tuning: `CLOUD_ACCESS_TELEM_STREAM` (Redis stream namespace), `CLOUD_ACCESS_TELEM_KAFKA_TOPIC` (stub topic id), `CLOUD_ACCESS_TELEM_BUFFER_PATH` (Badger directory), `CLOUD_ACCESS_TELEM_FLUSH_SECONDS` (buffer flush cadence), `CLOUD_ACCESS_TELEM_STREAM_MAXLEN` (Redis trimming threshold).

The Prometheus exporter is mounted at `/metrics`; start a Prometheus scrape or `curl http://localhost:8080/metrics` to inspect counters.

### IAM & Policy APIs

*IAM* (Task 3) exposes:

- `POST /auth/login`, `POST /auth/refresh`, `POST /auth/logout`
- Device onboarding: `POST /devices/pairing/request|approve|claim`, `POST /devices/heartbeat`, `POST /devices/revoke`
- gRPC service `iam.v1.AuthService` + `iam.v1.DeviceService` (see `proto/iam/*.proto`)

*Policy* (Task 5) adds RBAC + quota enforcement:

- `GET /policies?tenant_id={uuid}` and `PUT /policies` for rule CRUD
- `POST /policies/check` (RBAC decision) and `POST /policies/evaluate` (quota check)
- `POST /groups` to upsert tenant groups/quotas, `POST /tenants` to create tenants
- gRPC `policy.v1.PolicyService` (see `proto/policy/policy.proto`) exposing `CheckAccess` and `EvaluateQuota`
- Redis-backed quota buckets (`policy.bucket_prefix`) and Redis Pub/Sub broadcast for `policy.updated` events

### Config Center (Task 7)

- Admins manage desktop profiles via `PUT /configs/profiles` and inspect them with `GET /configs/profiles?tenant_id=...&profile=...` (JWT w/ `admin` role required).
- Devices fetch encrypted bundles through `GET /configs/profile?device_id=...&profile=...` using their PASETO tokens; responses include the AES-GCM payload plus the profile signature.
- Real-time updates stream over `GET /configs/stream` (WebSocket). Devices subscribe with their PASETO session token and receive `config.update` events whenever a profile version increments.
- Redis Pub/Sub (`config_center.update_channel_prefix`) fans out updates so all nodes push changes within <5s.

### Gateway & `gatewayctl`

Task 6 introduces the chi-based gateway (`pkg/gateway`) that fronts `/api/*` and `/ws/*`. Routes are defined in `configs/gateway.routes.yaml` and can be hot-reloaded via the bundled CLI:

```bash
# push updated routes
go run ./cmd/gatewayctl routes push --file configs/gateway.routes.yaml --token dev-admin-token

# rotate gateway client cert bundle (placeholder for future mTLS wiring)
go run ./cmd/gatewayctl certs rotate --cert tls.crt --key tls.key --token dev-admin-token
```

Gateway error codes follow the deterministic pattern documented in `.spec-workflow/specs/cloud-access-core/tasks.md` (40101 unauthorized, 40301 policy deny, 42901 throttled). Successful authentication injects `X-CloudAccess-*` headers into upstream requests for tenant/user context.

## Development

Common commands (also available via the repository Makefile):

```bash
# Run with automatic recompile
make run

# Execute unit tests
make test

# Execute cloud-backed integration tests (see "Testing & QA")
make test-integration

# Format & tidy modules
make tidy
```

## Docker Compose Stack

Task 9 introduces a reproducible stack (Go service + PostgreSQL + Redis) defined in `docker/docker-compose.cloud-access-core.yml`. Copy the env file and bring everything up:

```bash
cp configs/.env.example configs/.env
make compose-up
```

This exposes REST (`:8080`), Prometheus metrics (`:9090`), and gRPC (`:9091`). Tear down with `make compose-down` and inspect logs via `make compose-logs`. The compose file mounts `configs/` read-only, so editing `configs/config.example.yaml` or `configs/gateway.routes.yaml` on the host immediately reflects inside the container.

## Database & Migrations

- SQL migrations live in `db/migrations/` and can be executed via tools like Goose or Atlas. The Go helper `pkg/storage/migrations.ApplyAll` loads the same SQL for tests and local setup (only the `-- +goose Up` section is applied).
- GORM models/repositories reside in `pkg/storage/gormdb`. The package exposes helpers for common inserts, transactions, and ensures the seed tenant/admin exist (`EnsureSeedData`).
- Integration tests under `tests/integration/storage` spin up a temporary Postgres container (via Testcontainers) and run the migration + repository smoke tests. Run them with `make test-integration` once Docker is available.

The bootstrap exposes `/healthz`, `/readyz`, and `/ping` endpoints for initial smoke testing:

```bash
curl -s http://localhost:8080/healthz
```

Future tasks will extend this scaffold with IAM, Policy, Gateway, Config Center, Telemetry, operations tooling, and QA harnesses per `.spec-workflow/specs/cloud-access-core/tasks.md`.

Operational procedures (compose usage, secret management, key rotation, metrics) are documented in `docs/cloud-access-core-operations.md`.

## Testing & QA

Unit, integration, and load tests live under `services/cloud-access-core/tests` and `k6/`.

### Unit tests

```bash
make test
```

This runs `go test ./...` and exercises the in-memory doubles for IAM, policy, config center, gateway, telemetry, etc.

### Cloud-backed integration tests

Integration suites hit the shared PostgreSQL/Redis that the user provisioned:

```
PostgreSQL: 38.165.21.136:5432 (user `heanyang`, password `2580heanyang`)
Redis:      38.165.21.136:6379 (password `heanyang`)
```

Export the DSN/endpoint variables and then run the tagged suites:

```bash
export CLOUD_ACCESS_TEST_POSTGRES_DSN="postgres://heanyang:2580heanyang@38.165.21.136:5432/cloud_access_core?sslmode=disable"
export CLOUD_ACCESS_TEST_REDIS_ADDR="38.165.21.136:6379"
export CLOUD_ACCESS_TEST_REDIS_PASSWORD="heanyang"

make test-integration   # executes `go test -tags=integration ./tests/...`
```

The helper automatically creates the `cloud_access_core` database if it does not exist, applies the baseline migration, seeds the root tenant/user, and flushes Redis before each run.

`scripts/checks.sh` wraps the full suite; when the cloud env variables are present it runs both unit and integration tests, otherwise it skips the latter:

```bash
./scripts/checks.sh
```

### Load testing (k6)

Use the bundled K6 script to stress the IAM login flow. The script reads env overrides for the base URL and credentials, so it can target either localhost (default) or the deployed gateway.

```bash
K6_BASE_URL="http://localhost:8080" \
K6_EMAIL="admin@cloud-access.local" \
K6_PASSWORD="ChangeMe!2024" \
K6_VUS=25 \
K6_DURATION=2m \
k6 run k6/iam-login.js
```

You can also run it via Docker without installing K6:

```bash
docker run --rm -i \
  -e K6_BASE_URL -e K6_EMAIL -e K6_PASSWORD -e K6_VUS -e K6_DURATION \
  -v "$PWD/k6:/scripts" grafana/k6:latest run /scripts/iam-login.js
```

### CI

`.github/workflows/cloud-access-core-ci.yml` runs `scripts/checks.sh` on every push; the integration stage executes only when the `CLOUD_ACCESS_TEST_POSTGRES_DSN` and `CLOUD_ACCESS_TEST_REDIS_ADDR` secrets are configured in the repository.
