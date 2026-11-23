# Backing up your setup

**Settings → Back up and restore** saves your whole setup to one file, to keep or to bring to
another Mac, and brings it back. The file is a [backup file](../backup-files.md), plain JSON
readable only by you.

**Back up your setup → Export…** asks which categories to include, all ticked to start with, and
where to save the file (`Nilbox backup <date>.json`):

| Category | Holds |
|---|---|
| **Settings** | Every setting but the window's memory: the agent and profile defaults, launching, worktrees and branch rules, trusted bots, snoozing, sensitive files, notifications and banners, the GitHub notification rules, refreshing, reviewing, appearance, the inbox's sorting, and the integrations' switches, options, and chosen copies |
| **Repositories** | The clone folder, the other repository locations, and the pinned and muted repositories and orgs |
| **Prompts & replies** | The whole [prompt library](prompts.md), its defaults and the record of deleted starters included |
| **Launch defaults** | Every org's and repository's [launch defaults](launch-defaults.md), naming prompts by nickname |
| **Your scripts** | Your own [scripts](repo-scripts.md), in their order |
| **Autopilot routines** | Your [Autopilot](autopilot/README.md) routines, in their order, never their history. A routine an import adds or saves over arrives switched off; one already the same stays as it is, on or off |

A backup never holds your GitHub sign-in (the app never stores one), caches and mirrors, history,
per-item triage, agent profiles (found on disk at every start), script trust (each repository
command asks again before its first run), the window's memory, or the tools' detection.

**Restore from a backup → Import…** opens a file and shows, for each category it holds, what
importing it would do: what it adds, changes, leaves as it is, and, under Replace, deletes, each
item listed, and notes on anything left out and why. Untick a category to leave it as it is.
Then choose how to treat what's on this Mac:

| | **Merge** (the default) | **Replace what's here** |
|---|---|---|
| **Settings** | Each setting the file holds is set; one it lacks is left as it is | The same |
| **Repositories** | The clone folder is set if it exists here; the file's locations that exist here are added; each pinned or muted target is set, the file's rule winning | The clone folder likewise; the locations are exactly the file's that exist here; the pinned and muted lists are exactly the file's |
| **Prompts & replies** | As **Import from a file…**: what you have is left out, a taken nickname is renamed, an unchanged starter is saved over; the file's defaults are set where they fit | Prompts are matched by nickname: each match is saved over, the rest added, and every other prompt deleted, but one a launch default you keep still names, which is kept with a note; the defaults become the file's |
| **Launch defaults** | The file's row for an org or repository replaces this Mac's; the others stay | Exactly the file's rows |
| **Your scripts** | A script identical to one here is skipped; the others are added at the end, in order | Exactly the file's scripts, in its order |
| **Autopilot routines** | Routines are matched by name: one the file holds as it is here is left alone, on or off; any other is saved over and switched off, and one this Mac lacks is added at the end, off | The same, in the file's order, and every other routine is deleted; a routine a newer Nilbox made is never saved over or deleted, under either mode |

Replace asks again before it imports, naming what it will delete in each category. Merge never
deletes anything.

Every value is checked as Settings, the prompt editor, and the script editor check one, and
anything this Mac can't take is left out with a note: a setting this version refuses, a profile
that isn't here, a copy of a tool that isn't a program here, a location that doesn't exist, or a
prompt a launch default names that isn't in the library (the launch default then inherits it).

Before it changes anything, an import saves your setup as it stands to
`backups/nilbox before import <UTC time>.json` in the [data directory](data-location.md), and
says where once it is done; the five newest are kept, so an import you regret is one more
import away. The import itself is all or nothing: every category is applied together, and a
failure in any leaves every one as it was. It reads the file again first, and refuses one that
changed since you opened it.
