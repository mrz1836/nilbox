package autopilot

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// itemReads is the inbox, counting the single-item reads an engine asks it for.
type itemReads struct {
	quiet

	n atomic.Int32
}

func (r *itemReads) ReadItem(context.Context, int64) error {
	r.n.Add(1)

	return nil
}

func TestAStartAutopilotWasTurnedOffBeforeReadsNothingFromGitHub(t *testing.T) {
	t.Parallel()

	svc := NewService(Config{Pool: testutil.DB(t), Prefs: switchedOn(false)})
	items := &itemReads{}
	e := NewEngine(svc, EngineConfig{Items: items, Lock: NewLock(filepath.Join(t.TempDir(), "autopilot.lock"))})

	got, err := e.freshRead(t.Context(), spec.Spec{}, Job{Item: ItemRef{ID: 7}})
	require.ErrorIs(t, err, ErrOff, "the freshness read before a start asks whether Autopilot is on")
	assert.Empty(t, got.hold)
	assert.Zero(t, items.n.Load(), "and spends no read while it is off")

	e.on.Store(true)

	_, err = e.freshRead(t.Context(), spec.Spec{}, Job{Item: ItemRef{ID: 7}})
	require.NoError(t, err)
	assert.Equal(t, int32(1), items.n.Load(), "on, it reads the item")
}
