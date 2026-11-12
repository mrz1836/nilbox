package worktrees

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
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

	// launched reports a worktree the app launched a session in, whose record names the
	// one item exactly.
	launched bool
}

// link fills in the item of each worktree made for a pull request or issue, and returns
// why GitHub could not be asked, or "". A record names the item exactly; otherwise the
// worktree's name, or its branch, gives the kind and number, and the clone's GitHub
// remotes the repositories it may be in. One request reads them all.
//
// remotes maps each repository's Path to its remotes (remote name to repository), and
// recorded holds the live records by their worktree's resolved path.
func (s *Service) link(ctx context.Context, repos []Repo, remotes map[string]map[string]repo.Name,
	recorded map[string]worklog.Record, tracker *listProgress,
) string {
	if s.github == nil {
		return ""
	}

	targets := linkTargets(repos, remotes, recorded)
	if len(targets) == 0 {
		return ""
	}

	refs := uniqueRefs(targets)
	tracker.Start(event.WorktreesAskingGitHub, len(refs))

	states, err := s.github.ItemStates(ctx, refs)
	tracker.Add(len(refs))

	if err != nil {
		err = fmt.Errorf("could not read the pull requests and issues from GitHub: %w", err)
		s.logger.DebugContext(ctx, "worktrees are listed without their pull requests and issues", "error", err)

		return uierr.Message(err)
	}

	for _, t := range targets {
		t.wt.Item = pickItem(t, states)
	}

	return ""
}

// linkTargets lists the worktrees of repos that may belong to a pull request or issue.
func linkTargets(repos []Repo, remotes map[string]map[string]repo.Name, recorded map[string]worklog.Record) []linkTarget {
	var targets []linkTarget

	for r, wt := range rows(repos) {
		if t, ok := linkTargetFor(wt, remotes[r.Path], recorded); ok {
			targets = append(targets, t)
		}
	}

	return targets
}

// linkTargetFor works out which items a worktree may be for: the one its record names, or
// for a worktree the app or Claude Code made, or one on an item's own branch, the one its
// name or branch names.
func linkTargetFor(wt *Worktree, remotes map[string]repo.Name, recorded map[string]worklog.Record) (linkTarget, bool) {
	if wt.Main {
		return linkTarget{}, false
	}

	if r, ok := recorded[paths.Resolved(wt.Path)]; ok {
		ref := item.Ref{Repo: repo.Name{Owner: r.Owner, Repo: r.Name}, Number: r.Number, Kind: r.Kind}

		return linkTarget{wt: wt, kind: r.Kind, refs: []item.Ref{ref}, launched: r.Launches > 0}, true
	}

	kind, number, ok := worktreeItem(wt)
	if !ok {
		return linkTarget{}, false
	}

	refs := remoteRefs(remotes, kind, number)
	if len(refs) == 0 {
		return linkTarget{}, false
	}

	return linkTarget{wt: wt, kind: kind, refs: refs}, true
}

// worktreeItem reads the kind and number of the item an unrecorded worktree is for: from
// its directory's name when the app or Claude Code made it, else from its branch when it
// is an item's own, worktree-pr-<n> or worktree-issue-<n>.
func worktreeItem(wt *Worktree) (item.Kind, int, bool) {
	if wt.Managed {
		if kind, number, ok := item.ParseWorktreeName(filepath.Base(wt.Path)); ok {
			return kind, number, true
		}
	}

	if name, ok := strings.CutPrefix(wt.Branch, ownBranchPrefix); ok && !wt.Detached {
		return item.ParseWorktreeName(name)
	}

	return "", 0, false
}

// remoteRefs lists item number in each GitHub repository the clone has a remote for:
// origin's first, then the others by remote name, each repository once.
func remoteRefs(remotes map[string]repo.Name, kind item.Kind, number int) []item.Ref {
	names := slices.SortedFunc(maps.Keys(remotes), func(a, b string) int {
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
		URL: state.URL, HeadOID: state.HeadOID, HeadRef: state.HeadRef, Launched: t.launched,
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

// assess works out a linked worktree's stage and whether it may be removed. GitHub
// decides when the work is done; whether removing the worktree is safe rests on git and
// the running processes alone: no uncommitted changes, every commit on GitHub, no lock,
// and no session running in it. A worktree whose status couldn't be read (Error), as one
// whose clone's or own config gitread's guards refuse, is never offered a removal, which
// runs a status of its own there. sessionsKnown reports that the running processes could
// be listed: without them a force removal, which discards changes, is not offered.
func assess(wt *Worktree, sessionsKnown bool) {
	if wt.Main {
		return
	}

	free := !wt.Locked && !wt.Prunable && len(wt.InUse) == 0
	wt.CanRemove = free && !wt.Dirty && wt.Error == ""
	wt.CanForceRemove = free && wt.Dirty && sessionsKnown

	wt.OnGitHub = wt.Unpushed == 0 || headIsItemHead(wt)
	wt.Stage = stageOf(wt)
	wt.CanCleanUp = wt.Stage == StageFinished
}

// headIsItemHead reports a worktree whose HEAD is its pull request's head commit.
func headIsItemHead(wt *Worktree) bool {
	return wt.Item != nil && wt.Item.HeadOID != "" && wt.Item.HeadOID == wt.Head
}

// stageOf is a linked worktree's stage. A worktree a running process works in is in use;
// Prune clears a worktree whose directory is gone, and one with no item, or locked by
// hand, has no stage.
func stageOf(wt *Worktree) Stage {
	switch {
	case len(wt.InUse) > 0 && !wt.Prunable:
		return StageInUse
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
// request merged, or its issue closed. A worktree the app did not launch a session in
// counts only when it is linked by commit too, its HEAD being the merged pull request's
// head, so a name or branch that only looks like an item's is never enough.
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
