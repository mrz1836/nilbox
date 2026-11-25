package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/periodic"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/scripts"
)

// autopilotDeps are what Autopilot's service is built over: the inbox's store, whose last
// refresh says the account signed in, and the inbox service, which the builder reads
// through; the prompt library, which names the starters; the copy of the routines kept
// beside the database; what a refresh's searches cost, for a test's estimate; and the
// sheets' services the GitHub step goes through.
type autopilotDeps struct {
	inbox   *inbox.Store
	service *inbox.Routines
	prompts *prompts.Store
	file    *autopilot.File
	costs   autopilot.Costs
	github  autopilot.GitHubActions

	// ready checks what the routines need on this Mac, and agents previews their agent
	// steps through the launcher, background previews those in the background.
	ready      *readyChecks
	agents     agentDeps
	background *backgroundAgents

	// signals suggest the gallery's templates, transfer carries an export's prompts, and
	// profiles lists what an import maps a routine's profiles to; core fills each in once
	// what it reads is built.
	signals  *templateSignals
	transfer *routinePromptTransfer
	profiles *routineProfiles

	// scripts are the user's own Repo scripts, which a script step picks from.
	scripts *scripts.Service
}

// newAutopilot builds Autopilot's service over pool and deps.
func newAutopilot(pool *sql.DB, opts Options, deps autopilotDeps, logger *slog.Logger) *autopilot.Service {
	cfg := autopilot.Config{
		Pool: pool, Prefs: autopilotPrefs{prefs: prefsstore.New(pool, opts.Clock)}, Accounts: deps.inbox,
		Names: starterNames{prompts: deps.prompts}, Library: promptLibrary{prompts: deps.prompts}, File: deps.file,
		Clock: opts.Clock, Logger: logger,
		Items: autopilotItems{store: deps.inbox, inbox: deps.service}, Costs: deps.costs, GitHub: deps.github,
	}

	if deps.ready != nil {
		cfg.Readiness = deps.ready
	}

	if deps.agents.launch != nil {
		cfg.Previews = runPreviews{deps: deps.agents, background: deps.background}
	}

	if deps.signals != nil {
		cfg.Signals = deps.signals
	}

	if deps.transfer != nil {
		cfg.Transfer = deps.transfer
	}

	if deps.profiles != nil {
		cfg.Profiles = deps.profiles
	}

	cfg.App = opts.Version

	if deps.scripts != nil {
		cfg.MyScripts = myScripts{routines: scripts.NewRoutines(deps.scripts)}
	}

	return autopilot.NewService(cfg)
}

// autopilotWatches gives the inbox the routines' searches (inbox.WatchSource): the
// snapshot Autopilot's service keeps, built again first when the account signed in
// changed since. Core fills routines in once Autopilot's service is built, before the
// scheduler starts.
type autopilotWatches struct {
	searches *autopilot.Searches
}

// HoldsWatches reports whether this copy of the app reads the routines' searches: only
// the copy that holds Autopilot's engine does (inbox.WatchHolder).
func (w *autopilotWatches) HoldsWatches() bool {
	return w.searches == nil || w.searches.HoldsWatches()
}

// Watches returns the routines' searches for the account login, and true: built again
// under ctx first when the snapshot is another account's, and none before Autopilot's
// service is built. A rebuild that still leaves another account's snapshot (ctx ended, a
// read failed, or the account changed again) can't answer for login: it hands back none
// of that account's searches, and false, so the inbox skips the pass rather than drop
// every search's rows and last answer.
func (w *autopilotWatches) Watches(ctx context.Context, login string) ([]inbox.Watch, bool) {
	if w.searches == nil {
		return nil, true
	}

	set := w.searches.Current()
	if !strings.EqualFold(set.Login, login) {
		set = w.searches.Rebuild(ctx)
		if !strings.EqualFold(set.Login, login) {
			return nil, false
		}
	}

	out := make([]inbox.Watch, 0, len(set.Searches))
	for _, s := range set.Searches {
		out = append(out, inbox.Watch{Key: s.Key, Query: s.Query, Follow: s.Follow})
	}

	return out, true
}

// autopilotItems reads the inbox for Autopilot's builder (autopilot.Items).
type autopilotItems struct {
	store *inbox.Store
	inbox *inbox.Routines
}

