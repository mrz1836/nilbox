package worktrees

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// originRemote is the remote a clone was cloned from; it is tried first.
const originRemote = "origin"

// linkTarget is a worktree that may belong to a pull request or issue.
type linkTarget struct {
	wt *Worktree

	// kind is the kind of item it is for.
	kind item.Kind

	// refs are the items it may be, most likely first.
	refs []item.Ref

	// launched reports a launch record, which names the one item exactly.
	launched bool
}

// launchKey identifies a recorded launch by its worktree's directory.
func launchKey(clonePath, worktree string) string {
	return resolvedPath(filepath.Join(clonePath, ".claude", "worktrees", worktree))
}

// link fills in the item of each worktree the agent made for a pull request or issue,
// and returns why GitHub could not be asked, or "". A launch record names the item
// exactly; otherwise the worktree's name gives the kind and number, and the clone's
// GitHub remotes the repositories it may be in. One request reads them all.
//
// remotes maps each repository's Path to its remotes (remote name to repository).
func (s *Service) link(ctx context.Context, repos []Repo, remotes map[string]map[string]repo.Name,
	launches []worklog.Launch,
) string {
	if s.github == nil {
		return ""
	}

	targets := linkTargets(repos, remotes, launches)
	if len(targets) == 0 {
		return ""
	}

	states, err := s.github.ItemStates(ctx, uniqueRefs(targets))
	if err != nil {
		return "Could not read the pull requests and issues from GitHub: " + err.Error()
	}

	for _, t := range targets {
		t.wt.Item = pickItem(t, states)
	}

	return ""
}

// linkTargets lists the worktrees of repos that may belong to a pull request or issue.
func linkTargets(repos []Repo, remotes map[string]map[string]repo.Name, launches []worklog.Launch) []linkTarget {
	recorded := make(map[string]worklog.Launch, len(launches))
	for _, l := range launches {
		recorded[launchKey(l.ClonePath, l.Worktree)] = l
	}

	var targets []linkTarget

	for r, wt := range rows(repos) {
		if t, ok := linkTargetFor(wt, remotes[r.Path], recorded); ok {
			targets = append(targets, t)
		}
	}

	return targets
}

// linkTargetFor works out which items a worktree may be for. Only a worktree the agent
// made (under .claude/worktrees) is linked, and only when a launch record or its name
// says which item.
func linkTargetFor(wt *Worktree, remotes map[string]repo.Name, recorded map[string]worklog.Launch) (linkTarget, bool) {
	if wt.Main || !wt.ClaudeCreated {
		return linkTarget{}, false
	}

	if l, ok := recorded[resolvedPath(wt.Path)]; ok {
		ref := item.Ref{Repo: repo.Name{Owner: l.Owner, Repo: l.Name}, Number: l.Number, Kind: l.Kind}

		return linkTarget{wt: wt, kind: l.Kind, refs: []item.Ref{ref}, launched: true}, true
	}

	kind, number, ok := item.ParseWorktreeName(filepath.Base(wt.Path))
	if !ok {
		return linkTarget{}, false
	}

	refs := remoteRefs(remotes, kind, number)
	if len(refs) == 0 {
		return linkTarget{}, false
	}

	return linkTarget{wt: wt, kind: kind, refs: refs}, true
}

// remoteRefs lists item number in each GitHub repository the clone has a remote for:
// origin's first, then the others by remote name, each repository once.
func remoteRefs(remotes map[string]repo.Name, kind item.Kind, number int) []item.Ref {
	names := make([]string, 0, len(remotes))
	for name := range remotes {
		names = append(names, name)
	}

	slices.SortFunc(names, func(a, b string) int {
		switch {
		case a == originRemote:
			return -1
		case b == originRemote:
			return 1
		default:
			return strings.Compare(a, b)
		}
	})

	refs := make([]item.Ref, 0, len(names))

	for _, name := range names {
		refs = appendRef(refs, item.Ref{Repo: remotes[name], Number: number, Kind: kind})
	}

	return refs
}

// uniqueRefs lists every ref the targets may be, each once.
func uniqueRefs(targets []linkTarget) []item.Ref {
	var refs []item.Ref

	for _, t := range targets {
		for _, ref := range t.refs {
			refs = appendRef(refs, ref)
		}
	}

	return refs
}

