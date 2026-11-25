package inbox

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/heldruns"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/trust"
)

// githubWebURL is the root of GitHub's website, for a profile link stored before the
// profile URL was.
const githubWebURL = "https://github.com/"

// Snapshot is the stored inbox as the UI shows it. Every field is a plain value.
type Snapshot struct {
	// Account is the signed-in account (all "" before the first refresh).
	Account Account `json:"account"`

	// ReviewRequested holds the items that ask for the signed-in user's review, and Yours
	// every other item the user opened or is assigned: each item is in exactly one of
	// them, so the two lists' items add up to the inbox (see addItems).
	ReviewRequested List `json:"reviewRequested"`
	Yours           List `json:"yours"`

	Rate RateGauge `json:"rate"`

	// LastRefreshedAt is the RFC 3339 time of the last successful refresh ("" if none).
	LastRefreshedAt string `json:"lastRefreshedAt"`

	// LastError is the last refresh's error message ("" when it succeeded).
	LastError string `json:"lastError"`

	// ScopeWarning is set when the token's scopes could not be verified.
	ScopeWarning string `json:"scopeWarning"`

	// ProjectAccess says whether the last refresh could read the projects each issue is
	// in, which needs the token's optional read:project scope.
	ProjectAccess ProjectAccess `json:"projectAccess"`

	// Refreshing reports that a whole refresh is under way as the snapshot is read,
	// whoever started it: the window shows it until a snapshot says it ended. It is the
	// Service's own state, never kept with a snapshot; the InboxUpdated that ends a refresh
	// always brings one with it false.
	Refreshing bool `json:"refreshing"`

	// Retry says when the inbox tries again a refresh that failed for a reason a later try
	// may well not meet, as GitHub answering a 500 (see retry.go): the window shows it in
	// place of LastError, which needs nothing of the user then. Like Refreshing, it is the
	// Service's own state, and the InboxUpdated that ends a refresh brings it.
	Retry RefreshRetry `json:"retry"`
}

// Account is the signed-in GitHub account.
type Account struct {
	// Login is the account's username ("" before the first refresh).
	Login string `json:"login"`

	// Name is the profile's display name ("" when the profile sets none).
	Name string `json:"name"`

	// ProfileURL is the account's page on GitHub.
	ProfileURL string `json:"profileUrl"`

	// Avatar is the profile picture as a data: URL, cached locally. It is "" until the
	// current avatar has been downloaded.
	Avatar string `json:"avatar"`
}

// List is one inbox list: its items, how many it shows, and Total, how many GitHub
// matched, which exceeds Shown only when its search matched more than one page returns.
type List struct {
	Items []Item `json:"items"`
	Shown int    `json:"shown"`
	Total int    `json:"total"`
}

// Item is one pull request or issue in a list.
type Item struct {
	ID     int64     `json:"id"`
	Kind   item.Kind `json:"kind"`
	Number int       `json:"number"`
	Title  string    `json:"title"`
	URL    string    `json:"url"`

	// Repo is "owner/name".
	Repo   string `json:"repo"`
	Author string `json:"author"`

	// UpdatedAt is the item's last activity on GitHub, as RFC 3339.
	UpdatedAt string `json:"updatedAt"`

	// Unread reports activity on the item the user hasn't seen: it arrived after the
	// account's first load and was never marked seen, or its last activity is later than
	// the activity it was marked seen at (see MarkSeen and MarkUnseen). The window marks an
	// item as it opens it, and shows the dot while the cards' unread setting is on.
	Unread bool `json:"unread"`

	// Trust is the item's trust class (see the trust package): "member" for the
	// repository's owner, org members, and collaborators, "bot" for a trusted bot, and
	// "outside" for everyone else, every untrusted bot, and every fork.
	Trust trust.Class `json:"trust"`

	// Trusted reports whether Trust is anything but "outside".
	Trusted bool `json:"trusted"`

	// AuthorIsBot reports whether GitHub reports the author as a bot.
	AuthorIsBot bool `json:"authorIsBot"`

	// AuthorAvatarURL is a bot author's picture, on the avatar host, as GitHub gave it,
	// for the window to ask the avatars service for; "" for a person, whose picture is
	// asked for by login, and for a bot no read has brought one for yet.
	AuthorAvatarURL string `json:"authorAvatarUrl"`

	IsCrossRepository bool   `json:"isCrossRepository"`
	AuthorAssociation string `json:"authorAssociation"`

	// Branch is a pull request's head branch ("" for issues).
	Branch  string `json:"branch"`
	IsDraft bool   `json:"isDraft"`

	// Details is nil until a refresh has read the item's details.
	Details *Details `json:"details"`

	// Files is what checking a pull request's changed files found, with the user's
	// categories applied, or nil for an item whose files are not checked: an issue, or a
	// pull request from the repository's own people, a trusted bot, or the signed-in user
	// (see the sensitive package).
	Files *sensitive.Check `json:"files"`

	// DismissedAt is when the item was dismissed, as RFC 3339, or "" when it is not. A
	// dismissed item stays in its lists here, and the UI sets it apart until it is
	// restored.
	DismissedAt string `json:"dismissedAt"`

	// SnoozedUntil is when the item's snooze ends, as RFC 3339, or "" when it was not
	// snoozed. The UI sets the item apart until then; a time already passed means it is
	// back from a snooze.
	SnoozedUntil string `json:"snoozedUntil"`

	// WokeBy is the new activity that ended the item's snooze early, at SnoozedUntil, or
	// nil when its snooze ran its course, is still running, or there was none.
	WokeBy *snooze.Reasons `json:"wokeBy"`

	// NudgedAt is when the user last nudged the author of a pull request from the inbox
	// (Nudge), as RFC 3339, or "" when they haven't since their standing verdict: a nudge
	// from before the user's latest review is a past round's, and shows as none. The card
	// measures how long the author has been quiet from the later of it and UpdatedAt.
	NudgedAt string `json:"nudgedAt"`

	// PinnedAt is when the item was pinned, as RFC 3339, or "" when it is not. Pinned
	// items come first in each list, the most recently pinned first. A pin outlasts a
	// snooze or a dismissal.
	PinnedAt string `json:"pinnedAt"`

	// SetAsideBy is the Autopilot routine that set the item's dismissal or snooze, or last
	// brought it back from one, and PinnedBy the routine that pinned it; 0 when the user
	// did, or nothing did. The window says a routine did it only while the item is set
	// aside, or pinned.
	SetAsideBy int64 `json:"setAsideBy"`
	PinnedBy   int64 `json:"pinnedBy"`

	// RepoRule is the rule the user set for the item's repository or its org, the
	// repository's own winning (see the reporules package): "pinned" counts the item as
	// pinned, after the items pinned themselves, and "muted" sets it apart in its lists'
	// Muted tab, where it needs no one; "" is neither.
	RepoRule reporules.Rule `json:"repoRule"`

	// Authored reports that the signed-in user opened the item, a pull request the authored
	// search found or any item whose author is the user, an issue included.
	Authored bool `json:"authored"`

	// Assigned reports that the item is assigned to the signed-in user, who can take
	// themselves off it (Unassign).
	Assigned bool `json:"assigned"`

	// ReviewRequest says how a pull request in the Review requested list asks for the
	// signed-in user's review, which decides whether they can take themselves off it
	// (RemoveReviewRequest).
	ReviewRequest ReviewRequest `json:"reviewRequest"`

	// ReviewRequestedAt is when the inbox first saw a pull request in Review requested for
	// a request of the user's review, while it is there, as RFC 3339, or "" otherwise (one
	// listed only because its author pushed since the user's review included). It is
	// stamped again each time a request arrives anew, the user's own Re-request my review
	// included, so the card can tell an agent session that ended before the request came
	// back from one that answers it.
	ReviewRequestedAt string `json:"reviewRequestedAt"`

	// ReviewTeams are the slugs of the teams a pull request asks to review, in GitHub's
	// order. It is empty for an issue, and until a refresh has read them.
	ReviewTeams []string `json:"reviewTeams"`

	// Closing reports an open issue that one of its closing pull requests, as its details
	// were last read, merged in moments ago: GitHub closes it a second or two after the
	// merge, and the inbox reads it again shortly to see it closed (see closes.go), so the
	// card shows it closing until then rather than as though nothing happened. It is the
	// Service's own state, as Snapshot.Refreshing is, never kept with a snapshot, and it
	// ends with that follow: an issue GitHub keeps open, as one whose pull request merged
	// into a branch other than the default, shows as it was again.
	Closing bool `json:"closing"`
}

