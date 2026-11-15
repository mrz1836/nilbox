package svcstatus

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/statusprefs"
)

// runChecker runs a Checker over r's Service until the returned stop is called, which
// waits for it to return.
func runChecker(t *testing.T, r *rig) (*Checker, func()) {
	t.Helper()

	checker := NewChecker(r.svc)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		checker.Run(ctx)
	}()

	return checker, func() {
		cancel()
		<-done
	}
}

func TestTheCheckerChecksAtOnceThenAsOftenAsSet(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, nil)
		checker, stop := runChecker(t, r)

		synctest.Wait()
		assert.Equal(t, map[string]int{githubHost: 1, claudeHost: 1, openaiHost: 1}, r.fetch.count(), "at once")
		assert.Len(t, r.events.All(), 1, "one word to the window for the pass")

		time.Sleep(30*time.Minute - time.Second)
		synctest.Wait()
		assert.Equal(t, map[string]int{githubHost: 1, claudeHost: 1, openaiHost: 1}, r.fetch.count())

		time.Sleep(time.Second)
		synctest.Wait()
		assert.Equal(t, map[string]int{githubHost: 2, claudeHost: 2, openaiHost: 2}, r.fetch.count(),
			"then once an interval")

		// Off, it checks nothing however long it waits; on again, it checks at once what is due.
		r.rules.every(0)
		checker.Wake()
		time.Sleep(2 * time.Hour)
		synctest.Wait()
		assert.Equal(t, map[string]int{githubHost: 2, claudeHost: 2, openaiHost: 2}, r.fetch.count())

		r.rules.every(5 * time.Minute)
		checker.Wake()
		synctest.Wait()
		assert.Equal(t, map[string]int{githubHost: 3, claudeHost: 3, openaiHost: 3}, r.fetch.count())

		stop()
	})
}

func TestTheCheckerChecksAProviderTurnedOnAtOnceAndAlignsIt(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, nil)
		r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) { return c.SetProvider("openai", false) })

		checker, stop := runChecker(t, r)

		synctest.Wait()
		assert.Equal(t, map[string]int{githubHost: 1, claudeHost: 1}, r.fetch.count())

		time.Sleep(10 * time.Minute)
		r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) { return c.SetProvider("openai", true) })
		checker.RulesChanged()
		synctest.Wait()

		assert.Equal(t, map[string]int{githubHost: 1, claudeHost: 1, openaiHost: 1}, r.fetch.count(),
			"only the provider turned on is checked")

		// Twenty minutes on, GitHub and Claude are due, and OpenAI, checked twenty minutes
		// ago, more than half an interval, goes along: one timer again.
		time.Sleep(20 * time.Minute)
		synctest.Wait()
		assert.Equal(t, map[string]int{githubHost: 2, claudeHost: 2, openaiHost: 2}, r.fetch.count())

		time.Sleep(30 * time.Minute)
		synctest.Wait()
		assert.Equal(t, map[string]int{githubHost: 3, claudeHost: 3, openaiHost: 3}, r.fetch.count())

		stop()
	})
}

func TestTheCheckerLeavesAProviderCheckedRecentlyForItsOwnTurn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, nil)
		r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) { return c.SetProvider("openai", false) })

		checker, stop := runChecker(t, r)

		synctest.Wait()

		time.Sleep(25 * time.Minute)
		r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) { return c.SetProvider("openai", true) })
		checker.RulesChanged()
		synctest.Wait()

		// Five minutes on, GitHub and Claude are due; OpenAI, checked five minutes ago, waits.
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		assert.Equal(t, map[string]int{githubHost: 2, claudeHost: 2, openaiHost: 1}, r.fetch.count())

		stop()
	})
}

func TestTheCheckerForgetsAProviderTurnedOffAndTellsTheWindow(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, nil)
		checker, stop := runChecker(t, r)

		synctest.Wait()

		r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) { return c.SetProvider("claude", false) })
		checker.RulesChanged()
		synctest.Wait()

		assert.Len(t, r.events.All(), 2, "the pass, then the change")
		assert.Equal(t, map[string]int{githubHost: 1, claudeHost: 1, openaiHost: 1}, r.fetch.count(), "nothing read")

		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Equal(t, 1, r.fetch.count()[claudeHost], "off, it is never read")

		stop()
	})
}

func TestTheCheckerRecoversAPanic(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, nil)
		r.fetch.answer(githubHost, answer{panic: true})

		_, stop := runChecker(t, r)

		synctest.Wait()
		assert.Contains(t, r.logs.String(), "panicked")
		assert.Equal(t, 1, r.fetch.count()[githubHost], "tried once, not again at once")

		r.fetch.answer(githubHost, answer{body: string(fixture(t, "github-operational.json"))})
		time.Sleep(30 * time.Minute)
		synctest.Wait()

		assert.Equal(t, 2, r.fetch.count()[githubHost], "and again at its turn")
		assert.Equal(t, StateOperational, r.svc.Status(t.Context()).Providers[0].State)

		stop()
	})
}

func TestThePassRecoversAPanicAndWaits(t *testing.T) {
	t.Parallel()

	r := newRig(t, nil)
	r.svc.rules = func(context.Context) Rules { panic("the settings broke") }

	wait, timed := NewChecker(r.svc).pass(t.Context())

	assert.True(t, timed)
	assert.Equal(t, panicWait, wait)
	assert.Contains(t, r.logs.String(), "panicked")
}

func TestTheCheckerWakesWithoutBlocking(t *testing.T) {
	t.Parallel()

	checker := NewChecker(newRig(t, nil).svc)

	checker.Wake()
	checker.Wake()
	checker.RulesChanged()
	checker.RulesChanged()

	require.Len(t, checker.wake, 1, "a second wake while one waits is dropped")
	require.Len(t, checker.rules, 1)
}
