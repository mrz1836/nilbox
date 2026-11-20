package describeit

import (
	"context"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/periodic"
)

// The cards from the user's own Claude Code.
const (
	// settledShown is how long a card from outside stays once every part of it was dealt
	// with.
	settledShown = 24 * time.Hour

	// outsideEvery is how often the outside folder is looked at for a card that came or
	// went.
	outsideEvery = 3 * time.Second
)

// Proposals returns the cards from the user's own Claude Code, each checked against the
// live snapshot, the newest first: those with a part no one dealt with, and those settled
// in the last day.
func (s *Service) Proposals(ctx context.Context) ([]Card, error) {
	reader := s.cardReader(ctx, Outside)
	now := s.clock.Now()
	cards := make([]Card, 0, kit.MaxOutsidePending)

	for _, id := range s.outsideIDs(ctx) {
		card, ok := reader.card(id, 0)
		if !ok || (card.Settled && now.Sub(lastSettled(card.Outcomes)) > settledShown) {
			continue
		}

		cards = append(cards, card)
	}

	slices.SortStableFunc(cards, func(a, b Card) int {
		if c := b.At.Compare(a.At); c != 0 {
			return c
		}

		return strings.Compare(b.Ref.Proposal, a.Ref.Proposal)
	})

	return cards, nil
}

// outsideIDs returns the IDs of the outside folder's proposal files, the newest
// kit.MaxOutsideRead.
func (s *Service) outsideIDs(ctx context.Context) []string {
	ids, err := proposalIDs(s.proposalsDir(Outside))
	if err != nil {
		s.logger.WarnContext(ctx, "describe it couldn't read the proposals from outside", "error", err)

		return []string{}
	}

	if len(ids) > kit.MaxOutsideRead {
		ids = ids[len(ids)-kit.MaxOutsideRead:]
	}

	return ids
}

// pendingOutside counts the cards from the user's own Claude Code with a part no one has
// dealt with yet, as the tool mode counts them toward kit.MaxOutsidePending. It decodes
// each, and checks none.
func (s *Service) pendingOutside(ctx context.Context) int {
	outcomes := s.outcomes(ctx, Outside)
	n := 0

	for _, id := range s.outsideIDs(ctx) {
		f, err := s.readProposal(Outside, id)
		if err != nil {
			continue
		}

		if len(outcomesOf(f, outcomes)) < len(parts(f.Proposal)) {
			n++
		}
	}

	return n
}

// followOutside follows the outside proposals folder until ctx ends, telling the window
// (describeit:proposals) when a proposal comes or goes.
func (s *Service) followOutside(ctx context.Context) {
	last, first := uint64(0), true

	periodic.RunEvery(ctx, func(context.Context) time.Duration { return outsideEvery }, nil,
		"describe it's outside proposals", func(context.Context) error {
			mark := s.outsideMark()

			if !first && mark != last {
				s.sink.Emit(event.DescribeItProposals{})
			}

			last, first = mark, false

			return nil
		}, s.logger)
}

// outsideMark sums up the outside folder: each proposal file's name, size, and time, and
// the outcomes', so a mark that changed says a card came, went, or was dealt with.
func (s *Service) outsideMark() uint64 {
	h := fnv.New64a()
	dir := s.proposalsDir(Outside)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || filepath.Ext(e.Name()) != ".json" {
			continue
		}

		_, _ = h.Write([]byte(e.Name() + "\x00" + strconv.FormatInt(info.Size(), 10) + "\x00" +
			strconv.FormatInt(info.ModTime().UnixNano(), 10) + "\n"))
	}

	return h.Sum64()
}

// pruneOutside removes the cards from outside that every part of was dealt with more than
// keepConversations ago, with their outcomes, so neither the folder nor its outcomes grow
// for good.
func (s *Service) pruneOutside(ctx context.Context) {
	gone := s.settledLongAgo(ctx)
	if len(gone) == 0 {
		return
	}

	for id := range gone {
		if err := os.Remove(filepath.Join(s.proposalsDir(Outside), id+".json")); err != nil {
			s.logger.WarnContext(ctx, "describe it couldn't remove an old proposal", "proposal", id, "error", err)
		}
	}

	s.dropOutcomes(ctx, gone)
}

// settledLongAgo returns the IDs of the cards from outside that every part of was dealt
// with more than keepConversations ago: among every proposal file, not only the newest
// kit.MaxOutsideRead the cards are read from, so one that fell past them still goes.
func (s *Service) settledLongAgo(ctx context.Context) map[string]bool {
	gone := map[string]bool{}

	o, err := s.readOutcomes(Outside)
	if err != nil {
		return gone
	}

	ids, err := proposalIDs(s.proposalsDir(Outside))
	if err != nil {
		s.logger.WarnContext(ctx, "describe it couldn't read the proposals from outside", "error", err)

		return gone
	}

	now := s.clock.Now()

	for _, id := range ids {
		if f, readErr := s.readProposal(Outside, id); readErr == nil {
			mine := outcomesOf(f, o.Items)
			if len(mine) == len(parts(f.Proposal)) && now.Sub(lastSettled(mine)) > keepConversations {
				gone[id] = true
			}
		}
	}

	return gone
}

// dropOutcomes removes the outside outcomes of the proposals gone.
func (s *Service) dropOutcomes(ctx context.Context, gone map[string]bool) {
	s.outcomesMu.Lock()
	defer s.outcomesMu.Unlock()

	o, err := s.readOutcomes(Outside)
	if err != nil {
		return
	}

	o.Items = slices.DeleteFunc(o.Items, func(item kit.Outcome) bool { return gone[item.Proposal] })

	data, err := kit.EncodeOutcomes(o)
	if err == nil {
		err = appdata.WriteFile(s.outcomesFile(Outside), data)
	}

	if err != nil {
		s.logger.WarnContext(ctx, "describe it couldn't drop old outcomes", "error", err)
	}
}
