package statusprefs_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/statusprefs"
)

func TestTheCatalogNamesEachProviderAndPartOnce(t *testing.T) {
	t.Parallel()

	providers := map[statusprefs.ProviderID]bool{}
	parts := map[statusprefs.PartID]bool{}

	for _, p := range statusprefs.Catalog() {
		assert.False(t, providers[p.ID], "%s is listed twice", p.ID)
		providers[p.ID] = true

		assert.NotEmpty(t, p.Name)
		assert.NotEmpty(t, p.Parts, "%s lists no part", p.ID)
		assert.Contains(t, []statusprefs.Format{statusprefs.FormatStatuspage, statusprefs.FormatIncidentIO}, p.Format)
		assert.Equal(t, "https://"+p.Host+"/", p.PageURL, "%s's page is on the host its summary is read from", p.ID)
		assert.Equal(t, p.Path, (&url.URL{Path: p.Path}).EscapedPath(), "%s's path needs no escaping", p.ID)

		for _, part := range p.Parts {
			assert.False(t, parts[part.ID], "%s is listed twice", part.ID)
			parts[part.ID] = true

			assert.Regexp(t, `^`+string(p.ID)+`\.[a-z0-9-]+$`, string(part.ID), "a part is named after its provider")
			assert.NotEmpty(t, part.Name)
			assert.NotEmpty(t, part.PageIDs, "%s is matched by the page's id first", part.ID)
		}
	}

	assert.Equal(t, map[statusprefs.ProviderID]bool{"github": true, "claude": true, "openai": true}, providers)
}

func TestTheDefaultsAreWhatTheAppDependsOn(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statusprefs.Config{Watch: []statusprefs.Watch{
		{
			Provider: "github", On: true,
			Parts: []statusprefs.PartID{"github.git", "github.api", "github.issues", "github.pulls", "github.actions"},
		},
		{Provider: "claude", On: true, Parts: []statusprefs.PartID{"claude.api", "claude.code"}},
		{
			Provider: "openai", On: true,
			Parts: []statusprefs.PartID{"openai.codex-api", "openai.codex-cli", "openai.responses"},
		},
	}}, statusprefs.Defaults())
}

func TestTheTwoLoginsAreToldApartByGroup(t *testing.T) {
	t.Parallel()

	_, login, ok := statusprefs.PartOf("openai.login")
	require.True(t, ok)
	assert.Equal(t, "Login", login.MatchName())
	assert.Equal(t, "ChatGPT", login.Group)

	_, cli, ok := statusprefs.PartOf("openai.codex-cli")
	require.True(t, ok)
	assert.Equal(t, "Codex CLI", cli.Name, "the window's name")
	assert.Equal(t, "CLI", cli.MatchName(), "the page's name")
}

func TestDecodeReadsChangesFromTheDefaults(t *testing.T) {
	t.Parallel()

	got := statusprefs.Decode(map[string]string{
		statusprefs.KeyProvidersOff: `["openai","nope"]`,
		statusprefs.KeyPartsOff:     `["github.actions","claude.ai","github.nope"]`,
		statusprefs.KeyPartsOn:      `["claude.ai","github.api","openai.login"]`,
	})

	assert.Equal(t, statusprefs.Config{Watch: []statusprefs.Watch{
		{Provider: "github", On: true, Parts: []statusprefs.PartID{"github.git", "github.api", "github.issues", "github.pulls"}},
		{Provider: "claude", On: true, Parts: []statusprefs.PartID{"claude.api", "claude.code", "claude.ai"}},
		{
			Provider: "openai", On: false,
			Parts: []statusprefs.PartID{"openai.codex-api", "openai.codex-cli", "openai.responses", "openai.login"},
		},
	}}, got, "each list counts only for the parts that start its way, and unknown ids are dropped")

	assert.Equal(t, statusprefs.Defaults(), statusprefs.Decode(map[string]string{
		statusprefs.KeyProvidersOff: "not json", statusprefs.KeyPartsOff: `{"a":1}`, statusprefs.KeyPartsOn: `[`,
	}), "values that can't be read change nothing")
}

func TestWatchedNeedsTheProviderOnAndAPart(t *testing.T) {
	t.Parallel()

	assert.True(t, statusprefs.Watch{On: true, Parts: []statusprefs.PartID{"github.git"}}.Watched())
	assert.False(t, statusprefs.Watch{On: false, Parts: []statusprefs.PartID{"github.git"}}.Watched())
	assert.False(t, statusprefs.Watch{On: true, Parts: []statusprefs.PartID{}}.Watched())
}

