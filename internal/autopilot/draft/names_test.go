package draft_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestARoutinesNameIsOneLineOfPlainText(t *testing.T) {
	t.Parallel()

	got, err := draft.CheckName("  Fix checks  ")
	require.NoError(t, err)
	assert.Equal(t, "Fix checks", got, "trimmed")

	got, err = draft.CheckName(strings.Repeat("ü", draft.MaxNameRunes))
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("ü", draft.MaxNameRunes), got, "the cap counts characters, not bytes")

	for name, want := range map[string]string{
		"   ": "Give the routine a name.",
		strings.Repeat("a", draft.MaxNameRunes+1): "Keep the name to 80 characters.",
		"two\nlines":    "Keep the name to one line of plain text.",
		"tab\there":     "Keep the name to one line of plain text.",
		"Fix\u202eit":   "Keep the name to one line of plain text.",
		"bad \xff byte": "Keep the name to one line of plain text.",
	} {
		_, err := draft.CheckName(name)
		require.ErrorIs(t, err, draft.ErrInvalidName, "%q", name)
		assert.Equal(t, want, uierr.Message(err), "%q", name)
	}
}

func TestARoutinesDescriptionIsPlainTextOfItsLines(t *testing.T) {
	t.Parallel()

	got, err := draft.CheckDescription("  First line.\nSecond line.  ")
	require.NoError(t, err)
	assert.Equal(t, "First line.\nSecond line.", got, "trimmed, its line breaks kept")

	got, err = draft.CheckDescription("")
	require.NoError(t, err)
	assert.Empty(t, got, "a routine needs no description")

	for desc, want := range map[string]string{
		strings.Repeat("a", draft.MaxDescriptionRunes+1): "Keep the description to 500 characters.",
		"Fix\u202eit":   "Keep the description to plain text.",
		"tab\there":     "Keep the description to plain text.",
		"bad \xff byte": "Keep the description to plain text.",
	} {
		_, err := draft.CheckDescription(desc)
		require.ErrorIs(t, err, draft.ErrInvalidDescription, "%q", desc)
		assert.Equal(t, want, uierr.Message(err), "%q", desc)
	}
}

func TestATemplateKeyIsOneOfTheTemplatesShape(t *testing.T) {
	t.Parallel()

	for key, want := range map[string]string{
		"":                               "",
		" fix-failing-checks-on-my-prs ": "fix-failing-checks-on-my-prs",
		"re-run-flaky-checks-once":       "re-run-flaky-checks-once",
		strings.Repeat("a", 64):          strings.Repeat("a", 64),
	} {
		got, err := draft.CheckTemplate(key)
		require.NoError(t, err, "%q", key)
		assert.Equal(t, want, got, "%q", key)
	}

	for _, key := range []string{"Not a key", "fix--checks", "-fix", "fix-", "Fix", strings.Repeat("a", 65)} {
		_, err := draft.CheckTemplate(key)
		require.ErrorIs(t, err, draft.ErrInvalidTemplate, "%q", key)
		assert.Equal(t, "That isn't one of the templates.", uierr.Message(err), "%q", key)
	}
}

func TestHiddenIsWhatALineOfTextCantShow(t *testing.T) {
	t.Parallel()

	for _, r := range []rune{'\n', '\t', '\x00', '\x7f', '\u202e', '\u2066', '\u200e'} {
		assert.True(t, draft.Hidden(r), "%U", r)
	}

	for _, r := range []rune{'a', ' ', 'ü', '“', '🔧'} {
		assert.False(t, draft.Hidden(r), "%U", r)
	}
}

func TestANamesKeyFoldsItAsEqualFoldDoes(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"Über checks", "über checks", true},
		{"ÜBER CHECKS", "über checks", true},
		{"Σύνοψη", "σύνοψη", true},
		{"Kelvin", "\u212aelvin", true}, // the Kelvin sign folds with K
		{"Straße", "STRASSE", false},    // simple folding never turns one rune into two
		{"über checks", "uber checks", false},
		{"Fix checks", "fix checks", true},
	} {
		assert.Equal(t, strings.EqualFold(c.a, c.b), c.same, "%q and %q", c.a, c.b)
		assert.Equal(t, c.same, draft.NameKey(c.a) == draft.NameKey(c.b), "%q and %q", c.a, c.b)
	}

	assert.Equal(t, "FIX CHECKS", draft.NameKey("fIx checks"), "each rune folds to the smallest of its orbit")
	assert.Equal(t, "ÜBER", draft.NameKey("über"), "Ü is the smallest of ü's")
}
