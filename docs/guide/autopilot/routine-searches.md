# Routine searches

An Autopilot routine watches either the inbox's own lists, which every refresh reads already,
or a GitHub search of its own, such as Dependabot's pull requests in your account. While a
routine with a search is switched on, and Autopilot (beta) is, each inbox refresh reads that
search too, once it has
read the inbox's three, and stores what it finds apart from your lists: a watched item is never
in Review requested or Yours, never in their counts, and never in the menu bar's count. It shows in the routine's **Watching** list, and the searches keep it current for the
routines.

**What a search reads.** Identical searches across routines are read once. Each search
returns the 100 most recently updated open items it matches, in a request of its own sent once
the refresh has stored and shown your lists, so the inbox never waits for it, nor shares
GitHub's time with it: your lists come as fast as they do without searches, and the searches'
items are stored a few seconds later. A search reads only what tells whether an item changed
(its head commit, base branch, the checks' overall state, its update time, draft state, and
state); the rest of an item, its mergeability, merge state, review decision, labels, and
counts, comes from a read of the items that are new to the searches or changed since, or
whose checks were still running. An item whose last such read is over half an hour old is
read again in the room that read leaves, up to 15 a refresh, the oldest first, so with some
300 watched items each is read again about every 100 minutes at the five-minute refresh. A
read GitHub doesn't answer leaves its items as they were, to be read again next time. A
watched pull request whose routine acts on its checks is followed like a listed one while its
checks run (see [the inbox cards](../inbox-cards.md)), whatever repository it is in, a muted one
included. Reads are stored in the order they went out: a read of an item's own that finds it
merged or closed while the searches are being read stands, and the searches never bring it
back; the searches finding it merged or closed stand over a read of its own that went out
before them. A merged item's state never changes again, whatever a read says.

**When a search fails.** A search GitHub doesn't answer, or a request that times out, never
fails the refresh: its items are kept, marked as not read this time, so they don't look as if
they left it, until it answers again. A search that matched more than 100 items keeps the
items past its first page the same way, for up to a week, and so does a search GitHub answered
in part, as when it fails one of its results alone, which is left out. While the GraphQL budget
is down to its last quarter, no search is read at all, a routine's test included, and your
lists carry on. GitHub answers a search for a user, an org, or a repository it doesn't know
with nothing, and no error, so a routine's test warns about an owner none of your items has
come from.

**What it costs.** Searches are sent a few to a request, by how many items each matched last
time, up to about 120 items a request and four requests side by side, since GitHub gives up on
a request that runs past about 10 seconds. Each request costs one GraphQL point. The first
refresh after a search is added reads the details of everything it watches, 15 items to a
request, once: 300 newly watched items cost 20 points and take that refresh's searches about
20 seconds, which your lists never wait for.

| What | When | Cost |
|---|---|---|
| The searches | Each refresh, while a routine with one is on, once your lists are stored | 1 point a request; five typical searches take 2 or 3 |
| The details of watched items | Those new or changed; half-hour-old ones only in the room left, or one batch when none changed | 1 point a batch of up to 15; usually one small batch, once the searches are known |
| Following their checks | While a followed pull request's checks run | In the listed ones' requests, 1 point each of up to 25; a round reads 50 at most, your own cards first |
| A routine's test | When you test a draft | Its searches as above, the details of items new to the inbox or over half an hour old, and 1 point a batch of 25 check names |

The API budget's panel says what Autopilot spent in one line, once it spent something (see
[the window](../window.md)), and [performance.md](../../performance.md#routine-searches) records the
measured numbers.

**Old items.** An item that is in no list, that no routine's search returns, that you haven't
pinned, snoozed, or dismissed, and that no agent session or notification names is removed 30
days after the last read that brought it, whether a routine's search found it or it left your
lists; its details, file check, check names, and the sheets' cached views of it go with it.
