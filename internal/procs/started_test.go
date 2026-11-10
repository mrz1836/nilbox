//go:build darwin || linux

package procs_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/procs"
)

func TestStartedAtReadsThisProcess(t *testing.T) {
	t.Parallel()

	started := procs.StartedAt(os.Getpid())

	assert.False(t, started.IsZero(), "this process's start time is readable")
	assert.False(t, started.After(time.Now()), "a process starts before it is asked")
	assert.True(t, started.After(time.Now().Add(-24*time.Hour)), "this test process started recently: %s", started)
	assert.Equal(t, started, procs.StartedAt(os.Getpid()), "the same process reads the same start time every time")
}

func TestStartedAtIsZeroForNoProcess(t *testing.T) {
	t.Parallel()

	for _, pid := range []int{-1, 0, procs.MaxPID + 1, procs.MaxPID} {
		assert.True(t, procs.StartedAt(pid).IsZero(), "pid %d", pid)
	}
}
