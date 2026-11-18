package pushwatch

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/repo"
)

// The Watcher's pace.
const (
	// DefaultTick is how often the refs of the pull requests watched are looked at while
	// any has a clone: a stat each.
	DefaultTick = 3 * time.Second

	// DefaultSettle is how long after its branch moved a pull request is read again, so
	// GitHub has taken in the push by then.
	DefaultSettle = 2 * time.Second

	// DefaultIndexEvery is how often the clones are listed again, for a clone added or
	// removed, or an origin changed: a stat or two each, and git only for a clone whose
	// config changed.
	DefaultIndexEvery = 5 * time.Minute

	// DefaultTargetsEvery is how often the pull requests watched, and the switch, are read
	// again when nothing woke the Watcher sooner: the inbox wakes it whenever it changes.
	DefaultTargetsEvery = time.Minute

	// MaxTargets is the most pull requests watched at once, the first the Targets list.
	MaxTargets = 200
)

// minWait is the shortest wait between two passes, so a pass that finds the next one due
// at once still yields.
const minWait = time.Millisecond

// retryAfter are the waits before each further read of a pull request whose branch moved,
// while GitHub still reports another head after the read before: three reads at most, and
// then the refresh catches it up.
func retryAfter() []time.Duration {
	return []time.Duration{10 * time.Second, 30 * time.Second}
}

// Target is a pull request the watch follows: an open pull request whose head branch is
// in its own repository.
type Target struct {
	// ItemID is the inbox item the Rereader reads again.
	ItemID int64

	// Repo is the pull request's repository, whose clones the watch looks in.
	Repo repo.Name

	// Branch is the head branch, followed as origin's in each clone.
	Branch string

	// Head is the head commit GitHub last reported, "" before it was read.
	Head string
}

// Targets lists the pull requests to watch, the ones a push most likely moves first. The
// inbox implements it, through core.
type Targets interface {
	PushTargets(ctx context.Context) ([]Target, error)
}

// Clones lists the folders of the user's clones, in the clone root and the other
// repository locations, reading no git. Core implements it over the settings.
type Clones interface {
	CloneDirs(ctx context.Context) ([]string, error)
}

// Origins reads which GitHub repository the origin of the clone at dir points at, and
// whether it is one. Core implements it with localrepos.Inspect.
type Origins interface {
	Origin(ctx context.Context, dir string) (repo.Name, bool, error)
}

// Switch reads whether the user watches for pushes. The settings service implements it.
type Switch interface {
	WatchPushes(ctx context.Context) (bool, error)
}

// Rereader reads an item again from GitHub. Reread must not block. The inbox's Rereads
// implements it.
type Rereader interface {
	Reread(itemID int64)
}

// Git runs an allowlisted, read-only git command in the clone at dir; *gitread.Git
// implements it. Only a clone that keeps its refs in reftable needs it.
type Git interface {
	Run(ctx context.Context, dir string, args ...string) (stdout, stderr []byte, err error)
}

// Config holds a Watcher's dependencies. Targets, Clones, Origins, and Rereader are
// required.
type Config struct {
	Targets  Targets
	Clones   Clones
	Origins  Origins
	Rereader Rereader

	// Switch is the user's switch; without it, the watch is always on.
	Switch Switch

	// Git reads a reftable clone's refs; without it, such a clone isn't watched.
	Git Git

	// Clock tells the time; it defaults to the system clock.
	Clock clock.Clock

	Logger *slog.Logger

	// Tick, Settle, IndexEvery, and TargetsEvery default to the Default constants.
	Tick         time.Duration
	Settle       time.Duration
	IndexEvery   time.Duration
	TargetsEvery time.Duration
}

// refKey names one ref the watch looks at: origin's branch in one common git folder.
type refKey struct {
	common string
	branch string
}

// refState is what the last look at a ref found: the file it was (its own, or a reftable
// clone's table list), and the commit it was at, "" while that is unknown.
type refState struct {
	sig    fileSig
	commit string
}

