package settings

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// choiceSetting is a setting the choice contract holds, its values written as text: a
// fresh database reads as its default, each value it takes is stored, returned by its
// setter, and read back as itself, alone and with the rest of the settings; a value it
// doesn't take is refused, naming the choices, and neither stored nor told; a stored
// value it can't read is the default; the value survives a restart; and a closed
// database is reported, with nothing told.
type choiceSetting struct {
	name string

	// key is where the setting is stored, for planting a value it can't read.
	key string

	// fallback is what a fresh database, and a stored value that can't be read, read as.
	fallback string

	// values are stored in turn; the last differs from fallback.
	values []string

	// refused are refused with sentinel, and the last with message too.
	refused  []string
	sentinel error
	message  string

	// unreadable are stored values that read as fallback.
	unreadable []string

	// readErr is the getter's words for a closed database, and readOnErr what it returns
	// with them.
	readErr   string
	readOnErr string

	// get reads the setting alone, field picks it from the settings, and set stores it.
	get   func(ctx context.Context, svc *Service) (string, error)
	field func(s Settings) string
	set   func(ctx context.Context, svc *Service, value string) (Settings, error)

	// refuse tries a refused value; nil is set. Sections refuse the section, not its value.
	refuse func(ctx context.Context, svc *Service, value string) (Settings, error)

	// told is the event storing value emits, or nil for a setter that emits none.
	told func(value string) event.Event
}

// choiceSettings are the settings that take one of a few values.
func choiceSettings() []choiceSetting {
	return []choiceSetting{
		{
			name: "theme", key: KeyTheme, fallback: "system",
			values:  []string{"dark", "light", "light", "system", "dark"},
			refused: []string{"", "Dark", " dark", "auto", "sepia", "ui.theme", "dim"}, sentinel: ErrInvalidTheme,
			message:    `"dim": choose one of system, light, dark`,
			unreadable: []string{"", "auto", "DARK", "light "},
			readErr:    "read the theme", readOnErr: "",
			get: func(ctx context.Context, svc *Service) (string, error) {
				got, err := svc.Theme(ctx)

				return string(got), err
			},
			field: func(s Settings) string { return string(s.Theme) },
			set: func(ctx context.Context, svc *Service, value string) (Settings, error) {
				return svc.SetTheme(ctx, Theme(value))
			},
			told: func(value string) event.Event { return event.ThemeChanged{Theme: value} },
		},
		{
			name: "text size", key: KeyTextSize, fallback: "default",
			values:   []string{"large", "larger", "default", "larger"},
			refused:  []string{"", "Large", " large", "largest", "small", "115", "ui.textSize", "huge"},
			sentinel: ErrInvalidTextSize, message: `"huge": choose one of default, large, larger`,
			unreadable: []string{"", "largest", "LARGE", "large ", "130"},
			readErr:    "read the text size", readOnErr: "default",
			get: func(ctx context.Context, svc *Service) (string, error) {
				got, err := svc.TextSize(ctx)

				return string(got), err
			},
			field: func(s Settings) string { return string(s.TextSize) },
			set: func(ctx context.Context, svc *Service, value string) (Settings, error) {
				return svc.SetTextSize(ctx, TextSize(value))
			},
			told: func(value string) event.Event { return event.TextSizeChanged{Size: value} },
		},
		{
			name: "merge method", key: KeyMergeMethod, fallback: "repo",
			values:  []string{"squash", "merge_commit", "repo", "rebase_merge"},
			refused: []string{"", "Squash", "fast-forward"}, sentinel: ErrInvalidMergeMethod,
			message:    `"fast-forward": choose one of repo, squash, merge_commit, rebase_merge`,
			unreadable: []string{"", "fast-forward", "Squash"},
			readErr:    "read the merge method", readOnErr: "repo",
			get: func(ctx context.Context, svc *Service) (string, error) {
				got, err := svc.MergeMethod(ctx)

				return string(got), err
			},
			field: func(s Settings) string { return string(s.MergeMethod) },
			set: func(ctx context.Context, svc *Service, value string) (Settings, error) {
				return svc.SetMergeMethod(ctx, MergeMethod(value))
			},
		},
		{
			name: "menu bar", key: KeyMenuBar, fallback: "true",
			values:     []string{"false", "true", "false"},
			unreadable: []string{"", "sometimes", "on"},
			readErr:    "read the menu bar setting", readOnErr: "true",
			get: func(ctx context.Context, svc *Service) (string, error) {
				on, err := svc.MenuBar(ctx)

				return strconv.FormatBool(on), err
			},
			field: func(s Settings) string { return strconv.FormatBool(s.MenuBar) },
			set: func(ctx context.Context, svc *Service, value string) (Settings, error) {
				return svc.SetMenuBar(ctx, value == "true")
			},
			told: func(value string) event.Event { return event.MenuBarChanged{On: value == "true"} },
		},
		{
			name: "collapsed section", key: collapsedKey(ListAuthored), fallback: "",
			values:   []string{"authored", "", "authored"},
			refused:  []string{"", "Authored", "snoozed", "ui.collapsed.authored", " authored", "mentions"},
			sentinel: ErrUnknownSection, message: `"mentions": choose one of reviewRequested, authored, assigned`,
			unreadable: []string{"", "yes please", "on"},
			readErr:    "read the collapsed inbox sections", readOnErr: "",
			get: func(ctx context.Context, svc *Service) (string, error) {
				got, err := svc.CollapsedSections(ctx)

				return textutil.Join(got, ","), err
			},
			field: func(s Settings) string { return textutil.Join(s.CollapsedSections, ",") },
			set: func(ctx context.Context, svc *Service, value string) (Settings, error) {
				return svc.SetSectionCollapsed(ctx, ListAuthored, value != "")
			},
			refuse: func(ctx context.Context, svc *Service, value string) (Settings, error) {
				return svc.SetSectionCollapsed(ctx, ListKey(value), true)
			},
		},
	}
}