// Suggestions returns what the stored items hold that the builder suggests.
func (a autopilotItems) Suggestions(ctx context.Context) (autopilot.Suggestions, error) {
	s, err := a.store.Suggest(ctx)

	return autopilot.Suggestions{Authors: s.Authors, Bots: s.Bots, Owners: s.Owners, Labels: s.Labels, Checks: s.Checks}, err
}

// WatchedItems returns the stored items the lists name or the searches by key return.
func (a autopilotItems) WatchedItems(ctx context.Context, keys []string, lists []catalog.ListSource) ([]int64, error) {
	return a.store.WatchedItems(ctx, keys, inbox.Lists{
		ReviewRequested: slices.Contains(lists, catalog.SourceReviewRequested),
		Authored:        slices.Contains(lists, catalog.SourceAuthored),
		Assigned:        slices.Contains(lists, catalog.SourceAssigned),
	})
}

// Facts returns the facts of the stored items among ids, as of now.
func (a autopilotItems) Facts(ctx context.Context, ids []int64, now time.Time) ([]facts.Facts, error) {
	rows, err := a.store.FactsRows(ctx, ids)
	if err != nil {
		return nil, err
	}

	env, err := a.store.FactsEnv(ctx, now)
	if err != nil {
		return nil, err
	}

	out := make([]facts.Facts, 0, len(rows))
	for _, r := range rows {
		out = append(out, facts.Build(r, env))
	}

	return out, nil
}

// KnownOwners returns every owner of a repository the inbox has seen, in lower case.
func (a autopilotItems) KnownOwners(ctx context.Context) (map[string]bool, error) {
	return a.store.KnownOwners(ctx)
}

// SearchNow reads queries from GitHub now, storing what they return, or sends nothing
// while the GraphQL budget is low (autopilot.ErrLowBudget).
func (a autopilotItems) SearchNow(ctx context.Context, queries []string) (map[string]autopilot.SearchResult, int, error) {
	got, points, err := a.inbox.SearchNow(ctx, queries)

	switch {
	case errors.Is(err, inbox.ErrLowBudget):
		return nil, 0, fmt.Errorf("%w: %w", autopilot.ErrLowBudget, err)
	case err != nil:
		return nil, 0, err
	}

	out := make(map[string]autopilot.SearchResult, len(got))
	for q, o := range got {
		out[q] = autopilot.SearchResult{IDs: o.IDs, Count: o.Count, Truncated: o.Truncated, Err: o.Err}
	}

	return out, points, nil
}

// ReadChecks reads and stores the check names of the pull requests among ids, and
// returns what that cost in GraphQL points, or sends nothing while the GraphQL budget is
// low (autopilot.ErrLowBudget).
func (a autopilotItems) ReadChecks(ctx context.Context, ids []int64) (int, error) {
	points, err := a.inbox.ReadChecks(ctx, ids)
	if errors.Is(err, inbox.ErrLowBudget) {
		return 0, fmt.Errorf("%w: %w", autopilot.ErrLowBudget, err)
	}

	return points, err
}

// watchCosts estimates what routine searches add to a refresh (autopilot.Costs): as many
// requests as the refresh packs them into, each at what the last watch pass cost a
// request (a point before one ran), at the inbox's refresh pace, as a share of the GraphQL
// budget as last read (5,000 before it was).
type watchCosts struct {
	inbox  *inbox.Routines
	every  periodic.Every
	budget *budget.Service
}

// Estimate returns what searches that returned counts results each last add.
func (c watchCosts) Estimate(ctx context.Context, counts []int) (autopilot.Estimate, error) {
	requests := github.WatchRequests(counts)

	perRequest := 1.0
	if pass, ok := c.inbox.LastWatchPass(); ok && pass.Requests > 0 {
		perRequest = float64(pass.Points) / float64(pass.Requests)
	}

	points := int(math.Ceil(float64(requests) * perRequest))

	perHour := 0
	if every := c.every(ctx); every > 0 {
		perHour = points * int(time.Hour/every)
	}

	limit := c.budget.Gauge(ctx).GraphQL.Limit
	if limit <= 0 {
		limit = defaultGraphQLLimit
	}

	return autopilot.Estimate{
		Requests: requests, PointsPerRefresh: points, PointsPerHour: perHour,
		Percent: float64(perHour) * 100 / float64(limit),
	}, nil
}

// defaultGraphQLLimit is GitHub's hourly GraphQL budget for a user's token.
const defaultGraphQLLimit = 5000

