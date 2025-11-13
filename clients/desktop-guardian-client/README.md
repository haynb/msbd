# desktop-guardian-client

Bootstrapped workspace for the managed desktop agent defined in `desktop-guardian-client` spec. The repo uses **pnpm** workspaces plus an Electron main process, a Vite-powered React renderer, and shared packages for config/schema types.

## Prerequisites
- Node.js >= 20
- pnpm >= 9
- Rust toolchain (native crates live under `native/`)

## Install
```bash
cd clients/desktop-guardian-client
pnpm install
```

Copy `configs/client.example.json` to `configs/client.json` (or provide a custom path via `GUARDIAN_CLIENT_CONFIG`) and update the Cloud Access / realtime URLs before running the app. The Electron shell reads this file to launch the embedded login window and subscribe to config updates.

Guardian policies live in `configs/policies.example.json`. Copy it next to your client config (or point `client.json.guardian.policiesPath` at a custom file) to tune recorder watchlists, mitigation toggles, screenshot cadence, and encryption keys.

## Available scripts
| Command | Description |
| --- | --- |
| `pnpm dev` | Runs the renderer (Vite) and launches the Electron shell pointing at the dev server. |
| `pnpm build` | Builds all workspace packages. |
| `pnpm lint` | Runs ESLint for all sub-packages. |
| `pnpm test` | Runs package-level unit tests (renderer, shared packages, native fallback). |
| `pnpm typecheck` | Type-checks every package. |
| `pnpm native:build` | Builds the Rust guardian host (`native/guardian-host`). |
| `pnpm native:test` | Runs Rust unit tests for audio, guardian detectors, and screenshot crates. |
| `pnpm e2e` | Runs the renderer smoke suite (Vitest + Testing Library) with the guardian stub. |
| `pnpm check` | Convenience alias for lint + test + typecheck. |

## Project layout
```
clients/desktop-guardian-client/
├─ electron/           # Electron main process + preload scripts
├─ renderer/           # React UI rendered via Vite
├─ packages/
│  ├─ guardian-config/ # Shared config schema/types
│  ├─ network-client/  # Typed HTTP/WebSocket + realtime gRPC helpers
│  └─ ipc/             # IPC contract + native host loader (mock fallback)
├─ native/
│  ├─ audio/           # CPAL-based capture engine
│  ├─ guardian-host/   # Tokio/N-API host that bridges audio -> Electron
│  └─ net/             # Tonic gRPC streaming client used by guardian-host
├─ configs/            # Client bootstrap config templates
├─ scripts/            # Future helper scripts (placeholder)
└─ pnpm-workspace.yaml
```

### Native host quick start

The Rust workspace under `native/` produces the `guardian-host` Node module that Electron loads through `@guardian/ipc`. During development you can rely on the mock bridge (it emits synthetic PCM data), but to exercise real capture:

```bash
cd clients/desktop-guardian-client
pnpm native:build # or cargo build -p guardian-host
```

The compiled `.node` artifact is written to `native/guardian-host/`. Electron automatically loads it when present and streams PCM buffers to the renderer. The IPC package falls back to the mock bridge whenever the native binary is missing, so unit tests and CI remain hermetic.

### Realtime session control & streaming

Task 4 introduces the gRPC plumbing required by Requirement 2:

- `packages/network-client/src/realtime.ts` loads the shared proto definitions and exposes a `RealtimeControlClient` that the Electron main process uses for `CreateSession` / `TransitionSession` RPCs.
- `native/net` hosts a tonic-based `StreamAudio` uploader that the guardian host spins up whenever a session becomes active. It buffers PCM chunks, enforces the configured chunk rate, and retries transient failures before surfacing errors back to JS.
- The renderer now includes `modules/session/sessionStore.ts`, a Zustand store/bridge that reacts to session status updates pushed from Electron so the tray menu and UI controls remain in sync.

Update `configs/client.json` with the gRPC address + provider for your environment:

```jsonc
{
  "realtime": {
    "controlAddress": "dns:///127.0.0.1:9084",
    "provider": "aliyun",
    "useTls": false,
    "chunkMillis": 20
  }
}
```

The Electron shell caches the most recent config profile (sample rate, policies) and passes those values to the guardian host whenever a session starts or resumes. The renderer’s “Session Control” panel simply calls `window.guardian.startSessionControl/pauseSessionControl/stopSessionControl`, keeping tray actions and realtime streaming state aligned.

### Guardian runtime, mitigations, and screenshots

Task 5 ports the anti-recording flows from the legacy Electron/Tkinter projects into this workspace:

