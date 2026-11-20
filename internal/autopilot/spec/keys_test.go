package spec_test

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// envelope is DecodeExact's test type, shaped as a document that holds specs would be: a
// spec, a list and a map of them, a struct behind a pointer, and the fields of a struct it
// embeds without a name as its own.
type envelope struct {
	about

	Note   string               `json:"note"`
	Spec   spec.Spec            `json:"spec"`
	Specs  []spec.Spec          `json:"specs,omitempty"`
	ByName map[string]spec.Spec `json:"byName,omitempty"`
	Pinned *pinned              `json:"pinned,omitempty"`
	Count  int                  `json:"count,omitempty"`
}

// about is the struct an envelope embeds: its title is the envelope's own key.
type about struct {
	Title string `json:"title,omitempty"`
}

// pinned is the struct an envelope points to.
type pinned struct {
	Labels []string `json:"labels"`
}

// Caps DecodeExact's tests read an envelope within.
const (
	envelopeBytes = 1 << 16
	envelopeDepth = 16
)

func TestDecodeExactReadsWhatItsTypeHolds(t *testing.T) {
	t.Parallel()

	doc := `{"title":"Fixes","note":"two routines","spec":{"version":1,"trigger":{"event":"manual"}},` +
		`"specs":[{"version":1},{"version":1,"steps":[{"pin":{}}]}],"byName":{"nudge":{"version":1}},` +
		`"pinned":{"labels":["bug"]},"count":2}`

	got, err := spec.DecodeExact[envelope]([]byte(doc), envelopeBytes, envelopeDepth)
	require.NoError(t, err)

	assert.Equal(t, "Fixes", got.Title, "an embedded struct's field is the envelope's own key")
	assert.Equal(t, "two routines", got.Note)
	assert.Equal(t, "manual", string(got.Spec.Trigger.Event))
	require.Len(t, got.Specs, 2)
	assert.NotNil(t, got.Specs[1].Steps[0].Pin)
	assert.Equal(t, 1, got.ByName["nudge"].Version)
	assert.Equal(t, &pinned{Labels: []string{"bug"}}, got.Pinned)
	assert.Equal(t, 2, got.Count)
}

// TestDecodeExactIsStrictAtEveryDepth refuses, at the top and in every spec an envelope
// holds, what Decode refuses in a stored spec, each with the words that say why.
func TestDecodeExactIsStrictAtEveryDepth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
		want string
	}{
		{name: "a key the type doesn't have", json: `{"note":"x","mode":"auto"}`, want: `"mode"`},
		{name: "a key in another case", json: `{"Note":"x"}`, want: `"Note"`},
		{name: "a key given twice", json: `{"note":"x","note":"y"}`, want: "a key is given twice"},
		{name: "a spec's key the format doesn't have", json: `{"spec":{"version":1,"name":"x"}}`, want: `"name"`},
		{
			name: "a spec's key in another case", json: `{"spec":{"version":1,"Trigger":{"event":"manual"}}}`,
			want: `"Trigger"`,
		},
		{
			name: "a spec's key Unicode folds to one", json: "{\"spec\":{\"version\":1,\"\u017fteps\":[]}}",
			want: "spelled exactly",
		},
		{name: "a spec's key given twice", json: `{"spec":{"version":1,"version":2}}`, want: "a key is given twice"},
		{name: "a key in another case in a list's spec", json: `{"specs":[{"version":1},{"Version":1}]}`, want: `"Version"`},
		{name: "a step's key in a list's spec", json: `{"specs":[{"steps":[{"pin":{"days":3}}]}]}`, want: `"days"`},
		{
			name: "a map's key given twice", json: `{"byName":{"a":{"version":1},"a":{"version":2}}}`,
			want: "a key is given twice",
		},
		{name: "a key in another case in a map's spec", json: `{"byName":{"a":{"version":1,"Steps":[]}}}`, want: `"Steps"`},
		{name: "a key in another case behind a pointer", json: `{"pinned":{"Labels":[]}}`, want: `"Labels"`},
		{name: "the embedded struct's own name", json: `{"about":{"title":"x"}}`, want: `"about"`},
		{name: "an embedded struct's key in another case", json: `{"Title":"x"}`, want: `"Title"`},
		{name: "a value of the wrong type", json: `{"count":"2"}`, want: "json:"},
		{name: "a spec's value of the wrong type", json: `{"spec":{"limits":{"triesPerItem":2.5}}}`, want: "json:"},
		{name: "something after the document", json: `{"note":"x"} {}`, want: "more after the document"},
		{name: "not JSON", json: `note: x`, want: "invalid character"},
		{name: "empty", json: ``, want: "EOF"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := spec.DecodeExact[envelope]([]byte(tt.json), envelopeBytes, envelopeDepth)
			require.ErrorContains(t, err, tt.want)
			assert.NotErrorIs(t, err, spec.ErrUnreadable, "the caller says what it was reading")
		})
	}
}

// The structs TestDecodeExactReadsEmbeddingAsEncodingJSONDoes reads: a tangle embeds two,
// one through a pointer and the other embedding a third, whose names clash. Its own name
// wins over theirs, a tagged Y over an untagged one as deep, and the two untagged Boths
// are neither's.
type (
	tangle struct {
		left
		*right

		Name string `json:"name"`
	}

	left struct {
		inner

		Both     string
		Y        string `json:"Y"`
		OnlyLeft string `json:"onlyLeft"`
	}

	right struct {
		Both string
		Y    string
		Name string `json:"name"`
	}

	inner struct {
		Deep string `json:"deep"`
		Name string `json:"name"`
	}
)

