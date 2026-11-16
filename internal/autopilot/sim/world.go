package sim

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
)

// The scripted GitHub: one repository of pull requests and issues whose heads, checks,
// and states a scenario changes as time goes on. It answers the inbox's own requests, as
// GitHub would, so every item the engine sees went through the inbox's own store: the
// refresh, the watch pass, an item's own read, the check names, and the states.

// Owner and Repo are the scripted repository.
const (
	Owner  = "acme-org"
	Repo   = "example-repo"
	Viewer = "octo-user"
)

// ErrOffline is what every request answers while the world is offline.
var ErrOffline = errors.New("offline: no connection to GitHub")

// errNotAsked is a request the simulator never needs.
var errNotAsked = errors.New("the simulator never asks for this")

// Item is one scripted pull request or issue.
type Item struct {
	Number int
	Issue  bool

	Title       string
	Author      string
	Bot         bool
	Association string
	CrossRepo   bool

	// Head is a pull request's head commit, and State OPEN, CLOSED, or MERGED.
	Head  string
	State string

	// Checks is the head's checks' overall state (SUCCESS, FAILURE, PENDING, or ""), and
	// Failed, Passed, and Pending count them.
	Checks  string
	Failed  int
	Passed  int
	Pending int

	Labels   []string
	Comments int

	// Watched puts it in the routines' search, Listed in the user's Authored list, and
	// ReviewRequested and Assigned in those lists.
	Watched         bool
	Listed          bool
	ReviewRequested bool
	Assigned        bool

	Created time.Time
	Updated time.Time
}

// node is the item's node ID.
func (it *Item) node() string { return "NODE_" + strconv.Itoa(it.Number) }

// World is the scripted GitHub.
type World struct {
	clock interface{ Now() time.Time }

	mu      sync.Mutex
	items   map[int]*Item
	offline bool
	failing bool

	// failWith fails each search whose query holds it, "" for none.
	failWith string

	// Page is the most one search returns; past it, the search is truncated.
	Page int

	// gates hold an item's own read until they are opened.
	gates map[int]chan struct{}

	// reads counts the item reads, by number, and stateReads the reads of states.
	reads      map[int]int
	stateReads int

	// signedOut has the GitHub CLI signed out; statesGate holds the reads of states.
	signedOut  bool
	statesGate chan struct{}
}

// NewWorld returns an empty world on clock.
func NewWorld(clock interface{ Now() time.Time }) *World {
	return &World{clock: clock, items: map[int]*Item{}, Page: 100, gates: map[int]chan struct{}{}, reads: map[int]int{}}
}

// Add adds it, open, at the clock's time, and returns it.
func (w *World) Add(it Item) *Item {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := w.clock.Now().UTC()

	if it.State == "" {
		it.State = github.StateOpen
	}

	if it.Title == "" {
		it.Title = "Bump example-lib from 1.0.0 to 1.0." + strconv.Itoa(it.Number)
	}

	if it.Author == "" {
		it.Author, it.Bot = "dependabot", true
	}

	if it.Association == "" {
		it.Association = "MEMBER"
	}

	if !it.Issue && it.Head == "" {
		it.Head = Commit(it.Number, 1)
	}

	it.Created, it.Updated = now.Add(-time.Hour), now
	w.items[it.Number] = &it

	return &it
}

// Commit is the nth scripted commit of pull request number.
func Commit(number, n int) string {
	return fmt.Sprintf("%020d%020d", number, n)
}

// Change changes the item number, and stamps it as updated now.
func (w *World) Change(number int, fn func(it *Item)) {
	w.mu.Lock()
	defer w.mu.Unlock()

	it := w.items[number]
	fn(it)
	it.Updated = w.clock.Now().UTC()
}

// Push moves a pull request's head to commit, its checks running.
func (w *World) Push(number int, commit string) {
	w.Change(number, func(it *Item) {
		it.Head, it.Checks, it.Failed, it.Passed, it.Pending = commit, "PENDING", 0, 0, 1
	})
}

