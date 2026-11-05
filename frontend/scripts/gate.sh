#!/usr/bin/env bash
#
# The frontend gate: the one entry point for every frontend check. The local
# `magex frontend:gate` target and the frontend CI workflow both run this script,
# so local runs and CI cannot drift apart.
#
# Steps: frozen-lockfile install, format check, lint, typecheck, and tests with
# coverage. Any failure stops the gate.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

if ! command -v pnpm >/dev/null 2>&1; then
  # Read the pinned version without pnpm (or node), so the hint always works.
  pinned=$(sed -n 's/^[[:space:]]*"packageManager":[[:space:]]*"\([^"]*\)".*/\1/p' package.json)
  echo "pnpm is required. Install it once with: npm install -g ${pinned:-pnpm}" >&2
  exit 127
fi

pnpm install --frozen-lockfile
pnpm format:check
pnpm lint
pnpm typecheck
pnpm test:coverage
