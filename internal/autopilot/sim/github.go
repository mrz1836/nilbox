package sim

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/github"
)

// GitHub is the fake of the review and issue sheets' services the GitHub step goes
// through (autopilot.GitHubActions), over the scripted World: its read is the inbox's own
// single-item read, which rewrites the card as a sheet's does; a send is pinned to the
// head it names, so one whose pull request the World pushed to since sends nothing, and
// one on an item the World closed is no longer offered. By default the sheet offers every
// action on an open item with no warning; a scenario changes that per item (NotOffer,
// Warn), has the write gate refuse (HoldWrites), or changes the World as a send arrives
// (OnSend). It records each action GitHub took, and holds every answer while Hold says so,
// as a send a quit cuts off.
type GitHub struct {
	sim *Sim

	mu    sync.Mutex
	sends []autopilot.GitHubSend
	tried []autopilot.GitHubSend
	reads []int
	marks []int
	gate  chan struct{}

	notOffered map[int]string
	warnings   map[int]string
	held       time.Duration
	heldOnce   bool
	marked     int

	// before runs as each send reaches the sheet, before it decides (OnSend).
	before func(req autopilot.GitHubSend)

	// merges are the merges GitHub took, mergesTried every one asked, reviews the reviews
	// posted, needsReview the items whose merge GitHub holds for an approving review, and
	// awaitsApproval the user's own pull requests that lack only an approval from someone
	// else, which merge only past the rules.
	merges         []autopilot.MergeSend
	mergesTried    []autopilot.MergeSend
	reviews        []autopilot.ReviewSend
	needsReview    map[int]bool
	awaitsApproval map[int]bool

	// closesPastRules answers a merge the switch kept back as an earlier build did: no
	// longer offered, which closed its match (ClosesPastRules).
	closesPastRules bool

	// beforeFollow runs as each merge or review reaches the sheet (OnFollowUp).
	beforeFollow func(number int)
}

// OnFollowUp runs fn as each merge or review reaches the sheet, before it decides: what
// changes on GitHub between the read and the write.
func (g *GitHub) OnFollowUp(fn func(number int)) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.beforeFollow = fn
}

// OnSend runs fn as each send reaches the sheet, before the sheet decides it: what
// changes on GitHub between the read and the send.
func (g *GitHub) OnSend(fn func(req autopilot.GitHubSend)) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.before = fn
}

// newGitHub returns the fake over s.
func newGitHub(s *Sim) *GitHub {
	return &GitHub{sim: s, notOffered: map[int]string{}, warnings: map[int]string{}, marked: 1}
}

// Hold holds every send's answer until Release: GitHub took it, its answer not yet back.
func (g *GitHub) Hold() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.gate = make(chan struct{})
}

// Release lets the answers through.
func (g *GitHub) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.gate != nil {
		close(g.gate)
		g.gate = nil
	}
}

// NotOffer has the sheet of the item number no longer offer any action, saying why; ""
// offers them again.
func (g *GitHub) NotOffer(number int, why string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.notOffered[number] = why
}

// Warn has the sheet of the item number offer each action with warnings; "" offers them
// with none.
func (g *GitHub) Warn(number int, warnings string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.warnings[number] = warnings
}

// HoldWrites has the write gate refuse the next send, saying the next may go in d.
func (g *GitHub) HoldWrites(d time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.held, g.heldOnce = d, true
}

// Marks sets how many notifications Mark the GitHub notification done queues.
func (g *GitHub) Marks(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.marked = n
}

// Read reads the item through the inbox's single-item read, as the sheet's read rewrites
// the card.
func (g *GitHub) Read(ctx context.Context, it autopilot.ItemRef) error {
	g.mu.Lock()
	g.reads = append(g.reads, it.Number)
	g.mu.Unlock()

	_, err := g.sim.Inbox.RefreshItem(ctx, it.ID)

	return err
}

// Send decides the action as the sheet would, against the World, and records it when
// GitHub took it.
func (g *GitHub) Send(ctx context.Context, req autopilot.GitHubSend) (autopilot.GitHubSent, error) {
	g.mu.Lock()
	g.tried = append(g.tried, req)
	before := g.before
	g.mu.Unlock()

	if before != nil {
		before(req)
	}

	g.mu.Lock()
	gate := g.gate
	g.mu.Unlock()

	got, err := g.decide(req)

	if err == nil && got.Outcome == autopilot.GitHubSentIt {
		g.mu.Lock()
		g.sends = append(g.sends, req)
		g.mu.Unlock()
	}

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return autopilot.GitHubSent{}, ctx.Err()
		}
	}

	return got, err
}

