package describeit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// A card is one proposal file, untrusted: any process running as the user can write the
// folder. It is read only by a name the tool mode makes, never through a link, within
// kit.MaxProposalFileBytes, through kit's strict decoder, and only when it names the
// folder it is in; then it is checked again against the live snapshot, and what the user
// did with its parts comes from the folder's outcomes, which only this service writes.

// errMisplaced is returned for a proposal file that names another folder than its own, or
// another ID than its name's.
var errMisplaced = errors.New("the proposal file names another place")

// partRef is one part of a proposal: its kind and which of that kind.
type partRef struct {
	part  kit.Part
	index int
}

// outcomeKey names what an outcome is of: one part of one proposal.
func outcomeKey(proposal string, part kit.Part, index int) string {
	return proposal + "/" + string(part) + "/" + strconv.Itoa(index)
}

// parts returns the parts of p, in the order a card applies them: prompts, routines,
// settings, rules.
func parts(p kit.Proposal) []partRef {
	out := make([]partRef, 0, len(p.Prompts)+len(p.Routines)+2)

	for i := range p.Prompts {
		out = append(out, partRef{kit.PartPrompt, i})
	}

	for i := range p.Routines {
		out = append(out, partRef{kit.PartRoutine, i})
	}

	if p.Settings != nil {
		out = append(out, partRef{kit.PartSettings, 0})
	}

	if p.Rules != nil {
		out = append(out, partRef{kit.PartRules, 0})
	}

	return out
}

// hasPart reports p holding the part named.
func hasPart(p kit.Proposal, part kit.Part, index int) bool {
	for _, r := range parts(p) {
		if r.part == part && r.index == index {
			return true
		}
	}

	return false
}

// readProposal reads the proposal id from the folder of source, as a card reads one.
func (s *Service) readProposal(source, id string) (kit.ProposalFile, error) {
	if !kit.ValidProposalID(id) || !validSource(source) {
		return kit.ProposalFile{}, fmt.Errorf("%w: %q in %q", errNoCard, textutil.Truncate(id, 40),
			textutil.Truncate(source, 40))
	}

	path := filepath.Join(s.proposalsDir(source), id+".json")

	info, err := os.Lstat(path)
	if err != nil {
		return kit.ProposalFile{}, err
	}

	if !info.Mode().IsRegular() {
		return kit.ProposalFile{}, fmt.Errorf("%w: %s", paths.ErrNotRegular, id)
	}

	data, err := readFile(path, kit.MaxProposalFileBytes)
	if err != nil {
		return kit.ProposalFile{}, err
	}

	f, err := kit.DecodeProposalFile(data)
	if err != nil {
		return kit.ProposalFile{}, err
	}

	if err = placed(f, source, id); err != nil {
		return kit.ProposalFile{}, err
	}

	return f, nil
}

// placed refuses the proposal file f, named id in the folder of source, when it names
// another ID than its name's, or another folder than its own.
func placed(f kit.ProposalFile, source, id string) error {
	switch {
	case f.ID != id:
		return fmt.Errorf("%w: its ID isn't its name's", errMisplaced)
	case source == Outside && f.Conversation != "":
		return fmt.Errorf("%w: it names a conversation, in the outside folder", errMisplaced)
	case source != Outside && f.Conversation != source:
		return fmt.Errorf("%w: it names another conversation than its folder's", errMisplaced)
	default:
		return nil
	}
}

// validSource reports the folder of a card: Outside, or a conversation's ID.
func validSource(source string) bool {
	return source == Outside || agent.ValidSessionID(source)
}

// cardReader reads the cards of one proposals folder, each checked against one live
// snapshot, read once and only when a card needs it, and the folder's outcomes, read once.
type cardReader struct {
	s        *Service
	ctx      context.Context //nolint:containedctx // one read of a folder's cards, which lives no longer than its call
	source   string
	outcomes []kit.Outcome

	snap   kit.Snapshot
	read   bool
	failed bool
}

