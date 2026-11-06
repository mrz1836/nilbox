package github

// inboxQuery is the one GraphQL request behind an inbox refresh: the viewer, three aliased
// searches, and the query's own rate-limit cost. Each search returns the total match
// count and up to 100 items with the fields the trust rule needs (the author's type and
// authorAssociation, and isCrossRepository for pull requests). __typename tells pull
// requests from issues, and, on the author, a bot (Bot) from a person (User).
//
// Each search leaves out archived repositories (archived:false), so their items never
// take a result slot, count toward a total, or cost a details request.
//
// Each item carries its own state, since GitHub's index can trail a merge or a close for
// a while and keep returning the item to an is:open search, and its repository's
// description, state, settings, and the viewer's access to it (RepoFields). Like the
// viewer's name, profile URL, and avatar URL, they cost no extra points: GitHub prices
// lists, and a repository is a single object. The avatar is asked for at 128 pixels,
// enough for a sharp picture at menu size on a Retina display.
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
` + itemFieldsFragment + repoFieldsFragment

// itemFieldsFragment is what the inbox reads about a pull request or issue itself. The
// item query spreads it too, on a single node. The two states are aliased because a pull
// request's and an issue's are different enum types, which GitHub will not merge under
// one name.
const itemFieldsFragment = `
fragment ItemFields on SearchResultItem {
  __typename
  ... on PullRequest {
    id number title url createdAt updatedAt isDraft headRefName
    prState: state
    authorAssociation isCrossRepository
    author { __typename login }
    repository { ...RepoFields }
  }
  ... on Issue {
    id number title url createdAt updatedAt
    issueState: state
    authorAssociation
    author { __typename login }
    repository { ...RepoFields }
  }
}
`

// repoFieldsFragment is what the inbox reads about an item's repository.
const repoFieldsFragment = `
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
