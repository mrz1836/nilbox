package core

import (
	"context"

	"github.com/mrz1836/nilbox/internal/localrepos"
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
// locations, with ~ for home.
func rootsOf(s settings.Settings, home string) (localrepos.Roots, error) {
	return localrepos.NewRoots(s.CloneRoot, nil, home)
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
