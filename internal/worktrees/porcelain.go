package worktrees

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// ErrMalformedPorcelain is returned when `git worktree list --porcelain -z` output does
// not have the documented shape.
var ErrMalformedPorcelain = errors.New("malformed git worktree list output")

// headsPrefix is the ref prefix of local branches.
const headsPrefix = "refs/heads/"

// Entry is one worktree as `git worktree list --porcelain -z` describes it.
type Entry struct {
	// Path is the worktree's directory as git reports it.
	Path string

	// HEAD is the commit the worktree has checked out; empty for a bare repository.
	HEAD string

	// Branch is the checked-out branch without its refs/heads/ prefix; empty when the
	// worktree is detached or bare.
	Branch string

	// Detached reports a detached HEAD.
	Detached bool

	// Bare reports the bare repository itself rather than a working tree.
	Bare bool

	// Locked reports a worktree protected by `git worktree lock`.
	Locked bool

	// LockReason is the reason given to `git worktree lock`, if any.
	LockReason string

	// Prunable reports a worktree git can prune, usually because its directory is gone.
	Prunable bool

	// PrunableReason is git's explanation for Prunable.
	PrunableReason string

	// Main reports the main worktree (or bare repository), which git always lists first.
	Main bool
}

// ParsePorcelain parses `git worktree list --porcelain -z` output. Every attribute ends
// with a NUL and an empty attribute ends an entry. Attributes it does not know are
// ignored, so newer git versions keep working. The first entry is the main worktree.
func ParsePorcelain(data []byte) ([]Entry, error) {
	if len(data) == 0 {
		return []Entry{}, nil
	}

	if data[len(data)-1] != 0 {
		return nil, fmt.Errorf("%w: output does not end with a NUL", ErrMalformedPorcelain)
	}

	var p porcelainParser

	for attr := range bytes.SplitSeq(data[:len(data)-1], []byte{0}) {
		if err := p.attribute(string(attr)); err != nil {
			return nil, err
		}
	}

	p.flush()

	return p.entries, nil
}

// porcelainParser accumulates entries while ParsePorcelain walks the attributes.
type porcelainParser struct {
	entries []Entry
	current *Entry
}

// attribute handles one NUL-terminated attribute; an empty one ends the current entry.
func (p *porcelainParser) attribute(attr string) error {
	if attr == "" {
		p.flush()

		return nil
	}

	key, value, _ := strings.Cut(attr, " ")
	if key == worktreeCmd {
		return p.start(value)
	}

	if p.current == nil {
		return fmt.Errorf("%w: attribute %q before any worktree", ErrMalformedPorcelain, attr)
	}

	applyAttribute(p.current, key, value)

	return nil
}

// start begins a new entry; the previous one must have been ended.
func (p *porcelainParser) start(path string) error {
	if p.current != nil || path == "" {
		return fmt.Errorf("%w: unexpected worktree attribute for %q", ErrMalformedPorcelain, path)
	}

	p.current = &Entry{Path: path, Main: len(p.entries) == 0}

	return nil
}

// flush appends the entry being parsed, if there is one.
func (p *porcelainParser) flush() {
	if p.current != nil {
		p.entries = append(p.entries, *p.current)
		p.current = nil
	}
}

// applyAttribute records one attribute line on an entry.
func applyAttribute(e *Entry, key, value string) {
	switch key {
	case "HEAD":
		e.HEAD = value
	case "branch":
		e.Branch = strings.TrimPrefix(value, headsPrefix)
	case "detached":
		e.Detached = true
	case "bare":
		e.Bare = true
	case "locked":
		e.Locked = true
		e.LockReason = value
	case "prunable":
		e.Prunable = true
		e.PrunableReason = value
	}
}
