# The window

A single toolbar runs beside the macOS window controls. It holds the **Inbox**,
[**Notifications**](notifications-page.md), [**Autopilot**](autopilot/README.md), **Worktrees**, and
**Prompts** tabs, and
[**Scripts**](scripts-tab.md) while mage-x or go-pre-commit is on, or you turned Scripts on,
and, on the right, the page's [filter](filtering.md), the
[notification](menu-bar.md) bell, the [GitHub notifications](github-notifications.md)
button, the API budget gauge, and your profile picture. Drag the bar to move the window, and double-click it to zoom. As the window narrows,
the filter gives way first.

**Tab counts.** **Inbox** counts the items in your lists, leaving out what is snoozed or
dismissed. Each item is in one list, so the two lists' counts add up to it.

**Pushed since your review.** Submitting a review takes your review request off a pull
request, so one whose author pushed after you requested changes, without asking you again,
would leave your inbox. A fourth search finds the open pull requests you reviewed, each with
your own standing verdict and its head commit, at no extra cost: when your verdict requested
changes at a commit other than the head, the pull request is in **Review requested**, its
card's story **Re-review** and its track's review step ↻, and arrives with a banner once, when
the push is new. Nilbox
writes nothing to GitHub for it unless you click **Re-request my review** in its review sheet,
and it never dismisses a review. **Remove me as reviewer** isn't offered, since there is no
request to take yourself off; reviewing it again from the
[review sheet](reviewing-a-pull-request.md) takes it out at once. The author asking you again,
or you asking GitHub for your own review again with **Re-request my review**, makes it an
ordinary request, whose story then reads **Review re-requested**; the re-request does so at
once, with no extra read. The story goes by who the pull request asks to review, as its details
last read it, so a refresh whose search hasn't caught up with the request keeps it. Any move of
the head by someone else counts: a push, a rebase, or someone else's **Update branch**.
Commits you pushed yourself never do, such as a merge of its base you pushed to fix its checks:
the author still owes the changes, so the pull request stays out of Review requested, untagged,
and its review sheet offers no **Re-request my review**. Nilbox tells your commits apart by
their author, in one small read (1 GraphQL point) only for a pull request whose head moved past
your verdict, once per push; a read that fails leaves it pushed to, and is tried again on the
next refresh. Its [review sheet's](reviewing-a-pull-request.md) **Commits** tab shows the commits
since. **Notifications** counts what is unread: the app's own notifications, the bell's, and,
while Nilbox reads them, your [GitHub notifications](github-notifications.md); hover it for each,
as **3 unread in Nilbox · 12 unread on GitHub**. **Autopilot** counts the agents its routines
have working, with an amber dot while something waits for you and a red dot while a routine is
broken or its search fails; a small dot before its name glows softly while Autopilot runs your
routines and breathes while an agent works. **Worktrees** counts the linked worktrees on
disk across every clone in the repository
locations, main worktrees aside, with an amber dot while some are done on GitHub but still on
disk.
**Scripts** counts the runs going, with a red dot after a run failed until you open it, and an
amber dot while a tool that's on can't be used. Hover a tab for what its number or dot means.
**Prompts** has no count.

**Account menu.** Click your picture for who is signed in, **Settings** (also ⌘,), **View
GitHub profile** (or click your name or picture at the top), and **Service status**, a submenu
with each service you watch and where it stands, each opening its status page in your browser,
for when GitHub, Claude, or Codex is having an incident and the inbox, reviews, checks, or agents
lag, and **Choose what to watch…**, which opens Settings → Service status. Below them, **Report a bug** opens a
GitHub issue with your versions, settings, and integrations filled in, as Settings → About's
button does, and **Send feedback** opens a blank issue with a `[Feedback]` title, a few prompts,
and your version filled in. You read either on GitHub before you send it. After a refresh, the picture is downloaded from
`avatars.githubusercontent.com` without your token and cached in the database, so it shows
offline. It is downloaded again only when GitHub reports a new picture, such as after you
change it or sign in as another account with `gh auth switch`. Until it arrives, the button
shows your initial.

**Service status.** Nilbox checks the status pages of the services it depends on,
[GitHub's](https://www.githubstatus.com/), [Claude's](https://status.claude.com/), and
[OpenAI's](https://status.openai.com/) (Codex), once after it opens and then every 30 minutes.
**Settings → Service status** sets how often (or turns the check off), turns each service on or
off, and ticks the parts of each that you care about. To start with, those are what Nilbox and
the agents it launches use: GitHub's Git Operations, API Requests, Issues, Pull Requests, and
Actions; the Claude API and Claude Code; and the Codex API, Codex CLI, and Responses. Only a
part you watch counts, so an incident with GitHub's Copilot or Codespaces stays quiet, and so
does an incident that names no part, unless it is a major or critical one. Each check is one
small request to the service's own status page, outside any API, so it costs no budget and sends
no token or anything of yours; GitHub's and Claude's pages answer a check with nothing new for
free, and ticking a part needs no request at all. While a part you watch has a problem, a warning
icon shows just before the budget gauge: amber for degraded performance or maintenance, red for
an outage. Click it for a card per service in trouble, with the parts affected, the incidents
(each opens its page) and where they stand, how many other incidents don't touch what you watch,
when it was checked, **Check again**, and **Choose what to watch**. The icon goes away once every
part you watch is working. A check that can't reach a page keeps the last reading and says so.
A banner (**Service status changes** in Settings → Notifications, on by default) tells you when
a part you watch goes down, and again when it's back; the first check after Nilbox opens, or
after you change what you watch, never shows one.

**API budget gauge.** The dial follows whichever GitHub budget has the least left: GraphQL,
which the inbox spends, or REST and search, which `gh` and your agents spend from the same
account. Its needle points right when the budget is full and left when it is empty, and its
color follows the share left:

| Color | Share of the budget left |
|---|---|
| Green | More than 50% |
| Yellow | More than 25%, up to 50% |
| Orange | More than 10%, up to 25% |
| Red | 10% or less |

Click it for each budget's remaining points, when it resets, and who used it, and for what the
inbox's own refreshes cost. Once Autopilot's [routine searches](autopilot/routine-searches.md) or its own
reads spent something since the GraphQL budget last refilled, one line under the refreshes says
how much, such as **Autopilot: 12 GraphQL points since it last refilled (<1% of the budget)**;
there is no such line while it spent nothing. The line counts the routines' searches, the
details they read, and a routine's tests; the rounds that follow running checks count apart,
the watched pull requests a routine follows among them, since one request reads them beside
the inbox's own.

**How the readings stay current.** GitHub says what is left of a budget on every response it
sends, so the gauge follows every request the app makes, without making one of its own: an inbox
refresh, a single card's refresh, a check of a pull request's files, and a worktree sweep alike.
While the panel is open, it also reads every budget from GitHub's rate limit endpoint, which
costs nothing, as it opens and every 30 seconds after, so what `gh` and your agents spend shows
as you watch. **Live · read just now** says the newest reading is fresh. A budget whose reset
time has passed counts as full again.

**Who used it.** Each budget's row says what the app itself spent from it since it last
refilled, and what everything else signed in as you did, such as **Nilbox 45 · everything else
62**. When the app opened after the budget last refilled, it can't know what an earlier copy of
itself spent, and says **Nilbox 3 since it opened** instead.

**What the refreshes cost.** **Inbox refresh** shows what the last whole refresh spent of the
GraphQL budget and how many items it tracked, and what that comes to in an hour at the interval
set in **Settings → Refreshing** (every five minutes by default), as a share of the budget. Each refresh reads every tracked item, so more
items, or issues read with their [project fields](inbox-cards.md#issue-projects), cost more.

**Checks that are running.** A pull request whose checks were still running (a moving dot on
its card's track) when the inbox last read it is read again every 45 seconds, so its card shows
the checks end about as soon as GitHub's own page does, rather than at the next whole refresh.
They are read 25 to a request, the requests side by side, about one point each, at most 50 a
round (your own cards first, then those a routine follows), and none is read while no checks
run: at most some 160 points an hour, and only while checks run. One stops being read as soon
as its checks end, passed or failed, and a dismissed or snoozed one isn't read at all.

**After a merge.** A merge into a branch can give every other open pull request on it
conflicts, or put it behind, and GitHub works that out again only once asked. So about ten
seconds after Nilbox learns of a merge (yours from the review sheet, an Autopilot routine's, or
one a read of the inbox finds, as an auto-merge), it reads the repository's other open pull
requests on the same branch again, 25 to a request, and their cards show their conflicts without
waiting for the next whole refresh. While GitHub is still working one
out, it reads that one again 30 seconds later, three rounds at most. A pull request read since
the merge, dismissed, snoozed, or in a muted repository is left alone. It costs about a point
per 25 pull requests a round, counted with the running checks', and nothing while no merge
happened.

**How often.** **Settings → Refreshing** sets each interval: the inbox (every 2 minutes to every
hour, five minutes by default), the running checks (off, or every 30 seconds to every 2 minutes,
45 seconds by default), the worktrees' sweep (every 5 minutes to every hour, five minutes by
default), the [GitHub notifications](github-notifications.md) (every minute to every hour, five
minutes by default, and never sooner than GitHub asks). **Settings → Service status** sets how
often the [service status](#the-window) check runs (off, or every 5 minutes to every hour, 30
minutes by default; it costs no budget). A change applies at once: a refresh already due under the new interval starts
straight away, and otherwise the next waits the new interval from the last one's end.

**Pushes from this Mac.** Push to one of your inbox's pull requests from a clone on this Mac,
whatever runs the push (a terminal, an editor, an agent), and its card catches up within
seconds, for a point, rather than at the next refresh. Git runs nothing after a push, but a push
that lands moves the clone's own copy of the branch on origin (`refs/remotes/origin/<branch>`).
Every 3 seconds the app looks at that file for each open pull request whose branch is in its own
repository, in each clone whose origin is that repository, a stat each. Once the branch moves to
a commit GitHub hasn't shown for the pull request, the app reads the pull request again 2 seconds
later, once GitHub has taken the push in, and again after 10 and 30 seconds while GitHub still
shows the old head. A fetch that brings someone else's push catches the card up the same way. The
app reads only the files git keeps, runs git only to read a clone's origin (once, and again when
the clone's config changes) and the branches of a clone that keeps its refs in reftable, and
installs nothing: no hook, no setting. It doesn't see a push from another machine or made on
GitHub, a push to a URL rather than to origin, a fork's branch, or a pull request dismissed,
snoozed, or in a muted repository; the refresh brings those. **Settings → Refreshing → Notice pushes from this Mac** turns
it off.

**Changes on GitHub.** The panel also counts the [edits](item-menu.md)
Nilbox made on GitHub for you in the last hour, says how it paces them, and, while GitHub has
asked it to pause, when the pause ends.

**Housekeeping on GitHub.** Below it, the [GitHub notifications](github-notifications.md) Nilbox
marked done or read for you in the last hour, out of the most it clears in an hour. They are
paced apart from the edits, with the same reserve, and a pause GitHub asks for holds both.

**Kept across restarts.** The readings, what the app spent, and the last hour's edits are saved
a second after each change, and once more as the app quits, and come back when it opens. The app spends nothing
while it is closed, so the count it picks up still covers all it spent. Every GitHub request the
app makes goes through the one function that reports to the gauge; a repository test fails the
build for any code that would send one another way, or run `gh api`.

**Theme.** Settings → Appearance offers **System**, **Light**, and **Dark**. System follows
your Mac's appearance and switches when it does; Light and Dark stay put whatever the Mac does.
A choice shows at once: the window cross-fades to the new palette, and the picked option's icon
turns in (with **Reduce motion** on, the switch is instant). The app opens in the theme you
chose: its native appearance is set before the window appears, so the first frame is already in
that palette, and the window frame, menus, and pop-up lists match the theme rather than the
system.

**Text size.** Settings → Appearance offers **100%**, **115%**, and **130%**, apart from the
window's size. A larger size scales the text and the spacing around it together, the way a
browser's font size setting does, so nothing clips or crowds; the toolbar beside the window
controls keeps its height, and borders stay hairline-thin. A choice shows as soon as it is saved,
and the app opens at the size you chose: the window's first frame is already at it. The
smallest the window can be grows with the text, from 800 × 520 at 100% to 920 × 598 at 115% and
1040 × 676 at 130%, so the layout is never narrower than at 100%; picking a larger size grows a
window smaller than that, and every window size below clears it. The frontend sizes all text in
rem, never px, so it follows the setting; a repository test fails the build for text sized in px.

**Window size.** Settings → Appearance also sets the size the window opens at: **Compact**
(1100 × 720), **Standard** (1280 × 800), **Large** (1440 × 900, the default), or **Extra
large** (1680 × 1050). Choosing one resizes the window at once. A size larger than the screen
opens as large as the screen allows.

**Celebrations.** Settings → Appearance also sets how the app celebrates: **Milestones** (the
default), **Quiet**, or **Off**, each described under [Inbox nil](inbox-nil.md). The row says what
the chosen one does, and the next party follows it.

**Settings** is a page of its own. Its sidebar lists the sections in the order the app is used:
your account, appearance (the theme, the text size, the size the window opens at, and the
[celebrations](inbox-nil.md)),
[notifications](menu-bar.md) (the menu bar icon and which banners show),
[GitHub notifications](github-notifications.md) (whether the app reads them, and when they are
cleared), and refreshing (how often the inbox, the running checks, and the worktrees are read again); then,
under **Inbox**, snoozing (how long the [Snooze](snooze-dismiss-pin.md) button sets an item
aside, and what [wakes a snooze early](snooze-dismiss-pin.md#waking-a-snooze-early)), cards (the labels the
[cards](inbox-cards.md) leave out), and reviewing (the merge method and your
[review replies](prompts.md#review-replies)); under **Agents**, in the order you set them up,
repositories (the clone root, the other [repository locations](launching-a-session.md#repository-locations), and the
[launch defaults](launch-defaults.md) for orgs and repositories), [coding agents and
profiles](agents-and-profiles.md), launching (the terminal, the default coding agent, each
agent's default profile, the default prompt for pull requests, for issues, and for each
situation a pull request can be in, the worktree default, following agent sessions, and whether an agent may sign in with an [API key from your
shell](launching-a-session.md)), trust (the [trusted bots](outside-contributors.md)),
[sensitive files](sensitive-files.md), and worktrees (whether to [clean up worktrees after
merging](worktrees-view.md), which branches go with them, and protected branches); under
**Tools**, [integrations](tools.md) (mage-x, go-pre-commit, and
Scripts); **Labs**, the parts still being built, each off until you turn it on
([Autopilot (beta)](autopilot/README.md), whose own section, under **Autopilot**, shows only while it
is on); and the app's version. Each change is saved as you make it, except the clone root, which its **Save**
button saves (or **Choose…**, as soon as it picks a folder), and launch defaults, which are saved
with theirs. Escape or the back arrow returns to the view you came from.
