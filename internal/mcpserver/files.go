package mcpserver

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/paths"
)

// The describe folder's layout, as the app keeps it (internal/describeit names the same):
// a change here is a change of protocol.
const (
	// contextName is the snapshot the app writes and the tool mode reads (kit.Snapshot).
	contextName = "context.json"

	// proposalsName holds the proposals: each in-app conversation's folder, by its ID, and
	// the outside folder (outsideName), each proposal a file named for its ID
	// (proposalExt), and the folder's outcomes beside them (outcomesName).
	proposalsName = "proposals"
	outsideName   = "outside"
	outcomesName  = "outcomes.json"
	proposalExt   = ".json"
)

// Why a proposal isn't written.
var (
	// errNoFolder is a proposals folder the app didn't make: not there, or not a folder.
	errNoFolder = errors.New("the folder for proposals isn't there")

	// errFull is the outside folder with kit.MaxOutsidePending proposals waiting already.
	errFull = errors.New("too many proposals wait for the user")

	// errTaken is a proposal's file name another file has: never written over.
	errTaken = errors.New("a file has the proposal's name already")
)

// snapshot reads the snapshot the app wrote, as kit reads one: within
// kit.MaxSnapshotBytes, strictly, and of this build's version.
func (s *server) snapshot() (kit.Snapshot, error) {
	data, err := paths.ReadHead(filepath.Join(s.call.dir, contextName), kit.MaxSnapshotBytes+1)
	if err != nil {
		return kit.Snapshot{}, err
	}

	return kit.DecodeSnapshot(data)
}

// proposalsDir returns the folder this session's proposals go into, refusing one the app
// didn't make: the proposals folder, and in it the conversation's own in the app, or the
// outside folder, each a folder and never a link.
func (s *server) proposalsDir() (string, error) {
	source := outsideName
	if s.where == kit.InApp {
		source = s.call.conversation
	}

	parent := filepath.Join(s.call.dir, proposalsName)
	dir := filepath.Join(parent, source)

	for _, d := range []string{parent, dir} {
		info, err := os.Lstat(d)

		switch {
		case errors.Is(err, fs.ErrNotExist):
			return "", errNoFolder
		case err != nil:
			return "", err
		case !info.IsDir():
			return "", errNoFolder
		}
	}

	return dir, nil
}

// proposalIDs returns the IDs of the proposal files in dir, the oldest first: each a
// regular file, never a link, named for an ID the tool mode makes, as the app reads them.
func proposalIDs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(entries))

	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), proposalExt)
		if ok && kit.ValidProposalID(id) && e.Type().IsRegular() {
			ids = append(ids, id)
		}
	}

	slices.Sort(ids)

	return ids, nil
}

// readProposal reads the proposal file id in dir as the app reads one: within
// kit.MaxProposalFileBytes, strictly, and holding its own ID.
func readProposal(dir, id string) (kit.ProposalFile, error) {
	data, err := paths.ReadHead(filepath.Join(dir, id+proposalExt), kit.MaxProposalFileBytes+1)
	if err != nil {
		return kit.ProposalFile{}, err
	}

	f, err := kit.DecodeProposalFile(data)
	if err != nil {
		return kit.ProposalFile{}, err
	}

	if f.ID != id {
		return kit.ProposalFile{}, fmt.Errorf("%w: it holds another proposal's ID", kit.ErrUnreadableProposalFile)
	}

	return f, nil
}

// readOutcomes reads what the user did with the parts of the proposals in dir, as the app
// recorded it: none when it recorded nothing yet.
func readOutcomes(dir string) ([]kit.Outcome, error) {
	data, err := paths.ReadHead(filepath.Join(dir, outcomesName), kit.MaxOutcomesBytes+1)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return []kit.Outcome{}, nil
	case err != nil:
		return nil, err
	}

	o, err := kit.DecodeOutcomes(data)
	if err != nil {
		return nil, err
	}

	return o.Items, nil
}

// partRef names one part of a proposal, as an outcome does: which part, and which of its
// kind, 0 for the settings and the rules.
type partRef struct {
	part  kit.Part
	index int
}

// partsOf returns p's parts in the order a card applies them.
func partsOf(p kit.Proposal) []partRef {
	refs := make([]partRef, 0, len(p.Prompts)+len(p.Routines)+2)

	for i := range p.Prompts {
		refs = append(refs, partRef{part: kit.PartPrompt, index: i})
	}

	for i := range p.Routines {
		refs = append(refs, partRef{part: kit.PartRoutine, index: i})
	}

	if p.Settings != nil {
		refs = append(refs, partRef{part: kit.PartSettings})
	}

	if p.Rules != nil {
		refs = append(refs, partRef{part: kit.PartRules})
	}

	return refs
}

// settled are the parts of each proposal, by its ID, the user dealt with.
type settled map[string]map[partRef]bool

// settledBy returns the parts outcomes settle.
func settledBy(outcomes []kit.Outcome) settled {
	st := settled{}

	for _, o := range outcomes {
		if st[o.Proposal] == nil {
			st[o.Proposal] = map[partRef]bool{}
		}

		st[o.Proposal][partRef{part: o.Part, index: o.Index}] = true
	}

	return st
}

// waiting returns the parts of the proposal id, which holds p, the user hasn't dealt with
// yet.
func (st settled) waiting(id string, p kit.Proposal) []partRef {
	var out []partRef

	for _, ref := range partsOf(p) {
		if !st[id][ref] {
			out = append(out, ref)
		}
	}

	return out
}

// room refuses a proposal the folder dir has no room for: from the user's own Claude
// Code, kit.MaxOutsidePending proposals waiting there already, counted as the app counts
// the cards it shows: among the newest kit.MaxOutsideRead files, each one the app reads as
// a card from outside (never one naming a conversation) with a part the user hasn't dealt
// with. The app's own conversation has no cap: its user reads each proposal as it comes.
func (s *server) room(dir string) error {
	if s.where != kit.Outside {
		return nil
	}

	ids, err := proposalIDs(dir)
	if err != nil {
		return err
	}

	if len(ids) > kit.MaxOutsideRead {
		ids = ids[len(ids)-kit.MaxOutsideRead:]
	}

	if len(ids) < kit.MaxOutsidePending {
		return nil
	}

	// Outcomes that can't be read settle nothing, so each proposal counts as waiting.
	outcomes, _ := readOutcomes(dir)
	st := settledBy(outcomes)
	waiting := 0

	for _, id := range ids {
		if f, err := readProposal(dir, id); err == nil && f.Conversation == "" && len(st.waiting(id, f.Proposal)) > 0 {
			waiting++
		}
	}

	if waiting >= kit.MaxOutsidePending {
		return errFull
	}

	return nil
}

// write writes the proposal p, checked against snap, into dir as a new file, whole or not
// at all (appdata.WriteFile), and returns its ID. It never writes over another file.
func (s *server) write(dir string, snap kit.Snapshot, p kit.Proposal) (string, error) {
	now := s.host.now()

	id, err := kit.NewProposalID(now, s.host.random)
	if err != nil {
		return "", err
	}

	data, err := kit.EncodeProposalFile(kit.ProposalFile{
		ID: id, At: now, Conversation: s.call.conversation, Where: s.folder, SnapshotAt: snap.WrittenAt, Proposal: p,
	})
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, id+proposalExt)

	if _, err = os.Lstat(path); err == nil {
		return "", errTaken
	}

	if err = appdata.WriteFile(path, data); err != nil {
		return "", err
	}

	return id, nil
}
