package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// versionTable is next-version.json: what magex made for each bump over a repository's
// tags, recorded from the real magex.
type versionTable struct {
	Magex string `json:"magex"`
	Rows  []struct {
		Tags []string `json:"tags"`
		Bump Bump     `json:"bump"`
		Made string   `json:"made"`
		Exit int      `json:"exit"`
		Note string   `json:"note"`
	} `json:"rows"`
}

func TestNextMatchesMagex(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "magex-1.29.5", "next-version.json"))
	require.NoError(t, err)

	var table versionTable
	require.NoError(t, json.Unmarshal(data, &table))
	require.Equal(t, "1.29.5", table.Magex)
	require.Len(t, table.Rows, 26, "every recorded row is checked")

	for _, row := range table.Rows {
		name := strings.Join(row.Tags, ",") + " " + string(row.Bump) + " " + row.Note

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Zero(t, row.Exit, "magex made a version")
			assert.Equal(t, row.Made, Next(row.Tags, row.Bump).String())
		})
	}
}

func TestParseVersion(t *testing.T) {
	t.Parallel()

	for tag, want := range map[string]Version{
		"v1.4.2":          {1, 4, 2},
		"1.4.3":           {1, 4, 3},
		"v1.5.0-rc.1":     {1, 5, 0},
		"v2.0.0-beta.1":   {2, 0, 0},
		"v0.0.0":          {0, 0, 0},
		"v1.4.2+build.7":  {1, 4, 2},
		"v10.20.30-a.b-c": {10, 20, 30},
	} {
		got, ok := ParseVersion(tag)
		assert.True(t, ok, tag)
		assert.Equal(t, want, got, tag)
	}

	for _, tag := range []string{
		"v1.5", "sdk/v1.9.0", "release-7", "", "v", "v1.2.3.4", "V1.2.3", "v01.2.3", "v1.2.3-", "vv1.2.3",
		"v99999999999999999999.0.0",
	} {
		_, ok := ParseVersion(tag)
		assert.False(t, ok, tag)
	}
}

func TestVersionOrderAndBumps(t *testing.T) {
	t.Parallel()

	assert.True(t, Version{1, 4, 2}.Less(Version{1, 4, 10}), "by number, not text")
	assert.True(t, Version{1, 9, 9}.Less(Version{2, 0, 0}))
	assert.True(t, Version{1, 4, 9}.Less(Version{1, 5, 0}))
	assert.False(t, Version{1, 4, 2}.Less(Version{1, 4, 2}))

	assert.Equal(t, "v1.4.3", Version{1, 4, 2}.Bumped(BumpPatch).String())
	assert.Equal(t, "v1.5.0", Version{1, 4, 2}.Bumped(BumpMinor).String())
	assert.Equal(t, "v2.0.0", Version{1, 4, 2}.Bumped(BumpMajor).String())
	assert.Equal(t, Version{1, 4, 2}, Version{1, 4, 2}.Bumped("sideways"), "an unknown bump changes nothing")

	assert.True(t, BumpMajor.Valid())
	assert.False(t, Bump("").Valid())
}

func TestHighestAndNext(t *testing.T) {
	t.Parallel()

	_, found := Highest([]string{"release-7", "v1.5"})
	assert.False(t, found, "no version tags")

	assert.Equal(t, "v0.0.1", Next(nil, BumpPatch).String(), "no tags starts at v0.0.0")
	assert.Equal(t, "v1.4.4", Next([]string{"v1.4.2", "v1.4.3"}, BumpPatch).String())

	// A prerelease counts as the release it leads to, so a patch follows it.
	assert.Equal(t, "v1.5.1", Next([]string{"v1.4.2", "v1.5.0-rc.1"}, BumpPatch).String())
	assert.Equal(t, "v1.4.3", Next([]string{"v1.4.1", "1.4.2"}, BumpPatch).String(), "a tag without v counts")
}