func TestChoiceSettings(t *testing.T) {
	t.Parallel()

	for _, c := range choiceSettings() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			r := newChoiceRig(t, c)

			r.expect(t, c.fallback, "a fresh database reads as the default")
			r.storesEachValue(t)
			r.refusesWhatItDoesNotTake(t)
			r.readsAnUnreadableValueAsTheDefault(t)
			r.survivesARestart(t)
			r.reportsAClosedDatabase(t)
		})
	}
}

// choiceRig runs the choice contract for one setting: its database file, the service over
// it, the events it emits, and those it should have emitted so far.
type choiceRig struct {
	c      choiceSetting
	home   string
	path   string
	db     *sql.DB
	svc    *Service
	events *testutil.Events
	told   []event.Event
}

// newChoiceRig returns a rig over a fresh database file. It first holds the setting to
// what the checks rely on: a value to store, the last of them unlike the default, so a
// value lost on a refusal or a restart can't pass for the one stored.
func newChoiceRig(t *testing.T, c choiceSetting) *choiceRig {
	t.Helper()

	require.NotEmpty(t, c.values, "a choice setting stores at least one value")
	require.NotEqual(t, c.fallback, c.values[len(c.values)-1], "the last value differs from the default")

	r := &choiceRig{c: c, home: t.TempDir(), path: filepath.Join(t.TempDir(), "app.db"), events: &testutil.Events{}}
	r.open(t)

	return r
}

// open opens the rig's database file and a service over it that emits into the rig's
// events.
func (r *choiceRig) open(t *testing.T) {
	t.Helper()

	r.db = testutil.DBAt(t, r.path)
	r.svc = serviceOver(t, r.db, r.home, WithEvents(r.events))
}

// last is the value the rig stores last.
func (r *choiceRig) last() string {
	return r.c.values[len(r.c.values)-1]
}

// store stores value through the rig's service, and records what it should tell.
func (r *choiceRig) store(t *testing.T, value string) Settings {
	t.Helper()

	out, err := r.c.set(t.Context(), r.svc, value)
	require.NoError(t, err, "store %q", value)

	if r.c.told != nil {
		r.told = append(r.told, r.c.told(value))
	}

	return out
}

// expect checks that the setting reads as want, alone and with the rest of the settings.
func (r *choiceRig) expect(t *testing.T, want, why string) {
	t.Helper()

	got, err := r.c.get(t.Context(), r.svc)
	require.NoError(t, err)
	assert.Equal(t, want, got, why)

	all, err := r.svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, want, r.c.field(all), "%s, with the rest of the settings", why)
}

// storesEachValue stores each value in turn, the same one twice included, and reads it
// back; a service with no event sink stores it as well.
func (r *choiceRig) storesEachValue(t *testing.T) {
	t.Helper()

	for _, value := range r.c.values {
		out := r.store(t, value)
		assert.Equal(t, value, r.c.field(out), "the setter returns the settings with %q", value)
		r.expect(t, value, "a stored value reads back as itself")
	}

	quiet := serviceOver(t, r.db, r.home)
	require.NoError(t, testutil.ErrOf(r.c.set(t.Context(), quiet, r.last())), "a service with no event sink")

	assert.Equal(t, r.told, r.events.All(), "every stored value is told, in order")
}

// refusesWhatItDoesNotTake tries every refused value, which stores and tells nothing.
func (r *choiceRig) refusesWhatItDoesNotTake(t *testing.T) {
	t.Helper()

	refuse := r.c.refuse
	if refuse == nil {
		refuse = r.c.set
	}

	for _, value := range r.c.refused {
		require.ErrorIs(t, testutil.ErrOf(refuse(t.Context(), r.svc, value)), r.c.sentinel, "%q", value)
	}

	if len(r.c.refused) > 0 {
		named := r.c.refused[len(r.c.refused)-1]
		require.ErrorContains(t, testutil.ErrOf(refuse(t.Context(), r.svc, named)), r.c.message)
	}

	r.expect(t, r.last(), "a refused value stores nothing")
	assert.Equal(t, r.told, r.events.All(), "a refused value is not told")
}

// readsAnUnreadableValueAsTheDefault plants each unreadable value, which reads as the
// default.
func (r *choiceRig) readsAnUnreadableValueAsTheDefault(t *testing.T) {
	t.Helper()

	for _, stored := range r.c.unreadable {
		setPref(t, r.db, r.c.key, stored)
		r.expect(t, r.c.fallback, "an unreadable value reads as the default: "+strconv.Quote(stored))
	}
}

// survivesARestart stores the last value, closes the database, and reads the value from
// it again.
func (r *choiceRig) survivesARestart(t *testing.T) {
	t.Helper()

	r.store(t, r.last())
	require.NoError(t, r.db.Close())

	r.open(t)
	r.expect(t, r.last(), "the value survives a restart")
}

// reportsAClosedDatabase checks the getter's words and value, and the setter's error, on
// a closed database, which tells nothing.
func (r *choiceRig) reportsAClosedDatabase(t *testing.T) {
	t.Helper()

	require.NoError(t, r.db.Close())

	got, err := r.c.get(t.Context(), r.svc)
	require.ErrorContains(t, err, r.c.readErr)
	assert.Equal(t, r.c.readOnErr, got, "what the getter returns with its error")

	require.ErrorContains(t, testutil.ErrOf(r.c.set(t.Context(), r.svc, r.last())), "begin transaction")
	assert.Equal(t, r.told, r.events.All(), "a value that was not stored is not told")
}
