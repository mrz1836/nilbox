package watch

import (
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
)

// qualifier is one word of a query as a reader of GitHub's search syntax reads it: a
// qualifier, negated or not, with its values, or free text, whose name is "".
type qualifier struct {
	neg    bool
	name   string
	values []string
}

// qualifierWord matches a word that is a qualifier: an optional "-", the qualifier's
// name, a colon, and its values.
var qualifierWord = regexp.MustCompile(`(?s)^(-?)([a-z][a-z-]*):(.*)$`)

// readQuery reads a query as GitHub's search syntax is documented: words split at white
// space of any kind outside quotes, a word "-"?name":" its values, the values split at
// commas outside quotes, and a double quote opening and closing a quoted stretch. GitHub
// documents no escape; with escapes, it reads as a reader that takes a backslash to
// escape the character after it would, the worst case for a value ending in one.
func readQuery(s string, escapes bool) []qualifier {
	var out []qualifier

	for _, w := range splitOutside(s, escapes, unicode.IsSpace) {
		if w == "" {
			continue
		}

		m := qualifierWord.FindStringSubmatch(w)
		if m == nil {
			out = append(out, qualifier{values: []string{unquote(w, escapes)}})

			continue
		}

		q := qualifier{neg: m[1] == "-", name: m[2]}
		for _, v := range splitOutside(m[3], escapes, func(r rune) bool { return r == ',' }) {
			q.values = append(q.values, unquote(v, escapes))
		}

		out = append(out, q)
	}

	return out
}