// Fail has a pull request's checks fail.
func (w *World) Fail(number int) {
	w.Change(number, func(it *Item) { it.Checks, it.Failed, it.Passed, it.Pending = "FAILURE", 1, 3, 0 })
}

// Pass has a pull request's checks pass.
func (w *World) Pass(number int) {
	w.Change(number, func(it *Item) { it.Checks, it.Failed, it.Passed, it.Pending = "SUCCESS", 0, 4, 0 })
}

// Merge merges a pull request.
func (w *World) Merge(number int) {
	w.Change(number, func(it *Item) { it.State = github.StateMerged })
}

// Offline cuts the world off, or brings it back.
func (w *World) Offline(off bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.offline = off
}

// FailSearches has GitHub fail every search, or answer them again.
func (w *World) FailSearches(fail bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.failing = fail
}

// FailSearchesWith has GitHub fail each search whose query holds text, or, with "",
// answer them again.
func (w *World) FailSearchesWith(text string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.failWith = text
}

// Hold holds the item number's own reads until Release.
func (w *World) Hold(number int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.gates[number] = make(chan struct{})
}

// Release lets the item number's reads through.
func (w *World) Release(number int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if g, ok := w.gates[number]; ok {
		close(g)
		delete(w.gates, number)
	}
}

// Reads returns how many times the item number was read on its own.
func (w *World) Reads(number int) int {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.reads[number]
}

// Token lends a placeholder token.
func (w *World) Token(context.Context) (github.Token, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.signedOut {
		return github.Token{}, github.ErrNotLoggedIn
	}

	return github.NewToken("sim-token-not-real"), nil
}

// SignOut signs the GitHub CLI out, or back in.
func (w *World) SignOut(out bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.signedOut = out
}

// HoldStates holds the reads of states until ReleaseStates.
func (w *World) HoldStates() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.statesGate = make(chan struct{})
}

// ReleaseStates lets the reads of states through.
func (w *World) ReleaseStates() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.statesGate != nil {
		close(w.statesGate)
		w.statesGate = nil
	}
}

// StateReads returns how many times the states of items were read.
func (w *World) StateReads() int {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.stateReads
}

// Forget forgets nothing.
func (w *World) Forget(github.Token) {}

// rate is a GraphQL budget well above the watch pass's pause.
func rate() github.QueryRate {
	return github.QueryRate{Cost: 1, Limit: 5000, Remaining: 4900, Used: 100}
}

