package trust_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/trust"
)

func TestClassifyPeople(t *testing.T) {
	associations := map[string]bool{
		"OWNER":                  true,
		"MEMBER":                 true,
		"COLLABORATOR":           true,
		"CONTRIBUTOR":            false,
		"FIRST_TIME_CONTRIBUTOR": false,
		"FIRST_TIMER":            false,
		"MANNEQUIN":              false,
		"NONE":                   false,
		"":                       false,
		"SOMETHING_NEW":          false,
	}

	p := trust.NewPolicy(trust.DefaultBots())

	for assoc, trustedAssoc := range associations {
		for _, isPR := range []bool{true, false} {
			for _, crossRepo := range []bool{true, false} {
				checkPerson(t, p, assoc, trustedAssoc, isPR, crossRepo)
			}
		}
	}
}

// checkPerson checks one person's item: a fork pull request is never trusted, and
// otherwise the association decides.
func checkPerson(t *testing.T, p trust.Policy, assoc string, trustedAssoc, isPR, crossRepo bool) {
	t.Helper()

	name := fmt.Sprintf("%q/pr=%t/cross=%t", assoc, isPR, crossRepo)
	a := trust.Author{Login: "octo-user", Type: "User", Association: assoc}
	want := trustedAssoc && (!isPR || !crossRepo)

	wantClass := trust.ClassOutside
	if want {
		wantClass = trust.ClassMember
	}

	assert.Equal(t, want, p.Trusted(a, isPR, crossRepo), name)
	assert.Equal(t, wantClass, p.Classify(a, isPR, crossRepo), name)
}

func TestClassifyNormalizesTheAssociation(t *testing.T) {
	p := trust.NewPolicy(nil)

	assert.True(t, p.Trusted(trust.Author{Association: " owner "}, true, false))
	assert.True(t, p.Trusted(trust.Author{Association: "Member"}, false, false))
	assert.False(t, p.Trusted(trust.Author{Association: "contributor"}, false, false))
}

func TestClassifyBots(t *testing.T) {
	p := trust.NewPolicy([]string{"dependabot", "Renovate[bot]"})

	tests := []struct {
		name   string
		author trust.Author
		cross  bool
		want   string
	}{
		{name: "a trusted bot", author: trust.Author{Login: "dependabot", Type: "Bot", Association: "NONE"}, want: trust.ClassBot},
		{name: "matched ignoring case and suffix", author: trust.Author{Login: "renovate", Type: "Bot"}, want: trust.ClassBot},
		{name: "a bot not on the list", author: trust.Author{Login: "random-bot", Type: "Bot"}, want: trust.ClassOutside},
		{name: "a person named like a bot", author: trust.Author{Login: "dependabot", Type: "User"}, want: trust.ClassOutside},
		{name: "a bot before a refresh read its type", author: trust.Author{Login: "dependabot"}, want: trust.ClassOutside},
		{name: "a trusted bot from a fork", author: trust.Author{Login: "dependabot", Type: "Bot"}, cross: true, want: trust.ClassOutside},
		{
			name: "a bot that is also a collaborator", author: trust.Author{Login: "x", Type: "Bot", Association: "COLLABORATOR"},
			want: trust.ClassMember,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, p.Classify(tt.author, true, tt.cross))
			assert.Equal(t, tt.want != trust.ClassOutside, p.Trusted(tt.author, true, tt.cross))
		})
	}

	assert.Equal(t, trust.ClassBot, p.Classify(trust.Author{Login: "dependabot", Type: "Bot"}, false, false),
		"a trusted bot's issue")
	assert.True(t, trust.Author{Type: trust.TypeBot}.IsBot())
	assert.False(t, trust.Author{Type: "User"}.IsBot())
}

func TestNormalizeBots(t *testing.T) {
	got, err := trust.NormalizeBots([]string{" Renovate[bot] ", "app/dependabot", "", "dependabot", "github-actions"})
	require.NoError(t, err)
	assert.Equal(t, []string{"dependabot", "github-actions", "renovate"}, got)

	got, err = trust.NormalizeBots(nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got)

	for _, bad := range []string{"two words", "-dash", "dots.in.it", "under_score", "x/y"} {
		_, err = trust.NormalizeBots([]string{bad})
		require.ErrorIs(t, err, trust.ErrInvalidBot, bad)
	}

	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprintf("bot-%d", i)
	}

	_, err = trust.NormalizeBots(many)
	require.ErrorIs(t, err, trust.ErrTooManyBots)
}

func TestEncodeAndDecodeBots(t *testing.T) {
	assert.Equal(t, trust.DefaultBots(), trust.DecodeBots("", false), "unset means the defaults")
	assert.Equal(t, trust.DefaultBots(), trust.DecodeBots("not json", true), "unreadable means the defaults")
	assert.Equal(t, []string{}, trust.DecodeBots(trust.EncodeBots(nil), true), "an emptied list stays empty")
	assert.Equal(t, []string{}, trust.DecodeBots("null", true))
	assert.Equal(t, []string{"renovate"}, trust.DecodeBots(trust.EncodeBots([]string{"renovate"}), true))
	assert.JSONEq(t, `["a","b"]`, trust.EncodeBots([]string{"a", "b"}))
}
