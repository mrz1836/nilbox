# Backup files

A backup file holds your whole setup in a portable form, so you can move it to another Mac, or
keep it with your own backups and bring it back later: your settings, your repositories, your
prompts and review replies, your launch defaults, your own scripts, and your Autopilot
routines (one an import adds or saves over arrives switched off, and one already the same stays
as it is, on or off). The app writes one from
**Settings → Back up and restore → Export…**, and reads one from **Import…** there. How an import
treats what is already on the Mac is in [Backing up your setup](desktop.md#backing-up-your-setup).

A backup is UTF-8 JSON, at most 16 MB, written readable only by you. Each category you tick on
export is a section of the file; one you leave unticked is absent.

<br>

## Example

```json
{
  "format": "nilbox.backup",
  "version": 1,
  "exportedAt": "2026-11-14T17:00:00Z",
  "app": "v0.4.0",
  "settings": {
    "defaultAgent": "claude",
    "defaultProfiles": {"claude": "default", "codex": "default"},
    "theme": "dark",
    "refresh": {"inbox": 300, "checks": 45, "releaseChecks": 45, "worktrees": 300, "notifications": 300, "serviceStatus": 1800},
    "serviceStatus": {"off": [], "parts": ["github.pulls", "github.actions", "claude.api", "claude.code", "openai.codex-cli"]}
  },
  "repositories": {
    "cloneRoot": "~/projects",
    "locations": [{"path": "~/work", "kind": "folder"}],
    "pinned": ["acme-org/example-repo"],
    "muted": []
  },
  "prompts": {"format": "nilbox.prompts", "version": 3, "prompts": []},
  "launchDefaults": {"format": "nilbox.launch-defaults", "version": 2, "defaults": []},
  "scripts": {
    "scripts": [
      {
        "name": "Fix all",
        "command": "magex format:fix && magex lint",
        "icon": "play",
        "scope": "all",
        "repos": [],
        "runsIn": "",
        "confirm": false
      }
    ]
  },
  "autopilot": {
    "format": "nilbox-autopilot/1",
    "exportedAt": "2026-11-14T17:00:00Z",
    "app": "v0.4.0",
    "routines": [
      {
        "name": "Tell me about failing checks",
        "mode": "ask",
        "enabled": true,
        "spec": {
          "version": 1,
          "watch": {"sources": [{"lists": ["authored"]}]},
          "trigger": {"event": "checks_failed"},
          "conditions": {"match": "all", "of": []},
          "steps": [{"notify": {}}],
          "limits": {}
        }
      }
    ]
  }
}
```

The app writes every field of every section it exports, so a file you open shows each setting
as it was. The example leaves most out, as a file edited by hand may.

<br>

## Fields

| Field | Meaning |
|---|---|
| `format` | Always `nilbox.backup`. A file with any other value is refused, a [prompt file](prompt-files.md) among them. |
| `version` | The format's version, `1`. |
| `exportedAt` | When the file was written, as RFC 3339 in UTC. |
| `app` | The version of the app that wrote it. Informational only. |
| `settings` | Your settings ([below](#settings)). |
| `repositories` | Your clone folder, other repository locations, and pinned and muted repositories and orgs ([below](#repositories)). |
| `prompts` | Your prompt library, exactly as `prompts.json` holds it: a [prompt file](prompt-files.md) with its own `format` and `version`, its defaults, `startersAdded`, and `slotsSeeded` included. |
| `launchDefaults` | Your org and repository [launch defaults](desktop.md#launch-defaults), exactly as `launch-defaults.json` holds them: `{"format": "nilbox.launch-defaults", "version": 2, "defaults": [...]}`, each default prompt named by nickname: a row's prompt for pull requests (`prPrompt`) and issues (`issuePrompt`), and its prompt for a pull request whose checks fail (`checksPrompt`), that conflicts with its base (`conflictsPrompt`), that a reviewer asked to change (`changesPrompt`), and that you're asked to review again (`rereviewPrompt`). Those four are left out when they inherit, and a file from an older version, which has none, inherits them. |
| `scripts` | Your own [scripts](desktop.md#repo-scripts), in their order ([below](#scripts)). |
| `autopilot` | Your Autopilot routines, exactly as `autopilot.json` holds them ([below](#autopilot)). |

Every section is an object, or absent. The prompts, launch defaults, and Autopilot sections
keep their own formats and versions, so a newer prompt file inside an older backup is refused
by the same rule that refuses it on its own.

### Settings

Every setting but the window's memory, each under its own name. A setting left out is left as it
is on import; within `defaultProfiles`, `refresh`, and `integrations.switches` and `.paths`, so
is each entry left out.

| Field | Setting |
|---|---|
| `defaultAgent`, `defaultProfiles` | The coding agent launches use, and each agent's profile (by name). |
| `terminal`, `worktreeDefault`, `followSessions`, `codexEffort`, `codexFast` | Launching: the terminal app, *Start in a worktree*, following sessions, and Codex's effort and Fast mode. |
| `worktreeAutoCleanup`, `branches` | Cleaning up worktrees, and the branch rules (`own`, `pullRequest`, `merged`, `protected`). |
| `trustedBots` | The bots whose items are trusted, by login. |
| `snoozeDays`, `snoozeWake` | Snoozing (`commits`, `reviews`, `comments`). |
| `sensitiveScope`, `sensitiveOff`, `sensitivePatterns` | Sensitive files: whose are checked, the built-in categories turned off, and your own patterns. |
| `menuBar`, `bannersOff` | The menu bar icon, and the kinds of notification whose banners are off. |
| `githubNotifications` | The GitHub notification rules: `read`, `auto`, `action`, `triggersOff`, `watching`, `botActivity`. |
| `refresh` | How often, in seconds, the app reads `inbox`, `checks` (the pull requests' running checks), `releaseChecks` (the Release page's running checks), `worktrees`, and `notifications` again, and how often it checks `serviceStatus` (the status pages of the services you watch); `0` turns either checks follow, or the status check, off. A file from an older version names the status check `githubStatus`, which is read as `serviceStatus` unless the file holds that too. |
| `serviceStatus` | What the service status check watches: `off`, the status pages turned off (`github`, `claude`, `openai`), and `parts`, every part watched, across the services, by the app's own names for them (such as `github.pulls`, `claude.api`, or `openai.codex-cli`). `parts` is the whole list: a part it leaves out is unticked. A name this version doesn't know is left out of an import, with a note. |
| `mergeMethod`, `theme`, `textSize`, `toastSeconds` | Reviewing and appearance. |
| `inboxOrder` | Which inbox lists (`reviewRequested`, `yours`) sort by project status (`statusSorted`), the order of the statuses (`statusOrder`), and which show one kind of item (`kinds`). A file from an older version names the former `authored` and `assigned` lists: `assigned`'s sort and kind are read as `yours`'s, unless the file gives `yours` a kind of its own, and `authored`'s are dropped. |
| `hiddenLabels` | The patterns of the labels the inbox's cards leave out (`*` any run of characters, `?` one, ignoring case), such as `size/*`. An empty list hides none. |
| `cardAvatars` | *Author pictures*: whether the inbox's cards show each author's picture (`true`, the default) or initials and the bot mark (`false`). |
| `cardDensity` | *Density*: how much room each of the inbox's cards takes, in both lists: `comfortable` (two lines, the default) or `compact` (one line). |
| `cardUnread` | *Unread dots*: whether the inbox's cards show a dot on an item with activity since you last opened it, with Mark as read and Mark as unread in its menu (`true`, the default), or neither (`false`). |
| `integrations` | Each integration's switch (`switches`), the copy of each tool you chose (`paths`, absolute, `""` for the one your shell finds), `runsIn`, `offerAfterMerge`, `magexTargets`, `repoFiles`, and `showRuns`. |
| `updateMode` | What the app does when a new version is out: `download` (download it and tell me), `notify` (just tell me), or `off` (don't check). |
| `autopilot` | Autopilot's settings: `enabled` (Autopilot (beta), which a file brings to this Mac with the rest: on, it shows and runs here), `paused`, `defaultMode` (`dryRun`, `ask`, or `auto`), `agentsAtOnce` (1 to 3), `runsPerDay` and `actionsPerDay` (1 to 200), `dailyCap` (`{"amount", "unit"}`: `0` for none, else 1 to 500 in `usd` or 100,000 to 50,000,000 in `tokens`), `keepAwake`, `holdOnOutage`, `catchUpHours` (1 to 168), `historyDays` (7 to 365), and `logDays` (1 to 90, never more than the history). The history and the logs are checked together, against this Mac's when the file holds only one. |

### Repositories

| Field | Meaning |
|---|---|
| `cloneRoot` | The clone folder, with `~` for home. |
| `locations` | The other repository locations, each `{"path", "kind"}`, `kind` being `folder` or `clone`. The kind is worked out again on import. |
| `pinned`, `muted` | The pinned and the muted repositories (`owner/name`) and orgs (`owner`). |

### Scripts

`scripts` lists your scripts in their order, each with every field the script editor saves:
`name`, `command`, `icon` (`play`, `test`, `lint`, `build`, or `configure`), `scope` (`all`,
`go`, or `repos`), `repos` (`owner/name`, for `repos` alone), `runsIn` (`app`, `terminal`, or
`""` for the setting), and `confirm` (*Ask before running*). A script has no ID in a file.

### Autopilot

The Autopilot section is the routines' own format, the one `autopilot.json` beside the
database holds, so a routine moves between Macs whole. **Export…** and **Import…** offer it
as **Autopilot routines**, and the copies the app saves before an import or a reset hold it
too. A routine an import adds or saves over arrives switched off, one already the same is
left as it is, and a routine's history never travels with it.

| Field | Meaning |
|---|---|
| `format` | `nilbox-autopilot/<version>`, `nilbox-autopilot/1` today. Another value is refused, and a newer version asks you to update the app. |
| `exportedAt`, `app` | When it was written, as RFC 3339 in UTC, and the version of the app that wrote it. Informational only. |
| `routines` | The routines, in their order. |
| `prompts` | Only in a file the Autopilot tab's **Export…** wrote with **Include the text of the prompts they run**: the prompts its routines name by nickname, in the [prompt file format](prompt-files.md). Never in a backup or the copy beside the database, whose prompts travel in their own section and file. |

The Autopilot tab's **Export…** and **Import routines…** read and write the same format on its
own, one routine or all of them, outside a backup. Such an import says first what would
arrive, and maps each prompt, profile, or agent its routines name that this Mac lacks to one
it has or to inherit; its `prompts` arrive only where no prompt here has the nickname, and
every routine arrives switched off, added at the end of the list under its name, or another
when a routine here has it.

Each routine has its `name` (unique, ignoring case), `description` (left out when empty),
`mode` (`dryRun`, `ask`, or `auto`), `enabled` (whether it was on where it was exported),
`template` (the template it was made from, left out for none), and `spec`, its definition: one
JSON object with its own `version`, naming prompts by nickname or by starter and profiles by
agent and name, never by ID, so it works on another Mac. A routine has no ID and no account in
a file, and never its history: what it matched, ran, or was told.

An import matches routines by name, ignoring case in any alphabet. A routine the same as the
file's (its `spec`, `description`, and `mode`) is left as it is, on or off, under either mode;
any other match is saved over and switched off, and a routine this Mac lacks is added,
switched off. Replace also deletes the routines the file doesn't name. A routine here that a
newer version of the app wrote is never saved over or deleted: it is left as it is, with a
note. One of the file's whose `spec` this version can't read (as one a newer version wrote,
or one that no longer passes its checks) is left out with a note, as is a second routine of
one name; a `mode` this version doesn't know takes the mode new routines start in, and a
routine with a step this version can't run yet arrives in Dry run. A `spec` that loads is
stored as the app writes one, its lists sorted and its texts trimmed.

<br>

## What a backup never holds

- **Your GitHub sign-in.** The app never stores a token: it borrows one from `gh` each time.
- **Caches and mirrors.** The inbox, the review sheet's diffs, the issue sheet's views, profile
  pictures, and the mirror of your GitHub notifications are read from GitHub again.
- **History.** Runs, agent sessions, notifications, worktree records, and Autopilot's history:
  what each routine matched, ran, and did.
- **Per-item triage.** Snoozes, dismissals, and pins hang off the inbox's cached items.
- **Agent profiles.** They are found in your home directory on every start.
- **Script trust.** A file from elsewhere could otherwise pre-approve a repository's command,
  so each repository command asks again before its first run.
- **The window's memory.** Its size, folded sections, tips put away, the Notifications page's
  tab, and the Scripts tab's last page and clone.
- **Tool detection and install records.** The app finds the tools again.
- **The app's own updates.** When it last checked, the version it last told you of, Later, and
  the downloaded update: the next check finds them again.

<br>

## Reading a file

- **Nothing in a file is trusted more than what you type.** Each setting is checked the way its
  setter in Settings checks it, each prompt the way the editor checks one, each launch default
  the way a restore checks one, each script the way the script editor does, and each routine
  the way the routine builder's save does. A value the app
  would refuse is left out, and the import dialog names it before you import: a setting this
  version doesn't know, a profile that isn't on this Mac, a copy of a tool that isn't a program
  here, a repository location that doesn't exist here, or a prompt a launch default names that
  isn't in the library (that launch default inherits it).
- **Fields the app doesn't know are ignored,** and a section the app doesn't know is ignored too,
  so a file a newer version wrote in the same format still imports.
- **A newer version of the format is refused** with a message asking you to update the app.
- **An import reads the file again,** and refuses it when it changed since you opened it.
