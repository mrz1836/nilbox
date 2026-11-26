# Prerequisites

[Docs](../README.md) › [Development](../README.md#development) · See also: [Build targets](build-targets.md)

- **macOS 13.3 or newer.** The app is built and supported on macOS only for now. macOS 13.3
  ships Safari 16.4, the oldest WebKit the window's bundle targets.
- **Xcode Command Line Tools** (`xcode-select --install`), which provide the C toolchain the
  desktop build needs.
- **Go** at the version in `go.mod`.
- **Node** at the major version in `frontend/.nvmrc` (Node 26).
- **pnpm**, installed once at the exact version pinned in `frontend/package.json`
  (`packageManager`). Node 26 no longer ships corepack, so install it with npm:

  ```bash
  npm install -g pnpm@12.23.0
  ```

  When `pnpm` is missing, the frontend targets stop with this same command, read from
  `packageManager`.
- **[MAGE-X](https://github.com/mrz1836/mage-x)** (`magex`) and
  **[go-pre-commit](https://github.com/mrz1836/go-pre-commit)**, as in the README.
- The **[GitHub CLI](https://cli.github.com)**, signed in with `gh auth login`. The app borrows
  its token and needs the `repo` and `read:org` scopes. The optional `read:project` scope lets
  issue cards show [the projects each issue is in](../guide/inbox-cards.md#issue-projects); `gh` doesn't grant it by
  default, so add it with `gh auth refresh -s read:project`.
- To launch sessions: the **`claude`** CLI (Claude Code), the **`codex`** CLI (OpenAI Codex),
  or both, and **iTerm2** (the default terminal), **Warp** (Stable, v0.2026.05.13 or newer, and
  v0.2026.05.27 or newer for Show in terminal, with its first-run welcome finished; see
  [Warp](../guide/launching-a-session.md#warp)), or Terminal.app. **Settings → Profiles** shows
  which agents are installed.

The Wails CLI is never installed. The targets run it with `go run` at the version `go.mod`
pins.