// FetchInbox answers with the user's lists: Review requested, Authored, and Assigned.
func (w *World) FetchInbox(context.Context, github.Token) (*github.Inbox, error) {
	if err := w.down(); err != nil {
		return nil, err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	var requested, authored, assigned []github.Item

	for _, n := range w.numbers() {
		it := w.items[n]
		if it.State != github.StateOpen {
			continue
		}

		if it.ReviewRequested {
			requested = append(requested, w.item(it))
		}

		if it.Listed {
			authored = append(authored, w.item(it))
		}

		if it.Assigned {
			assigned = append(assigned, w.item(it))
		}
	}

	return &github.Inbox{
		Viewer:          github.Viewer{Login: Viewer},
		ReviewRequested: github.SearchResult{IssueCount: len(requested), Items: requested},
		Authored:        github.SearchResult{IssueCount: len(authored), Items: authored},
		Assigned:        github.SearchResult{IssueCount: len(assigned), Items: assigned},
		RateLimit:       rate(), OAuthScopes: "repo, read:org",
	}, nil
}

// FetchDetails answers with each item's details.
func (w *World) FetchDetails(_ context.Context, _ github.Token, ids []string, opts github.DetailOptions,
) (*github.Details, error) {
	if err := w.down(); err != nil {
		return nil, err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	out := &github.Details{Items: map[string]github.ItemDetails{}, RateLimit: rate(), Requests: 1}

	for _, id := range ids {
		if it, ok := w.byNode(id); ok {
			out.Items[id] = w.details(it, opts.Repository)
		}
	}

	return out, nil
}

// FetchItem answers with one item, waiting while a scenario holds its reads.
func (w *World) FetchItem(ctx context.Context, _ github.Token, id string, _ github.DetailOptions,
) (*github.ItemUpdate, error) {
	if err := w.down(); err != nil {
		return nil, err
	}

	w.mu.Lock()
	it, ok := w.byNode(id)

	var gate chan struct{}
	if ok {
		gate = w.gates[it.Number]
		w.reads[it.Number]++
	}
	w.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if !ok {
		return &github.ItemUpdate{}, nil
	}

	return &github.ItemUpdate{Found: true, Item: w.item(it), Details: w.details(it, true), RateLimit: rate()}, nil
}

// FetchItems answers with each item, as FetchItem does.
func (w *World) FetchItems(ctx context.Context, tok github.Token, ids []string, opts github.DetailOptions,
) (*github.ItemUpdates, error) {
	out := &github.ItemUpdates{Items: map[string]github.ItemUpdate{}, RateLimit: rate()}

	for _, id := range ids {
		u, err := w.FetchItem(ctx, tok, id, opts)
		if err != nil {
			return nil, err
		}

		if u.Found {
			out.Items[id] = *u
		}
	}

	return out, nil
}

// FetchPullHead is never asked.
func (w *World) FetchPullHead(context.Context, github.Token, item.Ref) (*github.PullHead, error) {
	return nil, errNotAsked
}

// FetchChangedFiles is never asked.
func (w *World) FetchChangedFiles(context.Context, github.Token, item.Ref, string) (*github.ChangedFiles, error) {
	return nil, errNotAsked
}

// FetchRateLimits answers with budgets to spare.
func (w *World) FetchRateLimits(context.Context, github.Token) (*github.RateLimits, error) {
	if err := w.down(); err != nil {
		return nil, err
	}

	return &github.RateLimits{
		Core: github.Rate{Limit: 5000, Remaining: 4900}, GraphQL: github.Rate{Limit: 5000, Remaining: 4900},
		Search: github.Rate{Limit: 30, Remaining: 30},
	}, nil
}

// FetchAvatar is never answered.
func (w *World) FetchAvatar(context.Context, string) (*github.Avatar, error) {
	return nil, errNotAsked
}

// FetchWatches answers each search with the open watched items, most recently updated
// first, at most Page of them.
func (w *World) FetchWatches(_ context.Context, _ github.Token, searches []github.WatchSearch,
) (*github.Watches, error) {
	if err := w.down(); err != nil {
		return nil, err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	out := &github.Watches{Results: map[string]github.WatchResult{}, RateLimit: rate(), Requests: 1}

	var found []*Item

	for _, n := range w.numbers() {
		if it := w.items[n]; it.Watched && it.State == github.StateOpen {
			found = append(found, it)
		}
	}

	slices.SortStableFunc(found, func(a, b *Item) int { return b.Updated.Compare(a.Updated) })

	for _, s := range searches {
		if w.fails(s) {
			out.Results[s.Key] = github.WatchResult{Err: github.ErrSearchFailed, Items: []github.WatchItem{}}

			continue
		}

		page := found[:min(len(found), w.Page)]
		res := github.WatchResult{IssueCount: len(found), Items: make([]github.WatchItem, 0, len(page))}

		for _, it := range page {
			res.Items = append(res.Items, github.WatchItem{
				Item: w.item(it), HeadOID: it.Head, BaseRef: "main", ChecksState: it.Checks,
			})
		}

		out.Results[s.Key] = res
	}

	return out, nil
}

// FetchChecks answers with each pull request's checks on its head, finished an hour ago
// but those still running.
func (w *World) FetchChecks(_ context.Context, _ github.Token, ids []string) (*github.ChecksRead, error) {
	if err := w.down(); err != nil {
		return nil, err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	done := w.clock.Now().UTC().Add(-time.Hour)
	out := &github.ChecksRead{Items: map[string]github.HeadChecks{}, RateLimit: rate(), Requests: 1}

	for _, id := range ids {
		it, ok := w.byNode(id)
		if !ok || it.Issue {
			continue
		}

		h := github.HeadChecks{Head: it.Head, Complete: true}

		for i, outcome := range append(append(repeat(github.CheckFailed, it.Failed), repeat(github.CheckPassed,
			it.Passed)...), repeat(github.CheckPending, it.Pending)...) {
			c := github.HeadCheck{
				Kind: "run", Name: "test " + strconv.Itoa(i), Workflow: "ci", Outcome: outcome, StartedAt: done.Add(-time.Minute),
			}

			if outcome != github.CheckPending {
				c.CompletedAt = done
			}

			h.Checks = append(h.Checks, c)
		}

		out.Items[id] = h
	}

	return out, nil
}

// repeat returns n copies of s.
func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}

	return out
}

// FetchItemStates answers each item's state.
func (w *World) FetchItemStates(ctx context.Context, _ github.Token, refs []item.Ref,
) (map[string]github.ItemState, error) {
	if err := w.down(); err != nil {
		return nil, err
	}

	w.mu.Lock()
	w.stateReads++
	gate := w.statesGate
	w.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	out := map[string]github.ItemState{}

	for _, ref := range refs {
		if it, ok := w.items[ref.Number]; ok {
			out[ref.Key()] = github.ItemState{PullRequest: !it.Issue, Title: it.Title, State: it.State}
		}
	}

	return out, nil
}

// FetchViewer answers with the user.
func (w *World) FetchViewer(context.Context, github.Token) (string, error) {
	return Viewer, w.down()
}

// RemoveAssignee is never asked.
func (w *World) RemoveAssignee(context.Context, github.Token, item.Ref, string) (*github.Edited, error) {
	return nil, errNotAsked
}

// RemoveReviewRequest is never asked.
func (w *World) RemoveReviewRequest(context.Context, github.Token, item.Ref, string) (*github.Edited, error) {
	return nil, errNotAsked
}

// fails reports a search the world fails.
func (w *World) fails(s github.WatchSearch) bool {
	return w.failing || w.failWith != "" && strings.Contains(s.Query, w.failWith)
}

// down returns ErrOffline while the world is offline.
func (w *World) down() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.offline {
		return ErrOffline
	}

	return nil
}

// item returns the GitHub item it is.
func (w *World) item(it *Item) github.Item {
	authorType := "User"
	if it.Bot {
		authorType = "Bot"
	}

	return github.Item{
		IsPR: !it.Issue, NodeID: it.node(), Number: it.Number, Title: it.Title,
		URL:       fmt.Sprintf("https://github.com/%s/%s/issues/%d", Owner, Repo, it.Number),
		CreatedAt: it.Created, UpdatedAt: it.Updated, State: it.State, HeadRef: "dependabot/go_modules/example-lib",
		AuthorLogin: it.Author, AuthorType: authorType, AuthorAssociation: it.Association,
		IsCrossRepository: it.CrossRepo, RepoOwner: Owner, RepoName: Repo,
		Repo: github.Repository{NodeID: "R_example", Visibility: "PRIVATE"},
	}
}

// details returns the item's details as GitHub reads them by ID.
func (w *World) details(it *Item, repo bool) github.ItemDetails {
	d := github.ItemDetails{Comments: it.Comments, Commits: 1}

	for _, l := range it.Labels {
		d.Labels = append(d.Labels, github.Label{Name: l, Color: "#ededed"})
	}

	if !it.Issue {
		d.HeadOID, d.BaseRef, d.Mergeable, d.MergeState = it.Head, "main", "MERGEABLE", github.MergeStateClean
		d.Checks = github.Checks{State: it.Checks, Passed: it.Passed, Failed: it.Failed, Pending: it.Pending}
	}

	if repo {
		d.Repo = &github.DetailsRepo{Owner: Owner, Name: Repo, Repo: github.Repository{NodeID: "R_example", Visibility: "PRIVATE"}}
	}

	return d
}

// byNode returns the item with a node ID.
func (w *World) byNode(id string) (*Item, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "NODE_"))
	if err != nil {
		return nil, false
	}

	it, ok := w.items[n]

	return it, ok
}

// numbers returns the items' numbers in order. The caller holds mu.
func (w *World) numbers() []int {
	out := make([]int, 0, len(w.items))
	for n := range w.items {
		out = append(out, n)
	}

	slices.Sort(out)

	return out
}
