// Package reporules holds the user's rules for whole repositories and orgs: a pinned one's
// pull requests and issues count as pinned in the inbox, and a muted one's are set apart
// in each list's Muted tab, counted nowhere, told in no banner, and followed by nothing.
// The Release page lists a muted repository in its folded Muted group, asking GitHub
// nothing about it.
//
// A rule's target is an org (or user), written "owner", or one repository, written
// "owner/name". A repository's own rule wins over its org's, so an org can be muted while
// one of its repositories is pinned. A target is in one list at most.
//
// Settings writes the rules, under the keys declared here, and the inbox and the Release
// page read them. It is a leaf: it imports only the repository names and the preference
// encoding, never the store or a service.
package reporules

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
	"github.com/mrz1836/nilbox/internal/repo"
)

// Rule is what applies to a repository or org.
type Rule string

// The rules.
const (
	// RuleNone is a repository or org without a rule of its own.
	RuleNone Rule = ""

	// RulePinned counts every item of the repository or org as pinned.
	RulePinned Rule = "pinned"

	// RuleMuted sets every item of the repository or org apart in each list's Muted tab.
	RuleMuted Rule = "muted"
)

// The preferences the rules are stored under, each a JSON list of targets.
const (
	// PrefPinned lists the pinned repositories and orgs.
	PrefPinned = "repos.pinned"

	// PrefMuted lists the muted repositories and orgs.
	PrefMuted = "repos.muted"
)

// MaxTargets caps each list.
const MaxTargets = 500

var (
	// ErrInvalidTarget is returned for a target that is neither an org nor a repository
	// GitHub could have.
	ErrInvalidTarget = errors.New("not a GitHub org or repository")

	// ErrInvalidRule is returned for a rule this build does not know.
	ErrInvalidRule = errors.New("unknown repository rule")

	// ErrTooManyTargets is returned when a list would hold more than MaxTargets.
	ErrTooManyTargets = errors.New("too many repositories and orgs")
)

// Valid reports whether r is one of the rules.
func (r Rule) Valid() bool {
	return r == RuleNone || r == RulePinned || r == RuleMuted
}

// Rules are the pinned and the muted repositories and orgs, each list sorted ignoring
// case and never nil. No target is in both.
type Rules struct {
	Pinned []string `json:"pinned"`
	Muted  []string `json:"muted"`
}

// Keys are every preference the rules are stored under.
func Keys() []string {
	return []string{PrefPinned, PrefMuted}
}

// ParseTarget reads a target as the user wrote it: "owner" for an org or a user, or
// "owner/name" for a repository, trimmed, with a trailing slash dropped. It keeps the
// casing written, and returns the target as Name, Repo "" for an org.
func ParseTarget(s string) (repo.Name, error) {
	t := strings.TrimSuffix(strings.TrimSpace(s), "/")

	if owner, name, found := strings.Cut(t, "/"); found {
		n, err := repo.New(owner, name)
		if err != nil {
			return repo.Name{}, fmt.Errorf("%w: %q", ErrInvalidTarget, strings.TrimSpace(s))
		}

		return n, nil
	}

	if !repo.ValidOwner(t) {
		return repo.Name{}, fmt.Errorf("%w: %q", ErrInvalidTarget, strings.TrimSpace(s))
	}

	return repo.Name{Owner: t}, nil
}

// targetText writes a target as it is stored: "owner" or "owner/name".
func targetText(n repo.Name) string {
	if n.Repo == "" {
		return n.Owner
	}

	return n.String()
}

// Set returns the rules with rule applied to target: the target taken out of both lists,
// then, unless rule is RuleNone, added to the rule's own, in the casing given. It refuses
// a target that is neither an org nor a repository, a rule it does not know, and a list
// that would grow past MaxTargets.
func (r Rules) Set(target string, rule Rule) (Rules, error) {
	if !rule.Valid() {
		return Rules{}, fmt.Errorf("%w %q", ErrInvalidRule, rule)
	}

	n, err := ParseTarget(target)
	if err != nil {
		return Rules{}, err
	}

	key := n.Key()
	out := Rules{Pinned: without(r.Pinned, key), Muted: without(r.Muted, key)}

	var list *[]string

	switch rule {
	case RulePinned:
		list = &out.Pinned
	case RuleMuted:
		list = &out.Muted
	case RuleNone:
		return out, nil
	}

	if len(*list) >= MaxTargets {
		return Rules{}, fmt.Errorf("%w: keep each list to %d", ErrTooManyTargets, MaxTargets)
	}

	*list = sorted(append(*list, targetText(n)))

	return out, nil
}

