package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/settings"
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
	return settingsPart(ctx, v.settings, v.part)
}

// rootsOf builds the repository locations the settings name: the clone root and the other
// locations, with ~ for home, the others read through reader.
func rootsOf(s settings.Settings, home string, reader *localrepos.Reader) (localrepos.Roots, error) {
	return localrepos.NewRoots(s.CloneRoot, s.CloneLocations, home, localrepos.WithReader(reader))
}

// settingsPart reads the settings as they are now and returns the part of them pick takes,
// or T's zero value and the error when they can't be read.
func settingsPart[T any](ctx context.Context, s *settings.Service, pick func(settings.Settings) T) (T, error) {
	all, err := s.Get(ctx)
	if err != nil {
		var zero T

		return zero, err
	}

	return pick(all), nil
}

// replyFiller fills in a reply from the prompt library for an inbox item, with what a
// launch on the item would fill a prompt with. The review sheet and the issue sheet both
// fill their replies through it.
type replyFiller struct {
	store  *prompts.Store
	source prompts.PreviewSource
}

// fill fills in the reply with replyID for the inbox item with itemID, which must be a kind
// of item the reply applies to.
func (f replyFiller) fill(ctx context.Context, replyID, itemID int64) (string, error) {
	p, err := f.store.Lookup(ctx, replyID)
	if err != nil {
		return "", err
	}

	if !p.IsReply() {
		return "", fmt.Errorf("%w: %q", prompts.ErrNotReply, p.Nickname)
	}

	tmpl, err := p.Template()
	if err != nil {
		return "", err
	}

	data, err := f.source.PreviewData(ctx, itemID)
	if err != nil {
		return "", err
	}

	kind := item.KindIssue
	if data.IsPR {
		kind = item.KindPR
	}

	if !p.AppliesTo.Covers(kind) {
		return "", fmt.Errorf("%w: %q doesn't apply to %s", prompts.ErrWrongKind, p.Nickname, kind.Plural())
	}

	out, err := tmpl.Execute(data)
	if err != nil {
		return "", err
	}

	return strings.TrimRight(out, "\n"), nil
}
