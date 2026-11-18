// Package heldruns decides which of a pull request's workflow runs that GitHub holds for
// approval count toward where the pull request stands, and declares the setting that names
// the workflows whose held runs never do (checks.ignoredWorkflows). The settings package
// writes the setting, and the inbox's cards and the review sheet read it and count with it.
//
// A held run that GitHub approves only on its site (one a review started in the base
// repository, say) waits on something the app can't do, and often never runs at all, so it
// doesn't count: unless GitHub reports the pull request's merge blocked, when it may be what
// blocks it, and counts again. A run of a workflow the user named never counts, whatever
// GitHub reports, and whichever kind it is: the user said so.
//
// It is a leaf: it imports only prefval, never the store, the GitHub client, or a service,
// so settings can write the setting without depending on what reads it.
package heldruns

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// KeyIgnoredWorkflows is the names of the workflows whose held runs never count, as a JSON
// list, each matched whole and case-sensitively, as GitHub names a workflow. A key never
// stored holds none.
const KeyIgnoredWorkflows = "checks.ignoredWorkflows"

// Bounds on the list. A repository's workflows number far fewer; they only keep a stray call
// from storing anything unbounded.
const (
	MaxIgnored    = 100
	MaxNameLength = 200
)

var (
	// ErrTooMany is returned for more than MaxIgnored names.
	ErrTooMany = errors.New("too many ignored workflows")

	// ErrNameTooLong is returned for a name longer than MaxNameLength characters.
	ErrNameTooLong = errors.New("an ignored workflow's name is too long")

	// ErrInvalidName is returned for a name holding a control character, such as a line
	// break, which no workflow's name holds.
	ErrInvalidName = errors.New("an ignored workflow's name holds a control character")
)

// mergeBlocked is GitHub's mergeStateStatus of a pull request whose merge is blocked, as by
// a required check that hasn't run.
const mergeBlocked = "BLOCKED"

// Run is one workflow run GitHub holds for approval: its workflow's name ("" when GitHub
// gave none), and whether GitHub takes its approval only on its site.
type Run struct {
	Workflow string `json:"workflow"`
	OnGitHub bool   `json:"onGitHub"`
}

// Clean trims each name and leaves out the blank ones and those already in the list, keeping
// the first, in order. It refuses more than MaxIgnored names, one longer than MaxNameLength
// characters, and one that holds a control character. The list is never nil.
func Clean(names []string) ([]string, error) {
	out := make([]string, 0, len(names))

	for _, raw := range names {
		name := strings.TrimSpace(raw)

		if name == "" || slices.Contains(out, name) {
			continue
		}

		if utf8.RuneCountInString(name) > MaxNameLength {
			return nil, fmt.Errorf("%w: keep each to %d characters", ErrNameTooLong, MaxNameLength)
		}

		if strings.ContainsFunc(name, unicode.IsControl) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidName, name)
		}

		out = append(out, name)
	}

	if len(out) > MaxIgnored {
		return nil, fmt.Errorf("%w: %d; keep it to %d", ErrTooMany, len(out), MaxIgnored)
	}

	return out, nil
}

// Decode reads the stored list: none when nothing was ever stored or what is stored can't be
// read, and the stored list, cleaned, otherwise. The list is never nil.
func Decode(stored string) []string {
	list, ok := prefval.List[string](stored)
	if !ok {
		return []string{}
	}

	clean, err := Clean(list)
	if err != nil {
		return []string{}
	}

	return clean
}

// Encode writes a list as Decode reads it, cleaned, or refuses it as Clean does.
func Encode(names []string) (string, error) {
	clean, err := Clean(names)
	if err != nil {
		return "", err
	}

	return prefval.EncodeList(clean), nil
}

// Ignored reports whether run doesn't count toward where its pull request stands, whose
// merge state GitHub reports as mergeState, with ignored the workflows the user named: a
// run of a named workflow never counts, and one GitHub approves only on its site counts
// only while GitHub reports the merge blocked.
func Ignored(run Run, mergeState string, ignored []string) bool {
	if run.Workflow != "" && slices.Contains(ignored, run.Workflow) {
		return true
	}

	return run.OnGitHub && mergeState != mergeBlocked
}

// Count counts the runs that count toward where their pull request stands (see Ignored):
// those the user can approve from the app, and those GitHub approves only on its site.
func Count(runs []Run, mergeState string, ignored []string) (approvable, onGitHub int) {
	for _, r := range runs {
		switch {
		case Ignored(r, mergeState, ignored):
		case r.OnGitHub:
			onGitHub++
		default:
			approvable++
		}
	}

	return approvable, onGitHub
}

// Unnamed returns runs with a run of no name added for each held run counted but not
// listed, as approvable approvable ones and onGitHub ones held on GitHub: what details
// stored before the names were hold. A run of no name matches no ignored workflow, so it
// counts as Ignored says of its kind.
func Unnamed(runs []Run, approvable, onGitHub int) []Run {
	out := slices.Clone(runs)

	for _, kind := range []struct {
		onGitHub bool
		n        int
	}{{false, approvable}, {true, onGitHub}} {
		listed := 0

		for _, r := range runs {
			if r.OnGitHub == kind.onGitHub {
				listed++
			}
		}

		for range kind.n - listed {
			out = append(out, Run{OnGitHub: kind.onGitHub})
		}
	}

	if out == nil {
		out = []Run{}
	}

	return out
}