// ReviewRequest says how a pull request asks the signed-in user to review it.
type ReviewRequest string

const (
	// ReviewRequestNone is an item not in the Review requested list.
	ReviewRequestNone ReviewRequest = "none"

	// ReviewRequestYou is a pull request that asks the user by name, which the user can
	// take themselves off. A pull request whose requests no refresh has read yet counts
	// as one, and GitHub's answer to the edit says otherwise.
	ReviewRequestYou ReviewRequest = "you"

	// ReviewRequestTeam is a pull request that asks only teams, one of them the user's:
	// GitHub takes a team's request off for every member or for none, so the user cannot
	// take themselves off it.
	ReviewRequestTeam ReviewRequest = "team"

	// ReviewRequestPushed is a pull request in the list only because its author pushed to
	// it since the user requested changes, without asking again: there is no request to
	// take oneself off.
	ReviewRequestPushed ReviewRequest = "pushed"
)

// ViewerVerdict is the signed-in user's own standing review of a pull request: their
// latest review that approved it or requested changes, as the item's row keeps it
// (viewer_review_state).
type ViewerVerdict string

const (
	// ViewerVerdictNone is a pull request the user holds no verdict on, as far as the
	// reads know (none read yet, or a dismissed review), and every issue.
	ViewerVerdictNone ViewerVerdict = ""

	// ViewerVerdictChanges is a pull request whose user's latest verdict requested
	// changes.
	ViewerVerdictChanges ViewerVerdict = "changes"

	// ViewerVerdictApproved is a pull request whose user's latest verdict approved it.
	ViewerVerdictApproved ViewerVerdict = "approved"
)

// viewerVerdict is the verdict a pull request's stored review state names.
func viewerVerdict(state string) ViewerVerdict {
	switch state {
	case github.ReviewStateChangesRequested:
		return ViewerVerdictChanges
	case github.ReviewStateApproved:
		return ViewerVerdictApproved
	default:
		return ViewerVerdictNone
	}
}

// Details is what an item's inbox card shows beyond its title. The pull request fields
// are zero, or "", for an issue.
type Details struct {
	Labels []Label `json:"labels"`

	// Projects lists the open GitHub projects an issue is in, with its status in each, in
	// GitHub's order. It is empty for a pull request, and for every issue while the
	// snapshot's ProjectAccess is not ProjectAccessOn.
	Projects []Project `json:"projects"`

	// SubIssues counts an issue's sub-issues, and those done. Both are 0 for a pull
	// request and an issue without sub-issues.
	SubIssues SubIssues `json:"subIssues"`

	// Issue is how an issue is planned beyond its projects: its type, issue fields,
	// milestone, parent, and the open issues blocking it and that it blocks. It is empty
	// for a pull request.
	Issue IssueInfo `json:"issue"`

	// ClosingPulls are the first five open or merged pull requests that close an issue once
	// they merge, in GitHub's order. It is empty for a pull request, whose card finds the
	// listed issues it closes in theirs.
	ClosingPulls []ClosingPull `json:"closingPulls"`

	// Comments counts what people wrote in the conversation: its comments and, for a pull
	// request, its review comments and the latest reviews that say something, as the review
	// sheet counts them.
	Comments int `json:"comments"`

	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
	ChangedFiles int `json:"changedFiles"`
	Commits      int `json:"commits"`

	// Mergeable is MERGEABLE, CONFLICTING, or UNKNOWN while GitHub works it out.
	Mergeable string `json:"mergeable"`

	Checks  Checks  `json:"checks"`
	Reviews Reviews `json:"reviews"`

	// AutoMerge is the method a pull request's auto-merge merges with (MERGE, SQUASH, or
	// REBASE), and "" while auto-merge is off and for an issue.
	AutoMerge string `json:"autoMerge"`

	// MergeBot is the listed auto-merge bot (trust.MergeBots) whose latest review approves
	// this pull request, so it merges by itself once its checks pass: the first one listed
	// when more than one did. It is "" when the setting is off, no listed bot approved, and
	// for an issue.
	MergeBot string `json:"mergeBot"`

	// FetchedAt is when the details were read, as RFC 3339.
	FetchedAt string `json:"fetchedAt"`

	// HeadOID is a pull request's head commit as the details last read it, "" for an
	// issue. The card compares it with the commit an agent session's review was left on,
	// so findings about a commit the pull request has moved past don't show as its latest.
	HeadOID string `json:"headOid"`

	// PushedSinceReview reports a pull request whose author pushed since the user requested
	// changes: the user's standing verdict requests changes at a commit other than its head,
	// and the user hasn't reviewed it from the inbox since it last moved. Whichever list it
	// is in, its card says so.
	PushedSinceReview bool `json:"pushedSinceReview"`

	// ReviewRequestedAgain reports such a pull request that asks the user by name to review
	// it again, as the details last read it: the author asked again, or the user did (the
	// review sheet's Re-request my review, recorded at once). Its card says Review
	// re-requested in place of Pushed since your review. It goes by the requests the details
	// read, never by the Review requested search, which can trail a request for a while.
	ReviewRequestedAgain bool `json:"reviewRequestedAgain"`

	// ViewerVerdict is the user's own standing review of a pull request, as the last read
	// of it found (the whole refresh's fourth search, or a read of it alone), so a card
	// whose user requested changes can say it waits on the author, and one the user
	// approved that they approved it. It costs no request: the refresh already reads it for
	// PushedSinceReview.
	ViewerVerdict ViewerVerdict `json:"viewerVerdict"`
}

