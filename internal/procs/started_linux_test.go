//go:build linux

package procs

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// statLine is a /proc/<pid>/stat line whose command holds a space and parentheses, with
// start time 250 ticks (2.5 s) after boot.
const statLine = "123 (a (b) c) S 1 123 123 0 -1 4194560 100 0 0 0 1 1 0 0 20 0 1 0 250 1000 100\n"

func TestStartedAtInReadsTheProcessTable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	testutil.WriteFile(t, filepath.Join(root, "stat"), "cpu 1 2 3\nbtime 1700000000\nprocesses 9\n")
	testutil.WriteFile(t, filepath.Join(root, "123", "stat"), statLine)

	assert.Equal(t, time.Unix(1700000000, 0).Add(2500*time.Millisecond), startedAtIn(root, 123))
}

func TestStartedAtInIsZeroWhenUnreadable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, boot, stat string
	}{
		{"no process", "btime 1700000000\n", ""},
		{"no boot time", "cpu 1\n", statLine},
		{"no closing parenthesis", "btime 1700000000\n", "123 (cut S 1"},
		{"too few fields", "btime 1700000000\n", "123 (sh) S 1 2 3\n"},
		{"not a number", "btime 1700000000\n", "123 (sh) S 1 123 123 0 -1 4 1 0 0 0 1 1 0 0 20 0 1 0 soon 1 1\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			testutil.WriteFile(t, filepath.Join(root, "stat"), tt.boot)

			if tt.stat != "" {
				testutil.WriteFile(t, filepath.Join(root, "123", "stat"), tt.stat)
			}

			assert.True(t, startedAtIn(root, 123).IsZero())
		})
	}
}
