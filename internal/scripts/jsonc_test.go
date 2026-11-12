package scripts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripJSONC(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct{ in, want string }{
		"plain JSON stays as it is": {
			in: `{"a": [1, 2], "b": {"c": "d"}}`, want: `{"a": [1, 2], "b": {"c": "d"}}`,
		},
		"a byte order mark": {in: "\xEF\xBB\xBF{}", want: "{}"},
		"line comments end before their newline": {
			in: "{\n  // a comment\n  \"a\": 1 // after\n}", want: "{\n  \n  \"a\": 1 \n}",
		},
		"a block comment becomes a space": {in: `{"a":/* one */1}`, want: `{"a": 1}`},
		"comment markers inside strings stay": {
			in: `{"url": "https://example.com/*x*/", "p": "a//b"}`, want: `{"url": "https://example.com/*x*/", "p": "a//b"}`,
		},
		"escaped quotes don't end a string": {
			in: `{"a": "say \"hi\" // not a comment", "b": "\\"}`, want: `{"a": "say \"hi\" // not a comment", "b": "\\"}`,
		},
		"trailing commas": {in: `{"a": [1, 2,], "b": 3,}`, want: `{"a": [1, 2], "b": 3}`},
		"a trailing comma behind comments and space": {
			in: "[1, // last\n /* really */ ]", want: "[1 \n   ]",
		},
		"a comma inside a string is no trailing comma": {in: `["a,", "b"]`, want: `["a,", "b"]`},
		"an unterminated block comment ends the input": {in: `{"a": 1} /* never`, want: `{"a": 1} `},
		"an unterminated string is kept":               {in: `{"a": "open`, want: `{"a": "open`},
	} {
		assert.Equal(t, tt.want, string(stripJSONC([]byte(tt.in))), name)
	}
}

func TestDecodeJSONC(t *testing.T) {
	t.Parallel()

	var got map[string]any
	require.NoError(t, decodeJSONC([]byte("\xEF\xBB\xBF{\n // c\n \"a\": [1, /* two */ 2,],\n}"), &got))
	assert.Equal(t, map[string]any{"a": []any{1.0, 2.0}}, got)

	require.Error(t, decodeJSONC([]byte(`{"a": }`), &got))
}

// FuzzStripJSONC checks that nothing panics, that plain JSON comes out unchanged, and that
// valid JSON written as JSONC, with comments and trailing commas everywhere they may go,
// comes out as valid JSON that decodes to the same values.
func FuzzStripJSONC(f *testing.F) {
	for _, seed := range []string{
		`{}`, `[]`, `{"a": [1, "two", {"b": null}], "c": true}`, `"just a string"`, `12.5e3`,
		`{"url": "http://example.com/*x*/", "p": "a//b", "q": "\"//\""}`,
		"{\n // comment\n \"a\": 1,\n}", "[1, /* x */ 2,]", "\xEF\xBB\xBF[]", `{"a": "\\"}`, `/*`, `"`, `//`, `,]`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		out := stripJSONC(data)

		if !json.Valid(data) {
			return
		}

		if !bytes.Equal(out, data) {
			require.Failf(t, "plain JSON changed", "\n in: %q\nout: %q", data, out)
		}

		roundTrip(t, data)
	})
}

// roundTrip checks that data, valid JSON, written as JSONC comes out as JSON that decodes
// to the same values.
func roundTrip(t *testing.T, data []byte) {
	t.Helper()

	want, err := decodeNumbers(data)
	if err != nil {
		require.Failf(t, "decode valid JSON", "%v", err)
	}

	var b strings.Builder
	writeJSONC(t, &b, want)

	got, err := decodeNumbers(stripJSONC([]byte(b.String())))
	if err != nil {
		require.Failf(t, "JSONC didn't come out as JSON", "%v\n%s", err, b.String())
	}

	if fmt.Sprint(got) != fmt.Sprint(want) {
		require.Failf(t, "JSONC decoded differently", "\nwant %v\n got %v", want, got)
	}
}

// decodeNumbers decodes JSON with its numbers as written, which no float64 overflows.
func decodeNumbers(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var v any

	err := dec.Decode(&v)

	return v, err
}

// writeJSONC writes v as JSONC: a comment before every value, of both kinds, with comment
// markers and quotes inside strings, and a trailing comma after every last element.
func writeJSONC(t *testing.T, b *strings.Builder, v any) {
	t.Helper()

	b.WriteString("/* a \"block\" // */ ")

	switch v := v.(type) {
	case map[string]any:
		b.WriteString("{ // open\n")

		for _, k := range slices.Sorted(maps.Keys(v)) {
			b.Write(encoded(t, k))
			b.WriteString(": ")
			writeJSONC(t, b, v[k])
			b.WriteString(", // next \"/*\n")
		}

		b.WriteString("}")
	case []any:
		b.WriteString("[\n")

		for _, e := range v {
			writeJSONC(t, b, e)
			b.WriteString(",\n")
		}

		b.WriteString("]")
	default:
		b.Write(encoded(t, v))
	}
}

// encoded returns v as JSON.
func encoded(t *testing.T, v any) []byte {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		require.Failf(t, "encode", "%v: %v", v, err)
	}

	return data
}