// Label is one label on an item.
type Label struct {
	Name string `json:"name"`

	// Color is "#rrggbb", or "" when GitHub's value was not a color.
	Color string `json:"color"`
}

// Project is an issue's place in one GitHub project.
type Project struct {
	// Title is the project's title, and URL its page on GitHub.
	Title string `json:"title"`
	URL   string `json:"url"`

	// Status is the issue's option in the project's Status field, such as "In progress",
	// or "" when it has none.
	Status string `json:"status"`

	// StatusColor is the status's color as "#rrggbb", or "" when it has none.
	StatusColor string `json:"statusColor"`

	// Fields are the issue's values in the project's other fields, such as Priority or
	// Size, in GitHub's order.
	Fields []ProjectField `json:"fields"`
}

// ProjectField is an issue's value in one project field.
type ProjectField struct {
	// Name is the field's name, such as "Priority".
	Name string `json:"name"`

	// Value is the value as the project shows it: an option's name, a number, text, a date
	// such as "Jun 27, 2026", or an iteration's title.
	Value string `json:"value"`

	// Color is a single-select option's color as "#rrggbb", and "" for any other value.
	Color string `json:"color"`
}

// IssueInfo is how an issue is planned, beyond the projects it is in, as GitHub's sidebar
// shows it.
type IssueInfo struct {
	// Type is the issue's type, such as "Task", and TypeColor its color as "#rrggbb"; both
	// "" when it has none.
	Type      string `json:"type"`
	TypeColor string `json:"typeColor"`

	// Fields are the issue's values in its organization's issue fields, such as Priority,
	// in GitHub's order.
	Fields []IssueField `json:"fields"`

	// Milestone is the milestone the issue is in, null when it is in none.
	Milestone *Milestone `json:"milestone"`

	// Parent is the issue it is a sub-issue of, null when it is none's.
	Parent *IssueRef `json:"parent"`

	// BlockedBy counts the open issues that block it, and Blocking the open issues it
	// blocks.
	BlockedBy int `json:"blockedBy"`
	Blocking  int `json:"blocking"`
}

// IssueField is an issue's value in one of its organization's issue fields.
type IssueField struct {
	// Name is the field's name, such as "Priority".
	Name string `json:"name"`

	// Value is the value as GitHub shows it: an option's name, the options' names joined
	// by commas, a number, text, or a date such as "Jun 27, 2026".
	Value string `json:"value"`

	// Color is a single-select option's color as "#rrggbb", and "" for any other value.
	Color string `json:"color"`
}

// Milestone is the milestone an issue is in.
type Milestone struct {
	Title string `json:"title"`
	URL   string `json:"url"`

	// DueOn is its due day as "2006-01-02", a day in no time zone, or "" when it has none.
	DueOn string `json:"dueOn"`

	// Closed reports a closed milestone.
	Closed bool `json:"closed"`

	// Progress is how far along it is, as the percentage of its issues and pull requests
	// closed, from 0 to 100.
	Progress float64 `json:"progress"`
}

