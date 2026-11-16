package spec_test

import (
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/item"
)

// breakout is a value written to end its quotes early and add a qualifier of its own.
const breakout = `x" repo:other/repo "`

// TestValidateRefusesWhatNoSearchCanHold checks that a value no search can hold, in each
// field a search writes, is a problem naming the watch and the field, saying why.
func TestValidateRefusesWhatNoSearchCanHold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*watch.Search)
		path   string
		says   string
	}{
		{
			name: "a label that closes its quote", mutate: func(s *watch.Search) { s.Labels = []string{breakout} },
			path: "watch.sources[0].search.labels[0]", says: "holds a quote, a comma, a backslash, or a control character",
		},
		{
			name: "a label written as a list", mutate: func(s *watch.Search) { s.Labels = []string{"bug", `label:"a","b"`} },
			path: "watch.sources[0].search.labels[1]", says: "holds a quote",
		},
		{
			name: "a label holding a backslash", mutate: func(s *watch.Search) { s.Labels = []string{`a\b`} },
			path: "watch.sources[0].search.labels[0]", says: "a backslash",
		},
		{
			name: "a label holding a newline", mutate: func(s *watch.Search) { s.Labels = []string{"a\nrepo:other/repo"} },
			path: "watch.sources[0].search.labels[0]", says: "a control character",
		},
		{
			name: "a label holding a fullwidth quote", mutate: func(s *watch.Search) { s.Labels = []string{"x\uff02 repo:other/repo"} },
			path: "watch.sources[0].search.labels[0]", says: "holds a quote",
		},
		{
			name: "a label left out that closes its quote", mutate: func(s *watch.Search) { s.NotLabels = []string{breakout} },
			path: "watch.sources[0].search.notLabels[0]", says: "holds a quote",
		},
		{
			name: "a base that closes a quote", mutate: func(s *watch.Search) { s.Base = `main"repo:other/repo` },
			path: "watch.sources[0].search.base", says: "holds a space, a quote, a backslash, or a control character",
		},
		{
			name: "a base holding a Unicode space", mutate: func(s *watch.Search) { s.Base = "main\u2003repo:other/repo" },
			path: "watch.sources[0].search.base", says: "holds a space",
		},
		{
			name: "a head prefix that closes its quote", mutate: func(s *watch.Search) { s.HeadPrefix = `deps/"` },
			path: "watch.sources[0].search.headPrefix", says: "a quote",
		},
		{
			name: "an author that closes a quote", mutate: func(s *watch.Search) { s.Authors = []string{breakout} },
			path: "watch.sources[0].search.authors[0]", says: `write "@me", a login, or "app/<bot>"`,
		},
		{
			name:   "a place that closes a quote",
			mutate: func(s *watch.Search) { s.Where = []string{"org:acme-org", `repo:acme-org/api"`} },
			path:   "watch.sources[0].search.where[1]", says: `"repo:<owner>/<name>"`,
		},
		{
			name: "a backslash typed by hand", mutate: func(s *watch.Search) { s.More = `label:"x\" repo:other/repo` },
			path: "watch.sources[0].search.more", says: "holds a quote, a backslash, or a control character",
		},
		{
			name: "a list typed by hand", mutate: func(s *watch.Search) { s.More = `label:"a","b"` },
			path: "watch.sources[0].search.more", says: "isn't a qualifier a routine's search can take",
		},
		{
			name: "a fullwidth backslash typed by hand", mutate: func(s *watch.Search) { s.More = "label:\"x\uff3c\"" },
			path: "watch.sources[0].search.more", says: "a backslash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := loadTemplate(t, flagship)
			tt.mutate(s.Watch.Sources[0].Search)

			problems := problemsOf(t, spec.Normalize(s))
			require.Len(t, problems, 1)
			assert.Equal(t, spec.PartWatch, problems[0].Part)
			assert.Equal(t, tt.path, problems[0].Path())
			assert.Contains(t, problems[0].Message, tt.says)
		})
	}
}

// problemsOf validates s, which must be invalid, and returns its problems.
func problemsOf(t *testing.T, s spec.Spec) []spec.Problem {
	t.Helper()

	var verr *spec.ValidationError

	require.ErrorAs(t, spec.Validate(s), &verr)

	return verr.Problems
}