// cardReader returns the reader of the cards of the proposals folder of source.
func (s *Service) cardReader(ctx context.Context, source string) *cardReader {
	return &cardReader{s: s, ctx: ctx, source: source, outcomes: s.outcomes(ctx, source)}
}

// card returns the card of the proposal id, which came in turn (0 for none), or false for
// one that can't be read, or checked.
func (r *cardReader) card(id string, turn int) (Card, bool) {
	f, err := r.s.readProposal(r.source, id)
	if err != nil {
		r.s.logger.WarnContext(r.ctx, "describe it couldn't read a proposal", "proposal", id, "error", err)

		return Card{}, false
	}

	snap, ok := r.snapshot()
	if !ok {
		return Card{}, false
	}

	return cardOf(r.ctx, r.source, f, turn, snap, r.outcomes), true
}

// snapshot returns the live snapshot, read the first time a card needs it.
func (r *cardReader) snapshot() (kit.Snapshot, bool) {
	if !r.read {
		r.read = true

		snap, err := r.s.liveSnapshot(r.ctx)
		if err != nil {
			r.s.logger.WarnContext(r.ctx, "describe it couldn't read the setup to check its cards", "error", err)

			r.failed = true
		}

		r.snap = snap
	}

	return r.snap, !r.failed
}

// cardOf returns the card of the proposal file f from the folder of source, which came in
// turn, checked against snap, with what the user did with its parts among outcomes.
func cardOf(ctx context.Context, source string, f kit.ProposalFile, turn int, snap kit.Snapshot,
	outcomes []kit.Outcome,
) Card {
	mine := outcomesOf(f, outcomes)

	card := Card{
		Ref: CardRef{Source: source, Proposal: f.ID}, At: f.At, Where: f.Where, Turn: turn,
		Summary: f.Proposal.Summary, Proposal: f.Proposal, Checked: kit.Check(ctx, snap, f.Proposal), Outcomes: mine,
		Settled: len(mine) == len(parts(f.Proposal)),
	}

	fillLists(&card)

	return card
}

// outcomesOf returns the outcomes of the proposal f's parts among all: the first for each
// part, in the order they came, and none for a part it doesn't have.
func outcomesOf(f kit.ProposalFile, all []kit.Outcome) []kit.Outcome {
	seen := map[string]bool{}
	mine := make([]kit.Outcome, 0, len(parts(f.Proposal)))

	for _, o := range all {
		key := outcomeKey(o.Proposal, o.Part, o.Index)
		if o.Proposal != f.ID || seen[key] || !hasPart(f.Proposal, o.Part, o.Index) {
			continue
		}

		seen[key] = true

		mine = append(mine, o)
	}

	return mine
}

// lastSettled returns when the last part of a card was dealt with.
func lastSettled(outcomes []kit.Outcome) time.Time {
	var last time.Time

	for _, o := range outcomes {
		if o.At.After(last) {
			last = o.At
		}
	}

	return last
}

// outcomes reads the outcomes file of the proposals folder of source: none when it isn't
// there, or can't be read.
func (s *Service) outcomes(ctx context.Context, source string) []kit.Outcome {
	o, err := s.readOutcomes(source)
	if err != nil {
		s.logger.WarnContext(ctx, "describe it couldn't read what was done with its cards", "folder", source, "error", err)

		return []kit.Outcome{}
	}

	return o.Items
}

// readOutcomes reads the outcomes file of the proposals folder of source: none when it
// isn't there.
func (s *Service) readOutcomes(source string) (kit.Outcomes, error) {
	data, err := readFile(s.outcomesFile(source), kit.MaxOutcomesBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return kit.Outcomes{Version: kit.OutcomesVersion, Items: []kit.Outcome{}}, nil
	}

	if err != nil {
		return kit.Outcomes{}, err
	}

	return kit.DecodeOutcomes(data)
}