// splitOutside splits s at each rune sep takes outside quotes, keeping the quotes and
// escapes in each part.
func splitOutside(s string, escapes bool, sep func(rune) bool) []string {
	var (
		parts            []string
		current          strings.Builder
		quoted, escaping bool
	)

	for _, r := range s {
		switch {
		case escaping:
			escaping = false

			current.WriteRune(r)
		case escapes && r == '\\':
			escaping = true

			current.WriteRune(r)
		case r == '"':
			quoted = !quoted

			current.WriteRune(r)
		case !quoted && sep(r):
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}

	return append(parts, current.String())
}

// unquote returns a part's text without its quotes and, with escapes, its escapes.
func unquote(s string, escapes bool) string {
	var (
		b        strings.Builder
		escaping bool
	)

	for _, r := range s {
		switch {
		case escaping:
			escaping = false

			b.WriteRune(r)
		case escapes && r == '\\':
			escaping = true
		case r == '"':
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}

// readBoth reads a query as both readers do, and fails the test when they disagree.
func readBoth(t *testing.T, query string) []qualifier {
	t.Helper()

	plain := readQuery(query, false)
	require.Equal(t, plain, readQuery(query, true), "a backslash reader reads %q otherwise", query)

	return plain
}

// q builds a qualifier for a test's expectations.
func q(name string, values ...string) qualifier {
	return qualifier{name: name, values: values}
}

// inAcmeOrg returns the qualifiers of a search in acme-org, with middle between the
// search's own and its sort.
func inAcmeOrg(middle ...qualifier) []qualifier {
	out := []qualifier{q("is", "open"), q("archived", "false"), q("org", "acme-org")}
	out = append(out, middle...)

	return append(out, q("sort", "updated-desc"))
}

// TestHostileValuesAddNoQualifier checks that a value meant to break out of its quotes,
// or to read as another qualifier, in any field, is refused with the field named, and
// that a hostile-looking value a search can hold stays one value of its own qualifier,
// read by a reader that takes no escape and by one that takes a backslash as one.
func TestHostileValuesAddNoQualifier(t *testing.T) {
	t.Parallel()

	const breakout = `x" repo:other/repo "`

	tests := []struct {
		name   string
		mutate func(*Search)

		// field and err are the problem a refused search names; want is what an
		// accepted one's query reads as.
		field string
		err   error
		want  []qualifier
	}{
		{name: "a label of one quote", mutate: func(s *Search) { s.Labels = []string{`"`} }, field: "labels[0]", err: ErrInvalidValue},
		{name: "a label that closes its quote", mutate: func(s *Search) { s.Labels = []string{breakout} }, field: "labels[0]", err: ErrInvalidValue},
		{name: "a label written as a list", mutate: func(s *Search) { s.Labels = []string{`label:"a","b"`} }, field: "labels[0]", err: ErrInvalidValue},
		{name: "a label ending in a backslash", mutate: func(s *Search) { s.Labels = []string{`a\`} }, field: "labels[0]", err: ErrInvalidValue},
		{
			name: "a label holding a newline", mutate: func(s *Search) { s.Labels = []string{"a\nrepo:other/repo"} },
			field: "labels[0]", err: ErrInvalidValue,
		},
		{
			name: "a label holding a tab", mutate: func(s *Search) { s.Labels = []string{"a\trepo:other/repo"} },
			field: "labels[0]", err: ErrInvalidValue,
		},
		{
			name: "a label holding a line separator", mutate: func(s *Search) { s.Labels = []string{"a\u2028repo:other/repo"} },
			field: "labels[0]", err: ErrInvalidValue,
		},
		{
			name:   "a label holding fullwidth quotes",
			mutate: func(s *Search) { s.Labels = []string{"x\uff02 repo:other/repo \uff02"} }, field: "labels[0]", err: ErrInvalidValue,
		},
		{
			name: "a label holding a fullwidth backslash", mutate: func(s *Search) { s.Labels = []string{"x\uff3c"} },
			field: "labels[0]", err: ErrInvalidValue,
		},
		{
			name: "a second label that closes its quote", mutate: func(s *Search) { s.Labels = []string{"bug", breakout} },
			field: "labels[1]", err: ErrInvalidValue,
		},
		{name: "a label left out that closes its quote", mutate: func(s *Search) { s.NotLabels = []string{breakout} }, field: "notLabels[0]", err: ErrInvalidValue},
		{name: "a base that closes a quote", mutate: func(s *Search) { s.Base = `main" repo:other/repo "` }, field: "base", err: ErrInvalidValue},
		{name: "a base holding an ideographic space", mutate: func(s *Search) { s.Base = "main\u3000repo:other/repo" }, field: "base", err: ErrInvalidValue},
		{name: "a head prefix that closes its quote", mutate: func(s *Search) { s.HeadPrefix = `deps/" repo:other/repo "` }, field: "headPrefix", err: ErrInvalidValue},
		{name: "a head prefix ending in a backslash", mutate: func(s *Search) { s.HeadPrefix = `deps\` }, field: "headPrefix", err: ErrInvalidValue},
		{name: "a head prefix holding a fullwidth quote", mutate: func(s *Search) { s.HeadPrefix = "deps\uff02" }, field: "headPrefix", err: ErrInvalidValue},
		{name: "an author that closes a quote", mutate: func(s *Search) { s.Authors = []string{`octocat" repo:other/repo "`} }, field: "authors[0]", err: ErrInvalidValue},
		{
			name:   "a place that closes a quote",
			mutate: func(s *Search) { s.Where = append(s.Where, `repo:acme-org/api" repo:other/repo "`) }, field: "where[1]", err: ErrInvalidValue,
		},
		{name: "a backslash typed by hand", mutate: func(s *Search) { s.More = `label:"x\" repo:other/repo` }, field: "more", err: ErrInvalidValue},
		{name: "a list typed by hand", mutate: func(s *Search) { s.More = `label:"a","b"` }, field: "more", err: ErrQualifier},
		{name: "a quote that closes early, typed by hand", mutate: func(s *Search) { s.More = `label:"x" repo:other/repo"` }, field: "more", err: errUnclosedQuote},
		{name: "a fullwidth quote typed by hand", mutate: func(s *Search) { s.More = "label:\"x\uff02 repo:other/repo\"" }, field: "more", err: ErrInvalidValue},
		{name: "a newline typed by hand", mutate: func(s *Search) { s.More = "label:\"a\nrepo:other/repo\"" }, field: "more", err: ErrInvalidValue},
		{
			name: "a label holding a non-breaking space", mutate: func(s *Search) { s.Labels = []string{"bug\u00a0author:x"} },
			want: inAcmeOrg(q("label", "bug\u00a0author:x")),
		},
		{
			name: "a label holding an ideographic space", mutate: func(s *Search) { s.Labels = []string{"bug\u3000repo:other/repo"} },
			want: inAcmeOrg(q("label", "bug\u3000repo:other/repo")),
		},
		{
			name: "a label that reads like a qualifier", mutate: func(s *Search) { s.Labels = []string{"bug repo:other/repo"} },
			want: inAcmeOrg(q("label", "bug repo:other/repo")),
		},
		{
			name: "a label with an apostrophe", mutate: func(s *Search) { s.Labels = []string{"won't fix", "bug"} },
			want: inAcmeOrg(q("label", "bug", "won't fix")),
		},
		{
			name: "a label left out that reads like a qualifier", mutate: func(s *Search) { s.NotLabels = []string{"a repo:other/repo"} },
			want: inAcmeOrg(qualifier{neg: true, name: "label", values: []string{"a repo:other/repo"}}),
		},
		{
			name: "a quoted value typed by hand holding a non-breaking space", mutate: func(s *Search) { s.More = "label:\"a\u00a0repo:other/repo\"" },
			want: inAcmeOrg(q("label", "a\u00a0repo:other/repo")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := Search{Kind: item.KindPR, Where: []string{"org:acme-org"}}
			tt.mutate(&s)

			query, err := Build(Normalize(s), "octo-user")

			if tt.want == nil {
				var fe *FieldError

				require.ErrorAs(t, err, &fe)
				assert.Equal(t, tt.field, fe.Field)
				require.ErrorIs(t, err, tt.err)

				problems := Check(Normalize(s))
				require.NotEmpty(t, problems, "Check refuses it too")
				assert.Equal(t, tt.field, problems[0].Field)

				return
			}

			require.NoError(t, err)

			want := append([]qualifier{q("is", "pr")}, tt.want...)
			assert.Equal(t, want, readBoth(t, query.Text), query.Text)
		})
	}
}

// TestBuildRefusesATokenItsFieldMissed checks the last check: a token holding a value no
// search can hold, which its field's own checks would have refused, is refused when the
// query is written, naming the field, and String writes it without its quotes, so it
// still reads as one value of its own qualifier.
func TestBuildRefusesATokenItsFieldMissed(t *testing.T) {
	t.Parallel()

	for _, v := range []string{`x" repo:other/repo "`, `a\`, "a\nb", "\uff02 repo:other/repo \uff02", ""} {
		t.Run(v, func(t *testing.T) {
			t.Parallel()

			b := &builder{}
			b.add(token{q: "label", values: []string{"bug", v}, quote: true, field: "labels"})

			text := b.text()

			require.Len(t, b.problems, 1)
			assert.Equal(t, "labels", b.problems[0].Field)
			require.ErrorIs(t, b.problems[0], ErrInvalidValue)

			got := readBoth(t, text)
			require.Len(t, got, 1, text)
			assert.Equal(t, "label", got[0].name)
			assert.Len(t, got[0].values, 2)
		})
	}
}

// qualifierName matches a qualifier's name, as the builder's own and every one typed by
// hand are.
var qualifierName = regexp.MustCompile(`^[a-z][a-z-]*$`)

// FuzzTokenString checks that whatever values a token holds, String writes it as exactly
// one qualifier, its own, negated as it is, between the query's neighbors, read by a
// reader that takes no escape and by one that takes a backslash as one; and that a token
// check takes reads back with exactly its own values, an unquoted one split at its commas
// as GitHub's list syntax splits it.
func FuzzTokenString(f *testing.F) {
	f.Add(false, "label", "bug", "help wanted", true)
	f.Add(true, "label", `x" repo:other/repo "`, `a","b`, true)
	f.Add(false, "label", `label:"a","b"`, "", false)
	f.Add(false, "base", `main\`, "", false)
	f.Add(false, "head", `deps\" repo:other/repo`, "", true)
	f.Add(false, "label", "a\nb", "c\td", false)
	f.Add(false, "label", "bug\u00a0author:x", "x\u3000repo:other/repo", false)
	f.Add(false, "label", "\uff02 repo:other/repo \uff02", "\uff3c", true)
	f.Add(false, "language", "go,rust", "", false)
	f.Add(false, "label", "", "", false)
	f.Add(true, "Repo", `"`, `\`, false)

	f.Fuzz(func(t *testing.T, neg bool, name, first, second string, quote bool) {
		if !qualifierName.MatchString(name) {
			name = "label"
		}

		values := []string{first}
		if second != "" {
			values = append(values, second)
		}

		tok := token{neg: neg, q: name, values: values, quote: quote}
		query := "is:open " + tok.String() + " sort:updated-desc"

		got := readBoth(t, query)
		require.Len(t, got, 3, query)
		assert.Equal(t, q("is", "open"), got[0])
		assert.Equal(t, q("sort", "updated-desc"), got[2])
		assert.Equal(t, neg, got[1].neg, query)
		assert.Equal(t, name, got[1].name, query)

		if tok.check() == nil {
			assert.Equal(t, ownValues(tok), got[1].values, query)
		}
	})
}

// ownValues returns the values a token reads back with: each quoted one as it is, and
// each unquoted one split at its commas, GitHub's list syntax.
func ownValues(t token) []string {
	var out []string

	for _, v := range t.values {
		if t.quote || strings.ContainsFunc(v, unicode.IsSpace) {
			out = append(out, v)

			continue
		}

		out = append(out, strings.Split(v, ",")...)
	}

	return out
}
