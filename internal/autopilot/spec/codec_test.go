package spec_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// flagship is the routine the build plan's flagship spells, exactly.
const flagship = "fix-dependabot-security"

func TestTemplatesLoad(t *testing.T) {
	t.Parallel()

	for _, name := range templateNames(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := loadTemplate(t, name)
			require.NoError(t, spec.Validate(s))
			assert.Equal(t, spec.Version, s.Version)
		})
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	for _, name := range templateNames(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := loadTemplate(t, name)

			data, err := spec.Encode(s)
			require.NoError(t, err)

			again, err := spec.Load(data)
			require.NoError(t, err)
			assert.Equal(t, s, again, "a stored spec loads as it was saved")
			assert.Equal(t, s, spec.Normalize(s), "normalizing a normalized spec changes nothing")

			reencoded, err := spec.Encode(again)
			require.NoError(t, err)
			assert.JSONEq(t, string(data), string(reencoded))
		})
	}
}

// TestFlagshipGolden pins the flagship's normalized JSON, the shape every later build
// stores. Rewrite with go test ./internal/autopilot/spec -run TestFlagshipGolden -update.
func TestFlagshipGolden(t *testing.T) {
	t.Parallel()

	data, err := json.MarshalIndent(loadTemplate(t, flagship), "", "  ")
	require.NoError(t, err)

	golden(t, filepath.Join("testdata", "flagship.golden"),
		"# The flagship routine as spec.Load normalizes it, then indented. Rewrite with\n"+
			"# go test ./internal/autopilot/spec -run TestFlagshipGolden -update\n\n",
		string(data)+"\n")
}

func TestDecodeIsStrict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
	}{
		{name: "a field the format doesn't have", json: `{"version":1,"name":"x"}`},
		{name: "a step's field the format doesn't have", json: `{"version":1,"steps":[{"pin":{"days":3}}]}`},
		{name: "the mode, which is the row's", json: `{"version":1,"mode":"auto"}`},
		{name: "a key given twice", json: `{"version":1,"trigger":{"event":"appears","event":"manual"}}`},
		{name: "a nested key given twice", json: `{"version":1,"limits":{"runsPerDay":1,"runsPerDay":2}}`},
		{name: "something after the document", json: `{"version":1} {}`},
		{
			name: "a key in another case, which encoding/json would take for the first",
			json: `{"version":1,"trigger":{"event":"appears"},"Trigger":{"event":"manual"}}`,
		},
		{name: "a list's key in capitals", json: `{"version":1,"steps":[{"pin":{}}],"STEPS":[{"dismiss":{}}]}`},
		{name: "a key Unicode folds to one", json: "{\"version\":1,\"\u017fteps\":[]}"},
		{name: "a nested key in another case", json: `{"version":1,"limits":{"RunsPerDay":1}}`},
		{name: "a search's key in another case", json: `{"version":1,"watch":{"sources":[{"search":{"Where":["@me"]}}]}}`},
		{name: "a key that isn't ASCII", json: "{\"version\":1,\"steps\":[{\"pin\u00e9\":{}}]}"},
		{name: "a value of the wrong type", json: `{"version":1,"limits":{"triesPerItem":"2"}}`},
		{name: "a number with a fraction", json: `{"version":1,"limits":{"triesPerItem":2.5}}`},
		{name: "not an object", json: `[1]`},
		{name: "not JSON", json: `version: 1`},
		{name: "empty", json: ``},
		{name: "nested too deep", json: `{"version":1,"x":` + strings.Repeat("[", 20) + strings.Repeat("]", 20) + `}`},
		{name: "too large", json: `{"version":1,"requires":["` + strings.Repeat("a", spec.MaxBytes) + `"]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := spec.Decode([]byte(tt.json))
			require.ErrorIs(t, err, spec.ErrUnreadable)
		})
	}
}

func TestDecodeKeepsArraysOfObjectsApart(t *testing.T) {
	t.Parallel()

	// The same key in two objects of one array is no repeat.
	_, err := spec.Decode([]byte(`{"version":1,"steps":[{"pin":{}},{"pin":{}}],"watch":{"sources":[` +
		`{"lists":["authored"]},{"lists":["assigned"]}]}}`))
	require.NoError(t, err)
}

func TestLoadVersions(t *testing.T) {
	t.Parallel()

	valid := string(templateJSON(t, flagship))

	tests := []struct {
		name   string
		json   string
		reason spec.Reason
		err    error
	}{
		{
			name: "a newer build's, with fields this build doesn't know",
			json: `{"version":2,"watch":{"sources":[]},"newPart":{"x":1}}`, reason: spec.ReasonNewer, err: spec.ErrNewer,
		},
		{name: "no version", json: `{"watch":{"sources":[]}}`, reason: spec.ReasonUnreadable, err: spec.ErrUnreadable},
		{name: "version zero", json: `{"version":0}`, reason: spec.ReasonUnreadable, err: spec.ErrUnreadable},
		{name: "a version that isn't a number", json: `{"version":"1"}`, reason: spec.ReasonUnreadable, err: spec.ErrUnreadable},
		{name: "unreadable", json: `{"version":1,"x":1}`, reason: spec.ReasonUnreadable, err: spec.ErrUnreadable},
		{
			name: "reads, but no longer valid", json: strings.Replace(valid, `"checks_failed"`, `"gone_event"`, 1),
			reason: spec.ReasonInvalid, err: spec.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, err := spec.Load([]byte(tt.json))

			var broken *spec.BrokenError

			require.ErrorAs(t, err, &broken)
			assert.Equal(t, tt.reason, broken.Reason)
			require.ErrorIs(t, err, tt.err)

			if tt.reason == spec.ReasonInvalid {
				assert.Equal(t, catalog.Event("gone_event"), s.Trigger.Event, "a broken spec comes back as far as it was read")
			}
		})
	}
}

func TestBrokenErrorSaysWhy(t *testing.T) {
	t.Parallel()

	_, err := spec.Load([]byte(`{"version":7}`))
	require.Error(t, err)
	assert.Equal(t, "made by a newer version: update to edit it (version 7)", err.Error())

	var broken *spec.BrokenError

	require.ErrorAs(t, err, &broken)
	assert.Equal(t, spec.ReasonNewer, broken.Reason)
}

// FuzzDecode checks that any bytes decode without a panic, and that whatever decodes
// encodes and decodes back the same; that Load never panics; and that what Load takes
// is normalized and valid.
func FuzzDecode(f *testing.F) {
	for _, name := range []string{flagship, "weekday-nudge", "triage-busy-issues"} {
		f.Add(templateJSON(f, name))
	}

	for _, seed := range []string{
		`{"version":1}`, `{"version":1,"steps":[{"github":[]}]}`, `{"version":2}`, `{}`, `[]`, `null`,
		`{"version":1,"conditions":{"match":"any","of":[{"group":{"match":"all","of":[]}}]}}`,
		`{"version":1,"trigger":{"event":"appears","event":"appears"}}`, "\xff",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := spec.Decode(data)
		if err == nil {
			out, encErr := spec.Encode(s)
			require.NoError(t, encErr)

			again, decErr := spec.Decode(out)
			require.NoError(t, decErr)

			n := spec.Normalize(again)
			assert.Equal(t, spec.Normalize(s), n)
			assert.Equal(t, n, spec.Normalize(n), "normalizing twice changes nothing")
		}

		loaded, err := spec.Load(data)
		if err == nil {
			assert.Equal(t, loaded, spec.Normalize(loaded))
			require.NoError(t, spec.Validate(loaded))
			assert.NotEmpty(t, spec.Sentence(loaded, nil))
		}
	})
}
