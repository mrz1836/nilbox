# Coding agents and profiles

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/profiles`](../architecture/profiles.md), [Shared building blocks](../architecture/shared-building-blocks.md), [`internal/launcher`](../architecture/launcher.md)

Nilbox launches two coding agents, **Claude Code** (`claude`) and **OpenAI Codex** (`codex`),
and treats them the same way: each has its own profiles, its own model and effort lists, and
its own permission flags, and every launch picks one. **Settings → Profiles** lists each agent
with its installed version and where its program was found, or that it wasn't, and its
profiles below it.

Profiles are config directories, discovered from your home directory on every start:

| Agent | Default profile | Named profiles | Config variable | A folder counts when it holds |
|---|---|---|---|---|
| Claude Code | `~/.claude` | `~/.claude_<name>`, `~/.claude-<name>` | `CLAUDE_CONFIG_DIR` | `.claude.json`, `settings.json`, or `projects/` |
| Codex | `~/.codex` | `~/.codex_<name>`, `~/.codex-<name>` | `CODEX_HOME` | `auth.json`, `config.toml`, or `sessions/` |

- Each agent's default directory becomes its `default` profile, whose sessions run without the
  config variable, so the agent uses its default login.
- Each named directory the agent has run with becomes that agent's profile `<name>`, and its
  sessions run with the config variable set to that directory. A directory counts only when it
  holds one of the agent's own markers, which the agent writes into a config directory it uses.
  Other tools name their folders the same way (session managers, usage monitors, or a
  `~/.codexbar`), and those folders are left out. If both spellings exist for one name, the
  underscore form wins. Files such as `~/.claude.json` are ignored.
- Each agent's profile names are its own, so `~/.claude_work` and `~/.codex_work` are two
  profiles called `work`, one per agent.
- A newly discovered profile starts with full access (Claude's `--dangerously-skip-permissions`,
  Codex's `--dangerously-bypass-approvals-and-sandbox`), the agent's default model and effort,
  and plan mode off.

## Signing in

Claude Code profiles are used as they are. A Codex profile needs its own sign-in, which Codex keeps
in `auth.json` in the profile's directory; Nilbox only checks that the file is there and never reads
it. A Codex profile without one, and whose environment sets no `OPENAI_API_KEY` or `CODEX_API_KEY`,
shows **Not signed in**, with the command to fix it, such as `CODEX_HOME=~/.codex_work codex login`.

Profiles can't be edited in the app yet: each one keeps the settings discovery gave it.
Discovery never changes a stored profile, though, and a profile whose directory disappears is
kept. On each start the app removes only the stored profiles that hold nothing worth keeping:
ones whose directory is still there but holds none of its agent's markers, and whose settings
are still the ones discovery gave them. Their folders are left untouched. If such a folder later
becomes an agent config directory, it is discovered again. When the removed profile was an
agent's default profile setting, the setting falls back to `default`.

**Settings → Profiles** lists every profile, grouped by agent, with the folder it runs from and
what it sets for a session (model, effort, plan mode, extras, and permissions, in each agent's
own words), and marks any whose folder has gone missing or that isn't signed in.

## Choosing the agent and the profile

**Settings → Launching** sets the default coding agent (the `agent.default` setting, initially
Claude) and a default profile for each agent (the `profile.default.claude` and
`profile.default.codex` settings, initially `default`). Each inbox card's launch uses the agent and
the profile its [launch defaults](launch-defaults.md) resolve to: the launch menu's switch for that
launch, else the repository's, else its org's, else the default agent; then that agent's profile
from the repository's defaults, else its org's, else the agent's default profile. A launch menu
choice applies to that launch only. Model, effort, and plan mode come from the profile unless the
launch defaults, the prompt, or the launch menu set them; extra directories and environment come
from the profile only.