// watchedRef is one ref the watch looks at, the clone it is in, and the pull requests
// whose branch it is: two pull requests can share a head branch.
type watchedRef struct {
	key     refKey
	clone   clone
	targets []Target
}

// pending is a pull request whose branch moved to commit, which GitHub didn't report for
// it yet: it is read again once due, and reads counts the reads made since it moved.
type pending struct {
	commit string
	due    time.Time
	reads  int
}

// Watcher follows the branches of the inbox's pull requests in the user's clones, a
// desktop.Worker that runs while the app is open, and has a pull request read again once
// its branch moves to a commit GitHub hasn't reported for it.
type Watcher struct {
	targets  Targets
	clones   Clones
	origins  Origins
	reread   Rereader
	toggle   Switch
	git      Git
	clock    clock.Clock
	logger   *slog.Logger
	tick     time.Duration
	settle   time.Duration
	reindex  time.Duration
	retarget time.Duration

	wake chan struct{}

	// Only Run reads and writes the fields below.

	// watched are the pull requests read last, at readAt.
	watched []Target
	readAt  time.Time

	// index maps each repository to its clones.
	index *cloneIndex

	// plan is the refs to look at, worked out from watched and index, nil once either
	// changed.
	plan []watchedRef

	// refs holds what the last look at each planned ref found.
	refs map[refKey]*refState

	// tables holds, during one look, each reftable clone's table list, stat'ed once.
	tables map[string]fileSig

	// moved holds, by item, the pull requests waiting to be read again.
	moved map[int64]*pending
}

// NewWatcher returns a Watcher over cfg that has not started.
func NewWatcher(cfg Config) *Watcher {
	return &Watcher{
		targets: cfg.Targets, clones: cfg.Clones, origins: cfg.Origins, reread: cfg.Rereader, toggle: cfg.Switch,
		git: cfg.Git, clock: clock.Or(cfg.Clock), logger: logging.Or(cfg.Logger),
		tick: orDefault(cfg.Tick, DefaultTick), settle: orDefault(cfg.Settle, DefaultSettle),
		reindex: orDefault(cfg.IndexEvery, DefaultIndexEvery), retarget: orDefault(cfg.TargetsEvery, DefaultTargetsEvery),
		wake: make(chan struct{}, 1), index: newCloneIndex(), refs: map[refKey]*refState{},
		tables: map[string]fileSig{}, moved: map[int64]*pending{},
	}
}

// orDefault returns d, or fallback when d isn't positive.
func orDefault(d, fallback time.Duration) time.Duration {
	if d <= 0 {
		return fallback
	}

	return d
}

// Wake asks the Watcher to read the switch and the pull requests again now, as a change
// to the inbox or to the settings does. It never waits.
func (w *Watcher) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run watches until ctx ends: it reads the switch and the pull requests at once, and again
// whenever woken or once DefaultTargetsEvery passed, lists the clones, and then looks at
// the refs once a Tick while any pull request has a clone. While the switch is off, it
// looks at nothing and waits to be woken.
func (w *Watcher) Run(ctx context.Context) {
	woken := true

	for {
		var ok bool

		if woken, ok = w.wait(ctx, w.pass(ctx, woken)); !ok {
			return
		}
	}
}

// wait waits next, or to be woken, and reports whether it was woken, and false once ctx
// ends. A next of zero waits to be woken alone.
func (w *Watcher) wait(ctx context.Context, next time.Duration) (woken, ok bool) {
	var due <-chan time.Time

	if next > 0 {
		timer := time.NewTimer(next)
		defer timer.Stop()

		due = timer.C
	}

	select {
	case <-ctx.Done():
		return false, false
	case <-w.wake:
		return true, true
	case <-due:
		return false, true
	}
}

