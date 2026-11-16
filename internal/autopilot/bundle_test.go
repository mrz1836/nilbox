package autopilot_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
)

func TestTheBundleFormatIsNamedForTheApp(t *testing.T) {
	t.Parallel()

	assert.Equal(t, brand.Slug+"-autopilot/1", autopilot.BundleFormat)
}

func TestBundleRoundTrip(t *testing.T) {
	t.Parallel()

	in := autopilot.Bundle{
		Format: autopilot.BundleFormat, ExportedAt: "2026-11-16T09:00:00Z", App: "v0.5.0",
		Routines: []autopilot.BundleRoutine{{
			Name: "Fix <checks> & more", Mode: autopilotprefs.ModeAsk, Enabled: true, Template: "fix",
			Spec: json.RawMessage(`{"version":1}`),
		}},
	}

	data, err := autopilot.EncodeBundle(in)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(string(data), "}\n"), "indented, ending in a newline")
	assert.Contains(t, string(data), "Fix <checks> & more", "written as the user wrote it")

	out, err := autopilot.DecodeBundle(data)
	require.NoError(t, err)
	assert.Equal(t, in.Routines[0].Name, out.Routines[0].Name)
	assert.JSONEq(t, `{"version":1}`, string(out.Routines[0].Spec))

	empty, err := autopilot.EncodeBundle(autopilot.Bundle{Format: autopilot.BundleFormat})
	require.NoError(t, err)
	assert.Contains(t, string(empty), `"routines": []`, "no routines is an empty list")
}

func TestDecodeBundleRefusesWhatIsntOne(t *testing.T) {
	t.Parallel()

	for doc, want := range map[string]error{
		`not json`:                       autopilot.ErrNotBundle,
		`{"format": "something-else/1"}`: autopilot.ErrNotBundle,
		`{"format": "` + brand.Slug + `-autopilot/"}`:               autopilot.ErrNotBundle,
		`{"format": "` + brand.Slug + `-autopilot/01"}`:             autopilot.ErrNotBundle,
		`{"format": "` + brand.Slug + `-autopilot/0"}`:              autopilot.ErrNotBundle,
		`{"format": "` + brand.Slug + `-autopilot/2"}`:              autopilot.ErrNewerBundle,
		`["` + strings.Repeat("x", autopilot.MaxBundleBytes) + `"]`: autopilot.ErrNotBundle,
	} {
		_, err := autopilot.DecodeBundle([]byte(doc))
		require.ErrorIs(t, err, want, doc[:min(len(doc), 60)])
	}

	got, err := autopilot.DecodeBundle([]byte(`{"format": "` + autopilot.BundleFormat + `", "later": true}`))
	require.NoError(t, err, "a field a later build adds is no reason to refuse")
	assert.NotNil(t, got.Routines)
}
