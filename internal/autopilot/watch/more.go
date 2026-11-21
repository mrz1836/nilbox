package watch

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/textutil"
)

var (
	// botSlug matches a GitHub App's slug, as "app/<bot>" names it.
	botSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}$`)

	// qualifierToken matches one hand-typed qualifier: an optional "-", the qualifier,
	// and a value, bare or in quotes.
	qualifierToken = regexp.MustCompile(`^(-?)([a-z][a-z-]*):(?:"([^"]+)"|([^\s"]+))$`)
)

// errUnclosedQuote is returned for hand-typed qualifiers with a quote left open.
var errUnclosedQuote = errors.New("a quote is left open")

// moreQualifiers are the qualifiers the hand-typed field takes, each with whether only
// pull requests have it. Kind, state, archiving, sorting, and free text are the search's
// own, so they are never typed by hand.
func moreQualifiers() map[string]bool {
	return map[string]bool{
		"author": false, "assignee": false, "mentions": false, "commenter": false, "involves": false,
		"label": false, "milestone": false, "project": false, "language": false, "user": false, "org": false,
		"repo": false, "created": false, "updated": false, "comments": false, "interactions": false,
		"reactions": false, "linked": false, "no": false, "is": false, "team": false,
		"base": true, "head": true, "draft": true, "status": true, "review": true, "review-requested": true,
		"reviewed-by": true, "team-review-requested": true,
	}
}

// isValues are the values is: takes by hand: the ones that don't change the search's
// own kind or state.
func isValues() []string {
	return []string{"public", "private", "locked", "unlocked"}
}

// more adds the hand-typed qualifiers.
func (b *builder) more(more string, kind item.Kind) {
	if utf8.RuneCountInString(more) > MaxMoreRunes {
		b.fail("more", fmt.Errorf("%w: at most %d characters", ErrTooMany, MaxMoreRunes))

		return
	}

	tokens, err := parseMore(more)
	if err != nil {
		b.fail("more", err)

		return
	}

	for _, t := range tokens {
		if err := checkQualifier(t, kind); err != nil {
			b.fail("more", err)

			continue
		}

		b.add(t)
	}
}

// parseMore splits hand-typed qualifiers into tokens, a quoted value kept whole, or
// returns why it can't.
func parseMore(more string) ([]token, error) {
	words, err := splitWords(more)
	if err != nil {
		return nil, err
	}

	tokens := make([]token, 0, len(words))

	for _, w := range words {
		if q, v, found := strings.Cut(w, ":"); found {
			w = strings.ToLower(q) + ":" + v
		}

		m := qualifierToken.FindStringSubmatch(w)
		if m == nil {
			return nil, fmt.Errorf(`%w: %q; type qualifiers such as language:go or label:"help wanted"`, ErrQualifier, w)
		}

		// A value is held to what every value of a search is (unsearchable): a backslash,
		// say, could read as an escaped quote and let what follows go live.
		value := m[3] + m[4]
		if !utf8.ValidString(value) || strings.ContainsFunc(value, unsearchable) {
			return nil, fmt.Errorf("%w: %q holds a quote, a backslash, or a control character", ErrInvalidValue, w)
		}

		tokens = append(tokens, token{neg: m[1] == "-", q: m[2], values: []string{value}, field: "more"})
	}

	return tokens, nil
}

// splitWords splits s at white space outside quotes.
func splitWords(s string) ([]string, error) {
	var (
		words   []string
		current strings.Builder
		quoted  bool
	)

	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted

			current.WriteRune(r)
		case unicode.IsSpace(r) && !quoted:
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if quoted {
		return nil, errUnclosedQuote
	}

	if current.Len() > 0 {
		words = append(words, current.String())
	}

	return words, nil
}

// checkQualifier refuses a hand-typed qualifier the search can't take.
func checkQualifier(t token, kind item.Kind) error {
	prOnly, ok := moreQualifiers()[t.q]

	switch {
	case !ok:
		return fmt.Errorf("%w: %s", ErrQualifier, t.q)
	case prOnly && kind == item.KindIssue:
		return fmt.Errorf("%s: %w", t.q, ErrPullRequestsOnly)
	case t.q == "is" && !slices.Contains(isValues(), strings.ToLower(t.values[0])):
		return fmt.Errorf("%w: is:%s; the search is always for open items of its kind", ErrQualifier, t.values[0])
	case t.q == "author" && slices.Contains(knownApps(), strings.ToLower(t.values[0])):
		return fmt.Errorf(`%w: author:%s finds nothing; write author:app/%s`, ErrInvalidValue, t.values[0],
			strings.ToLower(t.values[0]))
	default:
		return nil
	}
}

// validAuthor reports an author the search can take: "@me", a login, or "app/<bot>",
// but never a known app's bare slug, which GitHub's search matches nothing by.
func validAuthor(a string) bool {
	if a == Me {
		return true
	}

	if slug, ok := strings.CutPrefix(a, "app/"); ok {
		return botSlug.MatchString(slug)
	}

	return repo.ValidOwner(a) && !slices.Contains(knownApps(), strings.ToLower(a))
}

// unsearchable reports a character no value of a search may hold, quoted or not:
//
//   - a quote, which would end a quoted value early: GitHub's search has no escape for a
//     quote inside quotes, so a value holding one can't be written as itself at all;
//   - a backslash, which a reader could take for an escape of the closing quote, letting
//     what follows go live;
//   - the fullwidth quote and backslash, which a reader that folds Unicode's
//     compatibility forms reads as those two;
//   - a control character (a newline and a tab among them) and a line or paragraph
//     separator, which no label, branch, or qualifier holds.
//
// An apostrophe is fine: GitHub's search quotes only with double quotes.
func unsearchable(r rune) bool {
	switch r {
	case '"', '\\', '\uff02', '\uff3c', '\u2028', '\u2029':
		return true
	default:
		return unicode.IsControl(r)
	}
}

// checkValue refuses a value no search can hold: an empty one, one that isn't UTF-8, and
// one holding a character unsearchable names.
func checkValue(v string) error {
	switch {
	case v == "":
		return fmt.Errorf("%w: a value is empty", ErrInvalidValue)
	case !utf8.ValidString(v) || strings.ContainsFunc(v, unsearchable):
		return fmt.Errorf("%w: %q holds a quote, a backslash, or a control character", ErrInvalidValue, v)
	default:
		return nil
	}
}

// quoteless takes out of v every character that could end a quoted value early or escape
// its closing quote: the quote and the backslash, and their fullwidth forms. Every
// field's checks refuse a value holding one, and build refuses a token holding one
// (token.check), so this changes no value a query is written with: it is what keeps
// String from ever writing one raw, whoever calls it.
func quoteless(v string) string {
	v = strings.ReplaceAll(v, `"`, "")
	v = strings.ReplaceAll(v, "\uff02", "")
	v = strings.ReplaceAll(v, `\`, "")

	return strings.ReplaceAll(v, "\uff3c", "")
}

// checkLabel refuses a label that can't be quoted in a comma list: one holding a comma,
// or a character no search value can hold (unsearchable).
func checkLabel(l string) error {
	switch {
	case strings.TrimSpace(l) == "":
		return fmt.Errorf("%w: a label is empty", ErrInvalidValue)
	case utf8.RuneCountInString(l) > MaxLabelRunes:
		return fmt.Errorf("%w: %q is longer than GitHub's %d characters", ErrInvalidValue, l, MaxLabelRunes)
	case strings.ContainsRune(l, ',') || !utf8.ValidString(l) || strings.ContainsFunc(l, unsearchable):
		return fmt.Errorf("%w: %q holds a quote, a comma, a backslash, or a control character", ErrInvalidValue, l)
	default:
		return nil
	}
}

// checkBranch refuses a branch name, or the start of one, a search can't take: one longer
// than maxBranchRunes, or holding a space of any kind, Unicode's included, since the base
// goes in unquoted, or a character no search value can hold (unsearchable). Git allows no
// space, backslash, or control character in a branch's name either.
func checkBranch(b string) error {
	bad := func(r rune) bool { return unicode.IsSpace(r) || unsearchable(r) }

	switch {
	case utf8.RuneCountInString(b) > maxBranchRunes:
		return fmt.Errorf("%w: %q is longer than %d characters", ErrInvalidValue, b, maxBranchRunes)
	case !utf8.ValidString(b) || strings.ContainsFunc(b, bad):
		return fmt.Errorf("%w: %q holds a space, a quote, a backslash, or a control character", ErrInvalidValue, b)
	default:
		return nil
	}
}

// Normalize returns s cleaned as the spec stores it: each value trimmed, logins, orgs,
// and repositories lower-cased (GitHub reads them ignoring case), a bot's "x[bot]"
// written "app/x", every list sorted without repeats (labels compared ignoring case),
// and the hand-typed qualifiers written canonically, sorted. Normalize changes no value
// Check would refuse into one it takes, and the query for a normalized search is
// written in one order.
func Normalize(s Search) Search {
	s.Kind = item.Kind(strings.ToLower(strings.TrimSpace(string(s.Kind))))
	s.Authors = sortedSet(s.Authors, normalizeAuthor)
	s.Where = sortedSet(s.Where, func(w string) string { return strings.ToLower(strings.TrimSpace(w)) })
	s.Involving = sortedSet(s.Involving, func(i Involving) Involving { return Involving(strings.TrimSpace(string(i))) })
	s.Labels = labelSet(s.Labels)
	s.NotLabels = labelSet(s.NotLabels)
	s.Base = strings.TrimSpace(s.Base)
	s.HeadPrefix = strings.TrimSpace(s.HeadPrefix)
	s.State = sortedSet(s.State, func(st State) State { return State(strings.TrimSpace(string(st))) })
	s.More = normalizeMore(s.More)

	return s
}

// knownApps are the bots a routine most often watches, by their app's slug. GitHub's
// search finds a bot's items only as author:app/<slug> (or <slug>[bot]), never by the
// bare slug, so Normalize writes these as apps.
func knownApps() []string {
	return []string{"dependabot", "github-actions", "renovate"}
}

// normalizeAuthor writes an author as the search takes it: lower-cased, and a bot,
// written "x[bot]" or as one of the known apps' bare slugs, as "app/x".
func normalizeAuthor(a string) string {
	a = strings.ToLower(strings.TrimSpace(a))
	if bot, ok := strings.CutSuffix(a, "[bot]"); ok && bot != "" && !strings.HasPrefix(bot, "app/") {
		return "app/" + bot
	}

	if slices.Contains(knownApps(), a) {
		return "app/" + a
	}

	return a
}

// normalizeMore writes hand-typed qualifiers canonically: each "qualifier:value" with the
// qualifier lower-cased, sorted, without repeats, a value quoted only when it holds a
// space. Qualifiers it can't read are only trimmed, for Check to refuse.
func normalizeMore(more string) string {
	more = strings.TrimSpace(more)

	tokens, err := parseMore(more)
	if err != nil {
		return more
	}

	words := make([]string, 0, len(tokens))
	for _, t := range tokens {
		words = append(words, t.String())
	}

	slices.Sort(words)

	return strings.Join(slices.Compact(words), " ")
}

// sortedSet returns values cleaned by clean, without empty ones and repeats, sorted; nil
// for none, so an empty list is left out of the spec.
func sortedSet[T ~string](values []T, clean func(T) T) []T {
	out := make([]T, 0, len(values))

	for _, v := range values {
		if c := clean(v); c != "" && !slices.Contains(out, c) {
			out = append(out, c)
		}
	}

	if len(out) == 0 {
		return nil
	}

	slices.Sort(out)

	return out
}

// labelSet returns labels trimmed, without empty ones and repeats ignoring case (the
// first spelling kept), sorted ignoring case; nil for none.
func labelSet(labels []string) []string {
	out := textutil.UniqueFold(labels, strings.TrimSpace)
	if len(out) == 0 {
		return nil
	}

	slices.SortFunc(out, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})

	return out
}
