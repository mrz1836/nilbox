# internal/heldruns

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/heldruns`](../../internal/heldruns) · Guide: [Reviewing a pull request](../guide/reviewing-a-pull-request.md)

`internal/heldruns/` decides which of a pull request's workflow runs that GitHub holds for
approval count toward where it stands, and declares **Ignored workflows**
(`checks.ignoredWorkflows`, a list of workflow names, matched whole and case-sensitively),
which settings alone writes (`SetIgnoredWorkflows`, in `Portable`) and the inbox and the
review sheet read. `Ignored` is the one rule: a run of a named workflow never counts,
whichever kind it is and whatever GitHub reports, and a run GitHub approves only on its
site (`github.HeldRun.OnGitHub`) doesn't count either, unless GitHub reports the merge
`BLOCKED`, when it may be what blocks it and counts again (the safety net, from the
`mergeStateStatus` the details and the sheet's read already carry, no request more). Every
other held run counts. `Count` counts a card's, and `Unnamed` stands in runs of no name for
details stored before the names were, which match no ignored workflow. It is a leaf: it
imports only `prefval`.
