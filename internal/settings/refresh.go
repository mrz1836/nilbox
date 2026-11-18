package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/prefs/prefval"
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

	// RefreshReleaseChecks is the Release page's repositories whose newest commit's checks
	// are running, read again from GitHub together.
	RefreshReleaseChecks RefreshKind = "releaseChecks"

	// RefreshWorktrees is the worktrees' sweep: each clone's worktrees, the pull requests
	// and issues they belong to, and the automatic cleanup.
	RefreshWorktrees RefreshKind = "worktrees"

	// RefreshNotifications is the user's GitHub notifications, listed from GitHub: one
	// conditional request, free when nothing changed, and never sooner than GitHub's own
	// poll interval asks.
	RefreshNotifications RefreshKind = "notifications"

	// RefreshServiceStatus is the status pages of the services the app depends on
	// (GitHub's, Claude's, and OpenAI's), which say whether a part the user watches has an
	// incident: one small request to each page watched, outside any API, so it costs no
	// point of the budget.
	RefreshServiceStatus RefreshKind = "serviceStatus"
)

// The keys the refresh intervals are stored under, in seconds.
const (
	KeyRefreshInbox         = "refresh.inbox"
	KeyRefreshChecks        = "refresh.checks"
	KeyRefreshReleaseChecks = "refresh.releaseChecks"
	KeyRefreshWorktrees     = "refresh.worktrees"
	KeyRefreshNotifications = "refresh.notifications"
	KeyRefreshServiceStatus = "refresh.serviceStatus"
)

// The default refresh intervals, in seconds.
const (
	DefaultRefreshInbox         = 300
	DefaultRefreshChecks        = 45
	DefaultRefreshReleaseChecks = 45
	DefaultRefreshWorktrees     = 300
	DefaultRefreshNotifications = 300
	DefaultRefreshServiceStatus = 1800
)

// KeyWatchPushes is whether the app watches the branches of the inbox's pull requests in
// the user's clones, so one pushed from this Mac is read again at once.
const KeyWatchPushes = "refresh.watchPushes"

// DefaultWatchPushes is on: watching costs a few file checks every few seconds and no
// GitHub request until a pull request's branch moves.
const DefaultWatchPushes = true

// ErrInvalidRefresh is returned for a refresh kind, or an interval, that isn't one of the
// choices.
var ErrInvalidRefresh = errors.New("unknown refresh interval")

// RefreshIntervals are how often, in seconds, the app reads each thing again on its own.
// Both checks follows' 0 is off, and so is the service status check's.
type RefreshIntervals struct {
	Inbox         int `json:"inbox"`
	Checks        int `json:"checks"`
	ReleaseChecks int `json:"releaseChecks"`
	Worktrees     int `json:"worktrees"`
	Notifications int `json:"notifications"`
	ServiceStatus int `json:"serviceStatus"`
}

// RefreshChoices are the intervals each can be set to, in seconds, shortest first.
type RefreshChoices struct {
	Inbox         []int `json:"inbox"`
	Checks        []int `json:"checks"`
	ReleaseChecks []int `json:"releaseChecks"`
	Worktrees     []int `json:"worktrees"`
	Notifications []int `json:"notifications"`
	ServiceStatus []int `json:"serviceStatus"`
}

// refreshChoices returns the intervals each kind can be set to. The inbox's shortest is
// two minutes, since each whole refresh reads every item; either checks follow can be
// off; the notifications' shortest is GitHub's own poll interval, a minute; the service
// status check can be off, and its shortest is five minutes, since an incident lasts
// longer than that.
func refreshChoices() RefreshChoices {
	return RefreshChoices{
		Inbox:         []int{120, 300, 600, 900, 1800, 3600},
		Checks:        []int{0, 30, 45, 60, 120},
		ReleaseChecks: []int{0, 30, 45, 60, 120},
		Worktrees:     []int{300, 600, 900, 1800, 3600},
		Notifications: []int{60, 120, 300, 600, 900, 1800, 3600},
		ServiceStatus: []int{0, 300, 600, 900, 1800, 3600},
	}
}

// DefaultRefreshIntervals are the intervals of a fresh database.
func DefaultRefreshIntervals() RefreshIntervals {
	return RefreshIntervals{
		Inbox: DefaultRefreshInbox, Checks: DefaultRefreshChecks, ReleaseChecks: DefaultRefreshReleaseChecks,
		Worktrees: DefaultRefreshWorktrees, Notifications: DefaultRefreshNotifications,
		ServiceStatus: DefaultRefreshServiceStatus,
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
	case RefreshReleaseChecks:
		return refreshSpec{KeyRefreshReleaseChecks, choices.ReleaseChecks, DefaultRefreshReleaseChecks}, true
	case RefreshWorktrees:
		return refreshSpec{KeyRefreshWorktrees, choices.Worktrees, DefaultRefreshWorktrees}, true
	case RefreshNotifications:
		return refreshSpec{KeyRefreshNotifications, choices.Notifications, DefaultRefreshNotifications}, true
	case RefreshServiceStatus:
		return refreshSpec{KeyRefreshServiceStatus, choices.ServiceStatus, DefaultRefreshServiceStatus}, true
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
		Inbox: read(RefreshInbox), Checks: read(RefreshChecks), ReleaseChecks: read(RefreshReleaseChecks),
		Worktrees: read(RefreshWorktrees), Notifications: read(RefreshNotifications),
		ServiceStatus: read(RefreshServiceStatus),
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
	case RefreshReleaseChecks:
		return time.Duration(r.ReleaseChecks) * time.Second
	case RefreshWorktrees:
		return time.Duration(r.Worktrees) * time.Second
	case RefreshNotifications:
		return time.Duration(r.Notifications) * time.Second
	case RefreshServiceStatus:
		return time.Duration(r.ServiceStatus) * time.Second
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

// WatchPushes returns whether the app watches the branches of the inbox's pull requests
// in the user's clones: the stored setting, or DefaultWatchPushes when none is stored or
// it cannot be read.
func (s *Service) WatchPushes(ctx context.Context) (bool, error) {
	stored, _, err := s.prefs.Get(ctx, KeyWatchPushes)
	if err != nil {
		return DefaultWatchPushes, fmt.Errorf("read whether to watch for pushes: %w", err)
	}

	return prefval.Bool(stored, DefaultWatchPushes), nil
}

// SetWatchPushes stores whether the app watches the branches of the inbox's pull requests
// in the user's clones, and returns the settings with it. While it is off, no clone is
// looked at, and a push reaches its card at the next refresh. Once it is stored, an
// event.IntervalsChanged is emitted, so the watch starts or stops at once.
func (s *Service) SetWatchPushes(ctx context.Context, on bool) (Settings, error) {
	saved, err := s.save(ctx, setting(KeyWatchPushes, prefval.EncodeBool(on)))
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
	return []RefreshKind{
		RefreshInbox, RefreshChecks, RefreshReleaseChecks, RefreshWorktrees, RefreshNotifications, RefreshServiceStatus,
	}
}
