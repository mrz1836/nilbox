package launcher

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

var (
	errFakeItems    = errors.New("items unreadable")
	errFakeViewer   = errors.New("signed-in account unreadable")
	errFakeRemember = errors.New("clone not remembered")
)

// failingItems is an ItemStore whose calls fail where an error is set, and otherwise go
// to the store it wraps.
type failingItems struct {
	ItemStore

	launchErr, viewerErr, rememberErr error
}

func (f failingItems) LaunchItems(ctx context.Context, ids []int64) ([]StoredItem, error) {
	if f.launchErr != nil {
		return nil, f.launchErr
	}

	return f.ItemStore.LaunchItems(ctx, ids)
}

func (f failingItems) ViewerLogin(ctx context.Context) (string, error) {
	if f.viewerErr != nil {
		return "", f.viewerErr
	}

	return f.ItemStore.ViewerLogin(ctx)
}

func (f failingItems) RememberClone(ctx context.Context, repoID int64, path string) error {
	if f.rememberErr != nil {
		return f.rememberErr
	}

	return f.ItemStore.RememberClone(ctx, repoID, path)
}

func TestLaunchesFailWhenTheItemsCannotBeRead(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.cfg.Items = failingItems{ItemStore: h.cfg.Items, launchErr: errFakeItems}
	svc := h.service()

	_, err := svc.Resolve(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.ErrorIs(t, err, errFakeItems)
	require.ErrorContains(t, err, "load the item")

	_, err = svc.ResolveItems(t.Context(), []int64{h.trustedPR})
	require.ErrorIs(t, err, errFakeItems)

	_, err = svc.Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.ErrorIs(t, err, errFakeItems)

	_, err = NewPromptSource(svc).PreviewData(t.Context(), h.trustedPR)
	require.ErrorIs(t, err, errFakeItems)

	assert.Empty(t, h.iterm.opened, "nothing is launched")
}

func TestLaunchesFailWhenTheSignedInAccountCannotBeRead(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.cfg.Items = failingItems{ItemStore: h.cfg.Items, viewerErr: errFakeViewer}
	svc := h.service()

	_, err := svc.Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.ErrorIs(t, err, errFakeViewer)

	_, err = NewPromptSource(svc).PreviewData(t.Context(), h.trustedPR)
	require.ErrorIs(t, err, errFakeViewer)

	assert.Empty(t, h.iterm.opened, "nothing is launched")
}

func TestALaunchGoesOnWhenItsCloneCannotBeRemembered(t *testing.T) {
	t.Parallel()

	var logs testutil.Logs

	h := newLaunchHarness(t)
	h.cfg.Items = failingItems{ItemStore: h.cfg.Items, rememberErr: errFakeRemember}
	h.cfg.Logger = logs.Logger()

	res, _ := h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.Equal(t, "~/projects/example-repo", res.ClonePath)
	assert.Empty(t, h.cachedPath(t, exampleRepo), "the next launch scans for the clone again")
	assert.Contains(t, logs.String(), `level=WARN msg="could not remember a repository's local clone"`)
	assert.Contains(t, logs.String(), errFakeRemember.Error())
}