// TestDecodeExactReadsEmbeddingAsEncodingJSONDoes holds the keys DecodeExact takes for a
// type that embeds structs to those encoding/json writes for it, every field set: each of
// them, spelled exactly, and nothing else.
func TestDecodeExactReadsEmbeddingAsEncodingJSONDoes(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(tangle{
		left:  left{inner: inner{Deep: "v", Name: "v"}, Both: "v", Y: "v", OnlyLeft: "v"},
		right: &right{Both: "v", Y: "v", Name: "v"}, Name: "v",
	})
	require.NoError(t, err)

	var written map[string]string

	require.NoError(t, json.Unmarshal(data, &written))
	assert.ElementsMatch(t, []string{"name", "Y", "onlyLeft", "deep"}, slices.Collect(maps.Keys(written)))

	for name := range written {
		_, err := spec.DecodeExact[tangle]([]byte(`{"`+name+`":"v"}`), envelopeBytes, envelopeDepth)
		require.NoError(t, err, "encoding/json writes %q", name)
	}

	for _, name := range []string{"Both", "both", "Name", "y", "OnlyLeft", "Deep", "left", "right", "inner"} {
		_, err := spec.DecodeExact[tangle]([]byte(`{"`+name+`":"v"}`), envelopeBytes, envelopeDepth)
		require.Error(t, err, "encoding/json writes no %q", name)
	}
}

func TestDecodeExactHoldsItsCaps(t *testing.T) {
	t.Parallel()

	// Three levels: the envelope, the pinned struct, and its labels.
	doc := []byte(`{"pinned":{"labels":["bug"]}}`)

	_, err := spec.DecodeExact[envelope](doc, len(doc), 3)
	require.NoError(t, err)

	_, err = spec.DecodeExact[envelope](doc, len(doc)-1, 3)
	require.EqualError(t, err, "larger than "+strconv.Itoa(len(doc)-1)+" bytes")

	_, err = spec.DecodeExact[envelope](doc, len(doc), 2)
	require.EqualError(t, err, "it nests too deep")
}

// TestDecodeSaysWhyItCantRead pins Decode's words for each way a stored spec can't be
// read, which it says through DecodeExact.
func TestDecodeSaysWhyItCantRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "too large", json: `{"version":1,"requires":["` + strings.Repeat("a", spec.MaxBytes) + `"]}`,
			want: "the routine can't be read: larger than 65536 bytes",
		},
		{
			name: "a key given twice", json: `{"version":1,"trigger":{"event":"appears","event":"manual"}}`,
			want: `the routine can't be read: a key is given twice: "event"`,
		},
		{
			name: "a key in another case", json: `{"version":1,"Trigger":{"event":"manual"}}`,
			want: `the routine can't be read: a key isn't one the format has, spelled exactly: "Trigger"`,
		},
		{
			name: "nested too deep", json: `{"version":1,"requires":` + strings.Repeat("[", 20) + strings.Repeat("]", 20) + `}`,
			want: "the routine can't be read: it nests too deep",
		},
		{name: "something after it", json: `{"version":1} {}`, want: "the routine can't be read: more after the routine"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := spec.Decode([]byte(tt.json))
			require.ErrorIs(t, err, spec.ErrUnreadable)
			assert.EqualError(t, err, tt.want)
		})
	}

	// A value of the wrong type is refused in encoding/json's own words.
	data := []byte(`{"version":1,"limits":{"triesPerItem":"2"}}`)
	want := json.Unmarshal(data, &spec.Spec{})
	require.Error(t, want)

	_, err := spec.Decode(data)
	require.EqualError(t, err, "the routine can't be read: "+want.Error())
}

// FuzzDecodeExact checks that any bytes decode into an envelope without a panic; that
// encoding/json reads what DecodeExact takes the same, so DecodeExact only ever refuses
// more; and that what it takes encodes as a document it takes again, which encodes the
// same.
func FuzzDecodeExact(f *testing.F) {
	for _, name := range []string{flagship, "weekday-nudge"} {
		f.Add([]byte(`{"note":"x","spec":` + string(templateJSON(f, name)) + `}`))
	}

	for _, seed := range []string{
		`{"title":"t","note":"n","spec":{"version":1},"specs":[{"version":1}],"byName":{"a":{"version":1}}}`,
		`{"pinned":{"labels":["bug","bug"]},"count":3}`,
		`{"Note":"x"}`, `{"note":"x","note":"y"}`, `{"spec":{"Steps":[]}}`, "{\"spec\":{\"\u017fteps\":[]}}",
		`{"byName":{"a":{},"a":{}}}`, `{"about":{}}`, `{"note":"x"} {}`, `{}`, `[]`, `null`, "\xff",
		`{"specs":` + strings.Repeat("[", 20) + strings.Repeat("]", 20) + `}`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := spec.DecodeExact[envelope](data, envelopeBytes, envelopeDepth)
		if err != nil {
			return
		}

		var loose envelope
		require.NoError(t, json.Unmarshal(data, &loose), "encoding/json reads what DecodeExact takes")
		assert.Equal(t, loose, got)

		out, err := json.Marshal(got)
		require.NoError(t, err)

		// Encoding writes the fields a document left out, so it may nest deeper, and leaves
		// out an empty list it holds, so the two compare as they encode.
		again, err := spec.DecodeExact[envelope](out, len(out), 4*envelopeDepth)
		require.NoError(t, err, "it takes its own encoding")

		back, err := json.Marshal(again)
		require.NoError(t, err)
		assert.Equal(t, string(out), string(back))
	})
}
