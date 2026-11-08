# Performance

The first milestone set five targets before any further feature work: a fast cold start, a
fast inbox refresh, a fast agent launch, a small app, and a small share of the GitHub API
budget. This page records what was measured, on what, and how, so anyone can reproduce the
numbers with the `magex measure:*` targets.

<br>

## Results

| Metric | Target | Measured | Result |
|---|---|---|---|
| Cold start to first paint | < 1 s | 541 ms | Met on the M1 Max; the M4 Pro build host measured 1013 ms, at the target rather than under it |
| Inbox refresh | < 3 s | 2.30 s | Met once the inbox's items are known; the first refresh into an empty database takes 5–10 s |
| Launch to prompt loaded | < 3 s | Not recorded yet | Pending: the build host cannot grant the Automation permission unattended, so the number needs a run on the maintainer's Mac |
| App binary size | < 30 MB | 17.0 MB | Met |
| API budget at 5-minute cadence | < 2% | 1.20% | Met, for an inbox of 43 items |

Measured on an Apple Silicon Mac (M1 Max, 32 GB memory) running macOS 15.8, over its regular
network connection. The first numbers were taken on the build host, a second Mac (M4 Pro,
24 GB memory, macOS 15, with one 1920×1080 display at 60 Hz), which starts the app more slowly;
its runs are kept below for comparison. The refresh and API budget numbers are set by GitHub's
response times and by how many items the inbox holds, not by the machine. Each number comes
from the production bundle that `magex app:build` produces.

<br>

## Method

### Cold start to first paint

**What is measured.** The time from the moment macOS started the app's process (the start time
the kernel records for it) to the first frame of the app on screen. After React's first
render, the frontend waits for two animation frames (the second fires only once the first frame
has been painted) and reports to the backend, which logs `startup.first_paint_ms=<n>
source=os`. On macOS the window is shown only once its page has loaded, and animation frames
run only while it is visible, so the report marks content on screen, not a paint into a hidden
window.

**How.** `magex measure:startup` launches the bundle's binary with `--measure-startup` six times,
one after another; in that mode the app quits right after its report. The first launch of a
newly built binary includes macOS's one-time check of the new code signature, so it is reported
on its own. The table shows the median of launches 2–6, each a cold start: a new process, with
the app not running. The target also times each launch from outside the process, spawn to
timing line, which agrees with the app's own reading to within a few milliseconds. Each launch
uses the real data directory and starts the usual background refresh, as a normal launch does.
Run it with the screen unlocked: a window that is not on screen runs no animation frames, so on
a locked Mac the report never comes and the target gives up after a minute.

**Recorded run.** On the M1 Max (2026-11-07, a day before the size and refresh runs below):
first launch of the build 964 ms; launches 2–6 574, 541, 554, 523, and 537 ms, for a median of
541 ms.

On the M4 Pro build host: first launch 1453 ms; launches 2–6 930, 951, 1031, 1013, and
1057 ms, for a median of 1013 ms. An earlier run there measured 1491 ms for the first launch,
then 922, 1063, 922, 853, and 833 ms, for a median of 922 ms. Cold start on that host is
therefore at the 1 s target, not comfortably under it: runs vary by about ±100 ms, and 4 of
those 10 cold starts went over 1 s.

**Where the time goes.** A traced build (not committed) of a warm launch, in milliseconds from
process start:

| Step | Time |
|---|---|
| Process start to the Go `main` | 50–80 |
| macOS app launch, until the app reports it has started | about 250 |
| Creating the window and its web view | about 265 |
| Web view start-up, until it requests the page | about 130 |
| Loading the script and React's first render | about 130 |
| Showing the window and putting the first frame on screen | 90–110 |

The app's own start-up work (opening and migrating the database, discovering profiles) takes
about 10 ms. The rest is macOS's app launch and WebKit. Showing the window before its page
loads, and delaying the first background refresh until after the first paint, were both tried:
neither moved the median beyond the run-to-run noise.

