# internal/statusprefs

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/statusprefs`](../../internal/statusprefs) · Guide: [The window](../guide/window.md)

`internal/statusprefs/` is the service status check's catalog and the user's choices of
what it watches. Each `Provider` (GitHub, Claude, OpenAI) has its page, where its summary is
read (`Host`, `Path`, and `Format`: Statuspage's `summary.json`, or incident.io's documented
widget API, `/api/v1/summary`, since OpenAI's Statuspage-shaped summary leaves Codex out),
and the `Part`s a user may watch, each named by the app's own stable `PartID`
(`github.pulls`, `claude.api`, `openai.codex-cli`, ...) and matched to the page by its
component ids (`PageIDs`), so a provider that issues new ids needs only a catalog edit and
the stored choices survive it. The defaults are what the app depends on: GitHub's Git
Operations, API Requests, Issues, Pull Requests, and Actions; Claude's API and Claude Code;
and OpenAI's Codex API, Codex CLI, and Responses. The choices are stored as changes from the
defaults, as notifyprefs stores the banners (`status.providersOff`, `status.partsOff` for
the default parts unticked, `status.partsOn` for the others ticked), so a part a later build
adds with `Default` is watched without the user doing anything; settings alone writes them
(`SetStatusProvider`, `SetStatusPart`, each in one `prefs.Update`, emitting the Go-only
`event.StatusRulesChanged`). Adding a provider is one catalog entry and its host in
`github.DefaultStatusHosts`, and a parser only for a new format. It is a leaf: it imports
only `prefval`.