// pass makes one round, and returns how long to wait before the next, or zero to wait to
// be woken: the switch and the pull requests when woken or due, the clones when due, then
// a look at each ref and the reads that are due.
func (w *Watcher) pass(ctx context.Context, woken bool) time.Duration {
	now := w.clock.Now()

	if woken || !now.Before(w.readAt.Add(w.retarget)) {
		if !w.watching(ctx) {
			w.forget()

			return 0
		}

		w.readTargets(ctx, now)
	}

	if len(w.watched) == 0 {
		clear(w.refs)

		return max(w.readAt.Add(w.retarget).Sub(now), minWait)
	}

	if w.index.due(now, w.reindex) {
		w.index.rebuild(ctx, w.clones, w.origins, now, w.logger)
		w.plan = nil
	}

	looking := w.look(ctx, now)
	w.readMoved(ctx, now)

	return w.next(now, looking)
}

// watching reads the switch; a switch that can't be read counts as on, its default.
func (w *Watcher) watching(ctx context.Context) bool {
	if w.toggle == nil {
		return true
	}

	on, err := w.toggle.WatchPushes(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.WarnContext(ctx, "could not read whether to watch for pushes; watching", "error", err)
		}

		return true
	}

	return on
}

// forget lets go of everything the watch learned but the clones' origins, which hold while
// their config files do, so it starts afresh once on again.
func (w *Watcher) forget() {
	w.watched, w.readAt, w.plan = nil, time.Time{}, nil
	w.index.builtAt = time.Time{}

	clear(w.refs)
	clear(w.moved)
}

// readTargets reads the pull requests to watch, at now, keeping the first MaxTargets with a
// well-formed repository and branch; a list that can't be read keeps the last. A pull
// request waiting to be read that is no longer watched, or whose new commit GitHub now
// reports, waits no more.
func (w *Watcher) readTargets(ctx context.Context, now time.Time) {
	w.readAt = now

	got, err := w.targets.PushTargets(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.WarnContext(ctx, "could not read the pull requests to watch for pushes", "error", err)
		}

		return
	}

	got = slices.DeleteFunc(got, func(t Target) bool {
		return t.ItemID <= 0 || !t.Repo.Valid() || !gitread.ValidBranch(t.Branch)
	})
	w.watched, w.plan = got[:min(len(got), MaxTargets)], nil

	heads := make(map[int64]string, len(w.watched))
	for _, t := range w.watched {
		heads[t.ItemID] = t.Head
	}

	for id, p := range w.moved {
		if head, ok := heads[id]; !ok || strings.EqualFold(head, p.commit) {
			delete(w.moved, id)
		}
	}
}

// planRefs returns the refs to look at: origin's branch of each pull request watched, in
// each clone of its repository, worked out again once the pull requests or the clones
// changed, when what the refs no longer watched held is forgotten.
func (w *Watcher) planRefs() []watchedRef {
	if w.plan != nil {
		return w.plan
	}

	at := map[refKey]int{}
	plan := []watchedRef{}

	for _, t := range w.watched {
		for _, c := range w.index.of(t.Repo) {
			key := refKey{common: c.common, branch: t.Branch}

			i, ok := at[key]
			if !ok {
				i = len(plan)
				at[key] = i
				plan = append(plan, watchedRef{key: key, clone: c})
			}

			plan[i].targets = append(plan[i].targets, t)
		}
	}

	for key := range w.refs {
		if _, ok := at[key]; !ok {
			delete(w.refs, key)
		}
	}

	w.plan = plan

	return plan
}

// look looks at each planned ref, at now, and notes the pull requests of each one that
// moved. It reports whether there was any ref to look at.
func (w *Watcher) look(ctx context.Context, now time.Time) bool {
	plan := w.planRefs()
	clear(w.tables)

	for _, r := range plan {
		if ctx.Err() != nil {
			break
		}

		if commit, moved := w.observe(ctx, r); moved {
			for _, t := range r.targets {
				w.queueRead(ctx, t, commit, now)
			}
		}
	}

	return len(plan) > 0
}

