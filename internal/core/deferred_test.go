package core

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/shellenv"
)

// fakeShell answers the environment read with env, between the markers it was asked to
// print, and counts its runs.
func fakeShell(env string, runs *int) shellenv.Starter {
	return func(_ context.Context, argv, _ []string) ([]byte, error) {
		*runs++

		script := argv[len(argv)-1]
		marker, _, _ := strings.Cut(strings.TrimPrefix(script, "printf '%s' "), ";")

		return []byte(marker + env + marker), nil
	}
}

func TestDeferredReadsTheShellEnvironment(t *testing.T) {
	t.Parallel()

	runs := 0
	opts := testOptions(t, ".claude")
	opts.Version = "test"
	opts.ShellStarter = fakeShell("PATH=/opt/homebrew/bin:/usr/bin\x00", &runs)
	c := bootstrap(t, opts)

	assert.Zero(t, runs, "starting the app reads no shell: that waits for the first paint")

	_, read := c.ShellEnv.Env()
	assert.False(t, read)

	c.Deferred.Run(t.Context())

	assert.Equal(t, 1, runs)

	select {
	case <-c.ShellEnv.Ready():
	default:
		require.FailNow(t, "the deferred work read the shell's environment")
	}

	env, read := c.ShellEnv.Env()
	require.True(t, read)
	assert.Equal(t, []string{"PATH=/opt/homebrew/bin:/usr/bin"}, env)
}

func TestDeferredStopsBetweenStepsWhenCanceled(t *testing.T) {
	t.Parallel()

	ran := 0
	d := &Deferred{steps: []func(context.Context){
		func(context.Context) { ran++ },
		func(context.Context) { ran++ },
	}}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	d.Run(ctx)
	assert.Zero(t, ran)

	d.Run(t.Context())
	assert.Equal(t, 2, ran)
}