// stateNoter is the GitHub client ghthreads reads the state of the user's notifications'
// items through, which tells the inbox each state it read, as the inbox's own reads do.
type stateNoter struct {
	*github.Client

	store  *inbox.Store
	logger *slog.Logger
}

// FetchItemStates reads the states of refs, and records them in the inbox in the order
// the request went out.
func (n stateNoter) FetchItemStates(ctx context.Context, tok github.Token, refs []item.Ref,
) (map[string]github.ItemState, error) {
	read := startRead(n.store)

	states, err := n.Client.FetchItemStates(ctx, tok, refs)
	if err == nil {
		noteStates(ctx, n.store, read, refs, states, n.logger)
	}

	return states, err
}

// startRead numbers a read of items' states going out now in the inbox's write order; the
// zero StateRead, which records nothing, without a store.
func startRead(store *inbox.Store) inbox.StateRead {
	if store == nil {
		return inbox.StateRead{}
	}

	return store.StartRead()
}

// noteStates records in the inbox the states another service read, in the read numbered
// read, logging a failure: the inbox's next read records them anyway.
func noteStates(ctx context.Context, store *inbox.Store, read inbox.StateRead, refs []item.Ref,
	states map[string]github.ItemState, logger *slog.Logger,
) {
	if store == nil {
		return
	}

	if err := store.NoteStates(ctx, read, refs, states); err != nil {
		logger.DebugContext(ctx, "could not record the items' states in the inbox", "error", err)
	}
}

// autopilotPrefs reads Autopilot's settings from the prefs, which settings writes
// (autopilot.Prefs).
type autopilotPrefs struct {
	prefs *prefsstore.Store
}

// Autopilot returns Autopilot's settings as stored, each one missing or refused as its
// default.
func (p autopilotPrefs) Autopilot(ctx context.Context) (autopilotprefs.Config, error) {
	stored, err := p.prefs.GetMany(ctx, autopilotprefs.Keys()...)
	if err != nil {
		return autopilotprefs.Config{}, fmt.Errorf("read Autopilot's settings: %w", err)
	}

	return autopilotprefs.Decode(stored), nil
}

// starterNames names each starter prompt by the nickname of the user's copy of it, for a
// routine's sentence (autopilot.Names).
type starterNames struct {
	prompts *prompts.Store
}

// StarterNames returns each starter's key with the nickname of the user's copy of it; a
// starter the user deleted is left out.
func (n starterNames) StarterNames(ctx context.Context) (map[string]string, error) {
	all, err := n.prompts.All(ctx)
	if err != nil {
		return nil, err
	}

	out := map[string]string{}

	for _, p := range all {
		if p.StarterKey != "" {
			out[p.StarterKey] = p.Nickname
		}
	}

	return out, nil
}

// promptLibrary lists the prompt library for the builder's checks of the prompts a
// routine's steps name (autopilot.Library).
type promptLibrary struct {
	prompts *prompts.Store
}

// Prompts returns every prompt of the library, as a routine names it.
func (l promptLibrary) Prompts(ctx context.Context) ([]autopilot.LibraryPrompt, error) {
	all, err := l.prompts.All(ctx)
	if err != nil {
		return nil, err
	}

	return libraryPromptsOf(all), nil
}

// libraryPromptsOf returns the library's prompts all, in their order, as a routine names
// them: by UUID, starter, or nickname, with what each is for.
func libraryPromptsOf(all []prompts.Prompt) []autopilot.LibraryPrompt {
	out := make([]autopilot.LibraryPrompt, 0, len(all))

	for _, p := range all {
		kinds := make([]item.Kind, 0, len(item.Kinds()))
		for _, k := range item.Kinds() {
			if p.AppliesTo.Covers(k) {
				kinds = append(kinds, k)
			}
		}

		out = append(out, autopilot.LibraryPrompt{
			UUID: p.UUID, Starter: p.StarterKey, Nickname: p.Nickname, Reply: p.Purpose == prompts.PurposeReply,
			ReplyFor: autopilot.ReplyFor(p.ReplyAction), Kinds: kinds,
		})
	}

	return out
}