// addOutcomes records items in the outcomes of the proposals folder of source, each unless
// its part has one already, and reports whether any was.
func (s *Service) addOutcomes(ctx context.Context, source string, items []kit.Outcome) (bool, error) {
	s.outcomesMu.Lock()
	defer s.outcomesMu.Unlock()

	o, err := s.readOutcomes(source)
	if err != nil {
		s.logger.WarnContext(ctx, "describe it is writing over outcomes it can't read", "folder", source, "error", err)

		o = kit.Outcomes{Version: kit.OutcomesVersion, Items: []kit.Outcome{}}
	}

	have := make(map[string]bool, len(o.Items))
	for _, item := range o.Items {
		have[outcomeKey(item.Proposal, item.Part, item.Index)] = true
	}

	added := false

	for _, item := range items {
		key := outcomeKey(item.Proposal, item.Part, item.Index)
		if have[key] {
			continue
		}

		have[key] = true

		o.Items = append(o.Items, item)
		added = true
	}

	if !added {
		return false, nil
	}

	data, err := kit.EncodeOutcomes(o)
	if err != nil {
		return false, err
	}

	if err = ensureDir(s.proposalsDir(source)); err != nil {
		return false, err
	}

	return true, appdata.WriteFile(s.outcomesFile(source), data)
}

// Settle records what the user did with one part of a card, in its proposals folder's
// outcomes, which the next turn tells the agent and the tool mode's proposal status reads.
// A part already settled keeps its first outcome.
func (s *Service) Settle(ctx context.Context, req SettleRequest) error {
	f, err := s.readProposal(req.Ref.Source, req.Ref.Proposal)
	if err != nil {
		return uierr.New("That card isn't there any more.", err)
	}

	if !hasPart(f.Proposal, req.Part, req.Index) {
		return uierr.New("That card has no such part.", fmt.Errorf("%w: %s %d", errNoCard, req.Part, req.Index))
	}

	if !fits(req.Part, req.Kind) {
		return uierr.New("Describe it can't record that for this part of the card.",
			fmt.Errorf("%w: %s of a %s", errOutcome, req.Kind, req.Part))
	}

	o := kit.Outcome{
		Proposal: f.ID, Part: req.Part, Index: req.Index, Kind: req.Kind,
		Words: outcomeWords(req.Words, f.Proposal, req.Part, req.Index, req.Kind), At: s.clock.Now().UTC(),
	}

	if req.Part == kit.PartRoutine && req.RoutineID > 0 {
		o.RoutineID = req.RoutineID
	}

	added, err := s.addOutcomes(ctx, req.Ref.Source, []kit.Outcome{o})
	if err != nil {
		return uierr.New("Describe it couldn't record what you did with the card.", err)
	}

	if added {
		s.cardChanged(req.Ref)
	}

	return nil
}

// DiscardProposal discards every part of the card ref the user hasn't dealt with yet.
func (s *Service) DiscardProposal(ctx context.Context, ref CardRef) error {
	f, err := s.readProposal(ref.Source, ref.Proposal)
	if err != nil {
		return uierr.New("That card isn't there any more.", err)
	}

	done := map[string]bool{}
	for _, o := range outcomesOf(f, s.outcomes(ctx, ref.Source)) {
		done[outcomeKey(o.Proposal, o.Part, o.Index)] = true
	}

	now := s.clock.Now().UTC()

	var items []kit.Outcome

	for _, p := range parts(f.Proposal) {
		if done[outcomeKey(f.ID, p.part, p.index)] {
			continue
		}

		items = append(items, kit.Outcome{
			Proposal: f.ID, Part: p.part, Index: p.index, Kind: kit.OutcomeDiscarded,
			Words: defaultWords(f.Proposal, p.part, p.index, kit.OutcomeDiscarded), At: now,
		})
	}

	if len(items) == 0 {
		return nil
	}

	added, err := s.addOutcomes(ctx, ref.Source, items)
	if err != nil {
		return uierr.New("Describe it couldn't discard the card.", err)
	}

	if added {
		s.cardChanged(ref)
	}

	return nil
}

