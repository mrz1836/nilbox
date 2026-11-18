package inbox

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reasonFlagDecisions lists every query that reads or writes the reason flags (the inbox
// lists an item is in, reason_pushed among them, and the fourth search's raw match,
// reason_reviewed), with what it does about the items a routine's search watches, which are
// stored in no list, and about those pushed to since the user requested changes, which
// Review requested lists. A query not listed here fails TestEveryReasonFlagQueryIsDecided
// until its decision is written down, so a new one never lists, counts, or prunes watched
// items by accident.
func reasonFlagDecisions() map[string]string {
	return map[string]string{
		// The lists and the snapshot: watched items stay out of the inbox.
		"ListItems":         "none: watched items stay out of the lists; a pushed one is listed",
		"ListListedNodeIDs": "none: watched items get their details in the watch pass; a pushed one is listed",
		"ListListedFlags":   "none: arrivals are the lists' alone; a pushed one counts as Review requested",
		"ListListed":        "none: outcomes are the lists' alone; a pushed one is listed",

		// What suggests Autopilot's templates: "in your inbox" is the lists alone.
		"CountBotPullRequests": "none: it counts the lists' pull requests alone, a pushed one among them",

		// The writes of a whole refresh and of the user's edits.
		"ClearReasons":        "none: the watch pass's own upsert leaves the flags alone; clears all five",
		"UpsertItem":          "none: the inbox's searches set the flags, the fourth's pushed one gated by review_left_at",
		"ClearArrivalStamps":  "none: it follows GitHub's own lists' flags",
		"UnlistItem":          "also records the state, and takes a closed item out of every search and list",
		"LeaveAssigned":       "none",
		"LeaveReview":         "none: a review from the inbox takes a pushed one out too",
		"LeaveAnsweredReview": "none: only a read's answered request; a pushed one waits for the whole refresh",
		"LeavePushed":         "none: a read's verdict no longer pushed past takes a pushed one out, unstamped; never lists one",
		"RejoinReview":        "none: the user's own re-request puts it in Review requested, as GitHub asks; pushed stays",

		// What else reads them.
		"PruneFiles":          "keeps the file checks of watched items, and of pushed ones",
		"ListRepoCounts":      "counts watched items too, so a repository only watched can have launch defaults",
		"ListRunningChecks":   "adds the watched pull requests a routine follows, muted or not; a pushed one is listed",
		"ListOpenPullsInRepo": "adds the watched pull requests a routine follows, as ListRunningChecks does; a pushed one is listed",
		"GetEditItem":         "accepts a watched item (watched), so its sheet acts from the Watching list; pushed reads as requested",

		// The reads for the routines.
		"ListItemsByID":     "reads listed and watched items alike, for the Watching list",
		"ListWatchedItems":  "the routine's lists, and its searches' rows; GitHub's own lists only",
		"PruneItems":        "prunes only an item in no list nor the fourth search, watched by no search, with no triage",
		"ListWatchBaseline": "tells a listed item, whose details the refresh read, from a watched one; pushed is listed",
		"ListFactsRows":     "the lists an item is in, for the routines' facts; GitHub's own lists only",
		"ListItemStates":    "whether an item is listed, for the ledger's retention; pushed is listed",
		"GetReviewItem":     "none: it reads any stored item, so a watched one's sheet opens; pushed reads as requested",
		"GetIssueItem":      "none: it reads any stored item, so a watched one's sheet opens",
	}
}

// reasonFlag matches a reason flag's column.
var reasonFlag = regexp.MustCompile(`\breason_(review_requested|authored|assigned|reviewed|pushed)\b`)

// queryName matches a query's sqlc name comment.
var queryName = regexp.MustCompile(`(?m)^-- name: (\w+) :`)

// flagQueries returns the inbox's queries that name a reason flag, by name.
func flagQueries(t *testing.T) []string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("internal", "queries", "*.sql"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "no query files found; update this guard")

	var out []string

	for _, file := range files {
		raw, readErr := os.ReadFile(file) //nolint:gosec // the package's own query files
		require.NoError(t, readErr)

		text := string(raw)
		starts := queryName.FindAllStringSubmatchIndex(text, -1)

		for i, m := range starts {
			end := len(text)
			if i+1 < len(starts) {
				end = starts[i+1][0]
			}

			if reasonFlag.MatchString(strings.ToLower(text[m[1]:end])) {
				out = append(out, text[m[2]:m[3]])
			}
		}
	}

	slices.Sort(out)

	return out
}

func TestEveryReasonFlagQueryIsDecided(t *testing.T) {
	t.Parallel()

	decisions := reasonFlagDecisions()
	found := flagQueries(t)

	for _, name := range found {
		assert.Contains(t, decisions, name,
			"%s reads or writes the reason flags: decide what it does about watched items, in reasonFlagDecisions", name)
	}

	for name := range decisions {
		assert.Contains(t, found, name, "%s no longer names a reason flag; take it out of reasonFlagDecisions", name)
	}
}