// IssueRef names another issue: the one an issue is a sub-issue of.
type IssueRef struct {
	// Repo is its repository as "owner/name".
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

// ClosingPull is a pull request that closes an issue once it merges, open or merged: one
// whose description says it closes the issue, or one someone linked to it by hand.
type ClosingPull struct {
	// Repo is its repository as "owner/name", which may be another than the issue's.
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`

	// Merged reports one that merged; it is open otherwise.
	Merged  bool `json:"merged"`
	IsDraft bool `json:"isDraft"`

	// Trusted reports one the signed-in user opened, or one from someone trusted with its
	// repository, as an item's trust is decided (see the trust package): anyone can say a
	// pull request of theirs closes an issue, from a fork too, so only a trusted one tells
	// the issue's card that the work moved to it.
	Trusted bool `json:"trusted"`
}

// SubIssues counts an issue's sub-issues.
type SubIssues struct {
	Total int `json:"total"`

	// Completed is how many are closed as done; never more than Total.
	Completed int `json:"completed"`
}

// Checks counts the head commit's check runs and commit statuses by outcome.
type Checks struct {
	// State is GitHub's overall result: SUCCESS, FAILURE, ERROR, PENDING, or EXPECTED,
	// and "" when the head commit has no checks.
	State string `json:"state"`

	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Pending int `json:"pending"`
	Skipped int `json:"skipped"`

	// Awaiting counts the workflow runs held until a maintainer approves them whose
	// approval GitHub takes from the app (the review sheet's Approve workflows to run),
	// which State and the other counts leave out (github.Checks.Awaiting), but those of a
	// workflow the user ignores (heldruns.Ignored).
	Awaiting int `json:"awaiting"`

	// AwaitingOnGitHub counts the held runs GitHub approves only on its site, which State
	// and the other counts leave out too (github.Checks.AwaitingOnGitHub), only while GitHub
	// reports the merge blocked, when one may be what blocks it, and never those of a
	// workflow the user ignores (heldruns.Ignored).
	AwaitingOnGitHub int `json:"awaitingOnGitHub"`
}

// Reviews counts who was asked to review a pull request and who has.
type Reviews struct {
	// Decision is APPROVED, CHANGES_REQUESTED, or REVIEW_REQUIRED, and "" when the
	// repository requires no review.
	Decision string `json:"decision"`

	// Reviewers is everyone asked to review or who has, the author excluded; Reviewed
	// is how many of them have reviewed and are not asked to review again.
	Reviewers int `json:"reviewers"`
	Reviewed  int `json:"reviewed"`

	// Approvals and ChangesRequested count the reviewers whose latest review is one.
	Approvals        int `json:"approvals"`
	ChangesRequested int `json:"changesRequested"`
}

// RateGauge holds the latest reading of each budget.
type RateGauge struct {
	GraphQL Rate `json:"graphql"`
	Core    Rate `json:"core"`
	Search  Rate `json:"search"`
}

// Rate is one budget reading. Known is false until the first refresh records one.
type Rate struct {
	Known     bool `json:"known"`
	Limit     int  `json:"limit"`
	Remaining int  `json:"remaining"`
	Used      int  `json:"used"`

	// Cost is the point cost of the inbox query (GraphQL only).
	Cost int `json:"cost"`

	ResetAt string `json:"resetAt"`
	TakenAt string `json:"takenAt"`
}

// Snapshot returns the stored inbox: the two lists, the latest rate readings, and the
// refresh status.
//
// The snapshot is kept until the next write, and only built again when something changed
// what it shows: reading a kept one costs a read of the settings it depends on. The
// lists of every snapshot returned share their items with the one kept, so a caller
// must not change them.
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	seen := s.snapshots.load()

	if seen != nil && seen.snap != nil {
		stored, err := s.prefs.GetMany(ctx, settingsPrefs()...)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read the inbox status: %w", err)
		}

		if keyOf(stored) == seen.settings {
			return s.withRunState(*seen.snap, seen.accountID), nil
		}
	}

	snap, settings, accountID, err := s.buildSnapshot(ctx)
	if err != nil {
		return s.withRunState(snap, accountID), err
	}

	s.snapshots.keep(seen, snap, settings, accountID)

	return s.withRunState(snap, accountID), nil
}

// withRunState returns snap, a copy, saying whether a whole refresh runs now, when a
// failed one is tried again, and which of its issues GitHub is closing after a merge of
// the account with accountID (see closeQueue.mark). The snapshot kept is never changed,
// so a refresh's start or end, a try planned, or a merge's follow needs no new one.
func (s *Service) withRunState(snap Snapshot, accountID int64) Snapshot {
	snap.Refreshing = s.refreshing.Load()
	snap.Retry = s.retries.state()
	s.closes.mark(accountID, &snap.ReviewRequested, &snap.Yours)

	return snap
}

// buildSnapshot reads the stored inbox, and returns it with the settings it was built
// under and the ID of the account it is, 0 when none is signed in. When the account
// cannot be read, the snapshot returned holds the status alone.
func (s *Service) buildSnapshot(ctx context.Context) (Snapshot, settingsKey, int64, error) {
	status, err := readStatus(ctx, s.prefs)
	if err != nil {
		return Snapshot{}, settingsKey{}, 0, err
	}

	settings := keyOf(status)
	snap := newSnapshot(status)

	accountID, err := lookupAccount(ctx, s.q, status[prefViewer])
	if err != nil || accountID == 0 {
		return snap, settings, 0, err
	}

	rows, err := loadItems(ctx, s.q, accountID)
	if err != nil {
		return Snapshot{}, settingsKey{}, 0, err
	}

	snap.addItems(rows, s.scopes.scope(status), status[prefViewer], reporules.Decode(status), cardRulesOf(status))
	snap.applyTotals(status)

	if snap.Rate, err = loadRates(ctx, s.q, accountID); err != nil {
		return Snapshot{}, settingsKey{}, 0, err
	}

	return snap, settings, accountID, nil
}

// newSnapshot starts a snapshot with empty lists and the stored status.
func newSnapshot(prefs map[string]string) Snapshot {
	return Snapshot{
		Account:         accountFromPrefs(prefs),
		ReviewRequested: List{Items: []Item{}},
		Yours:           List{Items: []Item{}},
		LastRefreshedAt: prefs[prefLastRefreshAt],
		LastError:       prefs[prefLastError],
		ScopeWarning:    prefs[prefScopeWarning],
		ProjectAccess:   decodeProjectAccess(prefs[prefProjects]),
	}
}

// accountFromPrefs reads the signed-in account. The cached avatar is used only while it
// is the picture GitHub last reported, so another account's avatar never shows.
func accountFromPrefs(prefs map[string]string) Account {
	login := prefs[prefViewer]
	if login == "" {
		return Account{}
	}

	account := Account{Login: login, Name: prefs[prefViewerName], ProfileURL: prefs[prefViewerURL]}

	if account.ProfileURL == "" {
		account.ProfileURL = githubWebURL + login
	}

	if source := prefs[prefAvatarSource]; source != "" && source == prefs[prefViewerAvatarURL] {
		account.Avatar = prefs[prefAvatar]
	}

	return account
}

// addItems places each row in the one list it belongs to, deciding each item's trust, and
// whether its files are checked, with scope, how it asks viewer, the signed-in account, to
// review it, which of rules applies to its repository, and what its card says with cards,
// the user's card rules. The lists partition the rows:
// one that asks for the user's review, or was pushed to since the user requested changes,
// is in ReviewRequested, whatever else it is, and one
// the user opened or is assigned is otherwise in Yours, so no item is in both. Each list
// then holds the items pinned themselves first, as rows has them, then those pinned with
// their repository or org, then the rest, each in rows' order.
func (s *Snapshot) addItems(rows []itemRow, scope fileScope, viewer string, rules reporules.Rules, cards cardRules) {
	for _, r := range rows {
		item := r.item(scope, viewer, cards)
		item.RepoRule, _ = rules.For(repo.Name{Owner: r.RepoOwner, Repo: r.RepoName})

		switch {
		case r.ReasonReviewRequested || r.ReasonPushed:
			s.ReviewRequested.Items = append(s.ReviewRequested.Items, item)
		case r.ReasonAuthored || r.ReasonAssigned:
			s.Yours.Items = append(s.Yours.Items, item)
		}
	}

	for _, l := range []*List{&s.ReviewRequested, &s.Yours} {
		slices.SortStableFunc(l.Items, func(a, b Item) int { return cmp.Compare(a.pinRank(), b.pinRank()) })
	}
}

// cardRules are the user's settings that decide what a card says of its item beyond its
// trust and its files: the workflows whose held runs never count (heldruns), and the
// auto-merge bots whose approval it names (trust.MergeBots).
type cardRules struct {
	ignored   []string
	mergeBots trust.MergeBots
}

// cardRulesOf reads the card rules from the stored status.
func cardRulesOf(status map[string]string) cardRules {
	return cardRules{
		ignored:   heldruns.Decode(status[heldruns.KeyIgnoredWorkflows]),
		mergeBots: trust.DecodeMergeBots(status),
	}
}

// pinRank is where the item comes in its lists: 0 pinned itself, 1 pinned with its
// repository or org, 2 neither.
func (it Item) pinRank() int {
	switch {
	case it.PinnedAt != "":
		return 0
	case it.RepoRule == reporules.RulePinned:
		return 1
	default:
		return 2
	}
}

// applyTotals sets each list's Shown and Total: the items it shows, and those and the ones
// the last whole refresh's searches behind it matched past the page GitHub returned
// (beyond). Review requested is one search's. Yours is the authored and the assigned
// searches', so its Total is an upper bound once either was cut to a page: an item past
// one search's page may be in the other's too, or ask for the user's review. An item that
// left the list since is counted out of both. A missing or bad count adds none.
func (s *Snapshot) applyTotals(prefs map[string]string) {
	for _, l := range []struct {
		list *List
		keys []string
	}{
		{&s.ReviewRequested, []string{prefBeyondReviewRequested}},
		{&s.Yours, []string{prefBeyondAuthored, prefBeyondAssigned}},
	} {
		l.list.Shown = len(l.list.Items)
		l.list.Total = l.list.Shown

		for _, key := range l.keys {
			past, _ := strconv.Atoi(prefs[key]) // a missing or bad count adds none
			l.list.Total += max(past, 0)
		}
	}
}

// itemRow is one listed item as a snapshot reads it, with the columns of missing
// details or a missing file check at their zero values.
type itemRow struct {
	ID                    int64
	Kind                  item.Kind
	Number                int
	Title                 string
	URL                   string
	RepoOwner             string
	RepoName              string
	AuthorLogin           string
	AuthorType            string
	AuthorAvatarURL       string
	AuthorAssociation     string
	IsCrossRepository     bool
	IsDraft               bool
	HeadRef               string
	ItemUpdatedAt         time.Time
	ReasonReviewRequested bool
	ReasonAuthored        bool
	ReasonAssigned        bool

	// ReasonPushed reports a pull request listed because its author pushed to it since the
	// user requested changes, and ViewerReviewState and ViewerReviewCommit are the user's
	// verdict as the last whole refresh read it, "" with none. ReviewLeftAt is when the user
	// last took it out of Review requested from the inbox, nil when they haven't since it
	// moved.
	ReasonPushed       bool
	ViewerReviewState  string
	ViewerReviewCommit string
	ReviewLeftAt       *time.Time

	// SeenActivityAt is the activity the user last saw on the item, nil when they never
	// did (see unread).
	SeenActivityAt *time.Time

	// ViewerReviewAt is when the user's verdict was submitted, and NudgedAt when they last
	// nudged the author from the inbox, each nil with none (see nudged).
	ViewerReviewAt *time.Time
	NudgedAt       *time.Time

	// ReviewRequestedAt is when the inbox first saw the item in Review requested, while it
	// is there, nil otherwise (see Item.ReviewRequestedAt).
	ReviewRequestedAt *time.Time

	// HasDetails reports whether a refresh has read the item's details.
	HasDetails             bool
	Labels                 []storedLabel
	Projects               []storedProject
	Comments               int
	Additions              int
	Deletions              int
	ChangedFiles           int
	Commits                int
	Mergeable              string
	ChecksState            string
	ChecksPassed           int
	ChecksFailed           int
	ChecksPending          int
	ChecksSkipped          int
	ChecksAwaiting         int
	ChecksAwaitingOnGitHub int
	ReviewDecision         string

	// HeldRuns are the held runs ChecksAwaiting and ChecksAwaitingOnGitHub count, each with
	// its workflow's name: none in a row stored before the names were (Unnamed). MergeState
	// is GitHub's mergeStateStatus as the details read it, "" before they did.
	HeldRuns   []heldruns.Run
	MergeState string

	Reviewers        int
	Reviewed         int
	Approvals        int
	ChangesRequested int
	SubIssues        SubIssues
	IssueInfo        storedIssueInfo
	ClosingPulls     []storedClosingPull

	// ApprovingBots are the bots whose latest review approves a pull request, by login, as
	// the details last read them: none in a row stored before they were.
	ApprovingBots []string

	// ReviewTexts counts the latest reviews that say something, which Comments, GitHub's own
	// count, leaves out: 0 in a row stored before they were counted.
	ReviewTexts int

	DetailsFetchedAt *time.Time
	HeadOID          string
	AutoMerge        string

	// ReviewUsers and ReviewTeams are who a pull request asks to review: the logins of
	// the people and the slugs of the teams. Both are nil until a refresh reads them.
	ReviewUsers []string
	ReviewTeams []string

	// Stored is the item's last file check, or nil when there is none.
	Stored *sensitive.Result

	DismissedAt  *time.Time
	SnoozedUntil *time.Time
	WakeReasons  *string
	PinnedAt     *time.Time
	SetAsideBy   *int64
	PinnedBy     *int64
}

// itemRowOf converts a ListItems row, decoding its labels, its projects, and its file
// check.
func itemRowOf(r queries.ListItemsRow) (itemRow, error) {
	labels, err := decodeColumn[storedLabel](r.Labels)
	if err != nil {
		return itemRow{}, fmt.Errorf("read the labels of item %d: %w", r.ID, err)
	}

	projects, err := decodeColumn[storedProject](r.Projects)
	if err != nil {
		return itemRow{}, fmt.Errorf("read the projects of item %d: %w", r.ID, err)
	}

	reviewUsers, err := decodeColumn[string](r.ReviewUsers)
	if err != nil {
		return itemRow{}, fmt.Errorf("read the requested reviewers of item %d: %w", r.ID, err)
	}

	reviewTeams, err := decodeColumn[string](r.ReviewTeams)
	if err != nil {
		return itemRow{}, fmt.Errorf("read the requested teams of item %d: %w", r.ID, err)
	}

	held, err := decodeColumn[heldruns.Run](r.ChecksHeld)
	if err != nil {
		return itemRow{}, fmt.Errorf("read the held workflow runs of item %d: %w", r.ID, err)
	}

	issueInfo, err := decodeIssueInfo(r.IssueInfo)
	if err != nil {
		return itemRow{}, fmt.Errorf("read how issue %d is planned: %w", r.ID, err)
	}

	closingPulls, err := decodeColumn[storedClosingPull](r.ClosingPulls)
	if err != nil {
		return itemRow{}, fmt.Errorf("read the pull requests that close issue %d: %w", r.ID, err)
	}

	approvingBots, err := decodeColumn[string](r.ApprovingBots)
	if err != nil {
		return itemRow{}, fmt.Errorf("read the bots that approved item %d: %w", r.ID, err)
	}

	stored, err := filesColumns{
		itemID: r.FilesItemID, head: r.FilesHeadOID, total: r.FilesTotal, files: r.Files,
		complete: r.FilesComplete, err: r.FilesError, checkedAt: r.FilesCheckedAt,
	}.result()
	if err != nil {
		return itemRow{}, fmt.Errorf("item %d: %w", r.ID, err)
	}

	return itemRow{
		ID: r.ID, Kind: item.Kind(r.Kind), Number: int(r.Number), Title: r.Title, URL: r.URL,
		RepoOwner: r.RepoOwner, RepoName: r.RepoName, AuthorLogin: r.AuthorLogin, AuthorType: r.AuthorType,
		AuthorAvatarURL: r.AuthorAvatarURL, AuthorAssociation: r.AuthorAssociation,
		IsCrossRepository: r.IsCrossRepository, IsDraft: r.IsDraft,
		HeadRef: r.HeadRef, ItemUpdatedAt: r.ItemUpdatedAt, ReasonReviewRequested: r.ReasonReviewRequested,
		ReasonAuthored: r.ReasonAuthored, ReasonAssigned: r.ReasonAssigned, ReasonPushed: r.ReasonPushed,
		ViewerReviewState: r.ViewerReviewState, ViewerReviewCommit: r.ViewerReviewCommit, ReviewLeftAt: r.ReviewLeftAt,
		SeenActivityAt: r.SeenActivityAt, ViewerReviewAt: r.ViewerReviewAt, NudgedAt: r.NudgedAt,
		ReviewRequestedAt: r.ReviewRequestedAt,

		HasDetails: r.DetailsItemID != nil, Labels: labels, Projects: projects,
		Comments: int(store.Deref(r.Comments)), Additions: int(store.Deref(r.Additions)),
		ReviewTexts: int(store.Deref(r.ReviewTexts)),
		Deletions:   int(store.Deref(r.Deletions)), ChangedFiles: int(store.Deref(r.ChangedFiles)),
		Commits: int(store.Deref(r.Commits)), Mergeable: store.Deref(r.Mergeable),
		ChecksState: store.Deref(r.ChecksState), ChecksPassed: int(store.Deref(r.ChecksPassed)),
		ChecksFailed: int(store.Deref(r.ChecksFailed)), ChecksPending: int(store.Deref(r.ChecksPending)),
		ChecksSkipped: int(store.Deref(r.ChecksSkipped)), ChecksAwaiting: int(store.Deref(r.ChecksAwaiting)),
		ChecksAwaitingOnGitHub: int(store.Deref(r.ChecksAwaitingOnGithub)),
		HeldRuns:               held, MergeState: store.Deref(r.MergeState),
		ReviewDecision: store.Deref(r.ReviewDecision), Reviewers: int(store.Deref(r.Reviewers)),
		Reviewed:  int(store.Deref(r.Reviewed)),
		Approvals: int(store.Deref(r.Approvals)), ChangesRequested: int(store.Deref(r.ChangesRequested)),
		DetailsFetchedAt: r.DetailsFetchedAt, HeadOID: store.Deref(r.HeadOID), AutoMerge: store.Deref(r.AutoMergeMethod),
		SubIssues: SubIssues{
			Total: int(store.Deref(r.SubIssuesTotal)), Completed: int(store.Deref(r.SubIssuesCompleted)),
		},
		IssueInfo:     issueInfo,
		ClosingPulls:  closingPulls,
		ApprovingBots: approvingBots,
		ReviewUsers:   reviewUsers, ReviewTeams: reviewTeams,

		Stored: stored,

		DismissedAt: r.DismissedAt, SnoozedUntil: r.SnoozedUntil, WakeReasons: r.WakeReasons, PinnedAt: r.PinnedAt,
		SetAsideBy: r.SetAsideBy, PinnedBy: r.PinnedBy,
	}, nil
}

// item converts a row to its DTO, deciding its trust, and whether its files are
// checked, with scope, how it asks viewer to review it, and what its card says with cards,
// the user's card rules.
func (r *itemRow) item(scope fileScope, viewer string, cards cardRules) Item {
	isPR := r.Kind == item.KindPR
	author := trust.Author{Login: r.AuthorLogin, Type: r.AuthorType, Association: r.AuthorAssociation}
	class := scope.policy.Classify(author, isPR, r.IsCrossRepository)

	return Item{
		ID:                r.ID,
		Kind:              r.Kind,
		Number:            r.Number,
		Title:             r.Title,
		URL:               r.URL,
		Repo:              repo.Name{Owner: r.RepoOwner, Repo: r.RepoName}.String(),
		Author:            r.AuthorLogin,
		UpdatedAt:         clock.Format(r.ItemUpdatedAt),
		Unread:            r.unread(),
		Trust:             class,
		Trusted:           class != trust.ClassOutside,
		AuthorIsBot:       author.IsBot(),
		AuthorAvatarURL:   r.AuthorAvatarURL,
		IsCrossRepository: r.IsCrossRepository,
		AuthorAssociation: r.AuthorAssociation,
		Branch:            r.HeadRef,
		IsDraft:           r.IsDraft,
		Details:           r.details(scope.policy, viewer, cards),
		Files:             r.checkedFiles(scope, isPR, author),
		DismissedAt:       formatOptionalTime(r.DismissedAt),
		SnoozedUntil:      formatOptionalTime(r.SnoozedUntil),
		WokeBy:            decodeReasons(r.WakeReasons),
		NudgedAt:          formatOptionalTime(r.nudged()),
		PinnedAt:          formatOptionalTime(r.PinnedAt),
		SetAsideBy:        store.Deref(r.SetAsideBy),
		PinnedBy:          store.Deref(r.PinnedBy),
		Authored:          r.opened(viewer),
		Assigned:          r.ReasonAssigned,
		ReviewRequest:     r.reviewRequest(viewer),
		ReviewRequestedAt: formatOptionalTime(r.ReviewRequestedAt),
		ReviewTeams:       textutil.NonNil(r.ReviewTeams),
	}
}

// nudged is when the user last nudged the row's author, or nil when they never did, or did
// only before their standing verdict was submitted, in a round of review since over.
func (r *itemRow) nudged() *time.Time {
	if r.NudgedAt == nil || (r.ViewerReviewAt != nil && !r.NudgedAt.After(*r.ViewerReviewAt)) {
		return nil
	}

	return r.NudgedAt
}

// unread reports activity on the row the user hasn't seen: they never saw it, or its last
// activity is later than the activity they last saw (see Item.Unread).
func (r *itemRow) unread() bool {
	return r.SeenActivityAt == nil || r.ItemUpdatedAt.After(*r.SeenActivityAt)
}

// opened reports that viewer opened the row: the authored search found it, or, for what
// that search leaves out (an issue, or a pull request past its page), the author is viewer.
func (r *itemRow) opened(viewer string) bool {
	return r.ReasonAuthored || (viewer != "" && strings.EqualFold(r.AuthorLogin, viewer))
}

// reviewRequest says how the row asks viewer to review it: by name, or only through
// teams, when it is in the Review requested list. A row whose requests are not known, or
// that names neither viewer nor a team, as GitHub's search can still list it for a while
// after a change, counts as asking by name, and the edit's answer says otherwise. One in
// the list only because it was pushed to since the user requested changes asks nothing.
func (r *itemRow) reviewRequest(viewer string) ReviewRequest {
	switch {
	case r.ReasonReviewRequested:
	case r.ReasonPushed:
		return ReviewRequestPushed
	default:
		return ReviewRequestNone
	}

	asks := github.ReviewRequests{Users: r.ReviewUsers, Teams: r.ReviewTeams}
	if len(asks.Teams) > 0 && !asks.Asks(viewer) {
		return ReviewRequestTeam
	}

	return ReviewRequestYou
}

// checkedFiles evaluates the row's last file check with scope's categories, or returns nil
// when its files are not checked.
func (r *itemRow) checkedFiles(scope fileScope, isPR bool, author trust.Author) *sensitive.Check {
	if !scope.checks(isPR, author, r.IsCrossRepository) {
		return nil
	}

	return new(scope.matcher.Evaluate(r.Stored, r.HeadOID, r.URL))
}

// project converts a stored project. Its fields are never nil, even when a build before
// them stored it.
func (p storedProject) project() Project {
	fields := make([]ProjectField, 0, len(p.Fields))
	for _, f := range p.Fields {
		fields = append(fields, ProjectField(f))
	}

	return Project{Title: p.Title, URL: p.URL, Status: p.Status, StatusColor: p.StatusColor, Fields: fields}
}

// issueInfo converts how a stored issue is planned to its DTO, whose fields are never nil.
func (s storedIssueInfo) issueInfo() IssueInfo {
	fields := make([]IssueField, 0, len(s.Fields))
	for _, f := range s.Fields {
		fields = append(fields, IssueField(f))
	}

	info := IssueInfo{
		Type: s.Type, TypeColor: s.TypeColor, Fields: fields,
		BlockedBy: max(s.BlockedBy, 0), Blocking: max(s.Blocking, 0),
	}

	if m := s.Milestone; m != nil {
		info.Milestone = &Milestone{
			Title: m.Title, URL: m.URL, DueOn: m.DueOn, Closed: m.Closed, Progress: min(max(m.Progress, 0), 100),
		}
	}

	if p := s.Parent; p != nil {
		info.Parent = &IssueRef{Repo: p.Repo, Number: p.Number, Title: p.Title, URL: p.URL}
	}

	return info
}

// closingPulls converts an issue's stored closing pull requests, deciding each one's trust
// with policy: viewer's own are trusted however they were opened. Never nil.
func closingPulls(stored []storedClosingPull, policy trust.Policy, viewer string) []ClosingPull {
	out := make([]ClosingPull, 0, len(stored))

	for _, p := range stored {
		author := trust.Author{Login: p.AuthorLogin, Type: p.AuthorType, Association: p.AuthorAssociation}
		mine := viewer != "" && strings.EqualFold(p.AuthorLogin, viewer)

		out = append(out, ClosingPull{
			Repo: p.Repo, Number: p.Number, Title: p.Title, URL: p.URL, Merged: p.Merged, IsDraft: p.Draft,
			Trusted: mine || policy.Trusted(author, true, p.Fork),
		})
	}

	return out
}

// decodeColumn reads a nullable JSON array column, nil when it is NULL.
func decodeColumn[T any](raw *string) ([]T, error) {
	if raw == nil {
		return nil, nil
	}

	return store.DecodeJSON[[]T](*raw)
}

// decodeIssueInfo reads a stored issue_info column, or none when the item has no details.
func decodeIssueInfo(raw *string) (storedIssueInfo, error) {
	if raw == nil {
		return storedIssueInfo{}, nil
	}

	return store.DecodeJSON[storedIssueInfo](*raw)
}

// decodeReasons reads stored wake reasons, or nil when there are none or they cannot be
// read; a card without its reasons still shows the item as back.
func decodeReasons(raw *string) *snooze.Reasons {
	if raw == nil {
		return nil
	}

	reasons, err := store.DecodeJSON[snooze.Reasons](*raw)
	if err != nil {
		return nil
	}

	// A review outcome this build does not know, as another build may have stored, is
	// read as none.
	if !reasons.Review.Valid() {
		reasons.Review = snooze.ReviewNone
	}

	if !reasons.Woke() {
		return nil
	}

	return &reasons
}

// details converts the row's details, or nil when no refresh has read them, telling whether
// the pull request asks viewer to review it again after a push, counting its held runs
// as heldruns.Count does with the workflows cards ignores, naming the auto-merge bot of
// cards that approved it, and deciding with policy which of an issue's closing pull
// requests are trusted.
func (r *itemRow) details(policy trust.Policy, viewer string, cards cardRules) *Details {
	if !r.HasDetails {
		return nil
	}

	labels := make([]Label, 0, len(r.Labels))
	for _, l := range r.Labels {
		labels = append(labels, Label(l))
	}

	projects := make([]Project, 0, len(r.Projects))
	for _, p := range r.Projects {
		projects = append(projects, p.project())
	}

	pushed := r.pushedSinceReview()

	verdict := ViewerVerdictNone
	if r.Kind == item.KindPR {
		verdict = viewerVerdict(r.ViewerReviewState)
	}

	held := heldruns.Unnamed(r.HeldRuns, r.ChecksAwaiting, r.ChecksAwaitingOnGitHub)
	awaiting, onGitHub := heldruns.Count(held, r.MergeState, cards.ignored)

	return &Details{
		Labels:       labels,
		Projects:     projects,
		Comments:     r.Comments + r.ReviewTexts,
		Additions:    r.Additions,
		Deletions:    r.Deletions,
		ChangedFiles: r.ChangedFiles,
		Commits:      r.Commits,
		Mergeable:    r.Mergeable,
		Checks: Checks{
			State: r.ChecksState, Passed: r.ChecksPassed, Failed: r.ChecksFailed,
			Pending: r.ChecksPending, Skipped: r.ChecksSkipped, Awaiting: awaiting, AwaitingOnGitHub: onGitHub,
		},
		Reviews: Reviews{
			Decision: r.ReviewDecision, Reviewers: r.Reviewers, Reviewed: r.Reviewed,
			Approvals: r.Approvals, ChangesRequested: r.ChangesRequested,
		},
		SubIssues:    r.SubIssues,
		Issue:        r.IssueInfo.issueInfo(),
		ClosingPulls: closingPulls(r.ClosingPulls, policy, viewer),
		AutoMerge:    r.AutoMerge,
		MergeBot:     cards.mergeBots.Match(r.ApprovingBots),
		FetchedAt:    formatOptionalTime(r.DetailsFetchedAt),
		HeadOID:      r.HeadOID,

		PushedSinceReview:    pushed,
		ReviewRequestedAgain: pushed && github.ReviewRequests{Users: r.ReviewUsers}.Asks(viewer),
		ViewerVerdict:        verdict,
	}
}

// pushedSinceReview reports a pull request whose author pushed since the user requested
// changes (see Details.PushedSinceReview): the user's verdict requests changes at a commit
// other than the head its details read, and the user's own review from the inbox, which
// stamps ReviewLeftAt, isn't since it last moved.
func (r *itemRow) pushedSinceReview() bool {
	switch {
	case r.Kind != item.KindPR || r.ViewerReviewState != github.ReviewStateChangesRequested:
		return false
	case r.ViewerReviewCommit == "" || r.HeadOID == "" || strings.EqualFold(r.ViewerReviewCommit, r.HeadOID):
		return false
	default:
		return r.ReviewLeftAt == nil || r.ItemUpdatedAt.After(*r.ReviewLeftAt)
	}
}

// lookupAccount returns the id of the account with login, or 0 when there is none.
func lookupAccount(ctx context.Context, q *queries.Queries, login string) (int64, error) {
	if login == "" {
		return 0, nil
	}

	id, err := q.AccountID(ctx, login)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("read the account: %w", err)
	default:
		return id, nil
	}
}

// loadItems reads the account's listed items, in the order ListItems returns them.
func loadItems(ctx context.Context, q *queries.Queries, accountID int64) ([]itemRow, error) {
	stored, err := q.ListItems(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("read the inbox items: %w", err)
	}

	rows, err := store.ConvertRows(stored, itemRowOf)
	if err != nil {
		return nil, fmt.Errorf("read the inbox items: %w", err)
	}

	return rows, nil
}

// loadRates reads the newest snapshot of each budget.
func loadRates(ctx context.Context, q *queries.Queries, accountID int64) (RateGauge, error) {
	var (
		gauge RateGauge
		err   error
	)

	for _, r := range []struct {
		dst      *Rate
		resource string
	}{
		{&gauge.GraphQL, resourceGraphQL},
		{&gauge.Core, resourceCore},
		{&gauge.Search, resourceSearch},
	} {
		if *r.dst, err = loadRate(ctx, q, accountID, r.resource); err != nil {
			return RateGauge{}, err
		}
	}

	return gauge, nil
}

// loadRate reads the newest snapshot of one budget.
func loadRate(ctx context.Context, q *queries.Queries, accountID int64, resource string) (Rate, error) {
	r, err := q.GetLatestRate(ctx, queries.GetLatestRateParams{AccountID: accountID, Resource: resource})

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Rate{}, nil
	case err != nil:
		return Rate{}, fmt.Errorf("read the %s rate limit: %w", resource, err)
	}

	return Rate{
		Known:     true,
		Limit:     int(r.LimitTotal),
		Remaining: int(r.Remaining),
		Used:      int(r.Used),
		Cost:      int(r.Cost),
		ResetAt:   clock.Format(r.ResetAt),
		TakenAt:   clock.Format(r.TakenAt),
	}, nil
}

// formatOptionalTime renders a nullable stored time as RFC 3339 in UTC, or "" when it is
// NULL.
func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}

	return clock.Format(*t)
}
