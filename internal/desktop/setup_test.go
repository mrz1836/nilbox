package desktop_test

import (
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/settings"
)

func TestTheSetupHoldsTheBannerQuestionUntilItEnds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.settings.setup = true
		s := r.shell(func(c *desktop.Config) { c.Banners = &fakeBanners{log: r.log} })

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		assert.Equal(t, []opening{{
			theme: settings.ThemeDark, size: r.settings.size, textSize: settings.TextSizeLarger, setup: true,
		}}, r.window.openings(), "the window opens on the setup")
		assert.Empty(t, r.lines("banners"), "nothing is asked before the user knows what the app does")

		s.Emit(event.SetupFinished{})
		synctest.Wait()
		assert.Equal(t, []string{"banners asked"}, r.lines("banners"), "asked as the user lands in the inbox")

		s.Emit(event.SetupFinished{})
		synctest.Wait()
		assert.Equal(t, []string{"banners asked"}, r.lines("banners"), "once")

		require.NoError(t, s.Stop())
	})
}

func TestASetupRunAgainAsksNothingMore(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(func(c *desktop.Config) { c.Banners = &fakeBanners{log: r.log} })

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()
		require.Equal(t, []string{"banners asked"}, r.lines("banners"), "asked at launch, with no setup")

		s.Emit(event.SetupFinished{})
		synctest.Wait()
		assert.Equal(t, []string{"banners asked"}, r.lines("banners"))

		require.NoError(t, s.Stop())
	})
}

func TestTheSetupNeverHoldsAStartupMeasurementsBanners(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.settings.setup = true
		s := r.shell(func(c *desktop.Config) {
			c.Banners = &fakeBanners{log: r.log}
			c.MeasureStartup = true
		})

		s.Start(t.Context())
		s.Launched()
		s.Emit(event.SetupFinished{})
		synctest.Wait()

		assert.Empty(t, r.lines("banners"), "a measurement asks nothing")
		require.NoError(t, s.Stop())
	})
}

func TestAShellThatCannotReadTheSetupOpensTheInbox(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.settings.setupErr = errFakeSettings
		s := r.shell(func(c *desktop.Config) { c.Banners = &fakeBanners{log: r.log} })

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		openings := r.window.openings()
		require.Len(t, openings, 1)
		assert.False(t, openings[0].setup, "a passing problem never traps the user in the setup")
		assert.Equal(t, []string{"banners asked"}, r.lines("banners"))
		assert.Contains(t, r.logs.String(), `level=WARN msg="could not read whether the setup shows; opening the inbox"`)

		require.NoError(t, s.Stop())
	})
}

func TestAResetQuitsToOpenAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		guard := &fakeGuard{}
		s := r.shell(func(c *desktop.Config) { c.QuitGuard = guard })

		assert.False(t, s.Relaunching())

		s.Emit(event.ResetRequested{})
		synctest.Wait()

		assert.Equal(t, 1, r.app.quitCount(), "the app quits")
		assert.True(t, s.Relaunching(), "and opens again once it has shut down")

		// A run that starts between the reset's check and the quit never cancels it.
		guard.active.Store(true)
		assert.True(t, s.ShouldQuit())
		assert.Empty(t, r.page.events(), "nothing is asked")

		require.NoError(t, s.Stop())
	})
}
