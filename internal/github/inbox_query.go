package github

// inboxQuery is the one GraphQL request behind an inbox refresh: the viewer, three aliased
// searches, and the query's own rate-limit cost. Each search returns the total match
// count and up to 100 items with the fields the trust rule needs (authorAssociation, and
// isCrossRepository for pull requests). __typename tells pull requests from issues.
//
// Each search leaves out archived repositories (archived:false), so their items never
// take a result slot, count toward a total, or cost a details request.
//
// Each item carries its repository's description, state, settings, and the viewer's
// access to it (RepoFields). Like the viewer's name, profile URL, and avatar URL, they
// cost no extra points: GitHub prices lists, and a repository is a single object. The
// avatar is asked for at 128 pixels, enough for a sharp picture at menu size on a
// Retina display.
const inboxQuery = `query Inbox {
  viewer { login name url avatarUrl(size: 128) }
  reviewRequested: search(query: "is:pr is:open archived:false review-requested:@me", type: ISSUE, first: 100) {
    issueCount
    nodes { ...ItemFields }
  }
  authored: search(query: "is:pr is:open archived:false author:@me", type: ISSUE, first: 100) {
    issueCount
    nodes { ...ItemFields }
  }
  assigned: search(query: "is:open archived:false assignee:@me", type: ISSUE, first: 100) {
    issueCount
    nodes { ...ItemFields }
  }
  rateLimit { cost remaining resetAt limit used }
}

fragment ItemFields on SearchResultItem {
  __typename
  ... on PullRequest {
    id number title url createdAt updatedAt isDraft headRefName
    authorAssociation isCrossRepository
    author { login }
    repository { ...RepoFields }
  }
  ... on Issue {
    id number title url createdAt updatedAt
    authorAssociation
    author { login }
    repository { ...RepoFields }
  }
}

fragment RepoFields on Repository {
  id name owner { login } description visibility
  isArchived archivedAt isDisabled isLocked lockReason
  isFork parent { nameWithOwner }
  defaultBranchRef { name }
  viewerPermission viewerDefaultMergeMethod
  mergeCommitAllowed squashMergeAllowed rebaseMergeAllowed autoMergeAllowed
  deleteBranchOnMerge allowUpdateBranch
  primaryLanguage { name color }
  pushedAt
}
`
