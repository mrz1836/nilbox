package assignees

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

var (
	errRead = errors.New("fake: the read failed")
	errDir  = errors.New("fake: the directory failed")
)

// fakeDirectory is a Directory in memory: a read kept makes the whole list current, or the
// search covered, and names its people as listed.
type fakeDirectory struct {
	mu       sync.Mutex
	known    Known
	covered  map[string]bool
	kept     []string
	failed   []string
	assigned []string
	err      error
}

func newDirectory(k Known) *fakeDirectory {
	return &fakeDirectory{known: k, covered: map[string]bool{}}
}

func (d *fakeDirectory) Known(context.Context, repo.Name) (Known, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.known, d.err
}

func (d *fakeDirectory) Covers(_ context.Context, _ repo.Name, query string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.covered[query], d.err
}

func (d *fakeDirectory) Keep(_ context.Context, _ repo.Name, query string, found Found) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.kept = append(d.kept, query)
	d.covered[query] = true

	if query == "" {
		d.known.Stale, d.known.Total = false, found.Total
	}

	for _, p := range found.People {
		d.known.Assignable = append(d.known.Assignable, Assignable{Login: p.Login, Name: p.Name, Listed: true})
	}

	return d.err
}

func (d *fakeDirectory) MarkFailed(_ context.Context, _ repo.Name, query string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.failed = append(d.failed, query)

	return d.err
}

func (d *fakeDirectory) Assigned(_ context.Context, _ repo.Name, logins []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.assigned = append(d.assigned, logins...)

	return d.err
}

// reads is a Read that counts its calls by query and answers found, or err.
type reads struct {
	mu    sync.Mutex
	asked []string
	found Found
	err   error
}

func (r *reads) read(_ context.Context, query string) (Found, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.asked = append(r.asked, query)

	return r.found, r.err
}

func TestCandidatesAskNothing(t *testing.T) {
	t.Parallel()

	dir := newDirectory(Known{Stale: true, Assignable: []Assignable{{Login: octoA, Listed: true}}})
	f := NewFinder(dir, nil)

	got, err := f.Candidates(t.Context(), testItem())
	require.NoError(t, err)
	assert.Equal(t, []string{"octo-me", "octo-author", "octo-commenter", octoA}, candidateLogins(got.People))
	assert.True(t, got.Stale)
	assert.Empty(t, dir.kept)
}

func TestLearnReadsOnlyWhenDue(t *testing.T) {
	t.Parallel()

	dir := newDirectory(Known{Stale: true})
	r := &reads{found: Found{People: []Person{{Login: octoA, Name: "Octo A"}}, Total: 1}}
	f := NewFinder(dir, nil)

	got, err := f.Learn(t.Context(), testItem(), r.read)
	require.NoError(t, err)
	assert.Equal(t, []string{""}, r.asked, "the whole list")
	assert.Equal(t, []string{""}, dir.kept)
	assert.False(t, got.Stale)
	assert.Equal(t, 1, got.Total)
	assert.Contains(t, candidateLogins(got.People), octoA)

	_, err = f.Learn(t.Context(), testItem(), r.read)
	require.NoError(t, err)
	assert.Len(t, r.asked, 1, "not due again")
}

func TestLearnRecordsAFailedRead(t *testing.T) {
	t.Parallel()

	dir := newDirectory(Known{Stale: true})
	f := NewFinder(dir, nil)

	got, err := f.Learn(t.Context(), testItem(), (&reads{err: uierr.New("GitHub is down.", errRead)}).read)
	require.ErrorIs(t, err, errRead)
	assert.Equal(t, "Couldn't learn who can be assigned in acme-org/example-repo: GitHub is down.", uierr.Message(err))
	assert.Equal(t, []string{""}, dir.failed)
	assert.NotNil(t, got.People)
}

func TestLearnReadsARepositoryOnceAtATime(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		dir := newDirectory(Known{Stale: true})
		release := make(chan struct{})
		r := &reads{}
		gated := func(ctx context.Context, query string) (Found, error) {
			<-release

			return r.read(ctx, query)
		}
		f := NewFinder(dir, nil)

		var wg sync.WaitGroup

		for range 3 {
			wg.Go(func() {
				_, err := f.Learn(t.Context(), testItem(), gated)
				assert.NoError(t, err)
			})
		}

		synctest.Wait()
		close(release)
		wg.Wait()

		assert.Len(t, r.asked, 1, "the others joined the read under way")
	})
}