// cardChanged tells the window a card was dealt with: the conversation's turn it came in,
// or the cards from the user's own Claude Code.
func (s *Service) cardChanged(ref CardRef) {
	if ref.Source == Outside {
		s.sink.Emit(event.DescribeItProposals{})

		return
	}

	s.mu.Lock()

	rec := s.currentLocked()
	if rec == nil || rec.ID != ref.Source {
		rec, _ = s.readRecord(ref.Source)
	}

	n := 0
	if rec != nil {
		n = rec.proposals()[ref.Proposal]
	}

	s.mu.Unlock()

	s.updated(ref.Source, n)
}

// fits reports kind something done with a part: a prompt added or discarded, a routine
// added, saved, opened in the builder, or discarded, and a settings or rules change
// applied or discarded.
func fits(part kit.Part, kind kit.OutcomeKind) bool {
	switch part {
	case kit.PartPrompt:
		return kind == kit.OutcomeAdded || kind == kit.OutcomeDiscarded
	case kit.PartRoutine:
		return kind == kit.OutcomeAdded || kind == kit.OutcomeSaved || kind == kit.OutcomeBuilder ||
			kind == kit.OutcomeDiscarded
	case kit.PartSettings, kit.PartRules:
		return kind == kit.OutcomeApplied || kind == kit.OutcomeDiscarded
	default:
		return false
	}
}

// outcomeWords returns the words the window gave, one line within kit's cap, or, for none
// or words kit can't keep, the outcome's own (defaultWords).
func outcomeWords(given string, p kit.Proposal, part kit.Part, index int, kind kit.OutcomeKind) string {
	words := textutil.Truncate(textutil.OneLine(given), kit.MaxOutcomeRunes)
	if words != "" && keepable(words) {
		return words
	}

	return defaultWords(p, part, index, kind)
}

// keepable reports words an outcomes file can keep.
func keepable(words string) bool {
	_, err := kit.EncodeOutcomes(kit.Outcomes{Items: []kit.Outcome{{
		Proposal: "20000101T000000Z-000000000000", Part: kit.PartRules, Kind: kit.OutcomeDiscarded, Words: words,
		At: time.Unix(1, 0),
	}}})

	return err == nil
}

// defaultWords says what was done with a part as the next turn tells the agent, its
// names in curly quotes and with no full stop, as the window words an outcome: `Added the
// routine “Fix checks”`, `Discarded the settings change`.
func defaultWords(p kit.Proposal, part kit.Part, index int, kind kit.OutcomeKind) string {
	what := partWords(p, part, index)

	switch kind {
	case kit.OutcomeAdded:
		return "Added " + what
	case kit.OutcomeSaved:
		return "Saved the change to " + what
	case kit.OutcomeApplied:
		return "Applied " + what
	case kit.OutcomeBuilder:
		return "Opened " + what + " in the builder to finish it there"
	case kit.OutcomeDiscarded:
	}

	return "Discarded " + what
}

// partWords names a part of p: `the routine “Fix checks”`, `the prompt “Tests first”`, `the
// settings change`, `the rules change`.
func partWords(p kit.Proposal, part kit.Part, index int) string {
	switch {
	case part == kit.PartRoutine && index >= 0 && index < len(p.Routines):
		return "the routine “" + textutil.Truncate(kit.Name(p.Routines[index].Name), 80) + "”"
	case part == kit.PartPrompt && index >= 0 && index < len(p.Prompts):
		return "the prompt “" + textutil.Truncate(kit.Name(p.Prompts[index].Nickname), 80) + "”"
	case part == kit.PartSettings:
		return "the settings change"
	default:
		return "the rules change"
	}
}
