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

// reasonFlagDecisions lists every query that reads or writes the three reason flags (the
// inbox lists an item is in), with what it does about the items a routine's search watches,
// which are stored in no list. A query not listed here fails TestEveryReasonFlagQueryIsDecided
// until its decision is written down, so a new one never lists, counts, or prunes watched
// items by accident.
func reasonFlagDecisions() map[string]string {
	return map[string]string{
		// The lists and the snapshot: watched items stay out of the inbox.
		"ListItems":         "none: watched items stay out of the lists, the counts, and the menu bar",
		"ListListedNodeIDs": "none: watched items get their details in the watch pass",
		"ListListedFlags":   "none: arrivals are the lists' alone",
		"ListListed":        "none: outcomes are the lists' alone",

		// What suggests Autopilot's templates: "in your inbox" is the lists alone.
		"CountBotPullRequests": "none: it counts the lists' pull requests alone",

		// The writes of a whole refresh and of the user's edits.
		"ClearReasons":        "none: the watch pass's own upsert leaves the flags alone",
		"UpsertItem":          "none: the inbox's searches set the flags; a watched item's are left off",
		"ClearArrivalStamps":  "none: it follows the flags",
		"UnlistItem":          "also records the state, and takes a closed item out of every search",
		"LeaveAssigned":       "none",
		"LeaveReview":         "none",
		"LeaveAnsweredReview": "none",

		// What else reads them.
		"PruneFiles":        "keeps the file checks of watched items",
		"ListRepoCounts":    "counts watched items too, so a repository only watched can have launch defaults",
		"ListRunningChecks": "adds the watched pull requests a routine follows, muted or not",
		"GetEditItem":       "accepts a watched item (watched), so its sheet acts from the Watching list",

		// The reads for the routines.
		"ListItemsByID":     "reads listed and watched items alike, for the Watching list",
		"ListWatchedItems":  "the routine's lists, and its searches' rows",
		"PruneItems":        "prunes only an item in no list, watched by no search, with no triage",
		"ListWatchBaseline": "tells a listed item, whose details the refresh read, from a watched one",
		"ListFactsRows":     "the lists an item is in, for the routines' facts",
		"ListItemStates":    "whether an item is listed, for the ledger's retention",
		"GetReviewItem":     "none: it reads any stored item, so a watched one's sheet opens",
		"GetIssueItem":      "none: it reads any stored item, so a watched one's sheet opens",
	}
}

// reasonFlag matches a reason flag's column.
var reasonFlag = regexp.MustCompile(`\breason_(review_requested|authored|assigned)\b`)

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