// For returns the rule that applies to the repository name, and the target it comes from
// ("owner/name" or "owner", as stored): the repository's own rule, else its org's, else
// RuleNone and "".
func (r Rules) For(name repo.Name) (Rule, string) {
	if name.Repo != "" {
		if t, ok := find(r.Pinned, name.Key()); ok {
			return RulePinned, t
		}

		if t, ok := find(r.Muted, name.Key()); ok {
			return RuleMuted, t
		}
	}

	org := repo.Name{Owner: name.Owner}.Key()

	if t, ok := find(r.Pinned, org); ok {
		return RulePinned, t
	}

	if t, ok := find(r.Muted, org); ok {
		return RuleMuted, t
	}

	return RuleNone, ""
}

// IsMuted reports whether the repository name is muted, by its own rule or its org's.
func (r Rules) IsMuted(name repo.Name) bool {
	rule, _ := r.For(name)

	return rule == RuleMuted
}

// IsPinned reports whether the repository name is pinned, by its own rule or its org's.
func (r Rules) IsPinned(name repo.Name) bool {
	rule, _ := r.For(name)

	return rule == RulePinned
}

// Equal reports whether r and o hold the same targets.
func (r Rules) Equal(o Rules) bool {
	return slices.Equal(r.Pinned, o.Pinned) && slices.Equal(r.Muted, o.Muted)
}

// Encode returns the rules as preferences.
func (r Rules) Encode() map[string]string {
	return map[string]string{
		PrefPinned: prefval.EncodeList(sorted(r.Pinned)),
		PrefMuted:  prefval.EncodeList(sorted(r.Muted)),
	}
}

// Decode reads the rules from stored preferences. A list never stored, or one that cannot
// be read, is empty; a target that is no longer valid, a repeat, and one already in the
// pinned list are dropped from a list read back, so the rules always hold to Set's.
func Decode(stored map[string]string) Rules {
	pinned := decodeList(stored[PrefPinned], nil)
	muted := decodeList(stored[PrefMuted], pinned)

	return Rules{Pinned: pinned, Muted: muted}
}

// decodeList reads one stored list, leaving out a target that can't be read, a repeat,
// one in taken, and those past MaxTargets.
func decodeList(raw string, taken []string) []string {
	out := []string{}

	targets, ok := prefval.List[string](raw)
	if !ok {
		return out
	}

	seen := map[string]bool{}
	for _, t := range taken {
		seen[keyOf(t)] = true
	}

	for _, t := range targets {
		n, err := ParseTarget(t)
		if err != nil || seen[n.Key()] {
			continue
		}

		seen[n.Key()] = true
		out = append(out, targetText(n))

		if len(out) == MaxTargets {
			break
		}
	}

	return sorted(out)
}

// keyOf is a stored target's key, as repo.Name.Key gives it: "owner/" or "owner/name".
func keyOf(target string) string {
	owner, name, _ := strings.Cut(target, "/")

	return repo.Name{Owner: owner, Repo: name}.Key()
}

// find returns the target in list whose key is key, as stored.
func find(list []string, key string) (string, bool) {
	for _, t := range list {
		if keyOf(t) == key {
			return t, true
		}
	}

	return "", false
}

// without returns a copy of list without the target whose key is key. It is never nil.
func without(list []string, key string) []string {
	out := make([]string, 0, len(list))

	for _, t := range list {
		if keyOf(t) != key {
			out = append(out, t)
		}
	}

	return out
}

// sorted returns list sorted ignoring case, an org before its repositories, never nil.
func sorted(list []string) []string {
	out := slices.Clone(list)
	if out == nil {
		out = []string{}
	}

	slices.SortFunc(out, func(a, b string) int {
		return strings.Compare(keyOf(a), keyOf(b))
	})

	return out
}