// observe looks at one ref, and returns the commit it moved to since the last look, if it
// moved: never on the first look, which only learns where it is, nor when it can't be
// read now (packed away, or its branch deleted), which keeps the commit it was at. The
// ref is read only once its file changed: a loose ref's own, or a reftable clone's table
// list, stat'ed once a look.
func (w *Watcher) observe(ctx context.Context, r watchedRef) (string, bool) {
	var sig fileSig

	if r.clone.reftable {
		s, ok := w.tables[r.clone.common]
		if !ok {
			s = statFile(tablesPath(r.clone.common))
			w.tables[r.clone.common] = s
		}

		sig = s
	} else {
		sig = statFile(trackingPath(r.clone.common, r.key.branch))
	}

	last, seen := w.refs[r.key]
	if seen && last.sig == sig {
		return "", false
	}

	commit := ""

	switch {
	case !sig.exists:
	case r.clone.reftable:
		commit = readTable(ctx, w.git, r.clone.dir, r.key.branch)
	default:
		commit = readLoose(trackingPath(r.clone.common, r.key.branch))
	}

	if !seen {
		w.refs[r.key] = &refState{sig: sig, commit: commit}

		return "", false
	}

	last.sig = sig
	if commit == "" || strings.EqualFold(commit, last.commit) {
		return "", false
	}

	last.commit = commit

	return commit, true
}

// queueRead has the pull request t read again, Settle after now, its branch having moved to
// commit, unless GitHub reports that commit for it already, when nothing waits for it, or
// a read waits for that commit already.
func (w *Watcher) queueRead(ctx context.Context, t Target, commit string, now time.Time) {
	if strings.EqualFold(commit, t.Head) {
		delete(w.moved, t.ItemID)

		return
	}

	if p := w.moved[t.ItemID]; p != nil && strings.EqualFold(p.commit, commit) {
		return
	}

	w.moved[t.ItemID] = &pending{commit: commit, due: now.Add(w.settle)}
	w.logger.DebugContext(ctx, "a pull request's branch moved in a clone; reading it again", "item", t.ItemID,
		"repo", t.Repo.String(), "branch", t.Branch, "to", commit)
}

// readMoved hands the Rereader each pull request whose read is due, once the pull requests
// are read again so a head GitHub reported since counts: one no longer watched, or whose
// new commit GitHub reports now, waits no more. While GitHub still reports another head,
// the next read is due after the next of retryAfter's waits, and after the last read
// nothing waits.
func (w *Watcher) readMoved(ctx context.Context, now time.Time) {
	if !w.anyDue(now) {
		return
	}

	w.readTargets(ctx, now)

	retries := retryAfter()

	for id, p := range w.moved {
		if now.Before(p.due) {
			continue
		}

		w.reread.Reread(id)

		if p.reads >= len(retries) {
			delete(w.moved, id)

			continue
		}

		p.due = now.Add(retries[p.reads])
		p.reads++
	}
}

// anyDue reports whether a pull request waiting to be read again is due at now.
func (w *Watcher) anyDue(now time.Time) bool {
	for _, p := range w.moved {
		if !now.Before(p.due) {
			return true
		}
	}

	return false
}

// next returns how long to wait after a pass at now: until the pull requests are due to be
// read again, and sooner, a Tick, while there are refs to look at, or until the clones are
// due to be listed again while there are none; and no later than the first read due.
func (w *Watcher) next(now time.Time, looking bool) time.Duration {
	wait := w.readAt.Add(w.retarget).Sub(now)

	if looking {
		wait = min(wait, w.tick)
	} else {
		wait = min(wait, w.index.builtAt.Add(w.reindex).Sub(now))
	}

	for _, p := range w.moved {
		wait = min(wait, p.due.Sub(now))
	}

	return max(wait, minWait)
}