// TestAStoredSearchNoSearchCanHoldLoadsBroken checks that a routine stored with a value
// no search can hold, one an earlier build took among them, still loads: broken as
// invalid, with the problem naming the watch and the field for the builder to show, its
// value as stored, never rewritten, and no query built from it.
func TestAStoredSearchNoSearchCanHoldLoadsBroken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*watch.Search, string)
		value  string
		path   string
	}{
		{
			name: "a label that closes its quote", mutate: func(s *watch.Search, v string) { s.Labels = []string{v} },
			value: breakout, path: "watch.sources[0].search.labels[0]",
		},
		{
			name: "a label holding a fullwidth quote", mutate: func(s *watch.Search, v string) { s.Labels = []string{v} },
			value: "x\uff02 repo:other/repo", path: "watch.sources[0].search.labels[0]",
		},
		{
			name: "a label holding a line separator", mutate: func(s *watch.Search, v string) { s.Labels = []string{v} },
			value: "bug\u2028repo:other/repo", path: "watch.sources[0].search.labels[0]",
		},
		{
			name: "a head prefix holding a fullwidth quote", mutate: func(s *watch.Search, v string) { s.HeadPrefix = v },
			value: "deps/\uff02", path: "watch.sources[0].search.headPrefix",
		},
		{
			name: "a fullwidth quote typed by hand", mutate: func(s *watch.Search, v string) { s.More = v },
			value: "label:\"x\uff02 repo:other/repo\"", path: "watch.sources[0].search.more",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stored := loadTemplate(t, flagship)
			tt.mutate(stored.Watch.Sources[0].Search, tt.value)

			data, err := spec.Encode(stored)
			require.NoError(t, err)

			s, err := spec.Load(data)

			var (
				broken *spec.BrokenError
				verr   *spec.ValidationError
			)

			require.ErrorAs(t, err, &broken)
			assert.Equal(t, spec.ReasonInvalid, broken.Reason)
			require.ErrorAs(t, err, &verr)
			require.Len(t, verr.Problems, 1)
			assert.Equal(t, tt.path, verr.Problems[0].Path())

			assert.Equal(t, stored.Watch.Sources[0].Search, s.Watch.Sources[0].Search, "it comes back as stored")

			_, err = watch.Build(*s.Watch.Sources[0].Search, "octo-user")
			require.ErrorIs(t, err, watch.ErrInvalidValue, "and builds no query")

			quotedIn(t, spec.Sentence(s, nil))
		})
	}
}

