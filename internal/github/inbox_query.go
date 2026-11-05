package github

// inboxQuery is the one GraphQL request behind an inbox refresh: the viewer, three aliased
// searches, and the query's own rate-limit cost. Each search returns the total match
// count and up to 100 items with the fields the trust rule needs (authorAssociation, and
// isCrossRepository for pull requests). __typename tells pull requests from issues.
//
// The viewer's name, profile URL, and avatar URL cost no extra points. The avatar is
// asked for at 128 pixels, enough for a sharp picture at menu size on a Retina display.
const inboxQuery = `query Inbox {
  viewer { login name url avatarUrl(size: 128) }
  reviewRequested: search(query: "is:pr is:open review-requested:@me", type: ISSUE, first: 100) {
    issueCount
    nodes { ...ItemFields }
  }
  authored: search(query: "is:pr is:open author:@me", type: ISSUE, first: 100) {
    issueCount
    nodes { ...ItemFields }
  }
  assigned: search(query: "is:open assignee:@me", type: ISSUE, first: 100) {
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
    repository { name owner { login } }
  }
  ... on Issue {
    id number title url createdAt updatedAt
    authorAssociation
    author { login }
    repository { name owner { login } }
  }
}
`
