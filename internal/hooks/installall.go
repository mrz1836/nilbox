package hooks

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// installAllSlack is how much longer than a hooks run's own limit Install and update all
// waits for one to end.
const installAllSlack = 30 * time.Second

// InstallLine is one clone in Install and update all's result.
type InstallLine struct {
	// Name is what the page calls the clone, and Dir its folder, with ~.
	Name string `json:"name"`
	Dir  string `json:"dir"`

	// Action is what ran, or "" for a clone skipped; RunID is its run, or 0.
	Action Action `json:"action"`
	RunID  int64  `json:"runId"`

	// Outcome is what it did, or OutcomeSkipped, and Message says so, or why it was
	// skipped.
	Outcome ActionOutcome `json:"outcome"`
	Message string        `json:"message"`
}

// InstallAllResult is Install and update all's result: one line per clone, in the order
// asked, and a sentence that sums them up.
type InstallAllResult struct {
	Lines   []InstallLine `json:"lines"`
	Message string        `json:"message"`
}

// InstallAll installs or updates go-pre-commit's hook in each clone of dirs (folders as
// the page lists them, with ~), one at a time, each as its own run in the app, and returns
// a line per clone once all have ended. It never replaces another tool's hook, never
// touches a shared hooks folder, does a folder that two clones resolve to once, and skips
// a clone that is busy, turned off, current already, or without a config.
func (s *Service) InstallAll(ctx context.Context, dirs []string) (InstallAllResult, error) {
	if s.runs == nil {
		return InstallAllResult{}, ErrNotSetUp
	}

	tool := s.tool(ctx)
	if !tool.Usable() {
		return InstallAllResult{}, noToolError(tool)
	}

	out := InstallAllResult{Lines: []InstallLine{}}
	if len(dirs) == 0 {
		out.Message = installAllMessage(out.Lines)

		return out, nil
	}

	// The clone root and the clones under it are read once for every clone asked.
	l, err := s.folders().List(ctx)
	if err != nil {
		return InstallAllResult{}, err
	}

	done := map[string]string{} // hooks folders already done, and the clone that did each
	seen := map[string]bool{}

	for _, shown := range dirs {
		if seen[shown] {
			continue
		}

		seen[shown] = true

		out.Lines = append(out.Lines, s.installOne(ctx, shown, l, tool, done))
	}

	out.Message = installAllMessage(out.Lines)

	return out, nil
}

// installOne installs or updates the hook in the clone shown, unless it is one to skip,
// and waits for the run to end.
func (s *Service) installOne(ctx context.Context, shown string, l localrepos.Listing, tool ToolFacts, done map[string]string,
) InstallLine {
	line := InstallLine{Name: filepath.Base(shown), Dir: shown, Outcome: OutcomeSkipped}

	c, err := s.cloneIn(ctx, shown, l)
	if err != nil {
		line.Message = uierr.Message(err)

		return line
	}

	line.Name, line.Dir = display(c.name, c.dir), paths.Tilde(c.dir, s.home)

	action, hooks, why := s.planInstall(ctx, c, tool, done)
	if action == "" {
		line.Message = why

		return line
	}

	results := make(chan ActionResult, 1)

	spec, err := s.actionSpec(ctx, action, c, "", integrationprefs.RunsInApp, func(r ActionResult) { results <- r })
	if err == nil {
		line.RunID, err = s.runs.Start(ctx, spec)
	}

	switch {
	case errors.Is(err, clonelock.ErrBusy):
		line.Message = uierr.Message(err)

		return line
	case err != nil:
		line.Action, line.Outcome, line.Message = action, OutcomeFailed, uierr.Message(err)

		return line
	}

	line.Action = action
	done[hooks] = line.Name

	select {
	case r := <-results:
		line.Outcome, line.Message = r.Outcome, r.Message
	case <-ctx.Done():
		line.Outcome, line.Message = OutcomeUnverified, "Stopped waiting for it to end."
	case <-time.After(scriptrun.KindHooks.Timeout() + installAllSlack):
		line.Outcome, line.Message = OutcomeUnverified, "It didn't end in time."
	}

	return line
}

// planInstall works out what Install and update all does in c: install, update, or
// reinstall, and in which hooks folder, or why it skips it.
func (s *Service) planInstall(ctx context.Context, c clone, tool ToolFacts, done map[string]string,
) (action Action, hooks, why string) {
	l := s.look(ctx, c.dir)
	if why = s.skipReason(c, l); why != "" {
		return "", "", why
	}

	hooks = paths.Resolved(l.paths.Hooks)
	if by, ok := done[hooks]; ok {
		return "", "", "It shares its hooks folder with " + by + ", done just now."
	}

	switch state, _ := s.judge(ctx, l.hook, tool); state {
	case StateMissing:
		return ActionInstall, hooks, ""
	case StateOutdated:
		return ActionUpdate, hooks, ""
	case StateNotExecutable:
		return ActionReinstall, hooks, ""
	case StateUnknown, StateUnreadable:
		return "", "", "go-pre-commit couldn't say whether its hook is current."
	case StateReady, StateOther:
	}

	return "", "", "Its hook is current already."
}

// skipReason says why Install and update all leaves c, whose hooks are l, as it is: no
// config, turned off, hooks it can't read, a shared hooks folder, or another tool's hook;
// "" when nothing does.
func (s *Service) skipReason(c clone, l clonesHooks) string {
	cfg := readConfig(c.dir)

	switch {
	case !cfg.present:
		return "It has no go-pre-commit config."
	case !cfg.enabled():
		return "go-pre-commit is turned off in this repository."
	case l.err != nil:
		return uierr.Message(l.err)
	case l.shared:
		return "Its hooks folder, " + paths.Tilde(l.paths.Hooks, s.home) + ", is shared, so it's left as it is."
	case l.hook.err != nil:
		return uierr.Message(uierr.New("Couldn't read its pre-commit hook.", l.hook.err))
	case l.hook.present && !l.hook.ours:
		return textutil.Capitalize(owner(l.hook.tool)) + " hook is there, so it's left as it is."
	default:
		return ""
	}
}

// installAllMessage sums up Install and update all.
func installAllMessage(lines []InstallLine) string {
	count := map[ActionOutcome]int{}
	for _, l := range lines {
		count[l.Outcome]++
	}

	var parts []string

	for _, p := range []struct {
		outcome ActionOutcome
		word    string
	}{
		{OutcomeInstalled, "installed"},
		{OutcomeUpdated, "updated"},
		{OutcomeCurrent, "already current"},
		{OutcomeBlocked, "blocked by another tool's hook"},
		{OutcomeFailed, "failed"},
		{OutcomeUnverified, "unverified"},
		{OutcomeSkipped, "skipped"},
	} {
		if n := count[p.outcome]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, p.word))
		}
	}

	if len(parts) == 0 {
		return "Nothing to install or update."
	}

	return textutil.Capitalize(strings.Join(parts, ", ")) + "."
}