// TestSentenceQuotesAValueSafely checks how the sentence writes a value that holds a
// quote, a backslash, or a character that doesn't print: quoted and escaped, as Go quotes
// a string, and a plain word bare.
func TestSentenceQuotesAValueSafely(t *testing.T) {
	t.Parallel()

	tests := []struct {
		clause spec.Clause
		want   string
	}{
		{spec.Clause{Field: catalog.FieldTitle, Op: catalog.OpContains, Value: `say "hi"`}, `the title contains "say \"hi\""`},
		{spec.Clause{Field: catalog.FieldTitle, Op: catalog.OpContains, Value: breakout}, `the title contains "x\" repo:other/repo \""`},
		{spec.Clause{Field: catalog.FieldTitle, Op: catalog.OpContains, Value: `C:\temp`}, `the title contains "C:\\temp"`},
		{spec.Clause{Field: catalog.FieldTitle, Op: catalog.OpStartsWith, Value: "won't"}, "the title starts with won't"},
		{spec.Clause{Field: catalog.FieldTitle, Op: catalog.OpIs, Value: "do\u00a0not\u00a0merge"}, `the title is "do\u00a0not\u00a0merge"`},
		{
			spec.Clause{Field: catalog.FieldLabels, Op: catalog.OpHasAny, Values: []string{"evil\u202eexe", "bug"}},
			`the labels include "evil\u202eexe" or bug`,
		},
		{
			spec.Clause{Field: catalog.FieldLabels, Op: catalog.OpHasAll, Values: []string{"a\nb", `"`}},
			`the labels include "a\nb" and "\""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, spec.Phrase(tt.clause))
		})
	}

	s := withSteps(t, spec.Step{GitHub: []spec.GitHubAction{{Labels: &spec.LabelsAction{Add: []string{`say "hi"`}, Remove: []string{"bug"}}}}})
	s.Trigger = spec.Trigger{Event: catalog.EventLabeled, Label: breakout}
	s.Conditions = spec.Conditions{Match: spec.MatchAll}

	sentence := spec.Sentence(s, nil)
	assert.Contains(t, sentence, `is labeled "x\" repo:other/repo \""`)
	assert.Contains(t, sentence, `add the "say \"hi\"" label and remove the bug label`)
}

// TestSentenceStaysBalanced checks that a hostile value, wherever a routine names it,
// leaves the sentence's quotes balanced, each quoted value reading back as itself.
func TestSentenceStaysBalanced(t *testing.T) {
	t.Parallel()

	for _, v := range []string{
		`"`, breakout, `a\`, `\"`, "a\nb", "bug\u00a0author:x", "bug\u3000repo:other/repo", `label:"a","b"`,
		"x\uff02 repo:other/repo \uff02", "evil\u202eexe", "won't fix", `""`, "",
	} {
		t.Run(v, func(t *testing.T) {
			t.Parallel()

			sentence := spec.Sentence(everywhere(v), nil)
			values := quotedIn(t, sentence)

			if needsQuotes(v) {
				assert.Contains(t, values, v, sentence)
			}
		})
	}
}

// FuzzSentenceQuotes checks that any value, wherever a routine names it, leaves the
// sentence's quotes balanced, shows a value that needs quoting quoted as itself, and
// leaves nothing in the sentence that doesn't print.
func FuzzSentenceQuotes(f *testing.F) {
	for _, seed := range []string{`"`, breakout, `a\`, "a\nb", "bug\u00a0x", `label:"a","b"`, "x\uff02", "\u202e", "plain", ""} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, v string) {
		sentence := spec.Sentence(everywhere(v), nil)
		values := quotedIn(t, sentence)

		if needsQuotes(v) {
			assert.Contains(t, values, v, sentence)
		}

		assert.False(t, strings.ContainsFunc(sentence, func(r rune) bool { return !strconv.IsPrint(r) }),
			"nothing that doesn't print shows in %q", sentence)
	})
}

// everywhere returns a spec that names v in every place a routine's sentence shows a value:
// the search's fields, the trigger's label, a condition's value and values, and a labels
// action. It isn't valid, nor need it be: the sentence says any spec.
func everywhere(v string) spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch: spec.Watch{Sources: []spec.Source{{Search: &watch.Search{
			Kind: item.KindPR, Authors: []string{v, "app/" + v}, Where: []string{"org:" + v}, Labels: []string{v},
			NotLabels: []string{v}, Base: v, HeadPrefix: v, More: v,
		}}}},
		Trigger: spec.Trigger{Event: catalog.EventLabeled, Label: v},
		Conditions: spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{
			{Field: catalog.FieldTitle, Op: catalog.OpContains, Value: v},
			{Field: catalog.FieldLabels, Op: catalog.OpHasAny, Values: []string{v, "bug"}},
			{Field: catalog.FieldApprovals, Op: catalog.OpGte, Value: v},
		}},
		Steps: []spec.Step{{GitHub: []spec.GitHubAction{{Labels: &spec.LabelsAction{Add: []string{v}}}}}},
	}
}

// needsQuotes reports a value the sentence quotes: an empty one, or one holding a space of
// any kind, a quote, a backslash, or a character that doesn't print.
func needsQuotes(v string) bool {
	return v == "" || strings.ContainsFunc(v, func(r rune) bool {
		return r == '"' || r == '\\' || unicode.IsSpace(r) || !strconv.IsPrint(r)
	})
}

// quotedIn returns each value the sentence quotes, read back as Go reads a quoted string,
// and fails the test at a quote that doesn't close.
func quotedIn(t *testing.T, sentence string) []string {
	t.Helper()

	var values []string

	for rest := sentence; ; {
		i := strings.IndexByte(rest, '"')
		if i < 0 {
			return values
		}

		quoted, err := strconv.QuotedPrefix(rest[i:])
		require.NoError(t, err, "a quote doesn't close in %q", sentence)

		v, err := strconv.Unquote(quoted)
		require.NoError(t, err)

		values = append(values, v)
		rest = rest[i+len(quoted):]
	}
}
