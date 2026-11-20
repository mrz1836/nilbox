package describeit

import (
	"context"
)

// The plugin's half of the service, until it is built: each method says what it does, and
// refuses for now. The bound API is fixed here, so the window builds against it.

// Proposals returns the cards from the user's own Claude Code, each checked against the
// live snapshot, the newest first: those with a part no one dealt with, and those settled
// in the last day.
func (s *Service) Proposals(_ context.Context) ([]Card, error) {
	return []Card{}, notBuilt()
}

// DiscardProposal discards every part of the card ref the user hasn't dealt with yet.
func (s *Service) DiscardProposal(_ context.Context, _ CardRef) error {
	return notBuilt()
}

// Plugins returns, for each of Claude Code's profiles, whether the plugin is installed
// there, read from Claude Code's own list of its plugins.
func (s *Service) Plugins(_ context.Context) ([]PluginState, error) {
	return []PluginState{}, notBuilt()
}

// InstallPlugin installs the plugin in the profile's Claude Code: the plugin's folder
// written from this build (agent.PluginFiles), the link it runs pointed at this app, and
// Claude Code's own install run with the profile's environment. It returns every
// profile's state. An app that doesn't run from a stable place (a build from source, or
// one macOS translocated) can't point the link at itself, and says so.
func (s *Service) InstallPlugin(_ context.Context, _ string) ([]PluginState, error) {
	return []PluginState{}, notBuilt()
}

// RemovePlugin uninstalls the plugin from the profile's Claude Code, and removes its
// marketplace there, and returns every profile's state.
func (s *Service) RemovePlugin(_ context.Context, _ string) ([]PluginState, error) {
	return []PluginState{}, notBuilt()
}

// preparePlugin keeps the plugin current as the app starts: its folder rewritten when this
// build's differs, so an installed plugin loads this build's words at its next session
// (the spike's S9), and its link pointed at this app when it runs from a stable place.
func (s *Service) preparePlugin(_ context.Context) {}

// followOutside follows the outside proposals folder until ctx ends, telling the window
// (describeit:proposals) when a proposal comes or goes.
func (s *Service) followOutside(ctx context.Context) {
	<-ctx.Done()
}
