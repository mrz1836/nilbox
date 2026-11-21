package sim

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/item"
)

// The fakes of what holds an agent's start back beyond the engine's own limits: this Mac's
// readiness (Ready), what the app's other parts say right before a start (Holds), where
// agents may run on outside contributors' items (Outside), and the prompt library a
// step's prompt is checked against again before a start (Library). A scenario sets them
// before Begin; Needs your OK notices go to Notices, as the problems do.

// Ready is a fake readiness checker: every need is ok, but those Miss names, which are
// problems until Fix.
type Ready struct {
	mu      sync.Mutex
	missing map[string]bool
	checks  int
}

// NewReady returns a checker with nothing missing.
func NewReady() *Ready {
	return &Ready{missing: map[string]bool{}}
}

// Miss makes the needs with ids problems.
func (r *Ready) Miss(ids ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, id := range ids {
		r.missing[id] = true
	}
}

// Fix makes the needs with ids ok again.
func (r *Ready) Fix(ids ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, id := range ids {
		delete(r.missing, id)
	}
}

// Checks counts the checks run.
func (r *Ready) Checks() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.checks
}

// Check checks each need.
func (r *Ready) Check(_ context.Context, needs []autopilot.Need, _ bool) ([]autopilot.ReadyCheck, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.checks++

	out := make([]autopilot.ReadyCheck, 0, len(needs))
	for _, n := range needs {
		c := autopilot.ReadyCheck{
			Need: n, ID: n.ID(), Label: n.ID(), Level: autopilot.ReadyOK, Found: "found",
			CheckedAt: time.Now(),
		}
		if r.missing[n.ID()] {
			c.Level, c.Found, c.Fix, c.Command = autopilot.ReadyProblem, "is missing", "fix "+n.ID(), true
		}

		out = append(out, c)
	}

	return out, nil
}

// Defaults are Claude Code's default profile.
func (*Ready) Defaults(context.Context) (autopilot.LaunchDefaults, error) {
	return autopilot.LaunchDefaults{Agent: "claude", Profiles: map[string]string{}}, nil
}

// Holds is the fake of what the app's other parts say right before a start: nothing,
// until Set.
type Holds struct {
	mu    sync.Mutex
	hold  autopilot.Blocker
	words string
	asked []autopilot.Job
}

// Set has every start held on b, saying words; Set("", "") lets them through.
func (h *Holds) Set(b autopilot.Blocker, words string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.hold, h.words = b, words
}

// Asked returns the jobs it was asked of.
func (h *Holds) Asked() []autopilot.Job {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.asked)
}

// Hold says what holds job back.
func (h *Holds) Hold(_ context.Context, job autopilot.Job) (autopilot.Blocker, string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.asked = append(h.asked, job)

	return h.hold, h.words, nil
}

// Outside is the fake launch defaults' Autopilot on outside contributors' items: by org
// or repository, a repository's own over its org's.
type Outside struct {
	mu    sync.Mutex
	rules map[string]bool
}

// Allow sets whether agents run on outside contributors' items of target, an org or a
// repository.
func (o *Outside) Allow(target string, on bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.rules == nil {
		o.rules = map[string]bool{}
	}

	o.rules[strings.ToLower(target)] = on
}

// Outside returns the rules as they are now.
func (o *Outside) Outside(context.Context) (autopilot.OutsideIndex, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	rules := make(map[string]bool, len(o.rules))
	maps.Copy(rules, o.rules)

	return outsideIndex(rules), nil
}

// outsideIndex answers by a snapshot of the rules.
type outsideIndex map[string]bool

// Allowed reports whether repo's outside contributors' items take agents.
func (idx outsideIndex) Allowed(repo string) (bool, string) {
	if on, ok := idx[strings.ToLower(repo)]; ok {
		return on, "repo"
	}

	owner, _, _ := strings.Cut(repo, "/")
	if on, ok := idx[strings.ToLower(owner)]; ok {
		return on, "org"
	}

	return false, ""
}

// Library is the fake prompt library a step's prompt is checked against: the starters
// fix-checks and fix-dependency-security, for pull requests, until Remove, and the
// replies a scenario puts in (Put).
type Library struct {
	mu      sync.Mutex
	removed map[string]bool
	put     []autopilot.LibraryPrompt
}

// Put sets the other prompts the library holds, such as a GitHub step's replies, in place
// of those put before.
func (l *Library) Put(prompts ...autopilot.LibraryPrompt) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.put = slices.Clone(prompts)
}

// Remove takes the starter key's prompt out of the library.
func (l *Library) Remove(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.removed == nil {
		l.removed = map[string]bool{}
	}

	l.removed[key] = true
}

// Prompts returns the library.
func (l *Library) Prompts(context.Context) ([]autopilot.LibraryPrompt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	var out []autopilot.LibraryPrompt

	for _, key := range []string{"fix-checks", "fix-dependency-security"} {
		if !l.removed[key] {
			out = append(out, autopilot.LibraryPrompt{Starter: key, Nickname: key, Kinds: prKinds()})
		}
	}

	return append(out, l.put...), nil
}

// prKinds is pull requests alone.
func prKinds() []item.Kind {
	return []item.Kind{item.KindPR}
}

// MyScripts is the fake of the user's own Repo scripts, which a scenario saves and edits.
type MyScripts struct {
	mu   sync.Mutex
	list []autopilot.MyScript
}

// Set saves the user's scripts as list, as the user saving or editing them does.
func (m *MyScripts) Set(list ...autopilot.MyScript) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.list = slices.Clone(list)
}

// MyScripts returns the user's scripts.
func (m *MyScripts) MyScripts(context.Context) ([]autopilot.MyScript, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Clone(m.list), nil
}

// config adds the fakes a scenario set to the Service's and the engine's configs; one not
// set is left out, so a scenario that sets none runs as before.
func (s *Sim) config(svc *autopilot.Config, eng *autopilot.EngineConfig) {
	if s.MyScripts != nil {
		svc.MyScripts = s.MyScripts
	}

	if s.Scripts != nil {
		eng.Scripts = s.Scripts
	}

	if s.Ready != nil {
		svc.Readiness = s.Ready
	}

	if s.Library != nil {
		svc.Library = s.Library
	}

	if s.Holds != nil {
		eng.Holds = s.Holds
	}

	if s.Outside != nil {
		eng.Outside = s.Outside
	}

	if s.TellWaits {
		eng.NeedsYou = s.Notices
	}
}
