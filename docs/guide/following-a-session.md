# Following a session

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/sessions`](../architecture/sessions.md)

Every launch starts an agent session that the item's card follows until the agent quits:
what it is doing, how long it has been at it, and what it last said. You keep your own
terminal; the app only reads.

## Its ID

Before the terminal opens, the app mints the session an ID of its own. Claude Code takes it as
`--session-id`, so the session is linked from its first moment. Codex takes no ID: once the session
starts, the app finds its thread among the ones Codex lists for the item's folder, created after the
launch, preferring the one whose first message is the prompt the launch sent.

## On the card

The agent's button beside the card's next step follows the newest running session, or one that ended
in the last day, unless the pull request has moved past it since: the review it left was on an older
commit, or your review was requested again after it ended, as when the author pushes and asks again,
or you re-request your own review. Then the button offers a fresh launch instead. While it starts or
works, a pill with a slowly moving border, such as **Claude
working 2m**, that opens the item's sessions, with **Show in terminal** (or **Watch**, for a
routine's run in the background) beside it; waiting on you, the card's story says so (**Claude needs
approval · Bash: go test**) and its next step is **Show terminal**, breathing, or **Watch**; its
turn done, **Claude’s done · 2m**, which shows its terminal; ended, **✦ Findings · 2m**, which opens
the item's sheet, where the agent's verdict shows what it last said; failed today, the next step is
**What happened**, which opens the sessions, and a day later the card is back to its own next step,
with a fresh launch beside it. While an agent works, the card's story says **Claude is on it**, and
your own next step stays beside it, quiet. The [Worktrees view](worktrees-view.md) shows how the
session stands on its worktree's row, where its agent goes, such as **Claude 2.1.328 · needs
approval · 2m**.

| State | What it means |
|---|---|
| Starting | Launched, and the agent hasn't reported the session yet |
| Working | The agent is working on its turn |
| Needs approval | The agent waits for you to allow an action, such as a command |
| Needs input | The agent asked you something, or a dialog of its own is open |
| Idle | The agent finished its turn and waits for your next message |
| Failed | The agent reported an error it couldn't get past |
| Status unavailable | The session runs, but the agent couldn't be asked how it is |
| Ended | The agent quit |

## The panel

Click the pill for every session of the item, newest first: its agent, profile, and prompt, what it
is doing and since when, the permissions it started with and where it runs (**full access · new
worktree**, or **in the clone**), what came of the item on GitHub while it ran, how many tokens it
used, the folder, anything its launch worked around, and the start of what the agent last said. Each
has its actions:

- **Show in terminal** brings forward the tab the session runs in ([below](#show-in-terminal)).
- **Resume** reopens a session that ended, in the same folder and the terminal chosen in Settings
  → Launching, with `claude --resume=<id>` or `codex resume <id> <flags> --`, and the item's
  launch settings as they now resolve for the agent and profile it ran with. The item must still
  be in the inbox and its worktree still on disk; the resumed session is a new one on the card.
  It asks first, as a launch does, when a session is running in that folder, and when the pull
  request's [files](sensitive-files.md), as last checked, change agent configuration; it asks
  GitHub nothing. A conversation that already runs again, resumed from the app or outside it,
  offers no Resume.
- **Show in Finder** shows the folder the session runs in, its worktree or the clone, whether or
  not it still runs, and says so when the folder is gone.
- **Copy session ID** copies the agent's own ID, to resume or look the session up yourself.
- **×** (on a session that ended) removes it from the card and the panel, forgetting its
  record: Resume is no longer offered for it, and resuming it outside the app is no longer
  followed. The conversation itself stays in the agent's own files, so copy its ID first if you
  may want it. A session still running, or one that failed or whose status is unavailable,
  can't be removed. The card then follows the next session that ended within the day, or
  none.

## Show in terminal

**Show in terminal**, on the card (**Show terminal** while the session waits on you), in the
panel, and in the [menu bar](#the-menu-bar-and-banners), brings forward the tab the session runs
in, with its window, in the terminal it was launched in. It first reads the session's terminal
device (`ps -o tty=`), which shows the session still runs in a terminal; then each terminal
finds the tab its own way:

- **iTerm2 and Terminal.app** find the tab by that device, through AppleScript. iTerm2 uses the
  Automation permission you gave at the first launch; Terminal.app asks for it the first time.
- **Warp** has no AppleScript, so the app reads one environment variable, `WARP_FOCUS_URL`, from
  the session's agent process: the link (`warp://session/` and 32 hex digits) Warp exports in
  every shell it starts, which the agent inherits from its tab. It reads it only when you click,
  keeps and logs nothing else of that environment, and opens the link in Warp, which brings the
  tab's window forward and selects its pane. No permission is asked.

