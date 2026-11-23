# internal/reporules

`internal/reporules/` holds the pinned and muted repositories and orgs (`repos.pinned` and
`repos.muted`, each a list of `owner` or `owner/name`, a target in one list at most), which
settings alone writes (`SetRepoRule`, emitting the Go-only `event.RepoRulesChanged`, which
core turns into an `event.InboxUpdated` and the Release `Refresher`'s `RulesChanged`) and
the inbox and Release read; `Rules.For` gives a repository's own rule before its org's. In
the inbox, each snapshot item carries its `RepoRule` (the rules are in the snapshot cache's
`settingsKey`, so a change rebuilds it), a pinned repository's items come after the items
pinned themselves, and a muted one's are in each list's Muted tab, never `NeedsYou` (so
out of the menu bar's count), told in no arrival, followed by no checks follower, and
given no file check by a refresh, though their details still come with it. It is a leaf:
it imports only `internal/repo` and `prefval`.
