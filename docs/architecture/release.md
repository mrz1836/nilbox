# internal/release

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/release`](../../internal/release) · Guide: [Release](../guide/release.md)

`internal/release/` is the Release page's service. `Candidates` lists every clone in the repository
locations whose origin is on GitHub, grouped as unreleased, up to date, no releases yet, can't
release here, or muted (`GroupMuted`: a clone of a repository muted as the list is read is listed
with its name and folder alone and never asked about, a row muted later keeps what GitHub said and
is served regrouped, through `arranged`, which also puts pinned ones first in their group, and
unmuting a row never read reads that repository alone, as a release's patch does, `liftMuted`; the
rules are read through its `RuleSource` on every call), from two rounds of batched GraphQL
(`(*github.Client).FetchReleaseStatus`, then `FetchReleaseCompare` for the repositories with a
version, 15 to a query, adapted in core), and reads each unreleased clone's state with `gitread`,
never asking origin. It serves the last list however old it is: the whole list is read again only on
the page's **Read GitHub again**, for a list from other locations, for one out of date, or with none
yet (and behind a list kept from a past start, below), never on a timer. A refresh that fails keeps
the last list, with its failure, until the user reads it again.

A refresh tells the window how far it has got, at most four times a second
(`event.ReleasesProgress`: finding the clones, each round's repositories as GitHub answers each
batch, then the unreleased clones), and each list that lands is kept in the `release.lastCandidates`
pref with the locations' `Key`. After a restart, the first `Candidates` serves that list at once,
marked `Kept`, while the `Refresher` (a desktop worker) reads GitHub again behind the page and
`event.ReleasesUpdated` says when it lands; a list from other locations is never served, and a
refresh that fails or is cut short keeps nothing.

`version.go` works the next version out exactly as magex does, tested against a table recorded from
the real magex. A release follows the review sheet's template: the `Gatherer` reads its `Facts` at
once (magex through a `ToolSource`, GitHub's status of the one repository with its newest commit's
checks and statuses, which the checks line names when they fail, in `GitHub.RepoStatus`'s one query
(the list's batches read the rollup's state and its per-state counts, under the alias `counts`,
never the checks one by one; still one point a batch), origin's refs with one `ls-remote`, and the
clone with `gitread`), and the pure `Decide` works out the sheet's `Offer`: every check, every
bump's version over the clone's and origin's tags, the Major gate, and the argv, `push=true` (never
the bare word) plus `expect=`, `json=true`, and `ff-only` only for a magex whose
`Definition.Features` has them.

The bound `Prepare` takes the sheet's choice and, within `FactsFor`, decides again on the facts the
sheet was shown, so a choice asks GitHub nothing. Only its `Refresh` gathers them afresh, which the
sheet sets as it opens, on its **Check again**, and when the window comes back to the front while a
check blocks or warns (or none could be read), at most every 10 seconds and never while a release
starts or runs. A `Fix` is a check's own action, so none reads again: the sheet offers Check again
once, under every check, so no check can lack it.

The bound `Start` hands `scriptrun` a release whose `Before`, under the clone's lock, gathers the
facts once (origin and the clone afresh, GitHub's from the sheet while under `FactsFor` old) and
decides there what magex runs, so a release that isn't ready, whose version moved, or that the sheet
should never have sent (a major without its typed version, its go.mod box, or a magex with
`expect=`, logged as a bug) never runs. The run's `OutputReader` turns magex's lines into steps,
warnings, and unrecognized lines (the run's result leaves out the warning every release has, that
the newest version tag isn't on HEAD; the log keeps it, and the window's log view leaves out magex's
JSON result line), and its `Finish` verifies the release whatever magex's exit status: `ls-remote`
for the tag, peeled to its commit, then the clone's own tag, its branch, and any rebase in progress,
into a `Result` (released, not pushed, failed with `ExplainFailure`'s heading, or unverified). Only
this run's tag succeeds: origin's must point at the commit magex's JSON result names, or the clone's
tag that magex said it made; any other is someone else's (`Taken`). The commands it offers to copy
are shell-quoted. `release.Verifier`, which core registers for releases, verifies one left from
before a restart the same way. The reader's `Progress` turns Stop off from magex's switch to the
branch until its pull ends, and asks first once the tag exists; a release that reaches GitHub is
told once its outcome is recorded (the run's `Recorded`), through its `Notifier` (core: the notify
`Dispatcher`'s `Notify`, a `release_done` notification about no item, and its banner).

A release that ends doesn't put the page's whole list out of date: its `Finish` hands the repository
to the `Refresher` and emits `event.ReleasesUpdated` at once, and the list reads as `Refreshing`
until the patch lands. The `Refresher` reads the repository alone again (its status, a comparison
only when its newest version isn't on its newest commit, and its clones' state, telling the window
how far it has got with `event.ReleasesProgress`), once any refresh under way has landed (one read
of the list at a time), patches those rows into the list, however old, which stays as current as it
was and is kept for the next start, and emits `event.ReleasesUpdated` again. A refresh that began
after the release ended has read the repository already, so the patch asks GitHub nothing. A list it
can't patch (none yet, other locations', out of date, kept from a past start, one whose last refresh
failed, without that repository, or GitHub failing) goes out of date instead, as it does when the
`Refresher` isn't running or stops with a patch still queued, and a refresh under way when a list
goes out of date lands out of date too. A merge in the review sheet is patched in the same way: the
review service tells its `Merges` (core wires the `Refresher`'s `Merged`) the repository a pull
request merged into, and the `Refresher` reads it again, but only while the list could be patched
then, so a merge never puts the list out of date.

The `ChecksFollower`, a desktop worker as often as Settings → Refreshing → **Release checks** says
(`refresh.releaseChecks`, 45 seconds by default, 0 off, woken on `event.IntervalsChanged`), reads
again the rows whose default branch's newest commit's checks run (`PENDING` or `EXPECTED`), of no
repository muted now: at most `followChecksMax` (15, one status batch and at most one comparison
batch, about two points) a round, the longest unread first, each left alone while read within
`followChecksFresh` (25 s, by a refresh, a patch, or the round before, `Service.readAt`), and none
whose newest commit is older than `followChecksFor` (2 h), so a check never reported can't spend the
budget for good. The one exception is a `PENDING` row worked out while its commit was no older than
that (`Candidate.workedAt`, stamped by `candidateFor`): it gets one more read after, the first that
can find it stuck (below), and its row, worked out later, isn't followed again, stuck or still
running. It asks GitHub nothing while none runs, and follows only a list it could patch
(`patchable`: read from GitHub in this run, from the locations as set now, current, its last read
not failed), after the read of the list under way (`startReading`). It reads each row as a patch
does (`readRows`: status, comparison, and clone state, so a push the checks run for shows too), but
quietly: no `event.ReleasesProgress`, never `Refreshing`, and only a row that changed keeps the list
and emits `event.ReleasesUpdated` (the rows it read land in the list either way, so each one's
`workedAt` is current). A round GitHub fails is logged and leaves the list current, never out of
date; the next round tries again.

## Checks stuck on a run that never started

A check run GitHub queues and never starts, as an app's check with no workflow (Dependabot's config
check, say), keeps the rollup `PENDING` until the next commit, though the Actions page never lists
it. `checksAt` (`checks.go`), the one rule, used by `candidateFor` for the list and by
`Gatherer.repo` for the sheet, each with its own clock, reads such a commit's checks as
`ChecksStuck` (`STUCK`, never GitHub's own): the rollup is `PENDING`, so nothing failed; at least
one check run is `QUEUED` (`RepoStatus.Queued`, from `ReleaseStatus.HeadQueued`); no other check is
unfinished (`RepoStatus.Running`: a run in progress, waiting, or pending, a status pending or
expected, and a state this build doesn't know, so nothing really running is hidden); and the commit
is more than `followChecksFor` old. An `EXPECTED` rollup, a required check never reported, is left
as it is. The list reads a stuck row as ready (a green edge, no pulse, an amber ✓, and
`Candidate.Stuck` for *checks passed, 1 never started*). The sheet's checks line warns and names the
queued runs as the failing line names its checks (`checkNames`; the core adapter fills
`RepoStatus.QueuedChecks` from the sheet's one query), and its **View on GitHub** opens the run's
own page (`github.Check.RunURL`, the CheckRun's `url`, which only the release read asks for; its
`detailsUrl` can be the app's own site) when exactly one never started, else the commit's checks
page. A running line's **View on GitHub** opens the commit's checks page too.

The bound `ReleaseResult` reads a run's outcome back as a typed `Result`, and the bound
`OfferAfterMerge` answers the review sheet's offer after a merge from local state alone: the
`integrations.offerAfterMerge` setting, magex, the default branch the Release page or the
inbox already holds, and a clone with that origin, from the page's list or else only the
folders inside the clone root and each folder location named as the repository is, then
each clone location (one git command each), never a scan of a location.
The `Follower`, a deferred desktop worker, follows each release on GitHub to *published*: a
release's `Finish` (its `Verifier`'s too) starts a `Follow` in its `Result` (its state,
`until`, and `polls`) when the Service has `Workflows` and `Records`, and its `Recorded` hands
it to the `Follower` once the outcome is kept. It polls a minute after a follow starts or
resumes, then every three minutes, at most `FollowPolls` (20) over `FollowFor` (45 minutes),
on the real clock for its limits and its waits alike (`clock.Sleep`; tests run it in a
`testing/synctest` bubble). Each poll is `(*github.Client).FetchReleaseProgress` (core's
`ReleaseWorkflow`): the tag's release, one GraphQL query through `postGraphQL`, and, until
it is published, the tag's own workflow runs, one REST request through `do` (`GET
.../actions/runs` by the push event, the tag as the run's branch, and the tag's commit, each
checked again on the answer), so the budget counts both. *Published* (`publishedAt` set, not
a draft) wins; while one of the tag's runs goes on, the follow goes on; once they all ended,
one that failed, timed out, or never started ends it as failed, named by its workflow; all
of them ending well two polls in a row with no release published ends it as
`unpublished`, and no run of the tag's by the third poll as `noWorkflow`. Each poll that
leaves the follow going is written into the run's record with `scriptrun.Engine.Amend` (an
ended run's summary and outcome, in one transaction with its read, and only while the
record still says this follow goes on). An end is settled the same way, written again a
few times a moment apart without asking GitHub again, and, if it still can't be, logged
and let go of for the next start's resume. *Published* (`Notifier.Published`,
`release_published`, opening the release) and *failed* (`Notifier.WorkflowFailed`,
`release_failed`, opening the run, or else the tag's Actions runs) are told only once the
record holds them, with a context the quit doesn't end; `unpublished`, `noWorkflow`, and
the limits get the result's line alone, no banner. As it starts, it resumes every release
whose record says it follows (`Engine.Ended`, which reads only a kind's ended runs), within
the same limits; a quit leaves the follow recorded for the next start.
