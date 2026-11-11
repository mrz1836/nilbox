package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// hostileValue is a name, branch, or tag that would break out of an unquoted argument: a
// quote, a brace, a newline and a comment, other control characters, characters JSON
// escapes for HTML, and a line separator.
const hostileValue = "x\") { viewer { login } }\n#\t\x00<&>\u2028"

// TestBatchedQueriesText pins the whole text of every batched query, for a plain repository
// and a hostile one, so a change to how any is written, or quoted, shows here.
func TestBatchedQueriesText(t *testing.T) {
	t.Parallel()

	plain := repo.Name{Owner: filesOwner, Repo: exampleRepo}
	hostile := repo.Name{Owner: `acme"org\`, Repo: hostileValue}

	tests := []struct {
		name  string
		query func() (string, error)
		want  string
	}{
		{
			name: "states",
			query: func() (string, error) {
				return statesQuery([]item.Ref{{Repo: plain, Number: 184}, {Repo: hostile, Number: 7}})
			},
			want: `query ItemStates {
  r0: repository(owner: "acme-org", name: "example-repo") { item: issueOrPullRequest(number: 184) { ...StateFields } }
  r1: repository(owner: "acme\"org\\", name: "x\") { viewer { login } }\n#\t\u0000\u003c\u0026\u003e\u2028") { item: issueOrPullRequest(number: 7) { ...StateFields } }
  rateLimit { cost }
}

fragment StateFields on IssueOrPullRequest {
  __typename
  ... on PullRequest { title url prState: state closedAt mergedAt headRefOid headRefName }
  ... on Issue { title url issueState: state closedAt }
}
`,
		},
		{
			name: "merged branches",
			query: func() (string, error) {
				return mergedQuery([]mergedAsk{
					{repo: plain, branches: []string{"feature-x", hostileValue}},
					{repo: hostile, branches: []string{"main"}},
				})
			},
			want: `query MergedBranches {
  r0: repository(owner: "acme-org", name: "example-repo") {
    b0: pullRequests(headRefName: "feature-x", states: MERGED, first: 5, orderBy: {field: UPDATED_AT, direction: DESC}) { nodes { number title url mergedAt headRefOid } }
    b1: pullRequests(headRefName: "x\") { viewer { login } }\n#\t\u0000\u003c\u0026\u003e\u2028", states: MERGED, first: 5, orderBy: {field: UPDATED_AT, direction: DESC}) { nodes { number title url mergedAt headRefOid } }
  }
  r1: repository(owner: "acme\"org\\", name: "x\") { viewer { login } }\n#\t\u0000\u003c\u0026\u003e\u2028") {
    b0: pullRequests(headRefName: "main", states: MERGED, first: 5, orderBy: {field: UPDATED_AT, direction: DESC}) { nodes { number title url mergedAt headRefOid } }
  }
  rateLimit { cost }
}
`,
		},
		{
			name: "latest releases",
			query: func() (string, error) {
				return latestReleasesQuery([]repo.Name{plain, hostile})
			},
			want: `query LatestReleases {
  t0: repository(owner: "acme-org", name: "example-repo") { latestRelease { tagName url publishedAt releaseAssets(first: 30) { nodes { name downloadUrl size digest } } } }
  t1: repository(owner: "acme\"org\\", name: "x\") { viewer { login } }\n#\t\u0000\u003c\u0026\u003e\u2028") { latestRelease { tagName url publishedAt releaseAssets(first: 30) { nodes { name downloadUrl size digest } } } }
  rateLimit { cost }
}
`,
		},
		{
			name: "release status",
			query: func() (string, error) {
				return releaseStatusQuery([]repo.Name{plain, hostile})
			},
			want: `query ReleaseStatus {
  r0: repository(owner: "acme-org", name: "example-repo") { ...ReleaseStatusFields }
  r1: repository(owner: "acme\"org\\", name: "x\") { viewer { login } }\n#\t\u0000\u003c\u0026\u003e\u2028") { ...ReleaseStatusFields }
  rateLimit { cost }
}

fragment ReleaseStatusFields on Repository {
  isArchived viewerPermission
  defaultBranchRef {
    name
    target { oid ... on Commit { committedDate messageHeadline statusCheckRollup { state } } }
  }
  refs(refPrefix: "refs/tags/", first: 20, orderBy: {field: TAG_COMMIT_DATE, direction: DESC}) {
    nodes { name target { __typename oid ... on Tag { target { __typename oid } } } }
  }
}
`,
		},
		{
			name: "release compare",
			query: func() (string, error) {
				return releaseCompareQuery([]CompareRequest{
					{Repo: plain, Tag: "v1.4.2", Branch: "develop"},
					{Repo: hostile, Tag: hostileValue, Branch: hostileValue},
				})
			},
			want: `query ReleaseCompare {
  r0: repository(owner: "acme-org", name: "example-repo") { ref(qualifiedName: "refs/tags/v1.4.2") { compare(headRef: "develop") { ...ReleaseCompareFields } } }
  r1: repository(owner: "acme\"org\\", name: "x\") { viewer { login } }\n#\t\u0000\u003c\u0026\u003e\u2028") { ref(qualifiedName: "refs/tags/x\") { viewer { login } }\n#\t\u0000\u003c\u0026\u003e\u2028") { compare(headRef: "x\") { viewer { login } }\n#\t\u0000\u003c\u0026\u003e\u2028") { ...ReleaseCompareFields } } }
  rateLimit { cost }
}

fragment ReleaseCompareFields on Comparison {
  aheadBy
  commits(last: 10) { totalCount nodes { oid messageHeadline committedDate } }
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.query()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
