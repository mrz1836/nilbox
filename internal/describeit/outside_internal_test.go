package describeit

import (
	"context"
	"os"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
)

// settleAll records every part of the outside card id as dealt with at at.
func (r *rig) settleAll(id string, p kit.Proposal, at time.Time) {
	r.t.Helper()

	items := make([]kit.Outcome, 0, len(parts(p)))
	for _, part := range parts(p) {
		items = append(items, kit.Outcome{
			Proposal: id, Part: part.part, Index: part.index, Kind: kit.OutcomeDiscarded, Words: "Discarded it", At: at,
		})
	}

	_, err := r.svc.addOutcomes(r.t.Context(), Outside, items)
	require.NoError(r.t, err)
}

func TestProposalsAreTheCardsFromOutsideTheNewestFirst(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	p := pinProposal()

	pending := r.writeProposal(Outside, testNow.Add(-3*time.Hour), 0, p)
	settled := r.writeProposal(Outside, testNow.Add(-2*time.Hour), 1, p)
	old := r.writeProposal(Outside, testNow.Add(-50*time.Hour), 2, p)
	newest := r.writeProposal(Outside, testNow.Add(-time.Hour), 3, p)
	half := r.writeProposal(Outside, testNow.Add(-4*time.Hour), 4, p)

	r.settleAll(settled, p, testNow.Add(-2*time.Hour))
	r.settleAll(old, p, testNow.Add(-49*time.Hour))

	_, err := r.svc.addOutcomes(t.Context(), Outside, []kit.Outcome{{
		Proposal: half, Part: kit.PartRoutine, Kind: kit.OutcomeAdded, Words: "Added it", At: testNow.Add(-40 * time.Hour),
	}})
	require.NoError(t, err)

	cards, err := r.svc.Proposals(t.Context())
	require.NoError(t, err)

	ids := make([]string, 0, len(cards))
	for _, c := range cards {
		ids = append(ids, c.Ref.Proposal)
	}

	assert.Equal(t, []string{newest, settled, pending, half}, ids,
		"the pending cards and those settled in the last day, the newest first")

	card := cards[0]
	assert.Equal(t, CardRef{Source: Outside, Proposal: newest}, card.Ref)
	assert.Equal(t, "example-repo", card.Where, "where the session ran")
	assert.Zero(t, card.Turn)
	assert.True(t, cards[1].Settled)
	assert.False(t, cards[3].Settled, "a card with a part left is pending, however old")

	st, err := r.svc.Status(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 3, st.Pending, "the cards with a part no one dealt with")
}

func TestProposalsWithNoFolderAreNone(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	cards, err := r.svc.Proposals(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, cards)
	assert.Empty(t, cards)
}

func TestOnlyTheNewestOutsideFilesAreRead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	require.NoError(t, os.MkdirAll(r.path("proposals", Outside), 0o700))

	for i := range maxOutsideRead + 5 {
		id := testNow.Add(time.Duration(i)*time.Second).UTC().Format("20060102T150405Z") + "-0000000000" +
			strconv.FormatInt(int64(10+i%90), 10)
		require.NoError(t, os.WriteFile(r.path("proposals", Outside, id+".json"), nil, 0o600))
	}

	require.NoError(t, os.WriteFile(r.path("proposals", Outside, "notes.json"), nil, 0o600))

	ids := r.svc.outsideIDs(t.Context())
	require.Len(t, ids, maxOutsideRead, "past the tool mode's own cap, only something else wrote them")
	assert.Equal(t, testNow.Add(5*time.Second).UTC().Format("20060102T150405Z"), ids[0][:16], "the oldest left out")
}

func TestTheWindowHearsWhenACardComesOrGoes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		require.NoError(t, os.MkdirAll(r.path("proposals", Outside), 0o700))

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			r.svc.followOutside(ctx)
		}()

		synctest.Wait()
		assert.Zero(t, r.sink.proposalsEvents(), "what was there as the app started is no news")

		id := r.writeProposal(Outside, testNow, 0, pinProposal())

		time.Sleep(outsideEvery)
		synctest.Wait()
		assert.Equal(t, 1, r.sink.proposalsEvents(), "a card came")

		time.Sleep(outsideEvery)
		synctest.Wait()
		assert.Equal(t, 1, r.sink.proposalsEvents(), "nothing changed")

		r.settleAll(id, pinProposal(), testNow)
		time.Sleep(outsideEvery)
		synctest.Wait()
		assert.Equal(t, 2, r.sink.proposalsEvents(), "a card was dealt with")

		require.NoError(t, os.Remove(r.path("proposals", Outside, id+".json")))
		time.Sleep(outsideEvery)
		synctest.Wait()
		assert.Equal(t, 3, r.sink.proposalsEvents(), "a card went")

		cancel()
		<-done
	})
}

func TestCardsFromOutsideSettledLongAgoArePruned(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	p := pinProposal()

	old := r.writeProposal(Outside, testNow.Add(-40*24*time.Hour), 0, p)
	pending := r.writeProposal(Outside, testNow.Add(-40*24*time.Hour), 1, p)
	recent := r.writeProposal(Outside, testNow.Add(-40*24*time.Hour), 2, p)

	r.settleAll(old, p, testNow.Add(-31*24*time.Hour))
	r.settleAll(recent, p, testNow.Add(-29*24*time.Hour))

	r.svc.pruneOutside(t.Context())

	assert.NoFileExists(t, r.path("proposals", Outside, old+".json"), "settled more than 30 days ago")
	assert.FileExists(t, r.path("proposals", Outside, pending+".json"), "a card with a part left stays")
	assert.FileExists(t, r.path("proposals", Outside, recent+".json"))

	o, err := r.svc.readOutcomes(Outside)
	require.NoError(t, err)

	for _, item := range o.Items {
		assert.NotEqual(t, old, item.Proposal, "a pruned card's outcomes go with it")
	}

	assert.Len(t, o.Items, len(parts(p)))
}
