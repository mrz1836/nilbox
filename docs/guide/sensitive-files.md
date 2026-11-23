# Sensitive files

Reviewing a pull request from outside the project starts with its list of changed files: a
change to a workflow, a build script, or an agent's settings can do harm long before anyone
reads the code. The app does that first look for you. It checks the pull requests of:

- every fork, every outside contributor, and every bot not on the trusted list, always;
- collaborators (people with access to the repository who are not its owner or members of its
  org), unless you choose **Outside contributors** under **Settings → Sensitive files**.

The repository's owner and members, the trusted bots, and you are never checked, so
Dependabot's `go.mod` updates stay quiet. Whoever opened it, a pull request that one of
Autopilot's agents committed to is checked for the files the agent's commits changed, at the
heads it had and left, and holds the next launch there when they include agent configuration
([Fences](autopilot/fences.md)).

**What counts.** A changed file is sensitive when its path, or a renamed file's previous path,
matches a pattern of one of these categories. A file counts once, under the first category that
matches it:

| Category | Color | Examples | Why |
|---|---|---|---|
| Agent configuration | Red | `.claude/`, `.codex/`, `.gemini/`, `.mcp.json`, `CLAUDE.md`, `AGENTS.md`, `.cursor/` | Coding agents load them. Hooks run commands as soon as a session starts, even in plan mode. |
| Workflows and CI | Red | `.github/workflows/`, `.github/actions/`, `action.yml`, `.goreleaser.yml` | They run on CI with the repository's token and secrets. |
| Build and dev tooling | Red | `Makefile`, `magefiles/`, `*.sh`, `.husky/`, `.envrc`, `.vscode/`, `Dockerfile`, `package.json` | They run on your machine when you build, test, install, or commit, or open the project in an editor. |
| Review and merge rules | Amber | `CODEOWNERS`, `.github/dependabot.yml`, `renovate.json`, `.mergify.yml` | They decide who must review, and what updates or merges on its own. |
| Dependencies | Amber | `go.mod`, `go.sum`, lockfiles, `vendor/`, `.gitmodules` | They bring in third-party code, or change where it comes from. |

**Settings → Sensitive files** lists every pattern of each category behind its pattern count,
turns any category but agent configuration off (launches are guarded by that one), and takes
patterns of your own, flagged in red, such as `infra/` or `*.tf`. Patterns work like
`.gitignore`, ignoring case, since macOS opens `.Claude/Settings.json` for
`.claude/settings.json`: a name alone matches at any depth, one with a slash matches from the
repository's root, a trailing slash matches a folder's contents, and `**` matches any number of
folders. A pattern that would match every file, or an exclusion (`!`), is refused with the
reason. **Try a path** says whether a path would be flagged, and by which pattern. A change
there shows on every card at once, without asking GitHub.

**On the card.** The chip beside who opened the pull request names the categories it touches,
as in **Agent config · Workflows**, red when any category is red. A pull request whose files
could not all be checked shows an amber **Files not checked** or **Not all files checked**; one
pushed since its last check, **Files not checked yet**; and one whose every file was checked
with nothing found, a quiet checked shield, so "checked, nothing found" never looks like "not
checked". Click the chip for the files by category, each opening its diff on GitHub, a moved
file with the path it left (moving a workflow away disables it), the commit they were checked
at, and **Check now**.

**How the files are read.** A refresh checks a pull request when its head commit has moved since
its last check, or the last check failed, and otherwise reuses it, so most refreshes check
nothing. The paths come from GitHub's REST API, which, unlike GraphQL, says where a renamed file
came from: one request per 100 files, between two readings of the head commit, so the list
belongs to one commit. A refresh checks at most 20 pull requests, 4 at a time, within 30
seconds; the rest show **Files not checked yet** until the next one. A check that fails never
fails the refresh: its card says so, and the next refresh tries again. GitHub lists at most
3,000 files, so a larger pull request is marked as not fully checked. A check stores only paths,
never content, and is removed when its item leaves the inbox. (The [review
sheet](reviewing-a-pull-request.md) keeps the diffs of the pull requests you open in it, in its own
bounded cache on this Mac; when it reads a pull request's files, that list serves as the check
too, so one request does both.)

**Opening a session.** Launching on a checked pull request checks its files against GitHub again
at that moment, one request when nothing was pushed. When it changes agent configuration, nothing
starts: a dialog lists those files, each opening its diff, and says when the agent would load
them, at once in the pull request's worktree, or once a session checks it out (in that session
and every later one in the same folder). Nothing is fetched and no worktree is made until you
choose. **Cancel** is the default; **Open anyway** confirms that exact commit, so a push in
between asks again. A pull request whose files could not all be checked is held back the same
way, since it may change agent configuration unseen. The other categories never hold a launch
back.

Why it matters: the app marks the folder each session starts in trusted for the agent (see
[Workspace trust](launching-a-session.md#workspace-trust)), and plan mode limits what the agent does but not what a
project's hooks do. A `SessionStart` hook added only on a pull request's branch runs as soon as a
session starts in a worktree at that branch.

**In the prompt.** The prompt of a session on a pull request with sensitive files ends with a
line no prompt can leave out, after the outside-contributor line: "This pull request changes
sensitive files, which can run on CI or on this machine, or steer a coding agent:
.claude/settings.json (Agent config), .github/workflows/ci.yml (Workflows). Review those changes
first and most closely." Prompts can also use `{{.SensitiveFiles}}`.

**What it can't see.** Paths alone can't show what a change does inside an ordinary file, such
as a test that runs during a build, or a `go:generate` line. The check is a first look that
points the review at the riskiest files, not a review.
