// Package issues is the issue sheet: it shows one issue the user is assigned (its
// description, its conversation, where it stands, and the details beside it) and acts on it
// at the user's click: comment on it, or close it as completed or as not planned.
//
// Open shows what the sheet read before, from its own cache, with no request; Refresh reads
// the issue again with one GraphQL request when it may have changed, and ReadAgain whenever
// the user asks; Earlier reads an earlier page of the conversation. Every read and action
// goes through the inbox's Actor (the item's lock and the account check), so the card is
// rewritten from the same answer, and every action is decided again (decide.go) before
// anything is sent. A close is pinned to the comments the user saw: it reads the newest
// comments first, and closes nothing over one someone else wrote since.
//
// The package reaches the inbox only through the interfaces it declares, which the app's
// core adapts. It owns issue_views, the bounded cache of what it read.
package issues
