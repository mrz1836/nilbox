package ghthreads

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/ghthreads/internal/queries"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// stateRequests returns the refs each states query among got asked about.
func stateRequests(got []listedRequest) [][]string {
	var out [][]string

	for _, r := range got {
		if r.kind == "states" {
			out = append(out, r.refs)
		}
	}

	return out
}

// exampleRef is the fixtures' repository's item number, as a states query names it.
func exampleRef(number string) string {
	return owner + "/" + exampleRepo + "#" + number
}

func TestStatesCheckedWhen(t *testing.T) {
	t.Parallel()

	now := start.Add(time.Hour)
	ago := func(d time.Duration) *time.Time {
		at := now.Add(-d)

		return &at
	}

	tests := []struct {
		name    string
		state   string
		updated time.Time
		checked *time.Time
		want    bool
	}{
		{name: "unknown: never checked", updated: start, want: true},
		{name: "its thread updated since the check", state: "OPEN", updated: now.Add(-time.Minute), checked: ago(5 * time.Minute), want: true},
		{name: "closed, but its thread updated since", state: "CLOSED", updated: now.Add(-time.Minute), checked: ago(5 * time.Minute), want: true},
		{name: "open, checked 30 minutes ago", state: "OPEN", updated: start, checked: ago(StateRecheck), want: true},
		{name: "open, checked over 30 minutes ago", state: "OPEN", updated: start, checked: ago(31 * time.Minute), want: true},
		{name: "open, checked under 30 minutes ago", state: "OPEN", updated: start, checked: ago(29 * time.Minute), want: false},
		{name: "closed, not updated", state: "CLOSED", updated: start.Add(-72 * time.Hour), checked: ago(48 * time.Hour), want: false},
		{name: "merged, not updated", state: "MERGED", updated: start.Add(-72 * time.Hour), checked: ago(48 * time.Hour), want: false},
		{name: "unresolved, stamped under 30 minutes ago", updated: start, checked: ago(29 * time.Minute), want: false},
		{name: "unresolved, stamped over 30 minutes ago", updated: start, checked: ago(31 * time.Minute), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			row := queries.ListItemThreadsRow{
				ThreadID: firstID, RepoOwner: owner, RepoName: exampleRepo, ItemKind: "pr", Number: 42,
				UpdatedAt: tt.updated, SubjectState: tt.state, StateCheckedAt: tt.checked,
			}

			assert.Equal(t, tt.want, stateDue(&row, now))
		})
	}
}

