# Prompt files

A prompt file holds prompts in a portable form, so you can move them between Macs, share them,
or keep them in your own backups. The app writes one when you export prompts, reads one when
you import, and keeps one beside its database as `prompts.json`, a copy of your whole library
that it rewrites after every change. How the app uses that copy is in
[Keeping your prompts](desktop.md#keeping-your-prompts). A [backup](backup-files.md) of your
whole setup holds one as its prompts section.

A prompt file is UTF-8 JSON. The app writes every field, so a file you open shows every
setting a prompt has, and you can edit one by hand.

<br>

## Example

```json
{
  "format": "nilbox.prompts",
  "version": 3,
  "exportedAt": "2026-11-08T17:00:00Z",
  "app": "v0.4.0",
  "defaults": {
    "pr": "Review",
    "issue": "Work on it",
    "replies": {"approve": "LGTM", "approve_bots": "Update OK"}
  },
  "startersAdded": ["review-pr", "work-on-issue", "quick-look", "fix-checks", "reply-lgtm", "reply-update-ok"],
  "prompts": [
    {
      "name": "Look for security problems",
      "nickname": "Security",
      "appliesTo": "any",
      "body": "Look for security problems in {{.URL}}\n",
      "purpose": "agent",
      "action": "",
      "models": {"claude": "opus", "codex": "gpt-6-sol"},
      "effort": "high",
      "planMode": true,
      "worktree": null,
      "extraArgs": {"claude": ["--verbose"], "codex": ["--search"]},
      "starterKey": "",
      "createdAt": "2026-11-07T09:00:00Z",
      "updatedAt": "2026-11-07T09:30:00Z"
    }
  ]
}
```

<br>

## Fields

| Field | Meaning |
|---|---|
| `format` | Always `nilbox.prompts`. A file with any other value is refused. |
| `version` | The format's version, `3`. Version `1` and `2` files still import (see [Older versions](#older-versions)). |
| `exportedAt` | When the file was written, as RFC 3339 in UTC. |
| `app` | The version of the app that wrote it. Informational only. |
| `defaults` | The app's default prompt for pull requests (`pr`) and issues (`issue`), each by nickname among the file's prompts, or `""`; and `replies`, the default [review replies](desktop.md#review-replies) by slot (`approve`, `approve_bots`, `request_changes`, `comment`), each by nickname. A slot whose default is none of the file's prompts is left out, as is `replies` when none is. |
| `startersAdded` | The starter prompts the library had been given, by key. Restoring a library from the file keeps a starter you deleted from coming back. Only a whole-library file fills it in. |
| `prompts` | The prompts, at most 1,000. |

Each prompt:

| Field | Meaning |
|---|---|
| `name` | Its title, up to 80 characters on one line. |
| `nickname` | The short label the launch button shows, up to 20 characters. Nicknames are unique in a library, ignoring case. |
| `appliesTo` | `pr`, `issue`, or `any`. A reply's is `pr`, but a comment reply's, which may be for issues too. |
| `body` | The prompt's text: a Go [text/template](https://pkg.go.dev/text/template) using the variables in [Prompts](desktop.md#prompts). |
| `purpose` | `agent` for a prompt a launch fills in, or `reply` for a [review reply](desktop.md#review-replies), the text a sheet fills in: a review's in the review sheet, or a comment's in the issue sheet. A reply has no launch settings. |
| `action` | A reply's review: `approve`, `request_changes`, or `comment`; `""` for an agent prompt. |
| `models` | The model it launches each coding agent with, keyed by agent: `claude` or `codex`. An agent left out inherits its model. |
| `effort` | The effort level it launches with, shared by every agent, or `""` to inherit it. An agent that lacks the level, such as Claude Code for Codex's `ultra`, launches with its own default. |
| `planMode`, `worktree` | `true` or `false` to turn plan mode or the worktree on or off, or `null` to inherit. |
| `extraArgs` | Extra arguments for each coding agent, keyed by agent (`claude` or `codex`), one per list item, added after the profile's and the launch defaults'. Each agent only ever gets its own. |
| `starterKey` | The starter it came from (`review-pr`, `work-on-issue`, `quick-look`, `fix-checks`, and the replies `reply-lgtm`, `reply-update-ok`, `reply-needs-changes`, `reply-question`), or `""` for a prompt someone wrote. |
| `createdAt`, `updatedAt` | When it was added and last saved, as RFC 3339, or `""`. Informational only. |

<br>

## Reading a file

- **Prompts are named by nickname, never by a database ID,** so a file reads the same in any
  library.
- **Fields the app doesn't know are ignored,** and a field left out takes its empty value. A
  file a newer version of the app wrote in the same version of the format still imports.
- **A newer version of the format is refused** with a message asking you to update the app. The
  version goes up only for a change an older app would misread, not when a field is added.
- **Nothing in a file is trusted more than what you type.** Each prompt is checked the way the
  editor checks one before saving. A title or nickname that is too long is shortened, a missing
  one is filled in from the other, and a prompt with no `appliesTo` applies to both kinds. A
  model, effort level, or extra flag the editor would refuse is left out, as is a setting for an
  agent this version of the app doesn't know, so a file can never loosen a session's permissions
  (with `--dangerously-skip-permissions`, `--sandbox`, or a Codex `-c sandbox_mode=...`, say).
  Text with problems is kept, to be fixed in the editor before it launches. A reply whose `action`
  this version doesn't know becomes a reply for comments, and its launch settings are dropped. A
  reply other than a comment reply, or one whose `appliesTo` is missing or unknown, applies to
  pull requests; a version of the app from before comment replies for issues reads every reply
  so, which leaves it for pull requests alone rather than misreading it.
  Each change is shown before you import.
- **The file's defaults are offered, never taken unasked.** An import lists each of the file's
  `defaults` that would change your library's (the default prompt for pull requests or for
  issues, or one of the default replies), but only where its prompt fits the slot: an agent
  prompt that applies to that kind of item, or a reply for that review that applies to pull
  requests. Each has a box, ticked to start with, and says what it replaces. A default follows
  its prompt through the import, to the free nickname a prompt is renamed to, or to the prompt
  your library already holds when it is left out as already there. A default whose prompt you
  untick isn't imported, and one your library already has isn't offered. In a
  [backup](backup-files.md), the prompts section takes its defaults as the import's mode says.
- **Files are at most 8 MB.**

**Import from a file…** also reads a copy of the app's database file, such as a `nilbox.db`
restored from a backup, whichever version of the app wrote it, without changing it. The
databases the app keeps beside its own (`nilbox.db.backup-…` and `nilbox.db.broken-…`) are
listed under **Restore from a backup…** instead.

<br>

## Older versions

**Version 2** had no review replies: every prompt in it is an agent prompt, and it has no
`purpose`, `action`, or `defaults.replies`. It still imports, as agent prompts. An app that reads
version 2 refuses a version 3 file, since it would import replies as prompts a launch could use.

**Version 1.** Before the app launched more than one coding agent, a prompt kept one model and one list of
extra arguments, both Claude Code's:

```json
{
  "model": "opus",
  "extraArgs": ["--verbose"]
}
```

A version 1 file still imports: its `model` becomes `models.claude`, and its `extraArgs` list
becomes `extraArgs.claude`. The app always writes version 3, and a backup of the database from
before either change reads the same way.
