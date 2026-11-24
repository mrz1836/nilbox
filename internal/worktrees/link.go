package worktrees

import (
	"context"
	"fmt"
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

// link fills in the item of each worktree made for a pull request or issue, or on a pushed
// branch a pull request heads, and returns why GitHub could not be asked, or "". A record
// names the item exactly; otherwise the worktree's name, or an item's own branch, gives the
// kind and number, and the clone's GitHub remotes the repositories it may be in. A worktree
// with none of these on a pushed branch is linked by that branch (see branchlink.go), and
// one whose commits were copied onto a pushed branch by that branch, or by its copy record
// once that branch is gone (see copyjudge.go), whose pull request the same request reads.
// One request reads them all, unless there are more than fifty of either.
//
// remotes maps each repository's Path to its remotes (remote name to repository), refs to
// its refs as read for this listing, and recorded holds the live records by their
// worktree's resolved path.
func (s *Service) link(ctx context.Context, repos []Repo, remotes map[string]map[string]repo.Name,
	refs map[string]cloneRefs, recorded map[string]worklog.Record, tracker *listProgress,
) string {
	if s.github == nil {
		return ""
	}

	targets := linkTargets(repos, remotes, recorded)
	heads := branchTargets(repos, remotes, refs, namedOf(targets))
	copied := copyTargets(repos, remotes, refs)
	vouched := recordTargets(repos, refs)
	all := slices.Concat(heads, copied)
	now := s.clock.Now()
	plan := s.heads.plan(all, now)

	items := plan.refs(uniqueRefs(targets), all)
	for _, t := range vouched {
		items = appendRef(items, t.ref)
	}

	if len(items) == 0 && len(plan.asks) == 0 {
		return ""
	}

	tracker.Start(event.WorktreesAskingGitHub, len(items)+len(plan.asks))

	answer, err := s.github.LinkStates(ctx, items, headQueries(plan.asks))
	tracker.Add(len(items) + len(plan.asks))

	if err != nil {
		err = fmt.Errorf("could not read the pull requests and issues from GitHub: %w", err)
		s.logger.DebugContext(ctx, "worktrees are listed without their pull requests and issues", "error", err)

		return uierr.Message(err)
	}

	for _, t := range targets {
		if t.wt.Item = pickItem(t, answer.Items); t.wt.Item != nil {
			t.wt.LinkedBy = LinkName
		}
	}

	prs := s.heads.resolveHeads(all, plan, answer, now)
	linkHeads(heads, prs, LinkBranch)
	linkHeads(fullyCopied(copied), prs, LinkCopy)
	copiedInto(copied, prs)
	linkRecords(vouched, answer.Items)

	return ""
}

// namedOf is the set of the worktrees targets name, which a name, a record, or an item's
// own branch links.
func namedOf(targets []linkTarget) map[*Worktree]bool {
	named := make(map[*Worktree]bool, len(targets))
	for _, t := range targets {
		named[t.wt] = true
	}

	return named
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
	names := rankedRemotes(remotes)

	refs := make([]item.Ref, 0, len(names))

	for _, name := range names {
		refs = appendRef(refs, item.Ref{Repo: remotes[name], Number: number, Kind: kind})
	}

	return refs
}

// remoteRank orders the remote name among a clone's remotes: origin before every other.
func remoteRank(name string) int {
	if name == originRemote {
		return 0
	}

	return 1
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
// and no session running in it. Files missing from its folder that HEAD has as they were
// are no loss (onlyMissing), so a worktree whose only changes they are counts as clean,
// but its removal needs --force (removeLossless), so only once the processes are listed.
// A worktree whose status couldn't be read (Error), as one whose clone's or own config
// gitread's guards refuse, is never offered a removal, which runs a status of its own
// there. sessionsKnown reports that the running processes could be listed: without them a
// force removal, which discards changes, is not offered.
func assess(wt *Worktree, sessionsKnown bool) {
	if wt.Main {
		return
	}

	free := !wt.Locked && !wt.Prunable && len(wt.InUse) == 0
	wt.CanRemove = free && wt.Error == "" && plainRemovable(wt, sessionsKnown)
	wt.CanForceRemove = free && forceRemovable(wt, sessionsKnown)

	wt.OnGitHub = onGitHubByID(wt) || wt.copies.onGitHub
	wt.Stage = stageOf(wt)
	wt.CanCleanUp = wt.Stage == StageFinished
}

// plainRemovable reports a worktree Remove takes, as far as its files go: a clean one, or
// one whose only changes are files missing from its folder, whose removal needs --force,
// so only while the processes are listed.
func plainRemovable(wt *Worktree, sessionsKnown bool) bool {
	return !wt.Dirty || (wt.onlyMissing() && sessionsKnown)
}

// forceRemovable reports a worktree with changes ForceRemove would discard, any but files
// missing from its folder alone, while the processes are listed.
func forceRemovable(wt *Worktree, sessionsKnown bool) bool {
	return !wt.nothingUncommitted() && sessionsKnown
}

// headIsItemHead reports a worktree whose HEAD is its pull request's head commit.
func headIsItemHead(wt *Worktree) bool {
	return wt.Item != nil && wt.Item.HeadOID != "" && wt.Item.HeadOID == wt.Head
}

// stageOf is a linked worktree's stage. A worktree a running process works in is in use;
// Prune clears a worktree whose directory is gone, and one with no item, or locked by
// hand, has no stage. Files missing from its folder alone keep no done worktree from being
// finished: removing it loses none of them.
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
	case wt.nothingUncommitted() && wt.Error == "" && wt.OnGitHub:
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
// head, so a name or branch that only looks like an item's is never enough. One linked by
// the pushed branch it is on, or by its commits' copies, counts only when the app or Claude
// Code made it (Managed), so a worktree the user made by hand is only ever cleaned up by
// hand. One whose work is on GitHub only as copies counts only once its copy record's pull
// request merged with every copy (copyState.merged), and, linked by those copies, only when
// that is the pull request it is linked to: never while it is open.
func autoCleanable(wt Worktree) bool {
	it := wt.Item

	switch {
	case !sweepMayTake(wt):
		return false
	case wt.LinkedBy == LinkCopy:
		return it.Kind == item.KindPR && it.State == ItemMerged && wt.copies.merged && isRecordPR(it, wt.copies.record)
	case it.Kind == item.KindPR:
		return it.State == ItemMerged && (it.Launched || it.HeadOID == wt.Head)
	default:
		return it.State == ItemClosed && it.Launched
	}
}

// sweepMayTake reports a finished worktree with an item that the sweep may look at at all:
// one linked by a pushed branch or by copies only when the app or Claude Code made it, and
// one whose work is on GitHub only as copies only once their pull request merged with them.
func sweepMayTake(wt Worktree) bool {
	switch {
	case !wt.CanCleanUp || wt.Item == nil:
		return false
	case (wt.LinkedBy == LinkBranch || wt.LinkedBy == LinkCopy) && !wt.Managed:
		return false
	default:
		return onGitHubByID(&wt) || wt.copies.merged
	}
}

// isRecordPR reports it as the pull request rec's copies went into.
func isRecordPR(it *LinkedItem, rec *worklog.Copy) bool {
	return rec != nil && it.Kind == item.KindPR && it.Number == rec.Number &&
		repo.Name{Owner: it.Owner, Repo: it.Name}.Key() == repo.Name{Owner: rec.Owner, Repo: rec.Name}.Key()
}

// itemName names an item for a result, e.g. "acme-org/example-repo#184", or is "".
func itemName(it *LinkedItem) string {
	if it == nil {
		return ""
	}

	return repo.Name{Owner: it.Owner, Repo: it.Name}.Ref(it.Number)
}