Warp exports the link from v0.2026.05.27 on, so a session launched in an older Warp can't be
shown, and Show in terminal says so: *Couldn't tell which Warp tab runs this session. Warp
v0.2026.05.27 or newer can; update Warp, and sessions launched afterwards can be shown.* macOS
hides the environment of its own programs, such as a shell, which reads the same way, but an
agent is never one of them. For about a minute after you close a Warp tab, Warp
[keeps it](launching-a-session.md#warp), and Show in terminal only brings Warp forward and says
nothing, since Warp can't tell the tab is gone.

## How it is read

The app asks each agent through the interface it supports for this, and never the terminal:

- **Claude Code:** `claude agents --json`, which Claude Code documents as the supported way to
  read session state from outside it, run once for each profile's config directory. Its
  `busy`, `waiting` (for a permission prompt, a sandbox request, input, or a dialog), and
  `idle` become the states above. While Claude sessions run, the app checks the timestamps of
  the config directory's `sessions/` folder every second and asks Claude Code only when one
  changed, or every 20 seconds. One listing that fails leaves the card as it was; only one that
  keeps failing for 30 seconds shows **Status unavailable**. When a session's own process
  moves on to another conversation, as `/clear` and `/resume` do inside Claude Code, the card
  follows the new one. The last message comes from the end of the session's transcript under
  the config directory's `projects/` folder.
- **Codex:** the local app-server daemon, through `codex app-server proxy`, the protocol
  Codex's own editor and desktop clients use. The app sends only read methods (`initialize`,
  `thread/list`, `thread/read`, `thread/turns/list`, `thread/loaded/list`); a test holds the
  client to that list. A Codex session runs on the daemon unless it was started with
  command-line config overrides, and then the daemon reports it live, approvals and questions
  included. Codex runs a session started with overrides, which a launch adds only when you turn
  on [Codex's effort or Fast mode](launching-a-session.md), in its own process instead: the
  daemon then knows only what the session recorded, so the card shows **Working** or **Idle**,
  never that it waits for you, and says why. When the daemon can't be reached, the session is linked through the first
  line of the rollout file Codex writes, and the card shows **Status unavailable** while it
  runs.

## What came of it on GitHub

The panel says the newest of what happened to the item on GitHub while a session ran: **On GitHub:
Approved at abc1234 · 2m ago**, **Changes requested at …**, **Merged**, or **Closed**. It comes from
what Nilbox reads anyway (the read of the item after a session stops working, a card's or the review
sheet's refresh, the whole refresh) and costs no request of its own. It counts what happened between
the session's launch and its end, by GitHub's time, give or take a few seconds, and never says who
did it: the agent with `gh`, or you in your browser or in Nilbox. A review counts only when it
approves or requests changes, and only one made within two hours of when Nilbox sees it.

## When the item is read again

Each time a session stops working (it goes idle, waits for you, ends, or fails), Nilbox reads its
item from GitHub again, one GraphQL point, so the card catches up with what the agent did. While the
session still works, it also reads the item as soon as the agent finishes a command that changes
something on GitHub: `gh pr review`, `merge`, `close`, `reopen`, `comment`, `edit`, `ready`, or
`update-branch`; `gh issue close`, `reopen`, `comment`, or `edit`; a write through `gh api`; or `git
push`, also behind `cd`, `timeout`, `env`, or a shell's `-c`. The first such command reads the item
at once, and any more within 30 seconds once more when they're up, so an approval and a merge a
moment later both reach the card, at most a point per item every 30 seconds. A command that failed,
or one still running in the background, doesn't count. Nilbox spots these commands in the agent's
own files on this Mac, never the network: only what Claude Code added to the session's transcript
since the last look (the commands from before the launch never count), and the commands in the turns
Codex's daemon reports. A Codex session the daemon can't reach gets only the read once it stops
working.

## Its tokens

The panel says how many tokens each session used, such as **1.2M tokens · 840K cached**, with every
count (in, out, read from the cache, written to it) in its tooltip, read from the agent's own files
on this Mac when the panel opens and again each time the session changes state, never from the
network. Claude Code's come from the session's transcript: each assistant message's usage, counted
once though Claude Code writes a line per part of a message, read only as far as the file grew since
the last read, and at most its first 64 MB (past that, the panel says **at least**). Codex's come
from the last token count in the end of its rollout file, the thread's running total. An agent's run
in the background (**⚡ in the background**) counts what its run reported instead, as Autopilot
records it while the run goes, its writes to the cache within what went in. A session the agent
hasn't named yet, or whose files report no usage, shows none. These are the agent's own counts, not
a bill: a subscription's sign-in spends its plan's usage, and nothing here turns tokens into
dollars.

Whether a session still runs comes from its process, checked against the agent so a process ID
macOS has since given another program never keeps a session alive: Claude Code must list the
session within 30 seconds, and a Codex session's process must still be `codex` in its folder,
checked every 30 seconds. Where Claude Code can't be asked, a session it never listed is taken
for running only while a Claude Code process works in its folder, never another program's. A
Claude session resumed outside the app within a day of ending, as with `claude --resume <id>`
in a terminal, is followed again from its card as a new session resumed from the first; the
app asks Claude Code about it only when its `sessions/` folder changes, as a session starting
changes it, never on a timer. Polling stops while no session runs, and
starts again with the next launch or the next time the app opens: sessions left running when
the app quit are reconciled then. Nothing is ever sent to a session, no approval is answered,
and nothing is written into an agent's config or conversation beyond the launch's
`--session-id`. Nothing about sessions leaves your Mac.

## One session per folder, unless you say so

While a session is still running on an item, its card's button is **Show in terminal** rather than
**Open in Claude**: getting back to it is the likely reason to click. Its worktree's row in the
Worktrees view offers no launch while something runs in the worktree. Starting another from the
launch options asks first when one is still running in the same folder (the item's worktree, or its
clone, whichever item that session was started on, since items share their clone): two agents
working in one folder can undo each other's changes and wait on each other's git commands. The
question offers **Show in terminal**, or **Start another**, which starts it beside exactly the
sessions it listed; one that started since asks again. A launch waiting on that question asks GitHub
nothing, and a pull request whose [files](sensitive-files.md) also hold it back asks about them
next. Only sessions the app follows count: a shell, an editor, or a dev server in the folder never
holds a launch back, and while following is off nothing does.

## The menu bar and banners

Sessions waiting for your approval or input are listed first in the [menu bar](menu-bar.md), under
**Waiting for you**, the longest waiting first and five at most, then how many more; click one to
bring its terminal tab forward, or the item when the tab can't be found. The menu is redrawn only
when what it lists changes. Two banners, **Sessions waiting for you** (once a session has waited
five seconds) and **Sessions finishing a turn**, start off, since Claude Code and Codex can already
notify through your terminal. Turn them on in Settings → Notifications.

## Turning it off

**Follow agent sessions** under Settings → Launching is on by default. Turn it off and no agent is
asked about its sessions, the cards and the menu show none at once, and no session banner shows.
Launches are still recorded, which costs nothing. Turned on again, the sessions still running are
followed at once.

## What is kept

Sessions are recorded in the local database (the `agent_sessions` table): every one still running,
and each ended one for 30 days, at most the newest 20 per item, unless you remove it sooner with its
**×** in the panel. The last message keeps its first 500 characters.
