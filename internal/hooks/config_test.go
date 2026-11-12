package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestParseEnvReadsAsGoPreCommitDoes(t *testing.T) {
	t.Parallel()

	got := parseEnv([]byte(strings.Join([]string{
		"# a comment",
		"",
		"ENABLE_GO_PRE_COMMIT=false",
		"  GO_PRE_COMMIT_VERSION = v1.14.0  ",
		`QUOTED="a value # kept"`,
		`SINGLE='single'`,
		"INLINE=value # a comment",
		"TABBED=value\t# a comment",
		"HASH=a#b",
		"EMPTY=",
		"no equals sign",
		"=no key",
		"LAST=one",
		"LAST=two",
	}, "\n")))

	assert.Equal(t, map[string]string{
		"ENABLE_GO_PRE_COMMIT": "false", "GO_PRE_COMMIT_VERSION": "v1.14.0", "QUOTED": "a value # kept",
		"SINGLE": "single", "INLINE": "value", "TABBED": "value", "HASH": "a#b", "EMPTY": "", "LAST": "two",
	}, got)
}

func TestReadConfigModularFilesInOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testutil.WriteFileMode(t, filepath.Join(dir, ".github/env/10-pre-commit.env"),
		"ENABLE_GO_PRE_COMMIT=false\nGO_PRE_COMMIT_ENABLE_LINT=false\n", 0o600)
	testutil.WriteFileMode(t, filepath.Join(dir, ".github/env/00-core.env"),
		coreEnv+"GO_PRE_COMMIT_ENABLE_GITLEAKS=true\n", 0o600)
	testutil.WriteFileMode(t, filepath.Join(dir, ".github/env/99-local.env"), "ENABLE_GO_PRE_COMMIT=true\n", 0o600)
	testutil.WriteFileMode(t, filepath.Join(dir, ".github/env/notes.txt"), "ENABLE_GO_PRE_COMMIT=false\n", 0o600)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".github", "env", "folder.env"), 0o700))
	testutil.WriteFileMode(t, filepath.Join(dir, ".github/.env.base"), "ENABLE_GO_PRE_COMMIT=false\n", 0o600)

	cfg := readConfig(dir)
	require.True(t, cfg.present)
	assert.True(t, cfg.enabled(), "the last file wins, and the legacy file plays no part")
	assert.Equal(t, "1.14.0", cfg.ciVersion())
	assert.Equal(t, []CheckSetting{
		{Name: CheckEOF, On: true},
		{Name: CheckFumpt, On: true},
		{Name: CheckGitleaks, On: true},
		{Name: CheckLint, On: false},
		{Name: CheckModTidy, On: true},
		{Name: CheckWhitespace, On: true},
	}, cfg.settings())
}

func TestReadConfigLegacyFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testutil.WriteFileMode(t, filepath.Join(dir, ".github/.env.base"),
		"ENABLE_GO_PRE_COMMIT=true\nGO_PRE_COMMIT_ENABLE_MOD_TIDY=false\n", 0o600)
	testutil.WriteFileMode(t, filepath.Join(dir, ".github/.env.custom"), "ENABLE_GO_PRE_COMMIT=0\n", 0o600)

	cfg := readConfig(dir)
	require.True(t, cfg.present)
	assert.False(t, cfg.enabled(), ".env.custom overrides .env.base")
	assert.False(t, cfg.checkOn(CheckModTidy))
	assert.False(t, cfg.checkOn(CheckGitleaks), "gitleaks is off unless turned on")
	assert.Empty(t, cfg.ciVersion())

	// A modular folder with no .env file leaves the legacy files in charge.
	testutil.WriteFileMode(t, filepath.Join(dir, ".github/env/README.md"), "notes", 0o600)
	assert.False(t, readConfig(dir).enabled())
}

func TestReadConfigWithoutOne(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := readConfig(dir)
	assert.False(t, cfg.present)
	assert.True(t, cfg.enabled(), "with nothing set, go-pre-commit is on")

	// A folder where .env.base should be is no config.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".github", ".env.base"), 0o700))
	assert.False(t, readConfig(dir).present)
}

func TestConfigFlags(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]bool{
		"true": true, "TRUE": true, "1": true, "t": true, "false": false, "False": false, "0": false, "f": false,
		"": true, "maybe": true, "no": true,
	} {
		cfg := repoConfig{present: true, values: map[string]string{keyEnabled: value}}
		assert.Equal(t, want, cfg.enabled(), "ENABLE_GO_PRE_COMMIT=%q", value)
	}
}
