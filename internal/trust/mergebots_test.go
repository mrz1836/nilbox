package trust_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/trust"
)

// automerge is an org's own auto-merge app, as GitHub gives its slug.
const automerge = "acme-org-automerge"

func TestNormalizeMergeBots(t *testing.T) {
	t.Parallel()

	got, err := trust.NormalizeMergeBots([]string{
		"  Acme-Org-Automerge[bot] ", "app/example-merger", "", "acme-org-automerge", "EXAMPLE-MERGER",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{automerge, "example-merger"}, got,
		"trimmed, lower-cased, without app/ or [bot], each once, sorted")

	got, err = trust.NormalizeMergeBots(nil)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got, "an empty list is never nil")

	_, err = trust.NormalizeMergeBots([]string{automerge, "not a bot"})
	require.ErrorIs(t, err, trust.ErrInvalidBot)
	require.ErrorContains(t, err, `"not a bot"`)

	many := make([]string, 101)
	for i := range many {
		many[i] = "bot-" + strconv.Itoa(i)
	}

	_, err = trust.NormalizeMergeBots(many)
	require.ErrorIs(t, err, trust.ErrTooManyMergeBots)
	require.NotErrorIs(t, err, trust.ErrTooManyBots, "the list says which list is too long")
	require.ErrorContains(t, err, "too many auto-merge bots: at most 100")

	got, err = trust.NormalizeMergeBots(many[:100])
	require.NoError(t, err)
	assert.Len(t, got, 100)
}

func TestDecodeMergeBots(t *testing.T) {
	t.Parallel()

	assert.Equal(t, trust.MergeBots{Bots: []string{}}, trust.DecodeMergeBots(nil), "off, with none listed, until set")
	assert.Equal(t, trust.MergeBots{On: true, Bots: []string{automerge}}, trust.DecodeMergeBots(map[string]string{
		trust.PrefMergeBotsOn: "true", trust.PrefMergeBots: `["acme-org-automerge"]`,
	}))
	assert.Equal(t, trust.MergeBots{Bots: []string{}}, trust.DecodeMergeBots(map[string]string{
		trust.PrefMergeBotsOn: "maybe", trust.PrefMergeBots: "acme-org-automerge",
	}), "what no build stores reads as off and none")
	assert.Equal(t, trust.MergeBots{Bots: []string{}}, trust.DecodeMergeBots(map[string]string{
		trust.PrefMergeBots: "null",
	}))
}

func TestMergeBotsMatch(t *testing.T) {
	t.Parallel()

	listed := trust.MergeBots{On: true, Bots: []string{automerge, "example-merger"}}

	for _, c := range []struct {
		name      string
		bots      trust.MergeBots
		approving []string
		want      string
	}{
		{name: "a listed bot approved", bots: listed, approving: []string{"example-helper", automerge}, want: automerge},
		{name: "matched ignoring case and [bot]", bots: listed, approving: []string{"Acme-Org-Automerge[bot]"}, want: automerge},
		{
			name: "the first listed wins", bots: listed,
			approving: []string{"example-merger", automerge}, want: automerge,
		},
		{name: "no listed bot approved", bots: listed, approving: []string{"example-helper"}},
		{name: "no bot approved", bots: listed, approving: []string{}},
		{name: "off", bots: trust.MergeBots{Bots: listed.Bots}, approving: []string{automerge}},
		{name: "on with none listed", bots: trust.MergeBots{On: true, Bots: []string{}}, approving: []string{automerge}},
		{name: "a blank listed bot matches nothing", bots: trust.MergeBots{On: true, Bots: []string{""}}, approving: []string{""}},
	} {
		assert.Equal(t, c.want, c.bots.Match(c.approving), c.name)
	}
}

// FuzzNormalizeMergeBots checks that any auto-merge list, one login per line, cleans as a
// trusted list does, refusing a list too long as an auto-merge list, and that a list it
// keeps matches each bot the user listed once that bot approves.
func FuzzNormalizeMergeBots(f *testing.F) {
	for _, seed := range []string{
		"",
		"acme-org-automerge\nexample-merger",
		"  Acme-Org-Automerge[bot]  \napp/example-merger\n\nACME-ORG-AUTOMERGE",
		"app/[bot]\n[bot]\napp/",
		"not a bot\nx",
		strings.Repeat("bot\n", 3) + "été",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, listed string) {
		logins := strings.Split(listed, "\n")

		bots, err := trust.NormalizeMergeBots(logins)
		trusted, trustedErr := trust.NormalizeBots(logins)

		if err != nil {
			assert.True(t, errors.Is(err, trust.ErrInvalidBot) || errors.Is(err, trust.ErrTooManyMergeBots), "%v", err)
			assert.Equal(t, errors.Is(err, trust.ErrInvalidBot), errors.Is(trustedErr, trust.ErrInvalidBot))
			assert.Equal(t, errors.Is(err, trust.ErrTooManyMergeBots), errors.Is(trustedErr, trust.ErrTooManyBots))
			assert.Nil(t, bots)

			return
		}

		require.NoError(t, trustedErr)
		assert.Equal(t, trusted, bots, "cleaned as a trusted list is")

		on := trust.MergeBots{On: true, Bots: bots}
		for _, b := range bots {
			assert.Equal(t, b, on.Match([]string{b}), "a listed bot that approved matches: %q", b)
			assert.Empty(t, trust.MergeBots{Bots: bots}.Match([]string{b}), "never while off")
		}
	})
}
