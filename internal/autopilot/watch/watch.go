// Package watch turns a routine's search, as the builder writes it from simple fields,
// into the GitHub search query a refresh sends, and gives each query the key identical
// searches across routines share, so they go to GitHub once.
//
// Every query follows the same rules:
//
//   - it always adds is:open archived:false sort:updated-desc;
//   - it is scoped: it names a user, an org, or a repository, or asks for something of
//     the signed-in user's (an @me qualifier), so a routine can never scan all of GitHub;
//   - each value is checked, and a label is quoted, so no value can smuggle in another
//     qualifier ("bug author:x" stays one label): GitHub's search has no escape for a
//     quote inside quotes, so no value may hold a quote, a backslash, or a control
//     character, build refuses a query that would carry one whatever its field, and the
//     writer itself never writes a quote or a backslash in a value;
//   - it is at most 256 characters, GitHub's cap; a longer one is refused, with the hint
//     to split it in two;
//   - "your account" (@me in Where) becomes user:<login> when the query is built, so a
//     routine follows whichever account is signed in.
//
// The query is written in one canonical order from the normalized search, so the query
// itself is the search's key. It is a leaf: it imports only item and repo.
package watch

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// Me is the signed-in user, in Authors and Where. In Where it is "your account".
const Me = "@me"

// MaxQueryRunes is the longest query GitHub takes.
const MaxQueryRunes = 256

// Caps on a search's lists.
const (
	// MaxValues caps each list of a search.
	MaxValues = 10

	// MaxMoreRunes caps the qualifiers typed by hand.
	MaxMoreRunes = 200

	// MaxLabelRunes is the longest label GitHub allows.
	MaxLabelRunes = 50

	// maxBranchRunes caps a branch name or prefix.
	maxBranchRunes = 100
)

// Involving is a way the search's items involve the signed-in user.
type Involving string

// The ways an item can involve the user.
const (
	InvolvingAssigned        Involving = "assigned"
	InvolvingReviewRequested Involving = "reviewRequested"
	InvolvingMentioned       Involving = "mentioned"
)

// Involvings returns every way an item can involve the user, in the builder's order.
func Involvings() []Involving {
	return []Involving{InvolvingAssigned, InvolvingReviewRequested, InvolvingMentioned}
}

// States returns every state a search can ask for, in the builder's order.
func States() []State {
	return []State{
		StateReady, StateDraft, StateFailing, StatePassing, StatePending, StateChangesRequested, StateApproved,
		StateReviewRequired, StateUnreviewed,
	}
}

// State narrows a search by the items' state.
type State string

// The states a search can ask for.
const (
	StateReady            State = "ready"
	StateDraft            State = "draft"
	StateFailing          State = "failing"
	StatePassing          State = "passing"
	StatePending          State = "pending"
	StateChangesRequested State = "changesRequested"
	StateApproved         State = "approved"
	StateReviewRequired   State = "reviewRequired"
	StateUnreviewed       State = "unreviewed"
)

// Search is a routine's search, as the builder writes it.
type Search struct {
	// Kind is item.KindPR or item.KindIssue, or "" for both.
	Kind item.Kind `json:"kind,omitempty"`

	// Authors are who opened the items, any of them: "@me", a login, or "app/<bot>",
	// such as "app/dependabot".
	Authors []string `json:"authors,omitempty"`

	// Where are the places the items are in, any of them: "@me" (your account),
	// "user:<login>", "org:<org>", or "repo:<owner>/<name>".
	Where []string `json:"where,omitempty"`

	// Involving are the ways the items involve you, all of them.
	Involving []Involving `json:"involving,omitempty"`

	// Labels are labels the items carry, any of them, and NotLabels labels they don't
	// carry, none of them.
	Labels    []string `json:"labels,omitempty"`
	NotLabels []string `json:"notLabels,omitempty"`

	// Base is the branch pull requests merge into, and HeadPrefix the start of the
	// branch they come from, such as "dependabot/".
	Base       string `json:"base,omitempty"`
	HeadPrefix string `json:"headPrefix,omitempty"`

	// State narrows by the items' state, all of them.
	State []State `json:"state,omitempty"`

	// More are qualifiers typed by hand, such as "language:go", checked before use.
	More string `json:"more,omitempty"`
}

