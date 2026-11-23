# The searches, and the builder's reads of the inbox

**The snapshot.** After every change to a routine (`changed`, a backup's import included) and
once at startup (core's `assemble`, before the scheduler starts), the Service builds again
the searches of the enabled routines of the account signed in that load: each search built
for that account (`watch.Build`, "your account" written as its login), each key once with the
routines that share it, and `Follow` set when one of their triggers reads the checks (Checks
fail, Checks pass). It keeps them in an atomic value, which `Searches.Current` reads at once,
never waiting on anything; `Searches` (`NewSearches`) is the inbox's face of it, never bound.
Builds run one at a time (`watchSet.build`, a one-slot channel held from the read of the
routines to the store), so the last to read stores last: two side by side, a change's and a
new account's, could otherwise store the older read over the newer. A routine that is off,
broken, another account's, or whose search won't build is left out, and every routine while
Autopilot (beta) is off; the engine builds the snapshot again as it is turned on or off.

**`Suggest`** returns what the stored items hold for the builder (`Items.Suggestions`): the
people and the bots who opened them, the owners of their repositories, their labels, and
their check names, each most common first, no list nil.

**`Watching(id)`** returns the IDs of the items the routine watches and counts, newest activity
first: those in the inbox with the reasons it names, and those its searches' rows hold, stale ones
included, so a search that failed still shows its items (`Items.WatchedItems`), less those its
conditions rule out (`counted`). Each item's conditions are decided on its stored facts
(`Items.Facts`) and the routine's history there, built from the ledger as the engine's cycle
builds it (`histories`), with `eval.Plan.Conditions`, which leaves the trigger out: an item
waiting on it, such as a pull request whose checks still run, stays, and so does one whose
decision waits on a read (check names, files), since only a definite no rules an item out. It
never asks GitHub. The inbox's bound `ItemsByID` turns the IDs into rows.

**`Test(draft)`** tries a draft on its items now, changing no routine and running nothing: its
spec is checked as `Save` checks it (a `ProblemsError` with each problem); its searches are
read from GitHub (`Items.SearchNow`) unless a test read them within a minute (`testCache`,
by query, answers that failed never kept), their items stored as the watch pass stores them,
with no rows; each search is checked for an owner the inbox has never seen (`KnownOwners`),
since GitHub answers an unknown user, org, or repository with nothing and no error; the
routine is evaluated on each item (`eval.Evaluate`, with no history yet) from its facts
(`Items.Facts`), and the check names of the pull requests whose decision waits on them are
read, one batch of 25 (`Items.ReadChecks`), before they are decided again; the matches come
first, then the unknowns. The result says what the test spent (`Points`: the searches, the
details they read, and the check names, the searches costing nothing again when `Cached`) and
what the draft's searches would add to each refresh (`Costs.Estimate`, core's `watchCosts`:
the requests the refresh packs them into, by what each matched, at most a page, and a full
page for one that failed, at what the last watch pass cost a request, at the refresh's pace,
as a share of the GraphQL budget). While the GraphQL budget is under a quarter of its size the
inbox sends nothing (`ErrLowBudget`, from `SearchNow` or, when every search came from the
cache, `ReadChecks`): the result says so (`LowBudget`), each search it had no answer for says
why, no check name is read, and the lists' items are still decided. A search that matched
more than a page warns that only the newest 100 are checked; each item says whether an inbox
list holds it (`Listed`), and when most don't, each triage step gets a warning (`Warnings`, at
the step), since triage changes only what the inbox shows; a watch of searches alone gets none,
since `Describe` warns each such step of it already. For a saved routine (`d.ID`), each item
says what the ledger holds of the occurrence it fires for now (`Record`, in the window's words:
*It already ran on this.*, *Waiting for your OK.*, that a Dry run recorded it, or why it closed
in Activity's own words, *Closed: it merged.*, from `reasonWords`, which a test holds to the
window's `reasonChips`), read from `ListRoutineMatchKeys`; a match with no record is one it
would run on.

**`MatchStored(draft)`** counts what a draft matches now among the stored items of the inbox's
lists it watches (`Items.WatchedItems`), that its routine has no record of and doesn't leave
alone, by kind (`Matching`: the pull requests and issues apart, `Unknown` for an item whose
decision waits on a read, and `Incomplete` for a draft that also watches a search, whose
items are `Test`'s to read, or more than 200 items): a Describe it card's *Matches 3 pull
requests in your inbox now*. It decides from what the inbox stored alone, asks GitHub nothing,
works with Autopilot off, and refuses a draft with problems as `Save` does.

**The window's builder** (`frontend/src/components/autopilot/builder`) renders from
`Catalog`, its pickers fed by `Suggest`, and holds the draft in the Autopilot tab, which stays
mounted while another tab shows, so a draft survives switching tabs. It calls `Describe` once
editing rests (300 ms), and `Test` only on the user's click, once the draft has no problem,
never as a routine opens or as it is edited, since a test's searches cost points; a test of an
earlier watch, trigger, conditions, or Once per stays shown, marked as such, its warnings
shown only while the steps are as tested (they are by step), and its cost line, once the
searches changed, said to be the last test's. `Test`'s minute-long cache keeps a second test
of the same searches from costing points again.
Saving a routine that is on goes through the same question as its switch (`useRoutineChange`'s
`save`): `Matching` with the draft, nothing asked only when nothing matches, nothing waits to
be told, and everything was read (`Matching.Incomplete`: a search with no answer or past a
page, a budget too low, or more items than a test tries), and `Save` with the answer as its
`Current`. The window's catalog fixture (`frontend/src/test/autopilotCatalog.json`) is the
catalog itself, written and checked by `TestTheWindowsCatalogFixtureIsTheCatalog` (`-update`
rewrites it).
