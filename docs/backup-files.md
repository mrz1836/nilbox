# Backup files

A backup file holds your whole setup in a portable form, so you can move it to another Mac, or
keep it with your own backups and bring it back later: your settings, your repositories, your
prompts and review replies, your launch defaults, and your own scripts. The app writes one from
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
    "refresh": {"inbox": 300, "checks": 45, "worktrees": 300, "notifications": 300}
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
| `prompts` | Your prompt library, exactly as `prompts.json` holds it: a [prompt file](prompt-files.md) with its own `format` and `version`, its defaults and `startersAdded` included. |
| `launchDefaults` | Your org and repository [launch defaults](desktop.md#launch-defaults), exactly as `launch-defaults.json` holds them: `{"format": "nilbox.launch-defaults", "version": 2, "defaults": [...]}`, each default prompt named by nickname. |
| `scripts` | Your own [scripts](desktop.md#repo-scripts), in their order ([below](#scripts)). |

Every section is an object, or absent. The prompts and launch defaults sections keep their own
formats and versions, so a newer prompt file inside an older backup is refused by the same rule
that refuses it on its own.

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
| `refresh` | How often, in seconds, the app reads `inbox`, `checks`, `worktrees`, and `notifications` again. |
| `mergeMethod`, `theme`, `textSize`, `toastSeconds` | Reviewing and appearance. |
| `inboxOrder` | Which inbox lists sort by project status, the order of the statuses, and which show one kind of item. |
| `integrations` | Each integration's switch (`switches`), the copy of each tool you chose (`paths`, absolute, `""` for the one your shell finds), `runsIn`, `offerAfterMerge`, `magexTargets`, `repoFiles`, and `showRuns`. |

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

<br>

## What a backup never holds

- **Your GitHub sign-in.** The app never stores a token: it borrows one from `gh` each time.
- **Caches and mirrors.** The inbox, the review sheet's diffs, the issue sheet's views, profile
  pictures, and the mirror of your GitHub notifications are read from GitHub again.
- **History.** Runs, agent sessions, notifications, and worktree records.
- **Per-item triage.** Snoozes, dismissals, and pins hang off the inbox's cached items.
- **Agent profiles.** They are found in your home directory on every start.
- **Script trust.** A file from elsewhere could otherwise pre-approve a repository's command,
  so each repository command asks again before its first run.
- **The window's memory.** Its size, folded sections, tips put away, and the Scripts tab's last
  page and clone.
- **Tool detection and install records.** The app finds the tools again.

<br>

## Reading a file

- **Nothing in a file is trusted more than what you type.** Each setting is checked the way its
  setter in Settings checks it, each prompt the way the editor checks one, each launch default
  the way a restore checks one, and each script the way the script editor does. A value the app
  would refuse is left out, and the import dialog names it before you import: a setting this
  version doesn't know, a profile that isn't on this Mac, a copy of a tool that isn't a program
  here, a repository location that doesn't exist here, or a prompt a launch default names that
  isn't in the library (that launch default inherits it).
- **Fields the app doesn't know are ignored,** and a section the app doesn't know is ignored too,
  so a file a newer version wrote in the same format still imports.
- **A newer version of the format is refused** with a message asking you to update the app.
- **An import reads the file again,** and refuses it when it changed since you opened it.