// Query is a search built into GitHub's words.
type Query struct {
	// Text is the query GitHub is sent.
	Text string `json:"text"`

	// Key is what identical searches share: the query itself, written in one canonical
	// order from the normalized search.
	Key string `json:"key"`
}

var (
	// ErrNoScope is returned for a search that names no user, org, or repository, and
	// asks for nothing of the user's.
	ErrNoScope = errors.New("name a user, an org, or a repository, or search for something of yours, " +
		"so the search can never scan all of GitHub")

	// ErrTooLong is returned for a query over MaxQueryRunes.
	ErrTooLong = errors.New("GitHub takes at most 256 characters in a search; split it into two searches")

	// ErrInvalidValue is returned for a value the search can't use.
	ErrInvalidValue = errors.New("can't be used in a search")

	// ErrTooMany is returned for a list over MaxValues.
	ErrTooMany = errors.New("too many values")

	// ErrQualifier is returned for a hand-typed qualifier the search can't take.
	ErrQualifier = errors.New("isn't a qualifier a routine's search can take")

	// ErrPullRequestsOnly is returned for a pull request's qualifier in a search for
	// issues.
	ErrPullRequestsOnly = errors.New("only pull requests have it; search for pull requests, or for both")

	// ErrNoAccount is returned when a search for your account is built with no login.
	ErrNoAccount = errors.New(`sign in to GitHub first: "your account" needs the signed-in login`)
)

// FieldError is a problem with one of a search's fields, named as the spec spells it:
// "labels[1]", "where[0]", "more".
type FieldError struct {
	Field string
	Err   error
}

// Error says the field and the problem.
func (e *FieldError) Error() string {
	return e.Field + ": " + e.Err.Error()
}

// Unwrap returns the problem.
func (e *FieldError) Unwrap() error {
	return e.Err
}

// Check returns every problem with a search, nil for one Build takes. It holds the query
// to MaxQueryRunes with your account as the longest login GitHub allows, so a search it
// passes fits whichever account signs in.
func Check(s Search) []*FieldError {
	_, problems := build(s, strings.Repeat("x", repo.MaxOwnerLen))

	return problems
}

// Build returns the query for a search, with your account as login. Its error is the
// first of Check's problems, or ErrNoAccount for a search for your account with no
// login.
func Build(s Search, login string) (Query, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	if login != "" && !repo.ValidOwner(login) {
		return Query{}, &FieldError{Field: "where", Err: fmt.Errorf("%w: the login %q", ErrInvalidValue, login)}
	}

	q, problems := build(s, login)
	if len(problems) > 0 {
		return Query{}, problems[0]
	}

	return q, nil
}

// build writes the query for a search, with your account as login, and returns every
// problem it finds on the way.
func build(s Search, login string) (Query, []*FieldError) {
	b := &builder{}

	b.kind(s.Kind)
	b.add(token{q: "is", values: []string{"open"}}, token{q: "archived", values: []string{"false"}})
	b.where(s.Where, login)
	b.authors(s.Authors)
	b.involving(s.Involving, s.Kind)
	b.labels(s.Labels, s.NotLabels)
	b.branches(s.Base, s.HeadPrefix, s.Kind)
	b.states(s.State, s.Kind)
	b.more(s.More, s.Kind)
	b.add(token{q: "sort", values: []string{"updated-desc"}})

	text := b.text()

	if !b.scoped() {
		b.fail("where", ErrNoScope)
	}

	if utf8.RuneCountInString(text) > MaxQueryRunes {
		b.fail("more", fmt.Errorf("%w (this one is %d)", ErrTooLong, utf8.RuneCountInString(text)))
	}

	if len(b.problems) > 0 {
		return Query{}, b.problems
	}

	return Query{Text: text, Key: text}, nil
}

// token is one qualifier: -label:"a","b" is {neg: true, q: "label", values: [a b]}.
type token struct {
	neg    bool
	q      string
	values []string

	// quote writes every value in quotes, as a label is.
	quote bool

	// field is the search's field the values came from, as a problem names it:
	// "labels", "where[0]", "more"; "" for the search's own qualifiers.
	field string
}

// String writes the token as GitHub reads it: a value holding a space of any kind,
// Unicode's included, is quoted, so it can never split into a second qualifier. A value
// is written without its quotes and backslashes (quoteless), so none can end its quotes
// early, whoever calls String; build refuses a token holding one (check) before the
// query is written, so no query is ever sent with a value changed this way.
func (t token) String() string {
	values := make([]string, len(t.values))

	for i, v := range t.values {
		v = quoteless(v)
		if t.quote || strings.ContainsFunc(v, unicode.IsSpace) {
			v = `"` + v + `"`
		}

		values[i] = v
	}

	prefix := ""
	if t.neg {
		prefix = "-"
	}

	return prefix + t.q + ":" + strings.Join(values, ",")
}

