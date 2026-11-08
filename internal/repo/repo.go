package repo

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Limits GitHub puts on names.
const (
	// MaxOwnerLen is the longest user or organization login GitHub allows.
	MaxOwnerLen = 39

	// MaxRepoLen is the longest repository name GitHub allows.
	MaxRepoLen = 100
)

// ErrInvalid is returned for a name that cannot be a GitHub repository.
var ErrInvalid = errors.New("not a valid GitHub repository name")

// Name is a GitHub repository: its owner (a user or organization) and its name, in the
// casing they were written with.
type Name struct {
	Owner string
	Repo  string
}

// New returns owner/repo as a Name, or ErrInvalid when either part breaks GitHub's rules
// (see ValidOwner and ValidRepo).
func New(owner, repo string) (Name, error) {
	n := Name{Owner: owner, Repo: repo}
	if !n.Valid() {
		return Name{}, fmt.Errorf("%w: %q", ErrInvalid, n.String())
	}

	return n, nil
}

// Parse reads "owner/repo" into a Name, or returns ErrInvalid.
func Parse(s string) (Name, error) {
	owner, name, found := strings.Cut(s, "/")
	if !found {
		return Name{}, fmt.Errorf("%w: %q", ErrInvalid, s)
	}

	return New(owner, name)
}

// Valid reports whether both the owner and the repository name follow GitHub's rules.
func (n Name) Valid() bool {
	return ValidOwner(n.Owner) && ValidRepo(n.Repo)
}

// String writes the name as "owner/repo", in its own casing.
func (n Name) String() string {
	return n.Owner + "/" + n.Repo
}

// Key is the name folded to lower case, "owner/repo", for comparing names and keying
// maps: GitHub treats Acme-Org/Example-Repo and acme-org/example-repo as one repository.
// An owner alone (Repo "") keys as "owner/".
func (n Name) Key() string {
	return strings.ToLower(n.String())
}

// Ref writes a pull request or issue of the repository as "owner/repo#number".
func (n Name) Ref(number int) string {
	return n.String() + "#" + strconv.Itoa(number)
}

// ValidOwner reports whether s can be a GitHub user or organization login: 1 to 39
// ASCII letters, digits, hyphens, and underscores, starting with a letter or digit.
// That covers every login GitHub has issued, including managed users' (name_shortcode)
// and older logins with a trailing or doubled hyphen, and never a ".", a path, or a
// leading "-" that could read as a command-line flag.
func ValidOwner(s string) bool {
	if s == "" || len(s) > MaxOwnerLen || !isAlnum(s[0]) {
		return false
	}

	for i := range len(s) {
		if c := s[i]; !isAlnum(c) && c != '-' && c != '_' {
			return false
		}
	}

	return true
}

// ValidRepo reports whether s can be a GitHub repository name: 1 to 100 ASCII letters,
// digits, '.', '_', and '-', but never "." or "..", so it is always one path element,
// and never starting with '-', so it can never be read as a command-line flag.
func ValidRepo(s string) bool {
	if s == "" || len(s) > MaxRepoLen || s == "." || s == ".." || s[0] == '-' {
		return false
	}

	for i := range len(s) {
		if !isRepoByte(s[i]) {
			return false
		}
	}

	return true
}

// isRepoByte reports whether c may appear in a repository name.
func isRepoByte(c byte) bool {
	return isAlnum(c) || c == '.' || c == '_' || c == '-'
}

// isAlnum reports whether c is an ASCII letter or digit.
func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
