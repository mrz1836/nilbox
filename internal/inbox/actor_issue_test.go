package inbox

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// closeIssue is an action that closes the issue, counting its runs.
func closeIssue(runs *int) func(context.Context, github.Token, IssueTarget) (IssueActed, error) {
	return func(context.Context, github.Token, IssueTarget) (IssueActed, error) {
		*runs++

		return IssueActed{Closed: true}, nil
	}
}

func TestActIssueRecordsAClose(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)

	var got IssueTarget

	snap, err := NewActor(h.svc).ActIssue(t.Context(), ids[3], func(_ context.Context, tok github.Token, target IssueTarget) (IssueActed, error) {
		got = target

		assert.False(t, tok.IsZero())

		return IssueActed{Closed: true}, nil
	})
	require.NoError(t, err)

	assert.Equal(t, ids[3], got.ID)
	assert.Equal(t, "acme-org/example-repo#3", got.Ref.String())
	assert.Equal(t, item.KindIssue, got.Ref.Kind)
	assert.Equal(t, nodeID(3), got.NodeID)
	assert.Equal(t, octoUser, got.Login)
	assert.True(t, got.Assigned)
	assert.False(t, got.Projects, "an action reads no projects")

	assert.Equal(t, []int{1}, numbers(snap.Assigned), "a close takes it out of every list at once")
	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 3))
}