func TestSearchAsksGitHubOnlyWhenNothingAnswersIt(t *testing.T) {
	t.Parallel()

	dir := newDirectory(Known{Assignable: []Assignable{{Login: "octo-known", Listed: true}}})
	dir.covered["oc"] = true
	r := &reads{found: Found{People: []Person{{Login: "octo-new"}, {Login: "octo-known"}}, Total: 2}}
	f := NewFinder(dir, nil)

	got, err := f.Search(t.Context(), testItem(), " oc ", r.read)
	require.NoError(t, err)
	assert.Empty(t, r.asked, "covered")
	assert.Equal(t, []string{"octo-me", "octo-author", "octo-commenter", "octo-known"}, candidateLogins(got.People),
		"those who match, from the directory")

	got, err = f.Search(t.Context(), testItem(), "octo-", r.read)
	require.NoError(t, err)
	assert.Equal(t, []string{"octo-"}, r.asked)
	assert.Equal(t, []string{"octo-"}, dir.kept)
	assert.Equal(t, map[string]Source{
		"octo-me": SourceYou, "octo-author": SourceAuthor, "octo-commenter": SourceParticipant,
		"octo-new": SourceFound, "octo-known": SourceFound,
	}, sourcesOf(got.People), "whom the search found")

	got, err = f.Search(t.Context(), testItem(), "", r.read)
	require.NoError(t, err)
	assert.Len(t, r.asked, 1, "no words, no search")
	assert.Len(t, got.People, 5)
}

func TestAFailedSearchIsRecorded(t *testing.T) {
	t.Parallel()

	dir := newDirectory(Known{})
	f := NewFinder(dir, nil)

	_, err := f.Search(t.Context(), testItem(), "octo", (&reads{err: errRead}).read)
	require.ErrorIs(t, err, errRead)
	assert.Equal(t, `Couldn't search GitHub for "octo" in acme-org/example-repo: Fake: the read failed.`, uierr.Message(err))
	assert.Equal(t, []string{"octo"}, dir.failed)
}

func TestDirectoryFailuresReachTheCaller(t *testing.T) {
	t.Parallel()

	dir := newDirectory(Known{Stale: true})
	dir.err = errDir
	f := NewFinder(dir, nil)
	r := &reads{}

	got, err := f.Candidates(t.Context(), testItem())
	require.ErrorIs(t, err, errDir)
	assert.NotNil(t, got.People)

	_, err = f.Learn(t.Context(), testItem(), r.read)
	require.ErrorIs(t, err, errDir)

	_, err = f.Search(t.Context(), testItem(), "octo", r.read)
	require.ErrorIs(t, err, errDir)
	assert.Empty(t, r.asked)

	// A failure to keep what was assigned is only logged.
	f.Seen(t.Context(), exampleRepo, []string{octoA})
	assert.Equal(t, []string{octoA}, dir.assigned)
}

func TestSeenKeepsWhoIsAssigned(t *testing.T) {
	t.Parallel()

	dir := newDirectory(Known{})
	f := NewFinder(dir, nil)

	f.Seen(t.Context(), exampleRepo, nil)
	assert.Empty(t, dir.assigned, "no one, nothing kept")

	f.Seen(t.Context(), exampleRepo, []string{octoA, "octo-b"})
	assert.Equal(t, []string{octoA, "octo-b"}, dir.assigned)
}

func TestANilFinderOffersNoOne(t *testing.T) {
	t.Parallel()

	var f *Finder

	r := &reads{}

	for _, call := range []func() (Candidates, error){
		func() (Candidates, error) { return f.Candidates(t.Context(), testItem()) },
		func() (Candidates, error) { return f.Learn(t.Context(), testItem(), r.read) },
		func() (Candidates, error) { return f.Search(t.Context(), testItem(), "octo", r.read) },
	} {
		got, err := call()
		require.NoError(t, err)
		assert.Equal(t, Candidates{People: []Candidate{}}, got)
	}

	f.Seen(t.Context(), exampleRepo, []string{octoA})
	assert.Empty(t, r.asked)
}
