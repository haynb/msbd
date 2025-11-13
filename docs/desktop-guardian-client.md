# desktop-guardian-client Operations Guide

This document supplements the spec requirements with the practical runbooks for diagnostics export, updater configuration, and packaging.

## Diagnostics Uploads

- **Trigger paths**: tray menu → *Diagnostics…*, renderer *Diagnostics* panel button, or remote IPC `guardian:send-diagnostics`.
- **Flow**:
  1. Electron main asks the native guardian host for system metrics (CPU %, memory, jitter, guardian counts).
  2. Payload is normalized with build/platform metadata and posted to `POST /api/diagnostics` on `cloud-access-core`.
  3. Renderer receives real-time status events via `guardian:diagnostics-status`.
- **Config**: `client.json.tray.diagnosticsUrl` overrides local uploads and simply opens the referenced URL (e.g., internal ops dashboard). Leave unset to enable uploads.
- **Retries**: uploads rely on the shared fetch client and bubble errors to the UI; cached artifacts are not persisted beyond the encrypted config cache (Requirement 4).

## Updater

- Implemented with `electron-updater` (Squirrel.Windows + Sparkle/generic feed).
- Configuration lives under `client.json.updates`:

```jsonc
"updates": {
  "enabled": true,
  "channel": "stable",
  "feedUrl": "https://updates.example.com/desktop",
  "autoCheckMinutes": 60
}
```

- The updater checks the feed on launch and every `autoCheckMinutes`. Downloaded packages are staged for the next restart; installs are blocked while a session is active. The renderer “Updates” panel exposes manual *Check* and *Install* buttons.

## Packaging

- Run `pnpm package:desktop` (or `scripts/build-desktop.sh`) from `clients/desktop-guardian-client/`.
- Script steps:
  1. `pnpm install`
  2. `pnpm build` (workspace TS)
  3. `pnpm native:build` (Rust host)
  4. `pnpm --filter desktop-guardian-client-electron run package`
- Artifacts land under `clients/desktop-guardian-client/dist/release/`.
- Customize signing and feed URLs via environment variables consumed by `electron-builder.yml` (e.g., `GUARDIAN_UPDATE_FEED`).

## Testing & QA automation

- `pnpm lint`, `pnpm test`, and `pnpm typecheck` run across every renderer/electron/shared package through the workspace root. These commands are wired into CI and the updated `scripts/checks.sh` helper (`run_guardian_client`).
- `pnpm native:test` executes the Rust suites for the audio, guardian, and screenshot crates. Run them locally whenever native code changes; CI also runs them after Node-based tests.
- `pnpm e2e` executes the renderer smoke test (`renderer/src/e2e/appSmoke.test.tsx`) with Vitest/Testing Library. The suite mounts the app with the guardian stub and drives login, streaming, guardian detections, and diagnostics flows without touching real services.
- For full end-to-end validation with real services, pair `pnpm native:build` with `pnpm dev`, sign in against staging `cloud-access-core`, and monitor guardian detections via the renderer dashboard. Guardian policy tweaks + runtime overrides can be replayed with the same tooling.
