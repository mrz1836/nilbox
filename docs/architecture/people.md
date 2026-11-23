# internal/people

`internal/people/` is the app's local directory of who works on each repository, which the
review sheet's reviewer picker suggests from with no request (see internal/review), and of who
can be assigned there, which both sheets' assignee pickers suggest from (see
internal/assignees). It takes plain values, people only (the sheets leave bots and mannequins
out), keyed by the repository's `repo.Name`, and imports no GitHub client and no service; the
sheet learns into it from every read of a pull request (`Learn`) and from one read of the
repository's recent pull requests at most weekly (`LearnReviewers`). It owns six tables, these
four from migration `0106_people` and two of who can be assigned (below):
`people`, one row per login, lower-cased as GitHub compares them, with the login as GitHub
spells it now and the name, which an empty one never overwrites; `people_seen`, what a person
was seen doing, keyed by repository, person, pull request number, and role (`author`,
`reviewer`, `committer`, `requested`, `suggested`, or `picked`, the last when the user asked
them from the app), so reading one pull request again never counts anything twice, with
whether GitHub gave them access and GitHub's time of it (a row seen again keeps the access
either saw and the later time); `people_teams`, a repository's teams, as its teams list named
them (`listed`, which a new list replaces) or as a pull request asked one (`requested_at`); and
`people_reads`, when a repository's recent pull requests were last read, how many, and whether
an organization owns it, and when a read last failed.

A person counts as working on a repository (`Ranked` lists them) only with a row that has
access, an association of OWNER, MEMBER, or COLLABORATOR on a pull request they opened or a
review they left, or a `requested`, `suggested`, or `picked` row: someone with access asked
them, GitHub suggested them, or the user did. Commits and reviews without access add weight but
never count on their own, which keeps out the people GitHub would refuse to ask. `rank` is pure:
each row counts its role's weight (reviewer 3, picked 2.5, requested 2, author 2, suggested 1,
committer 1), halved every 60 days of its age (a time ahead of now counts as now), and people
sort by score, then the latest seen, then login, with the pull requests they reviewed, opened,
and committed to counted. Teams list the most recently asked first, then by slug. `Ranked.Stale`
says a read is due: never read, or read a week ago, but never within 30 minutes of a failed
read. It is bounded, pruned in the transaction that writes: per repository the rows of its
newest 400 pull request numbers, rows a year old at most, the latest 100,000 rows in all (by
row, so rows that share a time are cut too), and no person nothing names; a team neither listed
nor asked for in a year, and a read neither made nor failed in a year, go. Every stamp comes
from its injected clock, and an entry without a valid login, a pull request number, or a known
role is skipped.

**Who can be assigned** (`assignable.go`, migration `0108_people_assignable`).
`people_assignable` holds, per repository and
person, when a read of GitHub's assignable users last named them (`listed_at`) and when the app
last saw them assigned there (`assigned_at`, from a sheet's read or the user's own edit,
`SeenAssigned`); `people_assignable_reads` holds each read of the list, by its words
lower-cased and trimmed (`QueryKey`, `""` for the whole list): when it went through, how many
GitHub said match (`total`) and named (`listed`), and when it last failed. A person is one row
of `people` however many repositories they work in, so what the app learns of them anywhere
serves everywhere, and `PruneUnseenPeople` keeps whoever `people_assignable` names.
`KeepAssignable` keeps a read in one transaction: the people (a name only when the read gave
one), each marked listed, the read, and, for a whole list that named everyone (`listed >=
total`), everyone it left out unlisted. `Assignable` returns everyone the repository's rows
name with what the whole list's read says (`Complete`, and `Stale` as `Ranked`'s: due weekly,
never within 30 minutes of a failure). `Covers` reports that a search needs no request: a whole
list that named everyone covers every search; a search that named everyone it matched covers,
for a week (`coversFor`), every longer one that starts with its words; and any search answers
the same words for ten minutes (`repeatFor`). `OrgRanked` ranks, with `rank`, the latest 2,000
rows of who works on the owner's other repositories (access, or asked to review), each person
with the one they were last seen in (`Person.Where`). It is bounded too: rows neither listed
nor seen assigned in a year go, then the oldest past 100,000, a search's read after a week and
the whole list's after a year. `Summary` counts the people and the repositories anything names,
and `Forget` empties every table in one transaction (the people's rows take what names them
with them, by their foreign keys).
