package catalog_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
)

// update makes TestTheWindowsCatalogFixtureIsTheCatalog rewrite the window's fixture from
// the catalog instead of comparing against it.
var update = flag.Bool("update", false, "rewrite the window's catalog fixture from the catalog") //nolint:gochecknoglobals // a test flag

// fixturePath is the catalog the window's tests render the routine builder from.
func fixturePath() string {
	return filepath.Join("..", "..", "..", "frontend", "src", "test", "autopilotCatalog.json")
}

// TestTheWindowsCatalogFixtureIsTheCatalog pins the window's fixture to the catalog the
// bound Catalog returns, byte for byte, so the builder's tests render what the app does.
// After a change to the catalog, rewrite it with go test ./internal/autopilot/catalog -run
// TestTheWindowsCatalogFixtureIsTheCatalog -update, and review the diff.
func TestTheWindowsCatalogFixtureIsTheCatalog(t *testing.T) {
	t.Parallel()

	got, err := json.MarshalIndent(catalog.All(), "", "  ")
	require.NoError(t, err)

	got = append(got, '\n')

	if *update {
		require.NoError(t, os.WriteFile(fixturePath(), got, 0o644)) //nolint:gosec // committed test data, world-readable
	}

	want, err := os.ReadFile(fixturePath())
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
	assert.Equal(t, string(want), string(got), "the fixture is the catalog as indented JSON")
}
