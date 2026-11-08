package desktop_test

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/timing"
)

const exampleURL = "https://github.com/acme-org/example-repo/pull/12"

func TestWindowEventsGoOnToThePage(t *testing.T) {
	t.Parallel()

	r := newRig()
	s := r.shell()

	for _, e := range event.WindowEvents() {
		if _, reveal := e.(event.RevealItem); reveal {
			continue
		}

		s.Emit(e)
	}

	s.Emit(event.ThemeChanged{Theme: string(settings.ThemeLight)})
	s.Emit(event.MenuBarChanged{On: false})
	s.Emit(event.FirstPaint{})

	assert.Equal(t, []event.WindowEvent{
		event.InboxUpdated{}, event.WorktreesUpdated{}, event.NotificationsUpdated{},
	}, r.page.events(), "the shell's own events stay in Go")
}

func TestARevealBringsTheWindowForward(t *testing.T) {
	t.Parallel()

	r := newRig()
	s := r.shell()

	s.Emit(event.RevealItem{ItemID: 7, URL: exampleURL})
	s.Emit(event.RevealItem{ItemID: 8})
	s.Emit(event.RevealItem{URL: exampleURL})
	s.Emit(event.RevealItem{})

	assert.Equal(t, 4, r.window.shown(), "every request brings the window forward")
	assert.Equal(t, []event.WindowEvent{
		event.RevealItem{ItemID: 7, URL: exampleURL},
		event.RevealItem{ItemID: 8},
		event.RevealItem{URL: exampleURL},
	}, r.page.events(), "a request that names nothing is not passed on")
}

func TestAThemeChangeReachesTheWindow(t *testing.T) {
	t.Parallel()

	r := newRig()
	s := r.shell()

	s.Emit(event.ThemeChanged{Theme: string(settings.ThemeLight)})
	s.Emit(event.ThemeChanged{Theme: string(settings.ThemeSystem)})

	assert.Equal(t, []settings.Theme{settings.ThemeLight, settings.ThemeSystem}, r.window.themesSet())
	assert.Empty(t, r.page.events())
}

func TestShowWindowAndQuit(t *testing.T) {
	t.Parallel()

	r := newRig()
	s := r.shell()

	s.ShowWindow()
	s.Quit()

	assert.Equal(t, 1, r.window.shown())
	assert.Equal(t, 1, r.app.quitCount())
}

func TestLaunchedOpensTheWindowFromTheSettings(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		banners := &fakeBanners{log: r.log}
		s := r.shell(func(c *desktop.Config) { c.Banners = banners })

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		assert.Equal(t, []opening{{theme: settings.ThemeDark, size: r.settings.size}}, r.window.openings())
		assert.Equal(t, []string{"tray shown"}, r.lines("tray shown"))
		assert.Len(t, r.tray.drawn(), 1, "a shown icon is drawn at once")
		assert.Equal(t, []string{"banners asked"}, r.lines("banners"))
		assert.True(t, s.HidesOnClose(), "with the icon on, closing the window hides it")
		assert.Empty(t, r.stderr.String())

		require.NoError(t, s.Stop())
	})
}

func TestLaunchedWithTheMenuBarOff(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.settings.menuBar = false
		s := r.shell()

		s.Start(t.Context())
		s.Launched()
		s.Emit(event.InboxUpdated{})
		synctest.Wait()

		assert.Empty(t, r.lines("tray"), "the icon is neither shown nor drawn")
		assert.Zero(t, r.inbox.readCount(), "a hidden icon reads nothing")
		assert.False(t, s.HidesOnClose(), "without the icon, closing the window quits")

		require.NoError(t, s.Stop())
	})
}

func TestLaunchedFallsBackWhenTheSettingsCannotBeRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.settings.err = errFakeSettings
		s := r.shell()

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		assert.Equal(t, []opening{{theme: settings.DefaultTheme, size: settings.DefaultWindowSize()}}, r.window.openings())
		assert.Equal(t, []string{"tray shown"}, r.lines("tray shown"), "the icon shows by default")

		notes := r.stderr.String()
		assert.Contains(t, notes, "read the theme, following the system: "+errFakeSettings.Error())
		assert.Contains(t, notes, "read the window size, opening at the default: "+errFakeSettings.Error())
		assert.Contains(t, notes, "read the menu bar setting, showing the icon: "+errFakeSettings.Error())

		require.NoError(t, s.Stop())
	})
}