// autopilotEngine is what Autopilot's engine is built over beside its service: the inbox's
// store and service, for its reads and the read right before a start; the GitHub client
// and token source, for the states of the items that left a watch; the lock in the data
// folder; and the notifications, which its notices and problems are told through.
type autopilotEngine struct {
	store   *inbox.Store
	inbox   *inbox.Service
	faces   *inbox.Routines
	tokens  *github.TokenSource
	client  *github.Client
	lock    string
	events  event.Sink
	logger  *slog.Logger
	service *autopilot.Service
	notices *notify.Dispatcher

	// agents launches the agent steps, ready checks what they need on this Mac, and
	// defaults says where agents may run on outside contributors' items.
	agents   agentDeps
	ready    *readyChecks
	defaults *launchdefaults.Store
	prefs    autopilot.Prefs

	// background runs the agent steps in the background, and hands those in the
	// terminal to the terminal's runner; it also keeps what the window reads of the runs,
	// and clears their logs.
	background *backgroundAgents

	// scripts runs the script steps, in the app, through Repo scripts' own run, which
	// history stops and reads; waker is where their ends go, once the engine is built.
	scripts *scripts.Service
	history *scriptrun.Service
	waker   *engineWaker
}

// newAutopilotEngine builds the engine that runs the routines. Each part registers its own
// dispatcher as it is built: the inbox's triage runs the triage steps (Triage, which the
// engine registers as the triage dispatcher), the notices run Notify me and Tell me
// through the notifications, which also tell each problem once and each match that comes
// to wait for the user, the agent runner launches an agent step in the user's terminal
// through the launcher, exactly as a click, once the holds on this Mac let it, and the
// GitHub step goes through the sheets' own services (githubSteps, which the Service holds
// and the engine registers as the GitHub dispatcher); until a part's is, the Service keeps
// a routine with one of its steps in Dry run, so the engine records what would run.
func newAutopilotEngine(deps autopilotEngine) *autopilot.Engine {
	cfg := autopilot.EngineConfig{
		Items: engineItems{
			autopilotItems: autopilotItems{store: deps.store, inbox: deps.faces}, service: deps.inbox,
			tokens: deps.tokens, client: deps.client, logger: deps.logger,
		},
		Triage: inboxTriage{faces: deps.faces},
		Lock:   autopilot.NewLock(deps.lock), Events: deps.events,
	}

	if deps.notices != nil {
		sink := autopilotNotices{dispatcher: deps.notices}
		cfg.Dispatchers = map[catalog.Dispatcher]autopilot.Dispatcher{catalog.DispatchNotices: autopilot.NewNotices(sink)}
		cfg.Problems = autopilot.NewProblemNotices(sink)
		cfg.NeedsYou, cfg.Finished, cfg.Checks = sink, sink, sink
	}

	if deps.agents.launch != nil {
		cfg.Runner = agentRuns{deps: deps.agents}
		cfg.Holds = agentHolds{deps: deps.agents, checks: deps.ready, prefs: deps.prefs}
	}

	if b := deps.background; b != nil {
		if deps.agents.launch != nil {
			b.terminal = agentRuns{deps: deps.agents}
		}

		cfg.Runner, cfg.Views, cfg.RunLogs = b, b, b
	}

	if deps.defaults != nil {
		cfg.Outside = outsideRules{defaults: deps.defaults}
	}

	if deps.scripts != nil && deps.history != nil && deps.agents.launch != nil {
		cfg.Scripts = newScriptSteps(deps.scripts, deps.agents.launch, deps.history, deps.waker)
	}

	return autopilot.NewEngine(deps.service, cfg)
}

// inboxTriage runs a routine's triage steps, and their Undo, through the inbox's own triage
// (autopilot.Triage): Pin, Snooze, Dismiss, and Restore, marked with the routine's ID, each
// recorded as Autopilot's effect in the triage's own transaction.
type inboxTriage struct {
	faces *inbox.Routines
}

// Apply runs req.
func (t inboxTriage) Apply(ctx context.Context, req autopilot.TriageRequest,
	record func(ctx context.Context, tx *sql.Tx, change autopilot.TriageChange) error,
) (autopilot.TriageChange, error) {
	got, err := t.faces.Triage(ctx, inbox.RoutineTriage{
		Routine: req.RoutineID, Item: req.ItemID, Action: inbox.TriageAction(req.Action), Days: req.Days,
		Expect: req.Expect,
		Record: func(ctx context.Context, tx *sql.Tx, c inbox.TriageChange) error {
			return record(ctx, tx, triageChangeOf(c))
		},
	})

	return triageChangeOf(got), err
}