// sentWords say what an action GitHub took did.
func sentWords(a spec.GitHubAction) string {
	switch a.Kind() {
	case catalog.GitHubRebase:
		return "Asked Dependabot to rebase."
	case catalog.GitHubUpdateBranch:
		return "Updated the branch."
	case catalog.GitHubReply:
		return "Commented."
	case catalog.GitHubLabels:
		return "Changed the labels."
	}

	return "Sent."
}

// MarkDone records the item, and says it queued the clears Marks set.
func (g *GitHub) MarkDone(_ context.Context, it autopilot.ItemRef) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.marks = append(g.marks, it.Number)

	return g.marked, nil
}

// Preview says what the action would send.
func (g *GitHub) Preview(_ context.Context, _ autopilot.ItemRef, a spec.GitHubAction) (string, error) {
	switch {
	case a.Rebase != nil:
		return "@dependabot rebase", nil
	case a.Labels != nil:
		return "Adds " + strings.Join(a.Labels.Add, ", "), nil
	}

	return sentWords(a), nil
}

// Jobs returns the actions GitHub took, in order.
func (g *GitHub) Jobs() []autopilot.GitHubSend {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.sends)
}

// Tried returns every send asked of the sheets, taken or not, in order.
func (g *GitHub) Tried() []autopilot.GitHubSend {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.tried)
}

// Reads returns the items read through their sheets, by number, in order.
func (g *GitHub) Reads() []int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.reads)
}

// Marked returns the items whose notifications were marked done, by number, in order.
func (g *GitHub) Marked() []int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.marks)
}

// AwaitsApproval makes the item number the user's own pull request that lacks only an
// approval from someone else, or no longer for false: the sheet merges it only past the
// rules, as core's does, and otherwise answers why not, a merge the switch kept back
// waiting for it.
func (g *GitHub) AwaitsApproval(number int, awaits bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.awaitsApproval == nil {
		g.awaitsApproval = map[int]bool{}
	}

	g.awaitsApproval[number] = awaits
}

// ClosesPastRules has the sheet answer a merge the switch kept back as an earlier build
// did, no longer offered, which closed its match, or as this one does for false.
func (g *GitHub) ClosesPastRules(closes bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.closesPastRules = closes
}

// NeedsReview has GitHub hold the item number's merge for an approving review, or no
// longer for false.
func (g *GitHub) NeedsReview(number int, needs bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.needsReview == nil {
		g.needsReview = map[int]bool{}
	}

	g.needsReview[number] = needs
}

// Merge merges the pull request in the World, or records auto-merge turned back on, as the
// review sheet would: only while it is open at req.Head, and not while GitHub holds it for
// an approving review.
func (g *GitHub) Merge(_ context.Context, req autopilot.MergeSend) (autopilot.GitHubSent, error) {
	g.mu.Lock()
	g.mergesTried = append(g.mergesTried, req)
	g.mu.Unlock()

	got, open := g.pinned(req.Item.Number, req.Head, req.Accept)
	if !open {
		return got, nil
	}

	g.mu.Lock()
	needs, awaits, closes := g.needsReview[req.Item.Number], g.awaitsApproval[req.Item.Number], g.closesPastRules
	g.mu.Unlock()

	switch {
	case awaits && !req.PastReview && req.PastRulesOff && !closes:
		return autopilot.GitHubSent{Outcome: autopilot.GitHubPastRulesOff, Words: autopilot.PastRulesOffWords}, nil
	case awaits && !req.PastReview:
		return autopilot.GitHubSent{Outcome: autopilot.GitHubNotOffered, Words: req.PastReviewWhy}, nil
	case needs:
		return autopilot.GitHubSent{Outcome: autopilot.GitHubNeedsReview}, nil
	}

	g.mu.Lock()
	g.merges = append(g.merges, req)
	g.mu.Unlock()

	if req.AutoMerge {
		return autopilot.GitHubSent{Outcome: autopilot.GitHubSentIt, Words: "Turned auto-merge back on."}, nil
	}

	g.sim.World.Merge(req.Item.Number)

	return autopilot.GitHubSent{Outcome: autopilot.GitHubSentIt, Words: "Merged."}, nil
}

