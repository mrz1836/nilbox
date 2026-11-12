package trust_test

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/trust"
)

// normalizedBot is what every login of a normalized trusted list looks like.
var normalizedBot = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}$`)

// FuzzNormalizeBots checks that any trusted list, one login per line, either fails as an
// invalid login or a list too long, or normalizes into a sorted list of distinct bot
// logins that normalizes to itself, holds each login the user listed as that login
// normalizes alone, and makes a policy that trusts every bot the user listed.
func FuzzNormalizeBots(f *testing.F) {
	for _, seed := range []string{
		"",
		"dependabot\ngithub-actions\nrenovate",
		"  Dependabot[bot]  \napp/renovate\n\nDEPENDABOT",
		"app/[bot]\n[bot]\napp/",
		"not a bot\nx",
		"-leading-dash",
		strings.Repeat("a", 40),
		strings.Repeat("bot\n", 3) + "été",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, listed string) {
		logins := strings.Split(listed, "\n")

		bots, err := trust.NormalizeBots(logins)
		if err != nil {
			assert.True(t, errors.Is(err, trust.ErrInvalidBot) || errors.Is(err, trust.ErrTooManyBots), "%v", err)
			assert.Nil(t, bots)

			return
		}

		require.NotNil(t, bots)
		assert.LessOrEqual(t, len(bots), 100)
		assert.True(t, slices.IsSorted(bots))
		assert.Len(t, slices.Compact(slices.Clone(bots)), len(bots), "no login twice")

		for _, b := range bots {
			assert.Regexp(t, normalizedBot, b)
		}

		again, err := trust.NormalizeBots(bots)
		require.NoError(t, err)
		assert.Equal(t, bots, again)

		checkEachLoginIsTrusted(t, logins, bots)
	})
}

// checkEachLoginIsTrusted checks that bots, logins normalized, holds each login as that
// login normalizes alone, and nothing else, and that a policy built from it trusts every
// bot the user listed.
func checkEachLoginIsTrusted(t *testing.T, logins, bots []string) {
	t.Helper()

	policy := trust.NewPolicy(bots)

	var each []string

	for _, login := range logins {
		alone, err := trust.NormalizeBots([]string{login})
		require.NoError(t, err)

		if len(alone) == 0 {
			continue
		}

		each = append(each, alone...)
		bot := trust.Author{Login: login, Type: trust.TypeBot}

		assert.Equal(t, trust.ClassBot, policy.Classify(bot, false, false), "a listed bot is trusted: %q", login)
	}

	assert.ElementsMatch(t, bots, slices.Compact(slices.Sorted(slices.Values(each))))
}