func TestSetProviderAndSetPart(t *testing.T) {
	t.Parallel()

	cfg, err := statusprefs.Defaults().SetProvider("claude", false)
	require.NoError(t, err)

	claude, ok := cfg.For("claude")
	require.True(t, ok)
	assert.False(t, claude.On)
	assert.Equal(t, []statusprefs.PartID{"claude.api", "claude.code"}, claude.Parts, "its parts are kept")
	assert.Equal(t, []statusprefs.ProviderID{"claude"}, cfg.Off())

	cfg, err = cfg.SetPart("claude.ai", true)
	require.NoError(t, err)
	cfg, err = cfg.SetPart("github.git", false)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{
		statusprefs.KeyProvidersOff: `["claude"]`,
		statusprefs.KeyPartsOff:     `["github.git"]`,
		statusprefs.KeyPartsOn:      `["claude.ai"]`,
	}, cfg.Encode())
	assert.Equal(t, cfg, statusprefs.Decode(cfg.Encode()), "the choices read back as themselves")

	cfg, err = cfg.SetProvider("claude", true)
	require.NoError(t, err)
	assert.Empty(t, cfg.Off())

	_, err = cfg.SetProvider("gitlab", true)
	require.ErrorIs(t, err, statusprefs.ErrUnknownProvider)

	_, err = cfg.SetPart("github.nope", true)
	require.ErrorIs(t, err, statusprefs.ErrUnknownPart)
}

func TestEncodeOfTheDefaultsListsNothing(t *testing.T) {
	t.Parallel()

	assert.Equal(t, map[string]string{
		statusprefs.KeyProvidersOff: `[]`, statusprefs.KeyPartsOff: `[]`, statusprefs.KeyPartsOn: `[]`,
	}, statusprefs.Defaults().Encode())
}

func TestPartKeyIsTheListAChangeGoesIn(t *testing.T) {
	t.Parallel()

	key, err := statusprefs.PartKey("github.git")
	require.NoError(t, err)
	assert.Equal(t, statusprefs.KeyPartsOff, key, "a default part is stored once unticked")

	key, err = statusprefs.PartKey("claude.ai")
	require.NoError(t, err)
	assert.Equal(t, statusprefs.KeyPartsOn, key, "another part is stored once ticked")

	_, err = statusprefs.PartKey("claude.nope")
	require.ErrorIs(t, err, statusprefs.ErrUnknownPart)
}

func TestChooseBuildsTheChoicesAndNamesWhatItDropped(t *testing.T) {
	t.Parallel()

	cfg, unknown := statusprefs.Choose(
		[]statusprefs.ProviderID{"gitlab", "openai"},
		[]statusprefs.PartID{"claude.ai", "github.pulls", "github.nope", "claude.ai"},
	)

	assert.Equal(t, []string{"gitlab", "github.nope"}, unknown)
	assert.Equal(t, []statusprefs.ProviderID{"openai"}, cfg.Off())
	assert.Equal(t, []statusprefs.PartID{"github.pulls", "claude.ai"}, cfg.Picked(), "in catalog order, each once")
}

func TestLookupAndPartOf(t *testing.T) {
	t.Parallel()

	p, ok := statusprefs.Lookup("openai")
	require.True(t, ok)
	assert.Equal(t, "OpenAI", p.Name)
	assert.Equal(t, statusprefs.FormatIncidentIO, p.Format)

	_, ok = statusprefs.Lookup("gitlab")
	assert.False(t, ok)

	provider, part, ok := statusprefs.PartOf("github.pulls")
	require.True(t, ok)
	assert.Equal(t, statusprefs.ProviderGitHub, provider.ID)
	assert.Equal(t, "Pull Requests", part.Name)

	_, _, ok = statusprefs.PartOf("github.nope")
	assert.False(t, ok)

	assert.Equal(t, []string{"status.providersOff", "status.partsOff", "status.partsOn"}, statusprefs.Keys())
}

func TestCatalogIsAFreshCopy(t *testing.T) {
	t.Parallel()

	first := statusprefs.Catalog()
	first[0].Parts[0].PageIDs[0] = "changed"

	assert.NotEqual(t, "changed", statusprefs.Catalog()[0].Parts[0].PageIDs[0])
}
