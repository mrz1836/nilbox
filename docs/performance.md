# Performance

The first milestone set five targets before any further feature work: a fast cold start, a
fast inbox refresh, a fast agent launch, a small app, and a small share of the GitHub API
budget. This page records what was measured, on what, and how, so anyone can reproduce the
numbers with the `magex measure:*` targets.

<br>

## Results

| Metric | Target | Measured | Result |
|---|---|---|---|
| Cold start to first paint | < 1 s | 1013 ms | Not met in the recorded run (13 ms over); an earlier run of the same code measured 922 ms |
| Inbox refresh | < 3 s | 2.54 s | Met once the inbox's items are known; the first refresh into an empty database takes 6–10 s |
| Launch to prompt loaded | < 3 s | measured in the maintainer verification run | Pending: the build host cannot grant the Automation permission unattended |
| App binary size | < 30 MB | 16.4 MB | Met |
| API budget at 5-minute cadence | < 2% | 0.72% | Met |

Measured on the build host: an Apple Silicon Mac (M4 Pro, 24 GB memory) running macOS 15, with
one 1920×1080 display at 60 Hz, over its regular network connection. The inbox refresh and API
budget rows were measured again when the inbox cards began showing pull request details, on a
second Mac (M1 Max, 32 GB memory, macOS 15) over its regular network connection; both numbers
are set by GitHub's response times rather than by the machine. Each number comes from the
production bundle that `magex app:build` produces.

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

**Recorded run.** First launch of the build: 1453 ms. Launches 2–6: 930, 951, 1031, 1013, and
1057 ms, for a median of 1013 ms. An earlier run of the same code measured 1491 ms for the
first launch, then 922, 1063, 922, 853, and 833 ms, for a median of 922 ms. Cold start on this
host is therefore at the 1 s target, not comfortably under it: runs vary by about ±100 ms, and
4 of these 10 cold starts went over 1 s.

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

### Inbox refresh

**What is measured.** One refresh as the app runs it: borrowing the token with `gh auth token`
(about 250 ms); then, concurrently, the REST `/rate_limit` request, the batched GraphQL inbox
query, and the details query for the items the last refresh listed; then a details query for
any item new to the lists; then writing the account, repositories, items, details, and rate
readings in one database transaction.

**How.** `magex measure:refresh` runs the bundle's binary with `--measure-refresh`. In that mode
the binary opens no window: it starts the app's core over a scratch data directory, deleted
afterwards, and times five refreshes one after another, using the GitHub CLI login of whoever
runs it. Every request is a read. Running through the app binary measures the production build,
and it is also the binary a per-app firewall has to allow.

**Recorded run.** 6414, 2565, 2418, 2543, and 2342 ms, for a median of 2.54 s. The run before it
measured 10039, 3863, 2282, 2202, and 2391 ms, a median of 2.39 s. Each run starts from an empty
scratch database, so its first refresh has no listed items to read alongside the inbox query:
the details of all 20 items wait for the inbox query to answer, which added 3.5 to 7.5 s, once.
Every later refresh read the listed items' details alongside the inbox query and finished in
2.2 to 3.9 s. An existing database already lists its items, so its first refresh is no
slower: refreshing a copy of a real database through the same code took 2.57, 2.33, and
2.51 s.

Before the inbox cards read details, the build host recorded 2822, 3419, 2693, 2328, and
2943 ms, a median of 2.82 s, and three earlier runs of five had medians of 2.99, 2.73, and
2.81 s.

**Where the time goes.** Timed separately with `curl`, GitHub takes 2.6–3.0 s to start answering
the batched query. Each of its three searches takes 1.2–1.7 s on its own, and GitHub runs them
partly in parallel. The REST request takes about 0.3 s and overlaps the query, and the database
write takes a few milliseconds. The margin under 3 s is set by GitHub's search latency, not by
the app, and it may differ for accounts with more matching items.

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
live run there would only leave the permission dialog on screen. The number comes from the
maintainer verification run instead, with `magex measure:launch` run from iTerm2 on the
maintainer's Mac. When the permission is missing, the target prints `NEEDS AUTOMATION
PERMISSION` and exits without a number.

### App binary size

`magex measure:size` reads the size of the production executable,
`bin/Nilbox.app/Contents/MacOS/nilbox`: 16,446,784 bytes, or 16.4 MB in decimal megabytes. The
whole bundle is 16.4 MB. The binary is built with `-tags desktop,production -trimpath -ldflags
"-s -w"` and embeds the built frontend (about 280 KB, without source maps).

### API budget at 5-minute cadence

Every refresh recorded the GraphQL point cost GitHub charged for its queries
(`rateLimit.cost`, summed): 3 points, 1 for the batched inbox query and 1 for each of the two
details requests that read the inbox's 20 items. At the scheduler's cadence of one refresh
every five minutes, that is 12 refreshes, and 36 points, per hour, out of the 5,000-point
hourly GraphQL budget GitHub reported: 0.72%. The REST `/rate_limit` request does not count
against GitHub's rate limits.

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