**The theme setting.** Opening in the chosen theme added one step before the window opens:
reading the stored theme and setting the app's native appearance. Cold start was measured again with and
without it on the second Mac (M1 Max, 32 GB memory, macOS 15), three `magex measure:startup`
runs of each build, alternating, for 15 cold starts each. The medians were 552 ms without it
and 551 ms with it; single runs ranged from 519 to 584 ms either way, so the step costs nothing
measurable. These runs compare with each other and with the M1 Max run above, not with the
build host's.

### Inbox refresh

**What is measured.** One refresh as the app runs it: the token, which the first refresh
borrows with `gh auth token` (about 50 ms on the M1 Max, 250 ms on the build host) and later
ones reuse from memory for up to ten minutes; then, concurrently, the REST `/rate_limit`
request, the batched GraphQL inbox query, and the details query for the items the last refresh
listed; then a details query for any item new to the lists; then writing the account,
repositories, items, details, and rate readings in one database transaction.

**How.** `magex measure:refresh` runs the bundle's binary with `--measure-refresh`. In that mode
the binary opens no window: it starts the app's core over a scratch data directory, deleted
afterwards, and times five refreshes one after another, using the GitHub CLI login of whoever
runs it. Every request is a read. Running through the app binary measures the production build,
and it is also the binary a per-app firewall has to allow.

**Recorded run.** On the M1 Max (2026-11-08), with 43 items in the inbox: 4807, 2316, 2165,
2149, and 2296 ms, for a median of 2.30 s. Earlier runs on the same Mac, with at most 20 items,
measured 6935, 1782, 1363, 1490, and 1673 ms (a median of 1.67 s); 6414, 2565, 2418, 2543, and
2342 ms (2.54 s); and 10039, 3863, 2282, 2202, and 2391 ms (2.39 s). The spread is GitHub's:
two builds timed alternately in the same hour gave medians of 2.36 and 2.58 s, while the app's
own work in a refresh of a 100-item inbox, against a fake GitHub that answers at once (reading
what the last refresh listed, the database write, and the inbox the window and the menu bar
read back), takes about 16 ms (`BenchmarkRefreshCycle` in `internal/inbox`).

Each run starts from an empty scratch database, so its first refresh has no listed items to
read alongside the inbox query: the details of all its items wait for the inbox query to
answer, which added 2.5 to 7.5 s, once. Every later refresh read the listed items' details
alongside the inbox query. An existing database already lists its items, so its first refresh
is no slower: refreshing a copy of a real database through the same code took 2.57, 2.33, and
2.51 s.

Before the inbox cards read details, the build host recorded 2822, 3419, 2693, 2328, and
2943 ms, a median of 2.82 s, and three earlier runs of five had medians of 2.99, 2.73, and
2.81 s.

**Where the time goes.** Timed separately with `curl` on the build host, GitHub took 2.6–3.0 s
to start answering the batched query, and each of its three searches 1.2–1.7 s on its own;
GitHub runs them partly in parallel, and its latency moves from hour to hour, so later
refreshes on the M1 Max took anywhere from 1.4 to 3.9 s. The REST request takes about 0.3 s
and overlaps the query, and the database write takes a few milliseconds. The margin under 3 s
is set by GitHub's search latency, not by the app, and it may differ for accounts with more
matching items.

GitHub answers the details query at roughly 150 ms per item, one item after another within a
request: 20 items in one request took 3.2–4.0 s, while two concurrent requests of 10 took
1.9–2.2 s. The app therefore sends batches of at least 10 items, up to 4 at a time, and reads
the listed items' details alongside the inbox query, where they finish first.

### Launch to prompt loaded

**What is measured.** The time from the launch request to the agent running in the clone with
its prompt: an agent process whose working directory is the clone (or one of its agent
worktrees) and whose first argument is the starter prompt.