func TestTheBannerQuestionIsNotedWhenItFails(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(func(c *desktop.Config) { c.Banners = &fakeBanners{log: r.log, err: errFakeBanners} })

		s.Launched()
		synctest.Wait()

		assert.Contains(t, r.stderr.String(), errFakeBanners.Error())
		require.NoError(t, s.Stop())
	})
}

func TestStartRunsTheWorkersOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(func(c *desktop.Config) {
			c.Workers = []desktop.Worker{
				&fakeWorker{name: "scheduler", log: r.log}, &fakeWorker{name: "sweeper", log: r.log},
			}
		})

		s.Start(t.Context())
		s.Start(t.Context())
		synctest.Wait()

		assert.ElementsMatch(t, []string{"scheduler started", "sweeper started"}, r.log.all(), "each worker runs once")

		require.NoError(t, s.Stop())
	})
}

func TestStopShutsDownInOrder(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(func(c *desktop.Config) {
			c.MeasureStartup = true
			c.Workers = []desktop.Worker{
				&fakeWorker{name: "scheduler", log: r.log, linger: time.Second},
				&fakeWorker{name: "sweeper", log: r.log, linger: 2 * time.Second},
			}
		})

		r.inbox.set(snoozedFor(time.Hour), nil)

		s.Start(t.Context())
		s.Launched()
		s.RefreshNow()
		synctest.Wait()

		start := time.Now()

		require.NoError(t, s.Stop())

		assert.Equal(t, 2*time.Second, time.Since(start), "Stop waits for the slowest work")

		lines := withoutTray(r.log.all())
		require.Len(t, lines, 7)
		assert.ElementsMatch(t, []string{"scheduler started", "sweeper started", "refresh started"}, lines[:3])
		assert.Equal(t, []string{"refresh returned", "scheduler returned", "sweeper returned", "data closed"}, lines[3:],
			"every piece of work returns before the data is closed")

		// The snooze and the measurement timers were stopped: neither fires later.
		draws := len(r.tray.drawn())

		time.Sleep(2 * time.Hour)
		synctest.Wait()

		assert.Len(t, r.tray.drawn(), draws, "no snooze redraws the icon")
		assert.Zero(t, r.app.quitCount(), "no measurement times out")
		assert.False(t, s.TimedOut())
	})
}

func TestStopGivesUpWaitingAfterTheTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		release := make(chan struct{})
		s := r.shell(func(c *desktop.Config) {
			c.StopTimeout = 3 * time.Second
			c.Workers = []desktop.Worker{&fakeWorker{name: "stuck", log: r.log, release: release}}
		})

		s.Start(t.Context())
		synctest.Wait()

		start := time.Now()

		require.NoError(t, s.Stop())

		assert.Equal(t, 3*time.Second, time.Since(start))
		assert.Equal(t, []string{"stuck started", "data closed"}, r.log.all(), "the data is closed anyway")

		close(release)
		synctest.Wait()

		assert.Equal(t, []string{"stuck started", "data closed", "stuck returned"}, r.log.all())
	})
}

func TestStopWaitsTheDefaultTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		release := make(chan struct{})
		s := r.shell(func(c *desktop.Config) {
			c.Workers = []desktop.Worker{&fakeWorker{name: "stuck", log: r.log, release: release}}
		})

		s.Start(t.Context())

		start := time.Now()

		require.NoError(t, s.Stop())
		assert.Equal(t, desktop.DefaultStopTimeout, time.Since(start))

		close(release)
	})
}

func TestStopReturnsTheCloseErrorEveryTime(t *testing.T) {
	t.Parallel()

	r := newRig()
	r.data.err = errFakeClose
	s := r.shell()

	require.ErrorIs(t, s.Stop(), errFakeClose, "Stop works without Start")
	require.ErrorIs(t, s.Stop(), errFakeClose)
	assert.Equal(t, []string{"data closed"}, r.log.all(), "the data is closed once")
}

func TestStopWithoutData(t *testing.T) {
	t.Parallel()

	r := newRig()
	s := r.shell(func(c *desktop.Config) { c.Data = nil })

	require.NoError(t, s.Stop())
}

func TestAStoppedShellDoesNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(func(c *desktop.Config) {
			c.MeasureStartup = true
			c.Workers = []desktop.Worker{&fakeWorker{name: "scheduler", log: r.log}}
		})

		require.NoError(t, s.Stop())
		assert.True(t, s.Stopping())

		s.Start(t.Context())
		s.Launched()
		s.ShowWindow()
		s.RefreshNow()

		for _, e := range []event.Event{
			event.InboxUpdated{},
			event.RevealItem{ItemID: 7},
			event.ThemeChanged{Theme: string(settings.ThemeDark)},
			event.MenuBarChanged{On: true},
			event.FirstPaint{},
		} {
			s.Emit(e)
		}

		synctest.Wait()

		assert.Equal(t, []string{"data closed"}, r.log.all(), "no work starts, and the icon is never touched")
		assert.Empty(t, r.page.events())
		assert.Empty(t, r.window.openings())
		assert.Zero(t, r.window.shown())
		assert.Empty(t, r.window.themesSet())
		assert.Zero(t, r.app.quitCount())
		assert.False(t, s.HidesOnClose(), "a quitting app closes its window")
		assert.Empty(t, r.stdout.String())
	})
}

func TestTheWorkEndsWithTheStartContext(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(func(c *desktop.Config) {
			c.Workers = []desktop.Worker{&fakeWorker{name: "scheduler", log: r.log}}
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)
		synctest.Wait()

		cancel()
		synctest.Wait()

		assert.Equal(t, []string{"scheduler started", "scheduler returned"}, r.log.all(), "the work ended without Stop")

		require.NoError(t, s.Stop())
	})
}

func TestRefreshNowIsTrackedWork(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell()

		s.Start(t.Context())
		s.RefreshNow()
		synctest.Wait()

		assert.Equal(t, []string{"refresh started"}, r.log.all())

		require.NoError(t, s.Stop())
		assert.Equal(t, []string{"refresh started", "refresh returned", "data closed"}, r.log.all())
	})
}

func TestAStartupMeasurementQuitsAtTheFirstPaint(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		banners := &fakeBanners{log: r.log}
		s := r.shell(func(c *desktop.Config) {
			c.MeasureStartup = true
			c.Banners = banners
		})

		s.Launched()
		time.Sleep(time.Second)
		s.Emit(event.FirstPaint{Measurement: timing.Measurement{FirstPaintMS: 412, Source: timing.SourceOS}})
		synctest.Wait()

		assert.Equal(t, "startup.first_paint_ms=412 source=os\n", r.stdout.String())
		assert.Equal(t, 1, r.app.quitCount())
		assert.Empty(t, r.lines("banners"), "a measurement asks nothing")

		require.NoError(t, s.Stop())

		time.Sleep(desktop.DefaultMeasureTimeout)
		assert.False(t, s.TimedOut(), "the timeout stopped with the shell")
	})
}

func TestAStartupMeasurementThatNeverPaintsTimesOut(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(func(c *desktop.Config) {
			c.MeasureStartup = true
			c.MeasureTimeout = 10 * time.Second
		})

		s.Launched()
		s.Launched()

		time.Sleep(10*time.Second - time.Nanosecond)
		synctest.Wait()
		assert.False(t, s.TimedOut())

		time.Sleep(time.Nanosecond)
		synctest.Wait()

		assert.True(t, s.TimedOut())
		assert.Equal(t, desktop.ErrNoFirstPaint.Error()+"\n", r.stderr.String())
		assert.Equal(t, 1, r.app.quitCount(), "one timeout, however often the app launched")

		require.NoError(t, s.Stop())
	})
}

func TestTheFirstPaintOutsideAMeasurementDoesNothing(t *testing.T) {
	t.Parallel()

	r := newRig()
	s := r.shell()

	s.Emit(event.FirstPaint{Measurement: timing.Measurement{FirstPaintMS: 412, Source: timing.SourceOS}})

	assert.Empty(t, r.stdout.String())
	assert.Zero(t, r.app.quitCount())
}

func TestNewFillsInTheDefaults(t *testing.T) {
	t.Parallel()

	// No writers and no clock: the shell still notes and draws without them.
	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.settings.err = errFakeSettings
		s := r.shell(func(c *desktop.Config) {
			c.Stdout, c.Stderr, c.Clock = nil, nil, nil
		})

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		assert.Len(t, r.tray.drawn(), 1)
		require.NoError(t, s.Stop())
	})
}

// withoutTray drops the menu bar icon's lines from a journal.
func withoutTray(lines []string) []string {
	out := make([]string, 0, len(lines))

	for _, line := range lines {
		if !strings.HasPrefix(line, "tray") {
			out = append(out, line)
		}
	}

	return out
}