// check returns why one of the token's values can't be written as itself, or nil. Every
// field's own checks refuse such a value before its token is added; build checks each
// token again before it writes the query, so a check a field missed still never lets one
// through.
func (t token) check() error {
	for _, v := range t.values {
		if err := checkValue(v); err != nil {
			return err
		}
	}

	return nil
}

// builder collects a query's tokens and its problems.
type builder struct {
	tokens   []token
	problems []*FieldError
}

// add appends tokens to the query.
func (b *builder) add(tokens ...token) {
	b.tokens = append(b.tokens, tokens...)
}

// fail records a problem with a field.
func (b *builder) fail(field string, err error) {
	b.problems = append(b.problems, &FieldError{Field: field, Err: err})
}

// text writes the query, recording a problem for each token holding a value no search can
// hold (token.check), so build never returns a query that carries one.
func (b *builder) text() string {
	parts := make([]string, len(b.tokens))

	for i, t := range b.tokens {
		if err := t.check(); err != nil {
			b.fail(t.field, err)
		}

		parts[i] = t.String()
	}

	return strings.Join(parts, " ")
}

// scoped reports a query that names a user, an org, or a repository, or asks for
// something of the user's, in a qualifier that isn't negated.
func (b *builder) scoped() bool {
	return slices.ContainsFunc(b.tokens, func(t token) bool {
		if t.neg {
			return false
		}

		return t.q == "user" || t.q == "org" || t.q == "repo" || slices.Contains(t.values, Me)
	})
}

// kind adds the item kind.
func (b *builder) kind(k item.Kind) {
	switch k {
	case item.KindPR:
		b.add(token{q: "is", values: []string{"pr"}})
	case item.KindIssue:
		b.add(token{q: "is", values: []string{"issue"}})
	default:
		if k != "" {
			b.fail("kind", fmt.Errorf("%w: %q is neither %q nor %q", ErrInvalidValue, k, item.KindPR, item.KindIssue))
		}
	}
}

// where adds the places, any of them, with your account as login.
func (b *builder) where(where []string, login string) {
	if !fits(b, "where", where) {
		return
	}

	for i, w := range where {
		field := fmt.Sprintf("where[%d]", i)

		t, err := whereToken(w, login)
		if err != nil {
			b.fail(field, err)

			continue
		}

		t.field = field
		b.add(t)
	}
}

// whereToken reads one place.
func whereToken(w, login string) (token, error) {
	if w == Me {
		if login == "" {
			return token{}, ErrNoAccount
		}

		return token{q: "user", values: []string{login}}, nil
	}

	q, v, _ := strings.Cut(w, ":")

	switch q {
	case "user", "org":
		if repo.ValidOwner(v) {
			return token{q: q, values: []string{v}}, nil
		}
	case "repo":
		if _, err := repo.Parse(v); err == nil {
			return token{q: q, values: []string{v}}, nil
		}
	}

	return token{}, fmt.Errorf(`%w: %q; write "@me", "user:<login>", "org:<org>", or "repo:<owner>/<name>"`,
		ErrInvalidValue, w)
}

// authors adds who opened the items, any of them.
func (b *builder) authors(authors []string) {
	if !fits(b, "authors", authors) {
		return
	}

	for i, a := range authors {
		field := fmt.Sprintf("authors[%d]", i)

		if !validAuthor(a) {
			b.fail(field, fmt.Errorf(`%w: %q; write "@me", a login, or "app/<bot>"`, ErrInvalidValue, a))

			continue
		}

		b.add(token{q: "author", values: []string{a}, field: field})
	}
}

// involving adds the ways the items involve the user, all of them.
func (b *builder) involving(involving []Involving, kind item.Kind) {
	if !fits(b, "involving", involving) {
		return
	}

	words := map[Involving]string{
		InvolvingAssigned: "assignee", InvolvingReviewRequested: "review-requested", InvolvingMentioned: "mentions",
	}

	for i, inv := range involving {
		q, ok := words[inv]

		switch {
		case !ok:
			b.fail(fmt.Sprintf("involving[%d]", i), fmt.Errorf("%w: %q", ErrInvalidValue, inv))
		case inv == InvolvingReviewRequested && kind == item.KindIssue:
			b.fail(fmt.Sprintf("involving[%d]", i), ErrPullRequestsOnly)
		default:
			b.add(token{q: q, values: []string{Me}})
		}
	}
}