// Undo puts back what undos changed, where their items are still as they left them.
func (t inboxTriage) Undo(ctx context.Context, undos []autopilot.TriageUndo,
	record func(ctx context.Context, tx *sql.Tx, put []bool) error,
) ([]bool, error) {
	return t.faces.UndoTriage(ctx, inboxUndos(undos), record)
}

// AsLeft reports which of undos' items are still as they left them.
func (t inboxTriage) AsLeft(ctx context.Context, undos []autopilot.TriageUndo) ([]bool, error) {
	return t.faces.AsLeft(ctx, inboxUndos(undos))
}

// triageChangeOf is the inbox's change in Autopilot's words, the same on both sides.
func triageChangeOf(c inbox.TriageChange) autopilot.TriageChange {
	return autopilot.TriageChange{Before: c.Before, After: c.After, Kept: autopilot.TriageKept(c.Kept)}
}

// inboxUndos is undos in the inbox's words.
func inboxUndos(undos []autopilot.TriageUndo) []inbox.TriageUndo {
	out := make([]inbox.TriageUndo, 0, len(undos))
	for _, u := range undos {
		out = append(out, inbox.TriageUndo{
			Routine: u.RoutineID, Item: u.ItemID, Action: inbox.TriageAction(u.Action), Before: u.Before, After: u.After,
		})
	}

	return out
}

// engineItems reads the inbox for Autopilot's engine (autopilot.EngineItems).
type engineItems struct {
	autopilotItems

	service *inbox.Service
	tokens  *github.TokenSource
	client  *github.Client
	logger  *slog.Logger
}

