#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo ">> Installing workspace deps"
corepack pnpm install

echo ">> Building TypeScript packages"
corepack pnpm build

echo ">> Building native host"
corepack pnpm native:build

echo ">> Packaging Electron app"
corepack pnpm --filter desktop-guardian-client-electron run package

echo "Artifacts written to electron/dist/release (see electron-builder.yml for targets)"
