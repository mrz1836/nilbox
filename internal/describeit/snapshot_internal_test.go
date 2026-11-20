package describeit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestTheLiveSnapshotIsKeptTwoSeconds(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	snap, err := r.svc.liveSnapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Nightly", snap.Setup.Routines[0].Name)

	_, err = r.svc.liveSnapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, r.setup.count(), "kept")

	r.clock.Advance(snapshotTTL - time.Millisecond)

	_, err = r.svc.liveSnapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, r.setup.count(), "for two seconds")

	r.clock.Advance(time.Millisecond)

	_, err = r.svc.liveSnapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, r.setup.count(), "then read again")

	NewWorker(r.svc).Changed()

	_, err = r.svc.liveSnapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 3, r.setup.count(), "a change forgets it at once")

	r.clock.Advance(-time.Hour)

	_, err = r.svc.liveSnapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 4, r.setup.count(), "a clock set back doesn't keep it longer")
}

// changingSetup is a setup that changes while it is read: each read tells the service of
// a change, as a save landing mid-read would.
type changingSetup struct {
	svc   *Service
	reads int
}

// Snapshot tells of a change, then returns the test's snapshot.
func (c *changingSetup) Snapshot(context.Context) (kit.Snapshot, error) {
	c.reads++
	c.svc.forget()

	return testSnapshot(), nil
}

func TestASnapshotReadAcrossAChangeIsntKept(t *testing.T) {
	t.Parallel()

	setup := &changingSetup{}
	svc := New(Config{Dir: t.TempDir(), Setup: setup})
	setup.svc = svc

	_, err := svc.liveSnapshot(t.Context())
	require.NoError(t, err)

	_, err = svc.liveSnapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, setup.reads, "what was read as the setup changed is read again")
}

func TestTheLastSnapshotStandsInWhenTheSetupFails(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.setup.fail(errFake)

	_, err := r.svc.liveSnapshot(t.Context())
	require.ErrorIs(t, err, errFake, "with none read before, a setup that fails is an error")
	assert.Equal(t, "Describe it couldn't read your setup.", uierr.Message(err))

	r.setup.fail(nil)

	_, err = r.svc.liveSnapshot(t.Context())
	require.NoError(t, err)

	changed := testSnapshot()
	changed.Setup.Routines[0].Name = "Renamed"
	r.setup.change(changed)
	r.setup.fail(errFake)
	r.svc.forget()

	snap, err := r.svc.liveSnapshot(t.Context())
	require.NoError(t, err, "the last snapshot read stands in")
	assert.Equal(t, "Nightly", snap.Setup.Routines[0].Name)

	bare := New(Config{Dir: t.TempDir()})

	_, err = bare.liveSnapshot(t.Context())
	require.ErrorIs(t, err, errNoSetup)
	require.ErrorIs(t, bare.writeContext(t.Context()), errNoSetup)
}

func TestACardWithNoSetupToCheckItAgainstIsntShown(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.conversationWith(pinProposal())
	r.setup.fail(errFake)

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err, "the conversation still shows")
	assert.Empty(t, conv.Turns[0].Cards, "without its card, which can't be checked")
	assert.NotNil(t, conv.Turns[0].Cards)
}