// ItemStates returns what is stored of each item's state.
func (a engineItems) ItemStates(ctx context.Context, ids []int64) (map[int64]autopilot.ItemState, error) {
	got, err := a.store.ItemStates(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := make(map[int64]autopilot.ItemState, len(got))
	for id, st := range got {
		out[id] = autopilot.ItemState{State: st.State, FetchedAt: st.FetchedAt, Listed: st.Listed, Watched: st.Watched}
	}

	return out, nil
}

// InboxStatus returns the account signed in, the last refresh, and the searches' answers.
func (a engineItems) InboxStatus(ctx context.Context) (autopilot.InboxStatus, error) {
	st, err := a.store.ReadStatus(ctx)
	if err != nil {
		return autopilot.InboxStatus{}, err
	}

	out := autopilot.InboxStatus{
		Viewer: st.Viewer, LastRefresh: st.LastRefresh, WatchedAt: st.WatchedAt, WatchPaused: st.Paused,
		SignedOut: st.SignedOut,
		Searches:  make(map[string]autopilot.SearchState, len(st.Searches)),
	}

	for key, s := range st.Searches {
		out.Searches[key] = autopilot.SearchState{
			Answered: s.Answered, At: s.At, Error: s.Error, Truncated: s.Truncated, Partial: s.Partial,
		}
	}

	return out, nil
}

// ReadStates reads the states of the stored items among ids from GitHub, one request for
// Autopilot, and records them in the inbox.
func (a engineItems) ReadStates(ctx context.Context, ids []int64) error {
	got, err := a.store.ItemStates(ctx, ids)
	if err != nil || len(got) == 0 {
		return err
	}

	refs := make([]item.Ref, 0, len(got))
	for _, st := range got {
		refs = append(refs, st.Ref)
	}

	ctx = github.WithPurpose(ctx, github.PurposeAutopilot)
	noter := stateNoter{Client: a.client, store: a.store, logger: a.logger}

	_, err = github.WithToken(ctx, a.tokens, func(tok github.Token) (map[string]github.ItemState, error) {
		return noter.FetchItemStates(ctx, tok, refs)
	})

	return err
}

// LocalStates returns the stored items' triage on this Mac, as the inbox encodes it.
func (a engineItems) LocalStates(ctx context.Context, ids []int64) (map[int64]string, error) {
	return a.inbox.LocalStates(ctx, ids)
}

// ReadItem reads one item again from GitHub, as its own refresh does.
func (a engineItems) ReadItem(ctx context.Context, id int64) error {
	_, err := a.service.RefreshItem(github.WithPurpose(ctx, github.PurposeAutopilot), id)

	return err
}

// WatchNow sends the routines' searches now.
func (a engineItems) WatchNow(ctx context.Context) error {
	return a.inbox.WatchNow(ctx)
}

// engineWaker wakes Autopilot's engine once it is built: the inbox is built before it, and
// tells it of the items it stored, and of what it saw happen to items.
type engineWaker struct {
	engine *autopilot.Engine
}

// Built returns the engine, nil until it is built.
func (w *engineWaker) Built() *autopilot.Engine {
	if w == nil {
		return nil
	}

	return w.engine
}

// ItemsChanged wakes the engine, once there is one.
func (w *engineWaker) ItemsChanged(ids []int64) {
	if w.engine != nil {
		w.engine.ItemsChanged(ids)
	}
}

// KeptItems returns the items the ledger refers to, which the inbox's prune keeps.
func (w *engineWaker) KeptItems(ctx context.Context) ([]int64, error) {
	if w.engine == nil {
		return nil, nil
	}

	return w.engine.KeptItems(ctx)
}

// KeptMarks returns the items whose routine's Restore the engine still offers to Undo,
// whose marks the inbox's prune keeps (inbox.MarkKeeper); none before there is an engine.
func (w *engineWaker) KeptMarks(ctx context.Context) ([]int64, error) {
	if w.engine == nil {
		return nil, nil
	}

	return w.engine.KeptMarks(ctx)
}

// Wake wakes the engine, once there is one, as a change of Autopilot's settings does.
func (w *engineWaker) Wake() {
	if w.engine != nil {
		w.engine.Wake()
	}
}

// outcomesFanOut tells what the user finished from the inbox, and the items the inbox saw
// close or leave its lists, both to Autopilot's engine and to the mirror of the user's
// GitHub notifications (inbox.OutcomeSink). The engine hears too what the user's own
// triage took back of a routine's, which leaves the routine hands off the item, so it is
// told first: it records that at once, never waiting on the mirror, whose record can wait
// on a clear on its way to GitHub.
//
// The agent sessions hear what came of their items on GitHub (sessionOutcomes), which only
// the sessions panel reads, and the sheets last what the inbox's own edits changed of the
// items they keep (sheetOutcomes).
type outcomesFanOut struct {
	threads  inbox.OutcomeSink
	engine   engineOutcomes
	sessions inbox.OutcomeSink
	sheets   inbox.OutcomeSink
}

// engineOutcomes is how the fan-out tells Autopilot's engine; *engineWaker is it.
type engineOutcomes interface {
	Outcomes(ctx context.Context, outcomes []autopilot.Outcome)
}

// Settle tells the engine, then the mirror, then the sessions, then the sheets.
func (o outcomesFanOut) Settle(ctx context.Context, outcomes []inbox.Outcome) {
	if o.engine != nil {
		o.engine.Outcomes(ctx, toEngineOutcomes(outcomes))
	}

	if o.threads != nil {
		o.threads.Settle(ctx, outcomes)
	}

	if o.sessions != nil {
		o.sessions.Settle(ctx, outcomes)
	}

	if o.sheets != nil {
		o.sheets.Settle(ctx, outcomes)
	}
}

// Outcomes tells the engine, once there is one, what the inbox saw.
func (w *engineWaker) Outcomes(ctx context.Context, outcomes []autopilot.Outcome) {
	if w.engine != nil {
		w.engine.Outcomes(ctx, outcomes)
	}
}

// toEngineOutcomes is outcomes in the engine's words, the same on both sides.
func toEngineOutcomes(outcomes []inbox.Outcome) []autopilot.Outcome {
	told := make([]autopilot.Outcome, 0, len(outcomes))

	for _, out := range outcomes {
		told = append(told, autopilot.Outcome{
			Kind: string(out.Kind), Ref: out.Ref, At: out.At,
			Reversed: autopilot.Reversal{
				RoutineID: out.Reversed.Routine, ItemID: out.Reversed.Item, Action: catalog.StepKind(out.Reversed.Did),
			},
		})
	}

	return told
}

// engineOnInbox passes the inbox's events on, and wakes Autopilot's engine when the inbox
// stored a change, as a whole refresh does, so the engine evaluates what it stored at once
// rather than at its next tick.
type engineOnInbox struct {
	sink   event.Sink
	engine *engineWaker
}

// Emit passes e on, waking the engine for an event.InboxUpdated.
func (o engineOnInbox) Emit(e event.Event) {
	event.Or(o.sink).Emit(e)

	if _, ok := e.(event.InboxUpdated); ok {
		o.engine.Wake()
	}
}
