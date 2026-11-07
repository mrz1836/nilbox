package core

import (
	"context"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

// settingsView hands a service the part of the settings it reads. Each Get reads the
// settings as they are now, so a change the user just made applies to the next launch
// or sweep.
type settingsView[T any] struct {
	settings *settings.Service
	part     func(settings.Settings) T
}

// Get reads the settings and returns the part.
func (v settingsView[T]) Get(ctx context.Context) (T, error) {
	all, err := v.settings.Get(ctx)
	if err != nil {
		var zero T

		return zero, err
	}

	return v.part(all), nil
}

// launchSettings is the part of the settings a launch reads.
func launchSettings(s settings.Settings) launcher.LaunchSettings {
	return launcher.LaunchSettings{
		DefaultProfile:  s.DefaultProfile,
		WorktreeDefault: s.WorktreeDefault,
		Terminal:        s.Terminal,
		CloneRoot:       s.CloneRoot,
		TrustedBots:     s.TrustedBots,
	}
}

// sweepSettings is the part of the settings the worktree service reads.
func sweepSettings(s settings.Settings) worktrees.SweepSettings {
	return worktrees.SweepSettings{CloneRoot: s.CloneRoot, AutoCleanup: s.WorktreeAutoCleanup}
}

// launchItems hands the launcher the inbox's stored items, the signed-in account, and
// the clone each repository resolved to. It implements launcher.ItemStore.
type launchItems struct {
	*inbox.Store
}

// LaunchItems returns the stored items among ids, by id.
func (s launchItems) LaunchItems(ctx context.Context, ids []int64) ([]launcher.StoredItem, error) {
	stored, err := s.Store.LaunchItems(ctx, ids)
	if err != nil {
		return nil, err
	}

	items := make([]launcher.StoredItem, 0, len(stored))

	// The two types have the same fields, so a conversion copies every one, and a field
	// added to one but not the other stops the build here.
	for _, it := range stored {
		items = append(items, launcher.StoredItem(it))
	}

	return items, nil
}

// itemStates reads the state of the pull requests and issues worktrees are linked to,
// with the GitHub CLI's token, for the worktree service.
type itemStates struct {
	tokens *github.TokenSource
	client *github.Client
}

// ItemStates asks GitHub about refs.
func (s itemStates) ItemStates(ctx context.Context, refs []item.Ref) (map[string]worktrees.ItemState, error) {
	tok, err := s.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}

	states, err := s.client.FetchItemStates(ctx, tok, refs)
	if err != nil {
		return nil, err
	}

	out := make(map[string]worktrees.ItemState, len(states))
	for key, state := range states {
		out[key] = worktreeState(state)
	}

	return out, nil
}

// worktreeState converts GitHub's answer about an item for the worktree service. An item
// GitHub reports neither merged nor closed is open.
func worktreeState(s github.ItemState) worktrees.ItemState {
	out := worktrees.ItemState{
		PullRequest: s.PullRequest, Title: s.Title, URL: s.URL, State: worktrees.ItemOpen, ClosedAt: s.ClosedAt,
		HeadOID: s.HeadOID,
	}

	switch s.State {
	case github.StateMerged:
		out.State = worktrees.ItemMerged
	case github.StateClosed:
		out.State = worktrees.ItemClosed
	}

	return out
}