// PostReview records the review, as the review sheet would post it: only while the pull
// request is open at req.Head.
func (g *GitHub) PostReview(_ context.Context, req autopilot.ReviewSend) (autopilot.GitHubSent, error) {
	got, open := g.pinned(req.Item.Number, req.Head, req.Accept)
	if !open {
		return got, nil
	}

	g.mu.Lock()
	g.reviews = append(g.reviews, req)
	g.mu.Unlock()

	return autopilot.GitHubSent{Outcome: autopilot.GitHubSentIt, Words: "Posted the review."}, nil
}

// Merges returns the merges GitHub took, auto-merge turned back on included, in order, and
// MergesTried every merge asked.
func (g *GitHub) Merges() []autopilot.MergeSend {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.merges)
}

// MergesTried returns every merge asked of the sheet, in order.
func (g *GitHub) MergesTried() []autopilot.MergeSend {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.mergesTried)
}

// Reviews returns the reviews posted, in order.
func (g *GitHub) Reviews() []autopilot.ReviewSend {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.reviews)
}

// pinned decides a merge or a review as the sheet would before sending it: one on an item
// not open, or pushed to since head, is no longer offered, and one with warnings the user
// didn't accept waits; it reports whether it may go.
func (g *GitHub) pinned(number int, head, accept string) (autopilot.GitHubSent, bool) {
	g.mu.Lock()
	before := g.beforeFollow
	g.mu.Unlock()

	if before != nil {
		before(number)
	}

	w := g.sim.World

	w.mu.Lock()
	it, ok := w.items[number]

	var now, state string
	if ok {
		now, state = it.Head, it.State
	}
	w.mu.Unlock()

	g.mu.Lock()
	defer g.mu.Unlock()

	switch {
	case !ok || state != github.StateOpen:
		return autopilot.GitHubSent{Outcome: autopilot.GitHubNotOffered, Words: "It's no longer open."}, false
	case !strings.EqualFold(now, head):
		return autopilot.GitHubSent{Outcome: autopilot.GitHubNotOffered, Words: "It was pushed to since."}, false
	case g.warnings[number] != "" && accept != g.warnings[number]:
		return autopilot.GitHubSent{Outcome: autopilot.GitHubWarning, Words: g.warnings[number]}, false
	}

	return autopilot.GitHubSent{}, true
}

// decide is how the sheet's own rules take req, as the World stands.
func (g *GitHub) decide(req autopilot.GitHubSend) (autopilot.GitHubSent, error) {
	w := g.sim.World

	w.mu.Lock()
	it, ok := w.items[req.Item.Number]

	var head, state string
	if ok {
		head, state = it.Head, it.State
	}
	w.mu.Unlock()

	g.mu.Lock()
	defer g.mu.Unlock()

	switch {
	case !ok || state != github.StateOpen:
		return autopilot.GitHubSent{Outcome: autopilot.GitHubNotOffered, Words: "It's no longer open."}, nil
	case g.notOffered[req.Item.Number] != "":
		return autopilot.GitHubSent{Outcome: autopilot.GitHubNotOffered, Words: g.notOffered[req.Item.Number]}, nil
	case head != "" && !strings.EqualFold(head, req.Head):
		return autopilot.GitHubSent{Outcome: autopilot.GitHubPushedSince}, nil
	case g.heldOnce:
		g.heldOnce = false

		return autopilot.GitHubSent{
			Outcome: autopilot.GitHubHeld, RetryIn: g.held,
			Words: "GitHub asked for a pause before the next change. Try again in a minute.",
		}, nil
	case g.warnings[req.Item.Number] != "" && req.Accept != g.warnings[req.Item.Number]:
		return autopilot.GitHubSent{Outcome: autopilot.GitHubWarning, Words: g.warnings[req.Item.Number]}, nil
	}

	return autopilot.GitHubSent{Outcome: autopilot.GitHubSentIt, Words: sentWords(req.Action)}, nil
}
