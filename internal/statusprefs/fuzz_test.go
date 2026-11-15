package statusprefs_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/statusprefs"
)

// FuzzDecode checks that any stored values decode to one Watch per provider in catalog
// order, with known parts of that provider alone, never nil, and that the choices read
// back unchanged once encoded, and again after any provider or part is changed.
func FuzzDecode(f *testing.F) {
	for _, seed := range [][3]string{
		{"", "", ""},
		{"[]", "[]", "[]"},
		{`["openai"]`, `["github.git","claude.api"]`, `["claude.ai","openai.login"]`},
		{`["openai","openai","nope"]`, `["github.git","github.git",""]`, `["claude.ai","claude.ai"]`},
		{`{"openai":true}`, `null`, `[1,2]`},
		{`["github"`, `["github.git"`, `["claude.ai"`},
		{`["été", 7, null]`, `["github.api", 7]`, `["openai.codex-web"]`},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}

	f.Fuzz(func(t *testing.T, providersOff, partsOff, partsOn string) {
		cfg := statusprefs.Decode(map[string]string{
			statusprefs.KeyProvidersOff: providersOff, statusprefs.KeyPartsOff: partsOff, statusprefs.KeyPartsOn: partsOn,
		})
		checkConfig(t, cfg)

		for _, p := range statusprefs.Catalog() {
			for _, on := range []bool{true, false} {
				changed, err := cfg.SetProvider(p.ID, on)
				require.NoError(t, err)
				checkConfig(t, changed)

				for _, part := range p.Parts {
					changed, err = cfg.SetPart(part.ID, on)
					require.NoError(t, err)
					checkConfig(t, changed)
					assert.Equal(t, on, slices.Contains(changed.Picked(), part.ID))
				}
			}
		}
	})
}

// checkConfig checks decoded choices: a Watch per provider in catalog order, known parts
// of its own, never nil, and the same once encoded and decoded again.
func checkConfig(t *testing.T, cfg statusprefs.Config) {
	t.Helper()

	catalog := statusprefs.Catalog()
	require.Len(t, cfg.Watch, len(catalog))

	for i, w := range cfg.Watch {
		assert.Equal(t, catalog[i].ID, w.Provider)
		require.NotNil(t, w.Parts)

		for _, id := range w.Parts {
			provider, _, ok := statusprefs.PartOf(id)
			assert.True(t, ok, "unknown part %q", id)
			assert.Equal(t, w.Provider, provider.ID, "%q is another provider's", id)
		}
	}

	assert.Equal(t, cfg, statusprefs.Decode(cfg.Encode()))
}
