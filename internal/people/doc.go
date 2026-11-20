// Package people is the app's local directory of the people who work on each repository:
// who opened, reviewed, and committed to its pull requests, who was asked to review them,
// whom GitHub suggested, and whom the user asked from the app, with the repository's teams;
// and who can be assigned there. The review sheet's reviewer picker suggests reviewers from
// it, and both sheets' assignee pickers suggest assignees from it, with no request.
//
// It learns only from what the app reads anyway (each read of a pull request's or an issue's
// sheet), from one read of a repository's most recently updated pull requests at most
// weekly, and from the assignee pickers' reads of who GitHub lists as assignable there (the
// whole list at most weekly, and a search only when nothing kept answers it: Covers), which
// the sheets make and hand over as plain values: the package knows nothing of GitHub's
// client. People only: the caller leaves bots and mannequins out.
//
// A person is one row however many repositories they work in, keyed by their login lower
// cased, as GitHub compares logins, so what the app learns of them in one repository serves
// every other: OrgRanked ranks the people who work on the owner's other repositories.
//
// What it learns is keyed by repository, person, pull request, and role, so reading one pull
// request again never counts anything twice, and it is bounded and pruned in the same
// transaction that writes it: per repository the newest 400 pull request numbers, a year at
// most, 100,000 rows in all, as many of who can be assigned where, a week of searches, and no
// person nothing names. Forget empties it.
//
// A person counts as working on a repository (Ranked lists them) only when GitHub gave them
// access there, as an owner, member, or collaborator on a pull request they opened or a
// review they left, or someone asked them to review there (someone with access did, GitHub
// suggested them, or the user did from the app): GitHub refuses to ask anyone else. Commits
// and reviews without access add weight, but never count on their own.
//
// The package owns the people, people_seen, people_teams, people_reads, people_assignable,
// and people_assignable_reads tables.
package people
