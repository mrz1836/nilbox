# internal/profiles

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/profiles`](../../internal/profiles) · Guide: [Coding agents and profiles](../guide/agents-and-profiles.md)

`internal/profiles/` is the owner of the coding agents' profiles (the `agent_profiles` table): it
discovers them in the home directory, keeps them, prunes the ones that hold nothing worth keeping,
and says of each where its folder is, whether the folder is still there, and whether it is signed
in. A profile is a config directory one agent runs with, so each agent has profiles of its own,
keyed by agent and name: `~/.claude_work` and `~/.codex_work` are two profiles called `work`, one
per agent. What the package reads of each agent's spec in `internal/agent` ([Shared building
blocks](shared-building-blocks.md)): `ConfigDir`, `Markers`, `AuthFile`, `AuthEnv`, and
`ModelsCache` (the launcher sets `ConfigEnv`). The `Store` is what every other package reads the
profiles through, directly or through a narrow interface it satisfies, and the `Service` is what the
window binds. It imports `agent`, `execpath`, `paths`, `clock`, `store`, and `logging`. The user's
side is [Coding agents and profiles](../guide/agents-and-profiles.md).

## A profile

`Profile` holds `Agent`, `Name`, `ConfigDir` (absolute, `""` for the default profile), `Model` and
`Effort` (`""` for the agent's default), `PermissionMode` (`PermissionFull`, `"full"`, one of the
agent's permission modes, or `""` for the agent's default), `PlanMode`, `AddDirs` (passed with
`--add-dir`), `ExtraArgs`, and `Env`, and three fields only `List` fills in: `Folder`, `Missing`,
and `SignedIn`. `DefaultName`, `"default"`, names the profile of each agent's default directory.
Nothing in the app edits a profile, and none of the package's queries updates a row: a profile
keeps the settings discovery gave it (`newDiscovered`), which are full access (`PermissionFull`,
Claude Code's `--dangerously-skip-permissions`, Codex's
`--dangerously-bypass-approvals-and-sandbox`), no model or effort, plan mode off, and empty lists
and environment.

## Discovery

`Discover(home, log)` reads the entries directly under the home directory (made absolute) once,
and for each agent in the registry's order (`agent.All`) maps each entry to a candidate
(`classify`):

- the agent's `ConfigDir` itself (`.claude`, `.codex`) is its `default` profile, with `ConfigDir`
  `""`, whenever it is a directory;
- `<ConfigDir>_<name>` or `<ConfigDir>-<name>`, where `<name>` matches
  `^[A-Za-z0-9][A-Za-z0-9._-]*$` (`namePattern`), is its profile `<name>`, with that absolute
  directory as its `ConfigDir`, but only when it is a directory that holds one of the agent's
  `Markers` of the right kind, a file or a directory (`inspectConfigDir`): Claude Code's
  `.claude.json`, `settings.json`, or `projects/`, and Codex's `auth.json`, `config.toml`, or
  `sessions/`, each of which the agent writes into a config directory it uses.

Both checks follow symbolic links, since people link their settings in from elsewhere
(`TestDiscoverFollowsSymlinks`), and a folder that can't be listed counts as holding no marker
(`TestDiscoverSkipsUnreadableFolders`). So files (`~/.claude.json`), names the pattern refuses, and
another tool's folder named the same way but holding none of the markers are left out. When two
directories of one agent map to one name, the default directory beats `_<name>`, which beats
`-<name>` (`candidate.rank`, the lower winning), and each loser is logged as a warning, `skipping
a duplicate profile directory`, naming the directory kept (`TestDiscoverDuplicateNames`). A folder
without the agent's markers never competes, so a configured `~/.claude-dup` wins over an empty
`~/.claude_dup` (`TestDiscoverPrefersAConfigOverAnEmptyTwin`). The result is sorted as `Store.All`
sorts.

## Keeping and pruning

`SyncHome(ctx, store, home, logger)` brings the stored profiles in line with the home directory.
Core's `Bootstrap` runs it on every start, right after the database opens and before the prompt
library and the launch defaults are restored, and `Service.Rediscover` runs it whenever the window
asks (the first-run setup's **Check again** on its Agents step), then returns `List`, so a profile
directory made, or an agent installed and run, since the start shows without a restart. In order:

1. `Store.Prune` removes the rows that hold nothing worth keeping (below), logged at info as
   `removed profiles whose directory holds no agent config`; it comes first, so a name a removed
   row frees can be taken by the directory that owns it now.
2. `Discover` reads the home directory. One that can't be read skips discovery with a warning,
   `profile discovery skipped`, and no error, since the profiles already stored still work and the
   inbox doesn't need them (`TestSyncHomeSkipsAHomeItCannotRead`).
3. `Store.Sync` stores every discovered profile whose agent has no row of that name yet, in one
   transaction, all or none, stamping `created_at` and `updated_at` from the injected clock in UTC.
   It never updates or deletes a row (`InsertProfile`'s `ON CONFLICT (agent, name) DO NOTHING`), so
   a stored profile keeps its settings, and one whose directory is gone keeps its row
   (`TestSyncDoesNotOverwriteOrDelete`, `TestRediscoverKeepsAProfileWhoseFolderIsGone`).

An error from `Prune` or `Sync` is `SyncHome`'s, and fails `Bootstrap`.

`Prune` removes a row only when it is a named profile (not `default`, and with a `ConfigDir`) of an
agent this build knows, its directory can be listed but holds none of the agent's markers, and its
settings are still discovery's (`untouched`: no model, effort, extra directories, arguments, or
environment, `PermissionFull`, and plan mode off). A profile whose directory is gone or can't be
read is kept, as is every profile of an agent this build doesn't know. One whose settings differ
from discovery's is kept with a warning, `keeping an edited profile whose directory holds no agent
config`, since its settings would be lost. `Prune` returns the rows it removed as
`<agent>/<name>`, sorted. It never touches the folders themselves, and a folder that later becomes
an agent's config directory is discovered again. A removed profile that an agent's default profile
setting names reads as `default` from then on (`settings.Service.Get`, through
`checkDefaultProfiles`), so the settings never name a profile the launch menu can't offer.

## The store and its queries

`Store` (`NewStore(db, clock)`) reads and writes `agent_profiles`, which no other package touches
(`tableOwners`). The table is in the base migration `0100_settings.sql`: `agent` (checked not to be
`''`) and `name`; `config_dir`, `model`, `effort`, and `permission_mode` (TEXT, `''` by default);
`plan_mode` (`BOOLEAN`, 0 by default); `add_dirs` and `extra_args` (each checked to be a JSON array,
`[]` by default) and `env` (checked to be a JSON object, `{}` by default); `created_at` and
`updated_at`; and `UNIQUE (agent, name)`, with no `NOCASE`. The queries, in
`internal/profiles/internal/queries/profiles.sql`:

- `ListProfiles`, every row in the order it was stored (`ORDER BY id`);
- `GetProfile`, one agent's profile by name, matched exactly, case included;
- `InsertProfile`, a new row, or nothing when the agent already has one of that name;
- `DeleteProfiles`, the rows of the given IDs (`sqlc.slice('ids')`), which `Prune` calls only with
  at least one.

`All` returns every profile grouped by agent in the registry's order, any agent this build doesn't
know after them by ID, each agent's `default` first and the rest by name (`sortProfiles`), with
`Folder`, `Missing`, and `SignedIn` unset. `Lookup(ctx, agent, name)` returns one, or an error
wrapping `ErrProfileNotFound`. A row whose JSON doesn't decode is an error naming the profile
(`TestReadsReportARowTheyCannotDecode`), and a profile's lists and map are never nil.

## Folders and sign-in

`List` returns `All`'s profiles, each with its folder described (`describeFolder`): the directory it
runs with (its `ConfigDir`, or for `default` the agent's `ConfigDir` under the home directory) as
`Folder`, with `~` for the home directory (`paths.Tilde`); `Missing` when that directory doesn't
exist (`fs.ErrNotExist`); and `SignedIn`. Only a sign-in file's presence is checked, never what it
holds, since it holds credentials (`signedIn`):

- an agent whose spec names no `AuthFile` is always signed in as far as this package says: Claude
  Code, whose `AuthFile` is `""`, and an agent this build doesn't know;
- Codex is signed in when its `AuthFile`, `auth.json`, is a regular file in the profile's
  directory, or when the profile's own `Env` sets one of its `AuthEnv` variables, `OPENAI_API_KEY`
  or `CODEX_API_KEY`.

The window marks a profile that isn't signed in (`ProfilesSection.tsx`, the setup's
`AgentsStep.tsx`, and `LaunchMenu.tsx`, whose title gives the command that signs it in;
[Signing in](../guide/agents-and-profiles.md#signing-in)). The deeper check,
which runs the agent's own sign-in command (`SignInArgs`, read by `agent.ParseSignIn`) under the
profile's `ConfigEnv` and without the agent's `KeyEnv` unless the user lets a key through, is
core's readiness check for Autopilot and Describe it, not this package's
([Readiness](autopilot/agents-in-the-terminal.md#readiness-readygo)).

## The agents and their models

`Agents` returns every agent in the registry's order, the default first, each an `AgentInfo`: the
`agent.Agent` with `Installed` and `Path`, set when the `Finder` the `Service` was built with
(core's `execpath.Search`) finds its `Command`; a nil finder reports every agent as not installed.
An agent that keeps a cache of the models it offers (`ModelsCache`, Codex's `models_cache.json`) has
its `Models` replaced by the cache's, read from the agent's default directory under the home
directory first and then from its stored profiles' `ConfigDir`s in `All`'s order, the first that
lists a model winning (`TestAgentsFallsBackToAnotherProfilesCache`); reading it asks no server.
`parseModelsCache` keeps the models whose `visibility` is `list` and whose slug is set and accepted
by `agent.ValidateModel`, ordered by `priority`, each slug once, labeled by its `display_name`, else
its slug. A cache over 16 MiB (`maxModelsCacheBytes`) or that isn't a regular file is skipped, and
`modelsCache` keeps each file's models until its size or modification time changes, so a file read
before is parsed again only once it changes (`TestModelsCacheRereadsOnlyAChangedFile`). An agent
without a cache keeps its spec's `Models`.

## Who reads them

- **Core** runs `SyncHome` in `Bootstrap` (above), builds the one `Store` the rest share
  (`Core.ProfileStore`), and builds the `Service` the window binds.
- **The launcher** reads every profile on each resolution (`ProfileReader`, the `Store`'s `All`)
  and picks the agent's: the launch menu's, the repository's and the org's launch defaults, the
  app's default profile for the agent, the agent's `default`, else its first by name
  ([internal/launchdefaults](launchdefaults.md#how-a-launch-resolves-them)). The profile's model,
  effort, and plan mode are the layer below the launch defaults; `resolvePermission` reads its
  `PermissionMode` beside the resolved plan mode; and `launcher.AgentEnv` sets the agent's
  `ConfigEnv` to its `ConfigDir`, which
  must be absolute, or unsets it for `default`, with the profile's `Env` and without the agent's
  `KeyEnv` unless the user lets a key through ([internal/launcher](launcher.md)).
- **The launch defaults and the settings** check each profile they are asked to name through
  `Lookup` (`launchdefaults.ProfileLookup`, `settings.ProfileLookup`): a launch default's when it is
  saved, though a restore and an import keep one by name that isn't stored, and an agent's default
  profile when `settings.Service.SetDefaultProfile` sets it.
- **Autopilot**, through core's adapters: readiness looks a step's profile up, checks its folder,
  and runs its sign-in command (`readyChecks.profile`), and `routineProfiles` lists each installed
  agent with its profiles' names, from `Agents` and `List` (`autopilot.Profiles`).
- **Describe it**, through core's adapters, reads Claude Code's profiles
  (`describeAgents.claudeProfiles`) and every profile for its snapshot (`describeSetup`), both
  through `All`.
- **A bug report** counts each agent's profiles, those signed in, and those whose folder is gone,
  never naming one, since a profile's name can name an employer (`profileParts`).
- **The window** binds the `Service` (`List`, `Agents`, `Rediscover`), reached through
  `frontend/src/platform/`.
- `internal/measure` stores each agent's `default` profile through `Store.Sync` for its launch
  measurement.

Profiles have no copy beside the database, and a backup never holds them: discovery finds them
again on every start (see [internal/store's
copies](store.md#prompts-launch-defaults-and-the-users-scripts-outlive-the-database) and [What a
backup never holds](../backup-files.md#what-a-backup-never-holds)).