// labels adds the labels the items carry, any of them, and the labels they don't, none
// of them, each as one comma list: GitHub reads label:"a","b" as any of the two, and
// -label:"a","b" as neither, while label:a label:b would ask for both.
func (b *builder) labels(labels, notLabels []string) {
	if fits(b, "labels", labels) && b.checkLabels("labels", labels) && len(labels) > 0 {
		b.add(token{q: "label", values: labels, quote: true, field: "labels"})
	}

	if fits(b, "notLabels", notLabels) && b.checkLabels("notLabels", notLabels) && len(notLabels) > 0 {
		b.add(token{neg: true, q: "label", values: notLabels, quote: true, field: "notLabels"})
	}
}

// checkLabels reports whether every label can be searched for, recording each that
// can't.
func (b *builder) checkLabels(field string, labels []string) bool {
	ok := true

	for i, l := range labels {
		if err := checkLabel(l); err != nil {
			b.fail(fmt.Sprintf("%s[%d]", field, i), err)

			ok = false
		}
	}

	return ok
}

// branches adds the base branch and the head branch's prefix. GitHub reads head: as a
// plain prefix of the branch's name ("dependabot/" for every Dependabot branch), so the
// prefix goes in exactly as written, quoted.
func (b *builder) branches(base, headPrefix string, kind item.Kind) {
	for _, br := range []struct {
		field, q, value string
		quote           bool
	}{
		{field: "base", q: "base", value: base}, {field: "headPrefix", q: "head", value: headPrefix, quote: true},
	} {
		if br.value == "" {
			continue
		}

		if kind == item.KindIssue {
			b.fail(br.field, ErrPullRequestsOnly)

			continue
		}

		if err := checkBranch(br.value); err != nil {
			b.fail(br.field, err)

			continue
		}

		b.add(token{q: br.q, values: []string{br.value}, quote: br.quote, field: br.field})
	}
}

// states adds the states the items are in, all of them.
func (b *builder) states(states []State, kind item.Kind) {
	if !fits(b, "state", states) {
		return
	}

	for i, s := range states {
		t, ok := stateTokens()[s]

		switch {
		case !ok:
			b.fail(fmt.Sprintf("state[%d]", i), fmt.Errorf("%w: %q", ErrInvalidValue, s))
		case kind == item.KindIssue:
			b.fail(fmt.Sprintf("state[%d]", i), ErrPullRequestsOnly)
		default:
			b.add(t)
		}
	}

	if family := contradiction(states); family != "" {
		b.fail("state", fmt.Errorf("%w: it asks for two %s states at once", ErrInvalidValue, family))
	}
}

// stateTokens are the qualifier each state becomes.
func stateTokens() map[State]token {
	return map[State]token{
		StateReady:            {q: "draft", values: []string{"false"}},
		StateDraft:            {q: "draft", values: []string{"true"}},
		StateFailing:          {q: "status", values: []string{"failure"}},
		StatePassing:          {q: "status", values: []string{"success"}},
		StatePending:          {q: "status", values: []string{"pending"}},
		StateChangesRequested: {q: "review", values: []string{"changes_requested"}},
		StateApproved:         {q: "review", values: []string{"approved"}},
		StateReviewRequired:   {q: "review", values: []string{"required"}},
		StateUnreviewed:       {q: "review", values: []string{"none"}},
	}
}

// contradiction returns the qualifier two of the states both set, which no item can
// meet, or "".
func contradiction(states []State) string {
	seen := map[string]bool{}
	tokens := stateTokens()

	for _, s := range states {
		t, ok := tokens[s]
		if !ok {
			continue
		}

		if seen[t.q] {
			return t.q
		}

		seen[t.q] = true
	}

	return ""
}

// fits reports whether a list is within MaxValues, recording a problem when it isn't.
func fits[T any](b *builder, field string, values []T) bool {
	if len(values) > MaxValues {
		b.fail(field, fmt.Errorf("%w: at most %d", ErrTooMany, MaxValues))

		return false
	}

	return true
}
