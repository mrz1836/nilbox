package core

import (
	"context"
	"os"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

// worktreeRunner returns the runner the worktree service runs git with: search's
// execpath.Exec with git's terminal prompts off, so a fetch that needs a password fails at
// once rather than waiting for an answer no one can give.
func worktreeRunner(search execpath.Search) execpath.Exec {
	return execpath.Exec{Search: search, Env: shellenv.With(os.Environ(), "GIT_TERMINAL_PROMPT", "0")}
}

// sweepSettings is the part of the settings the worktree service reads.
func sweepSettings(s settings.Settings) worktrees.SweepSettings {
	return worktrees.SweepSettings{
		CloneRoot: s.CloneRoot, CloneLocations: s.CloneLocations, AutoCleanup: s.WorktreeAutoCleanup, Branches: s.Branches,
	}
}

// itemStates reads the state of the pull requests and issues worktrees are linked to,
// with the GitHub CLI's token, for the worktree service.
type itemStates struct {
	tokens github.Tokens
	client *github.Client
}

// ItemStates asks GitHub about refs. A token GitHub refuses is borrowed again once.
func (s itemStates) ItemStates(ctx context.Context, refs []item.Ref) (map[string]worktrees.ItemState, error) {
	states, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (map[string]github.ItemState, error) {
		return s.client.FetchItemStates(ctx, tok, refs)
	})
	if err != nil {
		return nil, err
	}

	out := make(map[string]worktrees.ItemState, len(states))
	for key, state := range states {
		out[key] = worktreeState(state)
	}

	return out, nil
}

// MergedPullRequests asks GitHub which branch names were the head of a merged pull
// request. A token GitHub refuses is borrowed again once.
func (s itemStates) MergedPullRequests(ctx context.Context, queries []worktrees.BranchQuery,
) (map[worktrees.BranchKey][]worktrees.MergedPR, error) {
	asks := make([]github.BranchQuery, 0, len(queries))
	for _, q := range queries {
		asks = append(asks, github.BranchQuery{Repo: q.Repo, Branches: q.Branches})
	}

	answers, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (map[string][]github.MergedPR, error) {
		return s.client.FetchMergedPullRequests(ctx, tok, asks)
	})
	if err != nil {
		return nil, err
	}

	out := map[worktrees.BranchKey][]worktrees.MergedPR{}

	for _, q := range queries {
		for _, branch := range q.Branches {
			if prs := answers[github.MergedKey(q.Repo, branch)]; len(prs) > 0 {
				out[worktrees.BranchKey{Repo: q.Repo.Key(), Branch: branch}] = worktreeMerged(prs)
			}
		}
	}

	return out, nil
}

// worktreeMerged converts GitHub's merged pull requests for the worktree service.
func worktreeMerged(prs []github.MergedPR) []worktrees.MergedPR {
	out := make([]worktrees.MergedPR, 0, len(prs))
	for _, pr := range prs {
		out = append(out, worktrees.MergedPR{
			Number: pr.Number, Title: pr.Title, URL: pr.URL, MergedAt: pr.MergedAt, HeadOID: pr.HeadOID,
		})
	}

	return out
}

// worktreeState converts GitHub's answer about an item for the worktree service. An item
// GitHub reports neither merged nor closed is open.
func worktreeState(s github.ItemState) worktrees.ItemState {
	out := worktrees.ItemState{
		PullRequest: s.PullRequest, Title: s.Title, URL: s.URL, State: worktrees.ItemOpen, ClosedAt: s.ClosedAt,
		HeadOID: s.HeadOID, HeadRef: s.HeadRef,
	}

	switch s.State {
	case github.StateMerged:
		out.State = worktrees.ItemMerged
	case github.StateClosed:
		out.State = worktrees.ItemClosed
	}

	return out
}
