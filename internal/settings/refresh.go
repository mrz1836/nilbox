package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/event"
)

// RefreshKind names one of the things the app reads again on its own, on a timer.
type RefreshKind string

// What the app reads again on its own.
const (
	// RefreshInbox is the whole inbox, from GitHub.
	RefreshInbox RefreshKind = "inbox"

	// RefreshChecks is the pull requests whose checks are running, from GitHub, all in one
	// request.
	RefreshChecks RefreshKind = "checks"

	// RefreshWorktrees is the worktrees' sweep: each clone's worktrees, the pull requests
	// and issues they belong to, and the automatic cleanup.
	RefreshWorktrees RefreshKind = "worktrees"

	// RefreshNotifications is the user's GitHub notifications, listed from GitHub: one
	// conditional request, free when nothing changed, and never sooner than GitHub's own
	// poll interval asks.
	RefreshNotifications RefreshKind = "notifications"
)

// The keys the refresh intervals are stored under, in seconds.
const (
	KeyRefreshInbox         = "refresh.inbox"
	KeyRefreshChecks        = "refresh.checks"
	KeyRefreshWorktrees     = "refresh.worktrees"
	KeyRefreshNotifications = "refresh.notifications"
)

// The default refresh intervals, in seconds.
const (
	DefaultRefreshInbox         = 300
	DefaultRefreshChecks        = 45
	DefaultRefreshWorktrees     = 300
	DefaultRefreshNotifications = 300
)

// ErrInvalidRefresh is returned for a refresh kind, or an interval, that isn't one of the
// choices.
var ErrInvalidRefresh = errors.New("unknown refresh interval")

// RefreshIntervals are how often, in seconds, the app reads each thing again on its own.
// The checks' 0 is off.
type RefreshIntervals struct {
	Inbox         int `json:"inbox"`
	Checks        int `json:"checks"`
	Worktrees     int `json:"worktrees"`
	Notifications int `json:"notifications"`
}

// RefreshChoices are the intervals each can be set to, in seconds, shortest first.
type RefreshChoices struct {
	Inbox         []int `json:"inbox"`
	Checks        []int `json:"checks"`
	Worktrees     []int `json:"worktrees"`
	Notifications []int `json:"notifications"`
}

// refreshChoices returns the intervals each kind can be set to. The inbox's shortest is
// two minutes, since each whole refresh reads every item; the checks follow can be off;
// the notifications' shortest is GitHub's own poll interval, a minute.
func refreshChoices() RefreshChoices {
	return RefreshChoices{
		Inbox:         []int{120, 300, 600, 900, 1800, 3600},
		Checks:        []int{0, 30, 45, 60, 120},
		Worktrees:     []int{300, 600, 900, 1800, 3600},
		Notifications: []int{60, 120, 300, 600, 900, 1800, 3600},
	}
}

// DefaultRefreshIntervals are the intervals of a fresh database.
func DefaultRefreshIntervals() RefreshIntervals {
	return RefreshIntervals{
		Inbox: DefaultRefreshInbox, Checks: DefaultRefreshChecks, Worktrees: DefaultRefreshWorktrees,
		Notifications: DefaultRefreshNotifications,
	}
}

// refreshSpec is one kind's key, choices, and default.
type refreshSpec struct {
	key      string
	choices  []int
	fallback int
}

// specOf returns kind's key, choices, and default, or false for a kind this build doesn't
// know.
func specOf(kind RefreshKind) (refreshSpec, bool) {
	choices := refreshChoices()

	switch kind {
	case RefreshInbox:
		return refreshSpec{KeyRefreshInbox, choices.Inbox, DefaultRefreshInbox}, true
	case RefreshChecks:
		return refreshSpec{KeyRefreshChecks, choices.Checks, DefaultRefreshChecks}, true
	case RefreshWorktrees:
		return refreshSpec{KeyRefreshWorktrees, choices.Worktrees, DefaultRefreshWorktrees}, true
	case RefreshNotifications:
		return refreshSpec{KeyRefreshNotifications, choices.Notifications, DefaultRefreshNotifications}, true
	default:
		return refreshSpec{}, false
	}
}

// decodeRefresh reads one kind's stored interval: as stored, when it is one of its
// choices, else its default.
func decodeRefresh(spec refreshSpec, stored string) int {
	if v, err := strconv.Atoi(stored); err == nil && slices.Contains(spec.choices, v) {
		return v
	}

	return spec.fallback
}

// decodeRefreshIntervals reads every refresh interval from the stored settings.
func decodeRefreshIntervals(stored map[string]string) RefreshIntervals {
	read := func(kind RefreshKind) int {
		spec, _ := specOf(kind)

		return decodeRefresh(spec, stored[spec.key])
	}

	return RefreshIntervals{
		Inbox: read(RefreshInbox), Checks: read(RefreshChecks), Worktrees: read(RefreshWorktrees),
		Notifications: read(RefreshNotifications),
	}
}

// Every returns how often the app reads kind again, as set in s; zero is off, and a kind
// this build doesn't know is off.
func (r RefreshIntervals) Every(kind RefreshKind) time.Duration {
	switch kind {
	case RefreshInbox:
		return time.Duration(r.Inbox) * time.Second
	case RefreshChecks:
		return time.Duration(r.Checks) * time.Second
	case RefreshWorktrees:
		return time.Duration(r.Worktrees) * time.Second
	case RefreshNotifications:
		return time.Duration(r.Notifications) * time.Second
	default:
		return 0
	}
}

// SetRefreshInterval stores how often, in seconds, the app reads kind again on its own,
// one of its choices (RefreshChoices), and returns the settings with it. Once it is
// stored, an event.IntervalsChanged is emitted, so whatever reads kind again works its
// next wait out from the new interval at once.
func (s *Service) SetRefreshInterval(ctx context.Context, kind RefreshKind, seconds int) (Settings, error) {
	values, err := checkRefresh(kind, seconds)
	if err != nil {
		return Settings{}, err
	}

	saved, err := s.save(ctx, setMany(values, ""))
	if err != nil {
		return saved, err
	}

	s.events.Emit(event.IntervalsChanged{})

	return saved, nil
}

// checkRefresh refuses a kind this build doesn't know, and an interval that isn't one of
// its choices, and returns its write.
func checkRefresh(kind RefreshKind, seconds int) (map[string]string, error) {
	spec, ok := specOf(kind)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrInvalidRefresh, kind)
	}

	if !slices.Contains(spec.choices, seconds) {
		return nil, fmt.Errorf("%w: %d seconds for %s", ErrInvalidRefresh, seconds, kind)
	}

	return map[string]string{spec.key: strconv.Itoa(seconds)}, nil
}

// RefreshKinds returns every kind the app reads again on its own, in the order Settings
// lists them.
func RefreshKinds() []RefreshKind {
	return []RefreshKind{RefreshInbox, RefreshChecks, RefreshWorktrees, RefreshNotifications}
}
