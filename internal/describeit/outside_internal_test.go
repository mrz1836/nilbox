package describeit

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/mcpserver"
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

	for i := range kit.MaxOutsideRead + 5 {
		id := testNow.Add(time.Duration(i)*time.Second).UTC().Format("20060102T150405Z") + "-0000000000" +
			strconv.FormatInt(int64(10+i%90), 10)
		require.NoError(t, os.WriteFile(r.path("proposals", Outside, id+".json"), nil, 0o600))
	}

	require.NoError(t, os.WriteFile(r.path("proposals", Outside, "notes.json"), nil, 0o600))

	ids := r.svc.outsideIDs(t.Context())
	require.Len(t, ids, kit.MaxOutsideRead, "the newest, as the tool mode counts them")
	assert.Equal(t, testNow.Add(5*time.Second).UTC().Format("20060102T150405Z"), ids[0][:16], "the oldest left out")
}

// fillOutside writes n proposals of p from outside, a second apart from base, and returns
// their IDs, the oldest first.
func (r *rig) fillOutside(base time.Time, n int, p kit.Proposal) []string {
	r.t.Helper()

	ids := make([]string, 0, n)
	for i := range n {
		ids = append(ids, r.writeProposal(Outside, base.Add(time.Duration(i)*time.Second), 0, p))
	}

	return ids
}

// toolModeFull reports whether the tool mode, serving the user's own Claude Code over the
// rig's describe folder, refuses a proposal for the outside folder's cap.
func (r *rig) toolModeFull() bool {
	r.t.Helper()

	call, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": kit.ToolPropose, "arguments": map[string]any{"proposal": pinProposal()}},
	})
	require.NoError(r.t, err)

	var stdout, stderr bytes.Buffer

	code, handled := mcpserver.Serve([]string{mcpserver.Arg(r.dir)}, bytes.NewReader(append(call, '\n')), &stdout, &stderr)
	require.True(r.t, handled)
	require.Zero(r.t, code, stderr.String())

	return strings.Contains(stdout.String(), "already wait for the user")
}

func TestTheToolModeCountsWaitingProposalsAsStatusDoes(t *testing.T) {
	t.Parallel()

	p := pinProposal()

	for _, tt := range []struct {
		name    string
		fill    func(r *rig)
		pending int
	}{
		{"49 waiting", func(r *rig) { r.fillOutside(testNow, 49, p) }, 49},
		{"50 waiting", func(r *rig) { r.fillOutside(testNow, 50, p) }, 50},
		{"50, one dealt with whole", func(r *rig) {
			r.settleAll(r.fillOutside(testNow, 50, p)[0], p, testNow)
		}, 49},
		{"50, one dealt with in part", func(r *rig) {
			_, err := r.svc.addOutcomes(r.t.Context(), Outside, []kit.Outcome{{
				Proposal: r.fillOutside(testNow, 50, p)[0], Part: kit.PartRoutine, Kind: kit.OutcomeAdded,
				Words: "Added “Pin failing PRs”, switched off", At: testNow,
			}})
			require.NoError(r.t, err)
		}, 50},
		{"50, one naming a conversation", func(r *rig) {
			id := r.fillOutside(testNow, 50, p)[0]
			require.NoError(r.t, os.WriteFile(r.path("proposals", Outside, id+".json"),
				proposalFile(r.t, uuid.NewString(), id, testNow, p), 0o600))
		}, 49},
		{"50 waiting, older than the newest the app reads", func(r *rig) {
			r.fillOutside(testNow.Add(-time.Hour), 50, p)

			for _, id := range r.fillOutside(testNow, kit.MaxOutsideRead, p) {
				r.settleAll(id, p, testNow)
			}
		}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t)
			tt.fill(r)

			st, err := r.svc.Status(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.pending, st.Pending)
			assert.Equal(t, st.Pending >= kit.MaxOutsidePending, r.toolModeFull(),
				"the tool mode refuses another exactly when the app counts the cap's cards waiting")
		})
	}
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

func TestCardsSettledLongAgoArePrunedPastTheNewestRead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	p := pinProposal()

	old := r.writeProposal(Outside, testNow.Add(-40*24*time.Hour), 0, p)
	r.settleAll(old, p, testNow.Add(-31*24*time.Hour))
	r.fillOutside(testNow.Add(-time.Hour), kit.MaxOutsideRead, p)

	require.NotContains(t, r.svc.outsideIDs(t.Context()), old, "past the newest the cards are read from")

	r.svc.pruneOutside(t.Context())

	assert.NoFileExists(t, r.path("proposals", Outside, old+".json"), "a card dealt with long ago goes wherever it is")
}
