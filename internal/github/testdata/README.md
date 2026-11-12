# Recorded GitHub responses

The tests in this package decode real GitHub API responses. Each file here was recorded from
GitHub once, then scrubbed so that every owner, repository, login, name, node ID, and URL in it
is a placeholder, unless the table below says it was written by hand. No test calls GitHub: each
one serves a file from a local `httptest` server.

<br>

## The fixtures

| File | Request that produced it | Read by |
|---|---|---|
| `inbox_response.json` | The inbox query, `inboxQuery` in `inbox_query.go`: the viewer, the review-requested, authored, and assigned searches, and the query's rate-limit cost | `fetchFixtureInbox` and the `TestFetchInbox*` tests in `client_test.go` that serve it, and `BenchmarkDecodeInbox` in `bench_test.go` |
| `details_response.json` | The details query, `detailsQuery` in `details.go`, for three pull requests (passing checks, failing checks, no checks), an issue, and an ID GitHub could not resolve | `TestFetchDetailsReadsEachItem` in `details_test.go` |
| `item_response.json` | The item query, `itemQuery` in `item.go`, for one merged pull request opened from a fork | `TestFetchItemReadsTheItemAndItsDetails` in `item_test.go` |
| `pull_review_response.json` | The pull review query, `pullReviewQuery` in `pullreview.go`, for one open Dependabot pull request; a pending and a failing check, a commit status, reviews, a pending review, auto-merge, and `viewerCanClose` were added by hand | `TestFetchPullReviewReadsTheSheet` in `pullreview_test.go` |
| `rate_limit_response.json` | The REST endpoint `GET /rate_limit` | `TestFetchRateLimits` in `ratelimit_test.go` |
| `latest_releases_response.json` | The latest-release query, `latestReleasesQuery` in `releases.go`, for the two public tool repositories the integrations install from (`t0`, `t1`), which keep their real names since the integrations' definitions name them; a repository with no release (`t2`) and one GitHub could not find (`t3`) were added by hand | `TestFetchLatestReleases*` in `releases_test.go` |
| `tag_runs_response.json` | **Not recorded: written by hand** from GitHub's REST schema for `GET /repos/{owner}/{name}/actions/runs`, which `fetchTagRuns` in `releaseprogress.go` asks for a tag's push runs. Its shape follows P0's finding on a real release that a tag push's runs carry the tag as their `head_branch`: two runs for the tag (one failed, one in progress), the branch's own push run of the same commit, a run for another tag, and a schedule run, the last three of which never count | `TestFetchReleaseProgressReadsTheTagsOwnRuns` in `releaseprogress_test.go` |

The files also hold cases the tests rely on that a fresh recording may not reproduce, such as a
null node, a bot author, a pull request from a fork, and labels with an empty name or a color
that is not hex. A refreshed file must keep every case a test asserts on.

<br>

## The query golden files

The `*-query.golden` files are not responses: each holds the whole text of one batched query
(states, merged branches, latest releases, release status, release compare) as the package
writes it for a plain repository and a hostile one. `TestBatchedQueriesText` in `batch_test.go`
compares the queries with them. After a deliberate change to a query, rewrite them with
`go test ./internal/github -run TestBatchedQueriesText -update`, and review the diff.

<br>

## Refreshing a fixture

1. Copy the query into a scratch file outside the repository, with the fragments its Go
   constant appends: `inboxQuery` ends with `itemFieldsFragment` and `repoFieldsFragment`,
   `detailsQuery` with `detailFieldsFragment`, `itemQuery` with all three, and `pullReviewQuery`
   with those three and `reviewFieldsFragment`.
2. Run it with `gh`, which sends the token itself, and format the result the way the files
   are formatted:

   ```bash
   # The inbox query takes no variables.
   gh api graphql -F query=@/tmp/inbox.graphql | jq --indent 4 . > /tmp/inbox_response.json

   # The details query takes a list of node IDs, and the item query takes one.
   gh api graphql -F query=@/tmp/details.graphql -f 'ids[]=<node-id>' -f 'ids[]=<node-id>' | jq --indent 4 .
   gh api graphql -F query=@/tmp/item.graphql -f id=<node-id> | jq --indent 4 .
   gh api graphql -F query=@/tmp/pull_review.graphql -f id=<node-id> | jq --indent 4 .

   # The rate limits come from the REST API.
   gh api rate_limit | jq --indent 4 .
   ```

3. Scrub the output before it goes anywhere near the repository:
   - Replace every owner and repository with `acme-org`, `example-repo`, and `other-repo`.
     Replace the signed-in account with `octo-user`, and other people with other `octo-`
     logins, such as `octo-reviewer`. Replace display names to match.
   - Replace node IDs with readable fakes of the same shape (`PR_fakeNodeId7`,
     `I_fakeNodeId21`, `R_fakeRepoExample`), using the same fake wherever one object appears,
     and commit SHAs with one repeated digit (`1111…`).
   - Point every URL at the placeholders, such as
     `https://github.com/acme-org/example-repo/pull/7`, avatar URLs at
     `https://avatars.githubusercontent.com/u/101?v=4&s=128`, and any email address at
     `example.com`.
   - Rewrite titles, descriptions, branch names, and label names that say anything about the
     real project.
   - Keep the shape: field names, nulls, enum values, and the timestamp format.
4. Diff the new file against the old one, and carry over the cases the tests rely on. A file
   written by hand, as `tag_runs_response.json` is, is replaced by a recording only once one
   shows every case it holds; until then, keep it in step with the request by hand.
5. Run `go test ./internal/github/ ./internal/repoguard/`. The first package's tests prove the
   new file still decodes as they expect. The guard tests fail on an absolute home directory
   path, on the product name written outside `internal/brand`, and on any name a contributor
   lists in `.git/info/private-terms` (see the root README, "Keep private names out of the
   repository"). They only catch what they know about, so read the whole diff as well.

Never paste a token, an `Authorization` header, or the output of `gh auth token` into a file
or a test. Never record a private repository: record against public repositories, or against a
test organization of your own, so the response holds nothing private to begin with.