func TestDueRefsAsksEachItemOnceAndWhatIsAsked(t *testing.T) {
	t.Parallel()

	now := start.Add(time.Hour)
	checked := now.Add(-time.Minute)
	row := func(id int64, kind string, number int64, at *time.Time) queries.ListItemThreadsRow {
		return queries.ListItemThreadsRow{
			ThreadID: id, RepoOwner: owner, RepoName: exampleRepo, ItemKind: kind, Number: number, UpdatedAt: start,
			SubjectState: "OPEN", StateCheckedAt: at,
		}
	}

	rows := []queries.ListItemThreadsRow{
		row(firstID, "pr", 42, nil),
		row(firstID+1, "pr", 42, nil), // a second thread about the same pull request
		row(firstID+2, "issue", 7, &checked),
		row(firstID+3, "pr", 9, &checked),
		row(firstID+4, "", 0, nil), // no item: never asked about
	}

	name := repo.Name{Owner: owner, Repo: exampleRepo}
	asked := []item.Ref{
		{Repo: repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"}, Number: 7}, // what the inbox saw close
		{Repo: name, Number: 500}, // no thread is about it
	}

	assert.Equal(t, []item.Ref{
		{Repo: name, Number: 42, Kind: item.KindPR},
		{Repo: name, Number: 7, Kind: item.KindIssue},
	}, dueRefs(rows, asked, now))
}

func TestStatesChecked(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	issue := fakeThread{
		id: firstID + 2, unread: false, reason: "author", updated: start.Add(-2 * time.Minute), title: "The theme is forgotten",
		repo: owner + "/" + exampleRepo, subject: "issues/7", typ: "Issue",
	}
	second := prThread(firstID+1, 42, start.Add(-time.Minute))
	second.reason = "mention"

	r.gh.set([]fakeThread{
		prThread(firstID, 42, start),
		second, // a second thread about the same pull request
		issue,
		prThread(firstID+3, 43, start.Add(-3*time.Minute)), // GitHub can't find it
		{
			id: firstID + 4, unread: true, reason: "subscribed", updated: start.Add(-4 * time.Minute), title: "v1.2.0",
			repo: owner + "/" + exampleRepo, subject: "releases/1", typ: "Release",
		},
		{
			id: firstID + 5, unread: true, reason: "ci_activity", updated: start.Add(-5 * time.Minute), title: "CI failed",
			repo: owner + "/" + exampleRepo, typ: "CheckSuite",
		},
	})
	r.gh.configure(func(f *fakeGitHub) {
		f.states = map[string]string{exampleRef("42"): github.StateOpen, exampleRef("7"): github.StateClosed}
	})

	got := stateRequests(r.mustPass())
	require.Len(t, got, 1, "one request for every item due")
	assert.ElementsMatch(t, []string{exampleRef("42"), exampleRef("7"), exampleRef("43")}, got[0],
		"each pull request and issue once; a release and a check suite never")

	open, _ := r.row(viewerLogin, firstID)
	assert.Equal(t, "OPEN", open.state)
	require.NotNil(t, open.checkedAt)
	assert.True(t, open.checkedAt.Equal(start))

	shared, _ := r.row(viewerLogin, firstID+1)
	assert.Equal(t, "OPEN", shared.state, "both threads about it")

	closed, _ := r.row(viewerLogin, firstID+2)
	assert.Equal(t, "CLOSED", closed.state)

	missing, _ := r.row(viewerLogin, firstID+3)
	assert.Empty(t, missing.state, "GitHub couldn't find it: its state stays")
	require.NotNil(t, missing.checkedAt, "but it is stamped checked")

	release, _ := r.row(viewerLogin, firstID+4)
	assert.Nil(t, release.checkedAt, "a release is never checked")

	var closedAt *time.Time

	require.NoError(t, r.db.QueryRowContext(t.Context(),
		`SELECT subject_closed_at FROM gh_threads WHERE thread_id = ?`, firstID+2).Scan(&closedAt))
	require.NotNil(t, closedAt)
	assert.True(t, closedAt.Equal(start))

	// Five minutes on, nothing is due, on a 304 as on any pass.
	r.clock.Advance(5 * time.Minute)

	passed := r.mustPass()
	assert.Equal(t, []int{1}, pagesOf(passed))
	assert.Empty(t, stateRequests(passed))

	// Half an hour on: the open one and the one GitHub couldn't find, never the closed one.
	r.clock.Advance(26 * time.Minute)

	got = stateRequests(r.mustPass())
	require.Len(t, got, 1, "checked on a 304 too")
	assert.ElementsMatch(t, []string{exampleRef("42"), exampleRef("43")}, got[0])

	// The closed issue's thread moves, as when it is reopened: it is checked again.
	r.gh.update(firstID+2, start.Add(40*time.Minute))
	r.gh.configure(func(f *fakeGitHub) { f.states[exampleRef("7")] = github.StateOpen })
	r.clock.Advance(5 * time.Minute)

	got = stateRequests(r.mustPass())
	require.Len(t, got, 1)
	assert.Equal(t, []string{exampleRef("7")}, got[0])

	reopened, _ := r.row(viewerLogin, firstID+2)
	assert.Equal(t, "OPEN", reopened.state)
}

func TestStatesFailureKeepsListing(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(2, start))
	r.gh.configure(func(f *fakeGitHub) { f.statesStatus = http.StatusBadGateway })

	_, err := r.pass()
	require.Error(t, err)

	assert.Equal(t, 2, r.rows(viewerLogin), "the listing is stored")

	row, _ := r.row(viewerLogin, firstID)
	assert.Empty(t, row.state)
	assert.Nil(t, row.checkedAt, "no state stored")

	st := r.state()
	assert.NotEmpty(t, st.ETag, "the listing's ETag stands")
	assert.Equal(t, AccessOK, st.Access)
	assert.NotEmpty(t, st.LastError)
	assert.Equal(t, st.LastError, r.snapshot().Status.LastError)

	// GitHub answers again: the next pass checks them.
	r.gh.configure(func(f *fakeGitHub) {
		f.statesStatus = 0
		f.states = map[string]string{exampleRef("1"): github.StateMerged}
	})
	r.clock.Advance(5 * time.Minute)

	got := stateRequests(r.mustPass())
	require.Len(t, got, 1)
	assert.ElementsMatch(t, []string{exampleRef("1"), exampleRef("2")}, got[0])

	merged, _ := r.row(viewerLogin, firstID)
	assert.Equal(t, "MERGED", merged.state)
	assert.Empty(t, r.state().LastError)
}