// appendRef adds ref to refs unless one of them is the same item, whatever its casing.
func appendRef(refs []item.Ref, ref item.Ref) []item.Ref {
	key := ref.Key()

	if slices.ContainsFunc(refs, func(r item.Ref) bool { return r.Key() == key }) {
		return refs
	}

	return append(refs, ref)
}

// pickItem chooses the target's item among GitHub's answers: of the right kind, and for a
// pull request preferably the one whose head is the worktree's HEAD; otherwise the most
// likely. It is nil when GitHub knows none of them.
func pickItem(t linkTarget, states map[string]ItemState) *LinkedItem {
	var chosen *LinkedItem

	for _, ref := range t.refs {
		state, ok := states[ref.Key()]
		if !ok || state.PullRequest != (t.kind == item.KindPR) {
			continue
		}

		linked := linkedItem(t, ref, state)

		if t.kind == item.KindPR && state.HeadOID != "" && state.HeadOID == t.wt.Head {
			return linked
		}

		if chosen == nil {
			chosen = linked
		}
	}

	return chosen
}

// linkedItem converts GitHub's answer for ref.
func linkedItem(t linkTarget, ref item.Ref, state ItemState) *LinkedItem {
	linked := &LinkedItem{
		Kind: t.kind, Owner: ref.Repo.Owner, Name: ref.Repo.Repo, Number: ref.Number, Title: state.Title,
		URL: state.URL, HeadOID: state.HeadOID, Launched: t.launched,
	}

	switch state.State {
	case ItemMerged, ItemClosed:
		linked.State = state.State
	case ItemOpen:
		linked.State = ItemOpen
	default:
		// GitHub's answer reads as open unless it says merged or closed.
		linked.State = ItemOpen
	}

	if linked.State != ItemOpen && !state.ClosedAt.IsZero() {
		linked.ClosedAt = clock.Format(state.ClosedAt)
	}

	return linked
}

// assess works out a linked worktree's stage and whether CleanUp may remove it. GitHub
// decides when the work is done; whether removing the worktree is safe rests on git
// alone: no uncommitted changes, every commit on GitHub, and no session running in it.
func assess(wt *Worktree) {
	if wt.Main {
		return
	}

	wt.OnGitHub = wt.Unpushed == 0 || headIsItemHead(wt)
	wt.Stage = stageOf(wt)
	wt.CanCleanUp = wt.Stage == StageFinished
}

// headIsItemHead reports a worktree whose HEAD is its pull request's head commit.
func headIsItemHead(wt *Worktree) bool {
	return wt.Item != nil && wt.Item.HeadOID != "" && wt.Item.HeadOID == wt.Head
}

// stageOf is a linked worktree's stage. Prune clears a worktree whose directory is gone,
// and one with no item, or locked by hand, has no stage.
func stageOf(wt *Worktree) Stage {
	switch {
	case wt.Locked && !wt.StaleLock:
		return lockedStage(wt)
	case wt.Prunable, wt.Item == nil:
		return StageNone
	case !wt.Item.done():
		return StageActive
	case !wt.Dirty && wt.Error == "" && wt.OnGitHub:
		return StageFinished
	default:
		return StageNeedsReview
	}
}

// lockedStage is the stage of a worktree under a live lock: in use when an agent session
// holds it, and none when it was locked by hand.
func lockedStage(wt *Worktree) Stage {
	if wt.AgentLock {
		return StageInUse
	}

	return StageNone
}

// autoCleanable reports a finished worktree the automatic sweep may remove: its pull
// request merged, or its issue closed. A worktree the app did not launch counts only when
// it is linked by commit too, its HEAD being the merged pull request's head, so a name that
// only looks like an item's is never enough.
func autoCleanable(wt Worktree) bool {
	it := wt.Item

	switch {
	case !wt.CanCleanUp || it == nil:
		return false
	case it.Kind == item.KindPR:
		return it.State == ItemMerged && (it.Launched || it.HeadOID == wt.Head)
	default:
		return it.State == ItemClosed && it.Launched
	}
}

// itemName names an item for a result, e.g. "acme-org/example-repo#184", or is "".
func itemName(it *LinkedItem) string {
	if it == nil {
		return ""
	}

	return repo.Name{Owner: it.Owner, Repo: it.Name}.Ref(it.Number)
}
