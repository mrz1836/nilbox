//go:build unix

package manifests

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
)

// TestANamedPipeForFetchHeadNeverHangsARead checks that FETCH_HEAD is only looked at,
// never opened: a named pipe swapped in for it would hang a read that opened it, as no one
// writes to it.
func TestANamedPipeForFetchHeadNeverHangsARead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	fetchHead := filepath.Join(t.TempDir(), "FETCH_HEAD")
	require.NoError(t, syscall.Mkfifo(fetchHead, 0o600))
	require.NoError(t, os.Chtimes(fetchHead, daysAgo(2), daysAgo(2)))

	fr := moduleRepo(commitID(1), daysAgo(10), daysAgo(5))
	fr.fetchHead = fetchHead
	r.clone("api", fr)

	page := r.list(false)
	require.Len(t, page.Clones, 1)
	assert.Equal(t, clock.Format(daysAgo(2)), page.Clones[0].FetchedAt)
}

// TestRevealShowsANamedPipeWithoutOpeningIt checks that Reveal only looks at the file it
// shows: a named pipe at a manifest's path is shown, never opened.
func TestRevealShowsANamedPipeWithoutOpeningIt(t *testing.T) {
	t.Parallel()

	opener := &fakeOpener{}
	r, svc := openRig(t, opener)
	pipe := filepath.Join(r.root, "api", "go.mod")
	require.NoError(t, syscall.Mkfifo(pipe, 0o600))

	require.NoError(t, svc.Reveal(t.Context(), "~/projects/api", goModFile))
	assert.Equal(t, []string{pipe}, opener.shown)
}