- `electron/policy-loader.ts` parses the JSON policy file (process/VM watchlists, mitigation flags, screenshot cadence). The default lives in `configs/policies.example.json` and can be overridden per device.
- `native/guardian` scans running processes with `sysinfo` and emits guardian events via the N-API host. Events immediately hide the window, pause audio/session streaming, optionally kill the offending process, and only afterward call `/api/guardian/events`.
- `native/screenshot` captures the active display (DXGI/ScreenCaptureKit) and returns raw PNG bytes to Electron. The main process encrypts the payload with AES-GCM, requests a signed upload ticket, retries up to five times, and confirms via `/api/guardian/screenshots/confirm`.
- The renderer subscribes to `guardian:event` and `guardian:screenshot-status` bridges, surfacing detections and capture attempts in `GuardianNotifications`.

Update the policy file to change watchlists, mitigation behavior, or screenshot cadence without touching code. The guardian runtime auto-starts when the native host loads, ensuring prevention-first behavior before telemetry leaves the device.

### Secure config cache, offline fallback, and runtime overrides

Task 6 adds the config-center features promised in Requirement 4:

- **Encrypted cache (`@guardian/config-store`)** – Every successful config pull is wrapped in AES-GCM with a device-specific key that lives under `AppData/Roaming/guardian-client/secrets/`. Snapshots expire after 30 minutes and are replayed on launch so the agent keeps its last known profile even if Cloud Access is unreachable.
- **Renderer settings panel** – The React shell now surfaces the active profile, data source (`live`, `cache`, or `override`), last update timestamp, and remaining cache TTL. You can trigger a manual refresh without leaving the UI.
- **Runtime override watcher** – When `client.json.cloudAccess.configWebsocket` points at the Cloud Access override channel, the Electron main process opens a secured WebSocket, listens for Redis fan-out messages, and hot-applies overrides (audio sample rate, guardian toggles) in <5 s. Overrides are marked as such in the UI and automatically cleared when they expire.
- **Native host hot-reload** – Once a new profile or override lands, the Electron main process restarts the Rust audio service with the updated sample rate/chunk cadence and toggles the guardian runtime without killing the session.

To enable overrides in your environment, add `configWebsocket` to `configs/client.json`:

```json
"cloudAccess": {
  "baseUrl": "https://cloud-access-core.local",
  "authUrl": "https://cloud-access-core.local/auth/login",
  "callbackUrl": "guardian://auth/callback",
  "configWebsocket": "wss://cloud-access-core.local/ws/runtime-overrides"
}
```

Leave the property absent if your environment does not expose the WebSocket channel—polling continues via REST, and the cache continues to satisfy offline scenarios.

### Diagnostics exporter & updater (Task 7)

- The Electron main process now wires a diagnostics request flow that collects CPU/memory/jitter metrics from the native host, redacts sensitive values, and posts the payload to `/api/diagnostics`. Trigger it from the tray menu, the renderer panel, or the `guardian:send-diagnostics` IPC bridge. Status changes are streamed to the renderer via `guardian:diagnostics-status`.
- A platform updater built on `electron-updater` checks the configured feed (`client.json.updates`) every 60 minutes, downloads signed deltas (Squirrel on Windows, Sparkle/generic feed on macOS), and stages them for the next restart. Users can manually check/install from the “Updates” panel in the renderer.
- The packaging workflow lives in `electron-builder.yml` plus the helper script `scripts/build-desktop.sh`, which runs pnpm builds, compiles the Rust host, and invokes `electron-builder` to generate signed artifacts.

### Testing & QA automation (Task 8)

- **Unit tests** – `pnpm test` runs Vitest suites across the renderer stores, shared packages, and IPC helpers. Rust crates under `native/` also have per-crate unit tests runnable via `pnpm native:test`.
- **Renderer smoke** – `pnpm e2e` executes `renderer/src/e2e/appSmoke.test.tsx`, which mounts the App with the guardian stub and drives login, session control, guardian events, and diagnostics via Testing Library.
- **CI wiring** – `.github/workflows/desktop-guardian-client.yml` now runs lint, unit tests, type-checking, Rust tests, and the renderer smoke suite on every push/PR. The root `scripts/checks.sh` also includes a `run_guardian_client` helper invoked alongside the service specs.
- **Manual smoke** – After pulling a signed build, run `pnpm native:build && pnpm dev` to exercise the Electron shell against staging services. Use `GUARDIAN_SKIP_E2E=1` or `GUARDIAN_SKIP_NATIVE_TESTS=1` when you need to bypass parts of the QA pipeline locally.
