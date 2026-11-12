package github

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// updateGoldens makes TestBatchedQueriesText rewrite its golden files from the current
// output instead of comparing against them.
var updateGoldens = flag.Bool("update", false, "rewrite the golden files from the current output") //nolint:gochecknoglobals // a test flag

// hostileValue is a name, branch, or tag that would break out of an unquoted argument: a
// quote, a brace, a newline and a comment, other control characters, characters JSON
// escapes for HTML, and a line separator.
const hostileValue = "x\") { viewer { login } }\n#\t\x00<&>\u2028"

// goldenPath returns the golden file for the test case called name: the name in lower
// case, its spaces as hyphens, under testdata.
func goldenPath(name string) string {
	return filepath.Join("testdata", strings.ReplaceAll(strings.ToLower(name), " ", "-")+".golden")
}

// TestBatchedQueriesText compares the whole text of every batched query, for a plain
// repository and a hostile one, with its golden file under testdata, so a change to how any
// is written, or quoted, shows there. After a deliberate change, rewrite them with
// go test ./internal/github -run TestBatchedQueriesText -update, and review the diff.
func TestBatchedQueriesText(t *testing.T) {
	t.Parallel()

	plain := repo.Name{Owner: filesOwner, Repo: exampleRepo}
	hostile := repo.Name{Owner: `acme"org\`, Repo: hostileValue}

	tests := []struct {
		name  string
		query func() (string, error)
	}{
		{
			name: "states query",
			query: func() (string, error) {
				return statesQuery([]item.Ref{{Repo: plain, Number: 184}, {Repo: hostile, Number: 7}})
			},
		},
		{
			name: "merged branches query",
			query: func() (string, error) {
				return mergedQuery([]mergedAsk{
					{repo: plain, branches: []string{"feature-x", hostileValue}},
					{repo: hostile, branches: []string{"main"}},
				})
			},
		},
		{
			name: "latest releases query",
			query: func() (string, error) {
				return latestReleasesQuery([]repo.Name{plain, hostile})
			},
		},
		{
			name: "release status query",
			query: func() (string, error) {
				return releaseStatusQuery([]repo.Name{plain, hostile})
			},
		},
		{
			name: "release compare query",
			query: func() (string, error) {
				return releaseCompareQuery([]CompareRequest{
					{Repo: plain, Tag: "v1.4.2", Branch: "develop"},
					{Repo: hostile, Tag: hostileValue, Branch: hostileValue},
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.query()
			require.NoError(t, err)

			path := goldenPath(tt.name)
			if *updateGoldens {
				require.NoError(t, os.WriteFile(path, []byte(got), 0o644)) //nolint:gosec // committed test data, world-readable
				require.NoError(t, os.Chmod(path, 0o644))                  //nolint:gosec // committed test data, world-readable
			}

			want, err := os.ReadFile(path) //nolint:gosec // a fixed case name under testdata
			require.NoError(t, err)
			assert.Equal(t, string(want), got)
		})
	}
}

// TestBatchedQueriesRefuseValuesThatAreNotUTF8 checks that a value that is not valid UTF-8
// fails its query, saying which value it was, rather than being asked about rewritten.
func TestBatchedQueriesRefuseValuesThatAreNotUTF8(t *testing.T) {
	t.Parallel()

	plain := repo.Name{Owner: filesOwner, Repo: exampleRepo}
	badOwner := repo.Name{Owner: "acme-\xff", Repo: exampleRepo}
	badName := repo.Name{Owner: filesOwner, Repo: "example-\xff"}

	tests := []struct {
		name  string
		query func() (string, error)
		want  string
	}{
		{
			name: "states, an owner",
			query: func() (string, error) {
				return statesQuery([]item.Ref{{Repo: badOwner, Number: 7}})
			},
			want: `encode the repository's owner: "acme-\xff"`,
		},
		{
			name: "merged branches, a name",
			query: func() (string, error) {
				return mergedQuery([]mergedAsk{{repo: badName, branches: []string{"feature-x"}}})
			},
			want: `encode the repository's name: "example-\xff"`,
		},
		{
			name: "merged branches, a branch",
			query: func() (string, error) {
				return mergedQuery([]mergedAsk{{repo: plain, branches: []string{"feat-\xff"}}})
			},
			want: `encode a branch of acme-org/example-repo: "feat-\xff"`,
		},
		{
			name: "latest releases, an owner",
			query: func() (string, error) {
				return latestReleasesQuery([]repo.Name{badOwner})
			},
			want: `encode the repository's owner: "acme-\xff"`,
		},
		{
			name: "release status, a name",
			query: func() (string, error) {
				return releaseStatusQuery([]repo.Name{badName})
			},
			want: `encode the repository's name: "example-\xff"`,
		},
		{
			name: "release compare, a tag",
			query: func() (string, error) {
				return releaseCompareQuery([]CompareRequest{{Repo: plain, Tag: "v1\xff", Branch: "develop"}})
			},
			want: `encode the tag of acme-org/example-repo: "refs/tags/v1\xff"`,
		},
		{
			name: "release compare, a branch",
			query: func() (string, error) {
				return releaseCompareQuery([]CompareRequest{{Repo: plain, Tag: "v1.0.0", Branch: "dev-\xff"}})
			},
			want: `encode the branch of acme-org/example-repo: "dev-\xff"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			query, err := tt.query()
			require.ErrorIs(t, err, errNotUTF8)
			assert.Contains(t, err.Error(), tt.want)
			assert.Empty(t, query)
		})
	}
}

// errBatchFailed is a request fanOut sends failing.
var errBatchFailed = errors.New("fake: the batch failed")

// TestFanOut holds fanOut to its contract: every request is sent, at most limit at once,
// and the answers merge in the requests' order, a later request's answer under a key
// winning, however the sends finish; the first failure fails the call; and no requests
// merge to an empty answer, never nil.
func TestFanOut(t *testing.T) {
	t.Parallel()

	t.Run("merges in order", func(t *testing.T) {
		t.Parallel()

		var inFlight, most atomic.Int32

		got, err := fanOut(t.Context(), []int{0, 1, 2, 3, 4, 5}, 2, func(_ context.Context, req int) (map[string]int, error) {
			now := inFlight.Add(1)
			defer inFlight.Add(-1)

			for seen := most.Load(); now > seen && !most.CompareAndSwap(seen, now); seen = most.Load() {
			}

			return map[string]int{"last": req, strconv.Itoa(req): req}, nil
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"last": 5, "0": 0, "1": 1, "2": 2, "3": 3, "4": 4, "5": 5}, got)
		assert.LessOrEqual(t, most.Load(), int32(2), "at most limit requests are in flight at once")
	})

	t.Run("fails on the first failure", func(t *testing.T) {
		t.Parallel()

		got, err := fanOut(t.Context(), []int{0, 1, 2}, 1, func(_ context.Context, req int) (map[string]int, error) {
			if req == 1 {
				return nil, errBatchFailed
			}

			return map[string]int{strconv.Itoa(req): req}, nil
		})
		require.ErrorIs(t, err, errBatchFailed)
		assert.Nil(t, got)
	})

	t.Run("no requests", func(t *testing.T) {
		t.Parallel()

		got, err := fanOut(t.Context(), []int(nil), 4, func(context.Context, int) (map[string]int, error) {
			t.Error("nothing is sent")

			return nil, errBatchFailed
		})
		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})
}
