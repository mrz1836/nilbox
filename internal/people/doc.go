// Package people is the app's local directory of the people who work on each repository:
// who opened, reviewed, and committed to its pull requests, who was asked to review them,
// whom GitHub suggested, and whom the user asked from the app, with the repository's teams.
// The review sheet's reviewer picker suggests reviewers from it, with no request.
//
// It learns only from what the app reads anyway (each read of a pull request's sheet), and
// from one read of a repository's most recently updated pull requests at most weekly, which
// the review sheet makes and hands over as plain values: the package knows nothing of
// GitHub's client. People only: the caller leaves bots and mannequins out.
//
// What it learns is keyed by repository, person, pull request, and role, so reading one pull
// request again never counts anything twice, and it is bounded and pruned in the same
// transaction that writes it: per repository the newest 400 pull request numbers, a year at
// most, 100,000 rows in all, and no person nothing names.
//
// A person counts as working on a repository (Ranked lists them) only when GitHub gave them
// access there, as an owner, member, or collaborator on a pull request they opened or a
// review they left, or someone asked them to review there (someone with access did, GitHub
// suggested them, or the user did from the app): GitHub refuses to ask anyone else. Commits
// and reviews without access add weight, but never count on their own.
//
// The package owns the people, people_seen, people_teams, and people_reads tables.
package people
