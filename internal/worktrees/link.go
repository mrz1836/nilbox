package worktrees

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/launchlog"
)

// Item kinds, as LinkedItem.Kind reports them.
const (
	kindPR    = launchlog.KindPR
	kindIssue = launchlog.KindIssue
)

// originRemote is the remote a clone was cloned from; it is tried first.
const originRemote = "origin"

// itemNamePattern is the name the launcher and the agent give a worktree for a pull
// request or issue: pr-<n> or issue-<n>.
var itemNamePattern = regexp.MustCompile(`^(pr|issue)-([1-9][0-9]{0,9})$`)

// linkTarget is a worktree that may belong to a pull request or issue.
type linkTarget struct {
	wt *Worktree

	// kind is kindPR or kindIssue.
	kind string

	// refs are the items it may be, most likely first.
	refs []github.ItemRef

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
// remotes maps each repository's Path to its remotes (remote name to "owner/name").
func (s *Service) link(ctx context.Context, repos []Repo, remotes map[string]map[string]string,
	launches []launchlog.Launch,
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
func linkTargets(repos []Repo, remotes map[string]map[string]string, launches []launchlog.Launch) []linkTarget {
	recorded := make(map[string]launchlog.Launch, len(launches))
	for _, l := range launches {
		recorded[launchKey(l.ClonePath, l.Worktree)] = l
	}

	var targets []linkTarget

	for repo, wt := range rows(repos) {
		if t, ok := linkTargetFor(wt, remotes[repo.Path], recorded); ok {
			targets = append(targets, t)
		}
	}

	return targets
}

// linkTargetFor works out which items a worktree may be for. Only a worktree the agent
// made (under .claude/worktrees) is linked, and only when a launch record or its name
// says which item.
func linkTargetFor(wt *Worktree, remotes map[string]string, recorded map[string]launchlog.Launch) (linkTarget, bool) {
	if wt.Main || !wt.ClaudeCreated {
		return linkTarget{}, false
	}

	if l, ok := recorded[resolvedPath(wt.Path)]; ok {
		ref := github.ItemRef{Owner: strings.ToLower(l.Owner), Name: strings.ToLower(l.Name), Number: l.Number}

		return linkTarget{wt: wt, kind: l.Kind, refs: []github.ItemRef{ref}, launched: true}, true
	}

	m := itemNamePattern.FindStringSubmatch(filepath.Base(wt.Path))
	if m == nil {
		return linkTarget{}, false
	}

	number, err := strconv.Atoi(m[2])
	if err != nil {
		return linkTarget{}, false
	}

	refs := remoteRefs(remotes, number)
	if len(refs) == 0 {
		return linkTarget{}, false
	}

	return linkTarget{wt: wt, kind: m[1], refs: refs}, true
}

// remoteRefs lists item number in each GitHub repository the clone has a remote for:
// origin's first, then the others by remote name, each repository once.
func remoteRefs(remotes map[string]string, number int) []github.ItemRef {
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

	refs := make([]github.ItemRef, 0, len(names))

	for _, name := range names {
		owner, repo, ok := strings.Cut(remotes[name], "/")
		if !ok || owner == "" || repo == "" {
			continue
		}

		ref := github.ItemRef{Owner: owner, Name: repo, Number: number}
		if !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}

	return refs
}

// uniqueRefs lists every ref the targets may be, each once.
func uniqueRefs(targets []linkTarget) []github.ItemRef {
	var refs []github.ItemRef

	for _, t := range targets {
		for _, ref := range t.refs {
			if !slices.Contains(refs, ref) {
				refs = append(refs, ref)
			}
		}
	}

	return refs
}

// pickItem chooses the target's item among GitHub's answers: of the right kind, and for a
// pull request preferably the one whose head is the worktree's HEAD; otherwise the most
// likely. It is nil when GitHub knows none of them.
func pickItem(t linkTarget, states map[github.ItemRef]github.ItemState) *LinkedItem {
	var chosen *LinkedItem

	for _, ref := range t.refs {
		state, ok := states[ref]
		if !ok || state.PullRequest != (t.kind == kindPR) {
			continue
		}

		item := linkedItem(t, ref, state)

		if t.kind == kindPR && state.HeadOID != "" && state.HeadOID == t.wt.Head {
			return item
		}

		if chosen == nil {
			chosen = item
		}
	}

	return chosen
}

// linkedItem converts GitHub's answer for ref.
func linkedItem(t linkTarget, ref github.ItemRef, state github.ItemState) *LinkedItem {
	item := &LinkedItem{
		Kind: t.kind, Owner: ref.Owner, Name: ref.Name, Number: ref.Number, Title: state.Title, URL: state.URL,
		HeadOID: state.HeadOID, Launched: t.launched,
	}

	switch state.State {
	case github.StateMerged:
		item.State = ItemMerged
	case github.StateClosed:
		item.State = ItemClosed
	default:
		item.State = ItemOpen
	}

	if item.State != ItemOpen && !state.ClosedAt.IsZero() {
		item.ClosedAt = state.ClosedAt.UTC().Format(time.RFC3339)
	}

	return item
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
func stageOf(wt *Worktree) string {
	switch {
	case wt.Locked && !wt.StaleLock:
		return lockedStage(wt)
	case wt.Prunable, wt.Item == nil:
		return ""
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
func lockedStage(wt *Worktree) string {
	if wt.AgentLock {
		return StageInUse
	}

	return ""
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
	case it.Kind == kindPR:
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

	return it.Owner + "/" + it.Name + "#" + strconv.Itoa(it.Number)
}
