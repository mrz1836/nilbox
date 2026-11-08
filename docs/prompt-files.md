# Prompt files

A prompt file holds prompts in a portable form, so you can move them between Macs, share them,
or keep them in your own backups. The app writes one when you export prompts, reads one when
you import, and keeps one beside its database as `prompts.json`, a copy of your whole library
that it rewrites after every change. How the app uses that copy is in
[Keeping your prompts](desktop.md#keeping-your-prompts).

A prompt file is UTF-8 JSON. The app writes every field, so a file you open shows every
setting a prompt has, and you can edit one by hand.

<br>

## Example

```json
{
  "format": "nilbox.prompts",
  "version": 1,
  "exportedAt": "2026-11-08T17:00:00Z",
  "app": "v0.4.0",
  "defaults": {
    "pr": "Review",
    "issue": "Work on it"
  },
  "startersAdded": ["review-pr", "work-on-issue", "quick-look", "fix-checks"],
  "prompts": [
    {
      "name": "Look for security problems",
      "nickname": "Security",
      "appliesTo": "any",
      "body": "Look for security problems in {{.URL}}\n",
      "model": "opus",
      "effort": "high",
      "planMode": true,
      "worktree": null,
      "extraArgs": ["--verbose"],
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
| `version` | The format's version, `1`. |
| `exportedAt` | When the file was written, as RFC 3339 in UTC. |
| `app` | The version of the app that wrote it. Informational only. |
| `defaults` | The app's default prompt for pull requests (`pr`) and issues (`issue`), each by nickname among the file's prompts, or `""`. |
| `startersAdded` | The starter prompts the library had been given, by key. Restoring a library from the file keeps a starter you deleted from coming back. Only a whole-library file fills it in. |
| `prompts` | The prompts, at most 1,000. |

Each prompt:

| Field | Meaning |
|---|---|
| `name` | Its title, up to 80 characters on one line. |
| `nickname` | The short label the launch button shows, up to 20 characters. Nicknames are unique in a library, ignoring case. |
| `appliesTo` | `pr`, `issue`, or `any`. |
| `body` | The prompt's text: a Go [text/template](https://pkg.go.dev/text/template) using the variables in [Prompts](desktop.md#prompts). |
| `model`, `effort` | The model and effort level it launches with, or `""` to inherit them. |
| `planMode`, `worktree` | `true` or `false` to turn plan mode or the worktree on or off, or `null` to inherit. |
| `extraArgs` | Extra arguments for the agent, one per item, added after the profile's and the launch defaults'. |
| `starterKey` | The starter it came from (`review-pr`, `work-on-issue`, `quick-look`, `fix-checks`), or `""` for a prompt someone wrote. |
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
  model, effort level, or extra flag the editor would refuse is left out, so a file can never
  loosen a session's permissions (with `--dangerously-skip-permissions` or `--permission-mode`,
  say). Text with problems is kept, to be fixed in the editor before it launches. Each change is
  shown before you import.
- **Files are at most 8 MB.**

**Import from a file…** also reads a copy of the app's database file, such as a `nilbox.db`
restored from a backup, whichever version of the app wrote it, without changing it. The
databases the app keeps beside its own (`nilbox.db.backup-…` and `nilbox.db.broken-…`) are
listed under **Restore from a backup…** instead.
