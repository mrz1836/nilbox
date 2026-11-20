package kit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// What a snapshot's file may be.
const (
	// MaxSnapshotBytes caps a snapshot as it is stored: twice the largest export of every
	// routine the app reads (8 MiB), room for the routines' sentences beside their specs,
	// the names, and the vocabulary.
	MaxSnapshotBytes = 16 << 20

	// specDepth is how deep a spec's JSON may nest, as spec.Decode holds it.
	specDepth = 16

	// snapshotDepth fits a spec at its deepest under the four levels a snapshot holds it
	// in: the snapshot, its setup or its vocabulary, a list, and a routine or a template.
	snapshotDepth = 4 + specDepth
)

var (
	// ErrUnreadableSnapshot is returned for a snapshot that isn't one: not JSON, a key the
	// format doesn't have or doesn't spell exactly, a key given twice, a value of the wrong
	// type, anything after it, or more than MaxSnapshotBytes.
	ErrUnreadableSnapshot = errors.New("the snapshot can't be read")

	// ErrSnapshotVersion is returned for a snapshot of another version than this build's,
	// which it doesn't guess at: the app writes it again as it starts.
	ErrSnapshotVersion = errors.New("the snapshot is of another version")
)

// NewSnapshot returns the snapshot of setup, inbox, and vocab, written at now: of this
// build's version, every list there, none nil, and every name in it kept as Name keeps
// it, the inbox's lists as Names keeps them. What the source ordered keeps its order, and
// nothing given is changed: each list that holds a name is a copy. The rules are kept as
// given, each already held to its own package's rules (trust.NormalizeBots,
// reporules.ParseTarget, heldruns.Clean), since a card's change is applied over them.
func NewSnapshot(setup Setup, inbox InboxNames, vocab Vocabulary, now time.Time) Snapshot {
	s := snapshotLists(Snapshot{
		Version: SnapshotVersion, WrittenAt: now.UTC(), Setup: setup, Inbox: inbox, Vocabulary: vocab,
	})

	s.Setup.Routines = slices.Clone(s.Setup.Routines)
	for i := range s.Setup.Routines {
		s.Setup.Routines[i].Name = Name(s.Setup.Routines[i].Name)
	}

	s.Setup.Prompts = slices.Clone(s.Setup.Prompts)
	for i, p := range s.Setup.Prompts {
		s.Setup.Prompts[i].Name, s.Setup.Prompts[i].Nickname = Name(p.Name), Name(p.Nickname)
	}

	s.Setup.Scripts = slices.Clone(s.Setup.Scripts)
	for i := range s.Setup.Scripts {
		s.Setup.Scripts[i].Name = Name(s.Setup.Scripts[i].Name)
	}

	s.Setup.Profiles = slices.Clone(s.Setup.Profiles)
	for i := range s.Setup.Profiles {
		s.Setup.Profiles[i].Name = Name(s.Setup.Profiles[i].Name)
	}

	s.Inbox = InboxNames{
		Repositories: Names(inbox.Repositories), Owners: Names(inbox.Owners), People: Names(inbox.People),
		Bots: Names(inbox.Bots), Labels: Names(inbox.Labels), Checks: Names(inbox.Checks),
	}

	return s
}

// EncodeSnapshot writes a snapshot as the app stores it, compact JSON, refusing one past
// MaxSnapshotBytes, which the tool mode would refuse to read.
func EncodeSnapshot(s Snapshot) ([]byte, error) {
	data, err := storedJSON(s)
	if err != nil {
		return nil, fmt.Errorf("encode the snapshot: %w", err)
	}

	if len(data) > MaxSnapshotBytes {
		return nil, fmt.Errorf("encode the snapshot: %w %d bytes, at %d", errLarger, MaxSnapshotBytes, len(data))
	}

	return data, nil
}

// DecodeSnapshot reads a stored snapshot: its version first, so another version's says so
// (ErrSnapshotVersion) rather than that its keys are unknown, then strictly, as
// spec.DecodeExact reads one (ErrUnreadableSnapshot), with every list there, none nil.
func DecodeSnapshot(data []byte) (Snapshot, error) {
	err := checkVersion(data, MaxSnapshotBytes, SnapshotVersion)

	switch {
	case errors.Is(err, errOtherVersion):
		return Snapshot{}, fmt.Errorf("%w: %w; the app writes it again as it starts", ErrSnapshotVersion, err)
	case err != nil:
		return Snapshot{}, fmt.Errorf("%w: %w", ErrUnreadableSnapshot, err)
	}

	s, err := spec.DecodeExact[Snapshot](data, MaxSnapshotBytes, snapshotDepth)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrUnreadableSnapshot, err)
	}

	return snapshotLists(s), nil
}

// snapshotLists returns s with every list it holds there, none nil.
func snapshotLists(s Snapshot) Snapshot {
	s.Setup.Routines = textutil.NonNil(s.Setup.Routines)
	s.Setup.Prompts = textutil.NonNil(s.Setup.Prompts)
	s.Setup.Scripts = textutil.NonNil(s.Setup.Scripts)
	s.Setup.Profiles = textutil.NonNil(s.Setup.Profiles)
	s.Setup.Rules = rulesLists(s.Setup.Rules)

	s.Inbox = InboxNames{
		Repositories: textutil.NonNil(s.Inbox.Repositories), Owners: textutil.NonNil(s.Inbox.Owners),
		People: textutil.NonNil(s.Inbox.People), Bots: textutil.NonNil(s.Inbox.Bots),
		Labels: textutil.NonNil(s.Inbox.Labels), Checks: textutil.NonNil(s.Inbox.Checks),
	}

	s.Vocabulary.Templates = slices.Clone(textutil.NonNil(s.Vocabulary.Templates))
	for i := range s.Vocabulary.Templates {
		s.Vocabulary.Templates[i].Blanks = textutil.NonNil(s.Vocabulary.Templates[i].Blanks)
	}

	s.Vocabulary.Starters = textutil.NonNil(s.Vocabulary.Starters)
	s.Vocabulary.Variables = textutil.NonNil(s.Vocabulary.Variables)

	return s
}

// rulesLists returns r with each of its lists there, none nil.
func rulesLists(r Rules) Rules {
	return Rules{
		TrustedBots: textutil.NonNil(r.TrustedBots), Pinned: textutil.NonNil(r.Pinned),
		Muted: textutil.NonNil(r.Muted), IgnoredWorkflows: textutil.NonNil(r.IgnoredWorkflows),
	}
}

// storedJSON writes v as kit stores a document: compact JSON, with its text's <, >, and &
// as they are, ended by a newline.
func storedJSON(v any) ([]byte, error) {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(v); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