**How.** `magex measure:launch` creates a throwaway clone whose `origin` is the placeholder
repository `acme-org/example-repo`, without fetching anything. It stores one pull request from an
outside contributor, so the session starts in plan mode, and launches it without a worktree
through the real launch service and iTerm2. A launch on an outside contributor's pull request
first checks its [sensitive files](desktop.md#sensitive-files) on GitHub; the placeholder's
cannot be checked, so the launch is held back for confirmation, and the target confirms it at
once and launches again, as a user would. The time includes both requests, so it is an upper
bound for a pull request whose files check out. It then checks the process list (`ps`) and each new
agent process's working directory (`lsof`) every 100 ms, and stops the agent as soon as it is
found. The clone and the scratch data directory are deleted afterwards.

**Result.** Not measured on the build host. Its build session cannot answer macOS's one-time
prompt to let a process control iTerm2: asking macOS without prompting
(`AEDeterminePermissionToAutomateTarget`) returned "would require user consent" (-1744), so a
live run there would only leave the permission dialog on screen. No number has been recorded
yet: it is to come from `magex measure:launch` run from iTerm2 on the maintainer's Mac. When
the permission is missing, the target prints `NEEDS AUTOMATION PERMISSION` and exits without a
number.

### App binary size

`magex measure:size` reads the size of the production executable,
`bin/Nilbox.app/Contents/MacOS/nilbox`: 17,013,376 bytes, or 17.0 MB in decimal megabytes. The
whole bundle, the executable with its icon set and `Info.plist`, is 18.3 MB. The binary is
built with `-tags desktop,production -trimpath -ldflags "-s -w"` and embeds the built frontend,
about 620 KB without source maps: the 420 KB script the first paint needs (135 KB gzipped), the
Settings, Prompts, and Worktrees pages in scripts of their own (about 150 KB together), loaded
the first time each opens, and 54 KB of CSS.

An earlier build of the same features, with an ORM over its database, measured 17,791,344
bytes (17.8 MB), with a 19.1 MB bundle and a frontend of about 510 KB in one script. The first
desktop build, before the card details, the prompt library, snoozing, the app icon, worktree
cleanup, the sensitive-file check, and the menu bar, measured 16,446,784 bytes (16.4 MB), with
a 16.4 MB bundle and a frontend of about 280 KB.

### API budget at 5-minute cadence

Every refresh recorded the GraphQL point cost GitHub charged for its queries
(`rateLimit.cost`, summed): 5 points, 1 for the batched inbox query and 1 for each of the four
details requests that read the inbox's 43 items. At the scheduler's cadence of one refresh
every five minutes, that is 12 refreshes, and 60 points, per hour, out of the 5,000-point
hourly GraphQL budget GitHub reported: 1.20%. With 20 items, a refresh cost 3 points, 36 an
hour (0.72%). The REST `/rate_limit` request does not count against GitHub's rate limits.

The details cost grows with the inbox: one request per 10 items up to 40 items, then four
larger requests up to 200 items, then requests of 50 items; each costs a point, or two once it
carries more than about 25 items. Asking for
the same details inside the inbox query's three searches would cost 12 points a refresh
(2.9% of the budget), because GitHub prices nested lists by the result slots asked for, not
the items returned; that dry-run cost is why the details are a separate query. Before the
cards read details, a refresh cost 1 point (0.24%).

The [sensitive-file check](desktop.md#sensitive-files) costs no GraphQL points: it reads a
pull request's changed files from the REST API, whose separate budget is 5,000 requests an hour.
It runs only for pull requests from outside the repository (and collaborators', by default),
and only when one's head commit has moved since its last check: two requests for a pull request
of up to 100 files, plus one per further 100. A refresh that finds nothing pushed checks
nothing, and one refresh checks at most 20 pull requests. A launch on such a pull request asks
for its head once more, one request, and lists its files again only if it was pushed since.

<br>

## Reproducing

On a Mac signed in with the GitHub CLI (see [desktop.md](desktop.md) for the setup):

```bash
cd ~/projects/nilbox
magex app:build
magex measure:all
```

`magex measure:all` runs `measure:size`, `measure:startup`, `measure:refresh`, and
`measure:launch` in that order, and each prints `key=value` lines. Run `measure:launch` from
iTerm2, or allow the terminal you run it from to control iTerm2 under System Settings → Privacy
& Security → Automation. Until macOS allows it, the target reports `NEEDS AUTOMATION
PERMISSION`.
