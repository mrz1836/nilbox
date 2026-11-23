# internal/assignees

`internal/assignees/` is what both sheets' assignee pickers share: whom they offer to assign
to an issue or a pull request, in what order, and how that offer is learned and searched with
as few requests as it takes. It knows no store, no service, and neither sheet (the repoguard
boundary test): it reaches the People directory through its `Directory` interface (`Known`,
`Covers`, `Keep`, `MarkFailed`, `Assigned`), which core adapts, and GitHub only through the
`Read` each sheet hands it, one GraphQL point through that sheet's inbox footing. Core builds
one `Finder` for both sheets, so its one `flight.Group`, keyed by repository and words, makes
one read at a time whichever sheet asks, and checks again, as the read would start, that it is
still due. `Candidates` is local alone. `Learn` reads the whole list (`""`) when the directory
says it is stale, keeps it, or records a failure ("Couldn't learn who can be assigned in
acme-org/example-repo: ..."), and returns the list. `Search` filters what the directory holds
when `Covers` says it answers the words, else reads them once, keeps the people found, and
marks those the list holds nothing else of as `SourceFound`. `Seen` records who a read saw
assigned, or the user just assigned. A nil Finder offers no one and reads nothing; every list
is empty, never nil.

`rank` is pure: each person once, never a bot or a login GitHub couldn't have issued, at most
300 (enough for a whole list of 100 beside the item's own people, since a list that named
everyone is never searched past), in the order of their `Source`: the user (`you`), the author
(`author`, a person only), the participants (`participant`), the people assigned in the
repository before (`assigned_before`, the latest first), those who work on it (`works_here`,
as `Ranked` ranks them), those a search just found (`found`), the rest of those GitHub listed
(`listed`, by login), and, only while the whole list is cut short, those who work on the
owner's other repositories (`org`, with where). Once the whole list named everyone, someone
only the directory knows, not on it, is left out: GitHub would ignore their assignment. A query
keeps those whose login or name matches as the window's own filter matches (a field or a word
of one that starts with it first, then one that holds it), and everyone the search found.