func TestActIssueThatClosesNothingStoresNothing(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	before := len(h.events.All())

	snap, err := NewActor(h.svc).ActIssue(t.Context(), ids[3], func(context.Context, github.Token, IssueTarget) (IssueActed, error) {
		return IssueActed{}, nil
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []int{1, 3}, numbers(snap.Assigned))
	assert.Len(t, h.events.All(), before, "nothing changed")
}

func TestActIssueRefusesAPullRequestAndWhatIsNotListed(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	runs := 0
	actor := NewActor(h.svc)

	_, err := actor.ActIssue(t.Context(), ids[1], closeIssue(&runs))
	require.ErrorIs(t, err, ErrNotIssue, "a pull request")
	require.NotErrorIs(t, err, ErrNotPull)

	_, err = actor.ActIssue(t.Context(), 9999, closeIssue(&runs))
	require.ErrorIs(t, err, item.ErrNotFound)

	h.exec(t, "UPDATE items SET reason_assigned = 0 WHERE id = ?", ids[3])

	_, err = actor.ActIssue(t.Context(), ids[3], closeIssue(&runs))
	require.ErrorIs(t, err, ErrNotIssue, "an issue in no list")
	assert.Contains(t, uierr.Message(err), "acme-org/example-repo#3 is no longer in your inbox.")
	assert.Zero(t, runs, "nothing was sent")
}

func TestActIssueRefusesAnotherAccountsToken(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.svc = h.service()
	h.client.viewer = "someone-else"
	runs := 0

	_, err := NewActor(h.svc).ActIssue(t.Context(), ids[3], closeIssue(&runs))
	require.ErrorIs(t, err, ErrOtherAccount)
	assert.Zero(t, runs)
	assert.True(t, h.reasons(t, 3)[2], "still assigned")
}

func TestActIssueRetriesARefusedTokenWithoutRepeatingAStep(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	commented, tries := false, 0

	snap, err := NewActor(h.svc).ActIssue(t.Context(), ids[3], func(context.Context, github.Token, IssueTarget) (IssueActed, error) {
		tries++

		if !commented {
			commented = true

			return IssueActed{}, &github.StatusError{StatusCode: http.StatusUnauthorized, Message: "Bad credentials"}
		}

		return IssueActed{Closed: true}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, tries, "tried once more with a fresh token")
	assert.Equal(t, []int{1}, numbers(snap.Assigned))
}

func TestReadIssueRewritesTheCard(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)

	now := byOther(issue("example-repo", 3, "MEMBER"))
	now.State = github.StateOpen
	now.Title = "Item 3, retitled"
	now.UpdatedAt = h.clock.Now()
	details := github.ItemDetails{Comments: 6, Labels: []github.Label{{Name: "bug", Color: "#d73a4a"}}}

	var target IssueTarget

	snap, err := NewActor(h.svc).ReadIssue(t.Context(), ids[3], func(_ context.Context, _ github.Token, t IssueTarget) (IssueRead, error) {
		target = t

		return IssueRead{Update: &github.ItemUpdate{Found: true, Item: now, Details: details, RateLimit: itemRate()}}, nil
	})
	require.NoError(t, err)

	assert.Equal(t, ids[3], target.ID)
	it := itemByNumber(t, snap.Assigned, 3)
	assert.Equal(t, "Item 3, retitled", it.Title)
	require.NotNil(t, it.Details)
	assert.Equal(t, 6, it.Details.Comments)
	assert.Nil(t, it.Files, "an issue's files are never checked")
}

func TestReadIssueAsksForProjectsAsTheLastRefreshCould(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.setPrefs(t, map[string]string{prefProjects: string(ProjectAccessOn)})

	var target IssueTarget

	_, err := NewActor(h.svc).ReadIssue(t.Context(), ids[3], func(_ context.Context, _ github.Token, t IssueTarget) (IssueRead, error) {
		target = t

		return IssueRead{Update: &github.ItemUpdate{Found: true, Item: byOther(issue("example-repo", 3, "MEMBER")), RateLimit: itemRate()}}, nil
	})
	require.NoError(t, err)
	assert.True(t, target.Projects)
}

func TestReadIssueOfAClosedIssueUnlistsIt(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)

	closed := byOther(issue("example-repo", 3, "MEMBER"))
	closed.State = github.StateClosed

	snap, err := NewActor(h.svc).ReadIssue(t.Context(), ids[3], func(context.Context, github.Token, IssueTarget) (IssueRead, error) {
		return IssueRead{Update: &github.ItemUpdate{Found: true, Item: closed, RateLimit: itemRate()}}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, []int{1}, numbers(snap.Assigned))

	// It reads an issue in no list too: its card is still the sheet's to read.
	_, err = NewActor(h.svc).ReadIssue(t.Context(), ids[3], func(context.Context, github.Token, IssueTarget) (IssueRead, error) {
		return IssueRead{Update: &github.ItemUpdate{Found: false, RateLimit: itemRate()}}, nil
	})
	require.NoError(t, err)
}

func TestReadIssueFailures(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	actor := NewActor(h.svc)

	_, err := actor.ReadIssue(t.Context(), ids[3], func(context.Context, github.Token, IssueTarget) (IssueRead, error) {
		return IssueRead{}, errMergeRefused
	})
	require.ErrorIs(t, err, errMergeRefused)

	_, err = actor.ReadIssue(t.Context(), ids[3], func(context.Context, github.Token, IssueTarget) (IssueRead, error) {
		return IssueRead{}, nil
	})
	require.ErrorIs(t, err, ErrIncompleteFetch)

	_, err = actor.ReadIssue(t.Context(), ids[1], func(context.Context, github.Token, IssueTarget) (IssueRead, error) {
		return IssueRead{}, nil
	})
	require.ErrorIs(t, err, ErrNotIssue, "a pull request")
}

func TestPeekIssueStoresNothing(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	before := len(h.events.All())
	runs := 0

	err := NewActor(h.svc).PeekIssue(t.Context(), ids[3], func(_ context.Context, tok github.Token, target IssueTarget) error {
		runs++

		assert.False(t, tok.IsZero())
		assert.Equal(t, nodeID(3), target.NodeID)

		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, runs)
	assert.Len(t, h.events.All(), before, "nothing stored, so nothing changed")

	err = NewActor(h.svc).PeekIssue(t.Context(), ids[1], func(context.Context, github.Token, IssueTarget) error {
		runs++

		return nil
	})
	require.ErrorIs(t, err, ErrNotIssue)

	err = NewActor(h.svc).PeekIssue(t.Context(), ids[3], func(context.Context, github.Token, IssueTarget) error {
		return errMergeRefused
	})
	require.ErrorIs(t, err, errMergeRefused)
	assert.Equal(t, 1, runs)
}

func TestIssueWorkWaitsForOtherWorkOnTheItem(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	unlock, err := h.svc.items.lock(t.Context(), ids[3])
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	actor := NewActor(h.svc)

	_, err = actor.ActIssue(ctx, ids[3], closeIssue(new(int)))
	require.ErrorIs(t, err, context.Canceled)

	_, err = actor.ReadIssue(ctx, ids[3], func(context.Context, github.Token, IssueTarget) (IssueRead, error) {
		return IssueRead{}, nil
	})
	require.ErrorIs(t, err, context.Canceled)

	err = actor.PeekIssue(ctx, ids[3], func(context.Context, github.Token, IssueTarget) error { return nil })
	require.ErrorIs(t, err, context.Canceled)

	unlock()
}

func TestStoreReadsAnIssueItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	is := byOther(issue("example-repo", 3, "MEMBER"))
	h.client.inbox = fetched(nil, nil, []github.Item{is})
	h.client.details = map[string]github.ItemDetails{nodeID(3): {
		Comments: 4, Labels: []github.Label{{Name: "bug", Color: "#d73a4a"}},
		SubIssues: github.SubIssues{Total: 3, Completed: 1},
	}}
	snap := h.refresh(t)
	id := itemByNumber(t, snap.Assigned, 3).ID
	h.exec(t, `UPDATE repos SET viewer_permission = 'TRIAGE', default_branch = 'main'`)
	h.exec(t, `UPDATE item_details SET projects = '[{"title":"Roadmap","url":"https://github.com/orgs/acme-org/projects/1",
		"status":"In progress","statusColor":"#bf8700","fields":[{"name":"Size","value":"M","color":""}]}]'`)

	got, err := NewStore(h.pool, h.clock).IssueItem(t.Context(), id)
	require.NoError(t, err)

	assert.Equal(t, nodeID(3), got.NodeID)
	assert.Equal(t, item.KindIssue, got.Kind)
	assert.Equal(t, "Item 3", got.Title)
	assert.Equal(t, octoUser, got.Viewer)
	assert.Equal(t, "TRIAGE", got.ViewerPermission)
	assert.Equal(t, "main", got.DefaultBranch)
	assert.True(t, got.Assigned)
	assert.True(t, got.HasDetails)
	assert.Equal(t, 4, got.Comments)
	assert.Equal(t, []github.Label{{Name: "bug", Color: "#d73a4a"}}, got.Labels)
	assert.Equal(t, github.SubIssues{Total: 3, Completed: 1}, got.SubIssues)
	assert.Equal(t, []github.ProjectItem{{
		Title: "Roadmap", URL: "https://github.com/orgs/acme-org/projects/1", Status: "In progress",
		StatusColor: "#bf8700", Fields: []github.ProjectField{{Name: "Size", Value: "M"}},
	}}, got.Projects)
	assert.True(t, got.DetailsAt.Equal(h.clock.Now()), "read by the refresh at %s, not %s", h.clock.Now(), got.DetailsAt)

	_, err = NewStore(h.pool, h.clock).IssueItem(t.Context(), 9999)
	require.ErrorIs(t, err, item.ErrNotFound)
}

func TestReadIssueRefusesAnAnswerAboutAnotherItem(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	actor := NewActor(h.svc)

	for name, other := range map[string]github.Item{
		"another issue":  byOther(issue("example-repo", 4, "MEMBER")),
		"a pull request": func() github.Item { p := byOther(issue("example-repo", 3, "MEMBER")); p.IsPR = true; return p }(),
	} {
		_, err := actor.ReadIssue(t.Context(), ids[3], func(context.Context, github.Token, IssueTarget) (IssueRead, error) {
			return IssueRead{Update: &github.ItemUpdate{Found: true, Item: other, RateLimit: itemRate()}}, nil
		})
		require.ErrorIs(t, err, ErrIncompleteFetch, name)
	}

	assert.Equal(t, "Item 3", h.itemTitle(t, ids[3]), "the card is left as it was")
}

func TestReadIssueChecksTheAccountFirst(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.svc = h.service()
	h.client.viewer = "someone-else"
	reads := 0

	_, err := NewActor(h.svc).ReadIssue(t.Context(), ids[3], func(context.Context, github.Token, IssueTarget) (IssueRead, error) {
		reads++

		return IssueRead{Update: &github.ItemUpdate{Found: false, RateLimit: itemRate()}}, nil
	})
	require.ErrorIs(t, err, ErrOtherAccount)
	assert.Zero(t, reads, "another account's answer never takes the issue out of the lists")
	assert.True(t, h.reasons(t, 3)[2])
}
