package item

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/repo"
)

// Kind is what an item is: a pull request or an issue. It is stored as "pr" or "issue".
type Kind string

// The kinds of item.
const (
	KindPR    Kind = "pr"
	KindIssue Kind = "issue"
)

// maxNumberDigits caps the digits of a number in a worktree name, so the number always
// fits in an int.
const maxNumberDigits = 10

var (
	// ErrNotFound is returned for an item that is not in the local inbox.
	ErrNotFound = errors.New("item not found; refresh the inbox and try again")

	// ErrInvalidKind is returned for a kind other than "pr" or "issue".
	ErrInvalidKind = errors.New(`kind must be "pr" or "issue"`)
)

// Kinds returns every kind, pull requests first.
func Kinds() []Kind {
	return []Kind{KindPR, KindIssue}
}

// ParseKind reads a stored kind, or returns ErrInvalidKind.
func ParseKind(s string) (Kind, error) {
	if k := Kind(s); k.Valid() {
		return k, nil
	}

	return "", fmt.Errorf("%w: %q", ErrInvalidKind, s)
}

// Valid reports whether k is KindPR or KindIssue.
func (k Kind) Valid() bool {
	return k == KindPR || k == KindIssue
}

// Noun names the kind in a sentence: "pull request" or "issue".
func (k Kind) Noun() string {
	if k == KindPR {
		return "pull request"
	}

	return "issue"
}

// Plural names the kind's items in a sentence: "pull requests" or "issues".
func (k Kind) Plural() string {
	return k.Noun() + "s"
}

// Ref names one pull request or issue. GitHub's numbers are shared by a repository's
// pull requests and issues, so the repository and number alone pick the item; Kind
// says which it is when the caller knows.
type Ref struct {
	Repo   repo.Name
	Number int
	Kind   Kind
}

// String writes the item as "owner/repo#number".
func (r Ref) String() string {
	return r.Repo.Ref(r.Number)
}

// Key is the item's case-folded "owner/repo#number", for comparing items and keying
// maps; the kind is left out, since the number alone picks the item.
func (r Ref) Key() string {
	return r.Repo.Key() + "#" + strconv.Itoa(r.Number)
}

// WorktreeName is the name of the worktree a session on an item runs in: "pr-<n>" for a
// pull request and "issue-<n>" for an issue. The same item always gets the same name, so
// launching it again finds its worktree. The coding agent gives a worktree started at a
// pull request's head the same "pr-<n>" name.
func WorktreeName(kind Kind, number int) string {
	return string(kind) + "-" + strconv.Itoa(number)
}

// ParseWorktreeName reads a name WorktreeName gives: the kind and a positive number of at
// most ten digits without a leading zero. ok is false for any other name.
func ParseWorktreeName(name string) (kind Kind, number int, ok bool) {
	prefix, digits, found := strings.Cut(name, "-")
	if !found || !Kind(prefix).Valid() || !validNumber(digits) {
		return "", 0, false
	}

	n, err := strconv.Atoi(digits)
	if err != nil {
		return "", 0, false
	}

	return Kind(prefix), n, true
}

// validNumber reports whether s is a positive decimal of at most maxNumberDigits digits
// without a leading zero.
func validNumber(s string) bool {
	if s == "" || len(s) > maxNumberDigits || s[0] == '0' {
		return false
	}

	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return true
}
