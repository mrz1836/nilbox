// Package branchrules holds the user's rules for deleting local branches: which branches
// go with a worktree the app removes or cleans up, whether a branch whose pull request
// merged starts checked in the cleanup of a clone's merged branches, and which branches
// are never deleted.
//
// The rules are only defaults: every deletion the user starts is confirmed in a list of
// checkboxes that start from them. Settings writes them, under the keys declared here, and
// the worktrees package follows them. Whatever the rules say, a branch is deleted only when
// every commit on it is on GitHub, so nothing is ever lost.
package branchrules

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// The preferences the rules are stored under.
const (
	// PrefOwn is whether a worktree's own worktree-<name> branch goes with it.
	PrefOwn = "branches.own"

	// PrefPullRequest is whether a pull request's own branch, checked out in its worktree,
	// goes with the worktree.
	PrefPullRequest = "branches.pullRequest"

	// PrefMerged is whether a branch whose pull request merged starts checked in the
	// cleanup of a clone's merged branches.
	PrefMerged = "branches.merged"

	// PrefProtected is the patterns of the branches never deleted, a JSON list.
	PrefProtected = "branches.protected"
)

const (
	// maxPatterns caps the protected list.
	maxPatterns = 100

	// maxPatternLen caps one pattern, beyond any branch name a person writes.
	maxPatternLen = 200
)

var (
	// ErrInvalidPattern is returned for a protected pattern that cannot match a branch.
	ErrInvalidPattern = errors.New("not a branch pattern")

	// ErrTooManyPatterns is returned for a protected list over 100 patterns.
	ErrTooManyPatterns = errors.New("too many protected branch patterns")
)

// Rules are the user's defaults for deleting local branches.
type Rules struct {
	// Own deletes a worktree's own worktree-<name> branch with it.
	Own bool `json:"own"`

	// PullRequest deletes a pull request's own branch, checked out in its worktree (as
	// `gh pr checkout` does), with the worktree.
	PullRequest bool `json:"pullRequest"`

	// Merged starts each branch whose pull request merged checked in the cleanup of a
	// clone's merged branches.
	Merged bool `json:"merged"`

	// Protected are the patterns of the branches never deleted, such as "main" or
	// "release/*" (see Matches).
	Protected []string `json:"protected"`
}

// Defaults are the rules before the user changes any: every kind of branch goes, and main
// and master are protected.
func Defaults() Rules {
	return Rules{Own: true, PullRequest: true, Merged: true, Protected: DefaultProtected()}
}

// DefaultProtected is the protected list before the user changes it.
func DefaultProtected() []string {
	return []string{"main", "master"}
}

// Normalize returns patterns trimmed, without empty ones or repeats, in order. It refuses
// a pattern that cannot match a branch name, and a list over 100 patterns.
func Normalize(patterns []string) ([]string, error) {
	out := make([]string, 0, len(patterns))

	for _, raw := range patterns {
		p := strings.TrimSpace(raw)
		if p == "" || slices.Contains(out, p) {
			continue
		}

		if err := checkPattern(p); err != nil {
			return nil, err
		}

		out = append(out, p)
	}

	if len(out) > maxPatterns {
		return nil, fmt.Errorf("%w: %d; keep it to %d", ErrTooManyPatterns, len(out), maxPatterns)
	}

	return out, nil
}

// checkPattern refuses a pattern no branch could match: one too long, holding a space or
// a control character (which git refuses in a branch name), or with a malformed bracket.
func checkPattern(p string) error {
	switch {
	case len(p) > maxPatternLen:
		return fmt.Errorf("%w: longer than %d characters", ErrInvalidPattern, maxPatternLen)
	case strings.ContainsFunc(p, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }):
		return fmt.Errorf("%w: %q holds a space", ErrInvalidPattern, p)
	}

	if _, err := path.Match(p, ""); err != nil {
		return fmt.Errorf("%w: %q: %w", ErrInvalidPattern, p, err)
	}

	return nil
}

// Matches reports a branch one of patterns protects. A pattern is matched against the
// whole branch name as a path: * matches within one part ("release/*" protects
// release/1.2 but not release/1.2/fix), ? matches one character, and [a-z] one of a set.
// A pattern without a wildcard protects that one branch.
func Matches(branch string, patterns []string) bool {
	for _, p := range patterns {
		if ok, err := path.Match(p, branch); err == nil && ok {
			return true
		}
	}

	return false
}

// Validate refuses rules whose protected list Normalize would refuse.
func (r Rules) Validate() error {
	_, err := Normalize(r.Protected)

	return err
}

// Encode returns the rules as preferences, the protected list normalized. It fails as
// Normalize does.
func (r Rules) Encode() (map[string]string, error) {
	protected, err := Normalize(r.Protected)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		PrefOwn:         prefval.EncodeBool(r.Own),
		PrefPullRequest: prefval.EncodeBool(r.PullRequest),
		PrefMerged:      prefval.EncodeBool(r.Merged),
		PrefProtected:   prefval.EncodeList(protected),
	}, nil
}

// Decode reads the rules from stored preferences. A rule never stored, or one that cannot
// be read, keeps its default; a protected list stored empty stays empty.
func Decode(stored map[string]string) Rules {
	r := Defaults()

	for key, rule := range map[string]*bool{PrefOwn: &r.Own, PrefPullRequest: &r.PullRequest, PrefMerged: &r.Merged} {
		*rule = prefval.Bool(stored[key], *rule)
	}

	raw, ok := stored[PrefProtected]
	if !ok {
		return r
	}

	protected, ok := prefval.List[string](raw)
	if !ok {
		return r
	}

	if normalized, err := Normalize(protected); err == nil {
		r.Protected = normalized
	}

	return r
}

// Keys are every preference the rules are stored under.
func Keys() []string {
	return []string{PrefOwn, PrefPullRequest, PrefMerged, PrefProtected}
}
