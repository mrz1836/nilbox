package inbox

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// Sizes of the benchmark inbox: about what a busy account lists, a third of it pull
// requests from outside contributors whose files are checked.
const (
	benchOutside  = 40
	benchAuthored = 30
	benchAssigned = 30
)

// benchInbox builds the fake GitHub a benchmark refreshes from: every item with its
// details, and each outside pull request with changed files, a few of them sensitive.
func benchInbox() *fakeClient {
	client := &fakeClient{
		limits: restLimits(), avatar: fakeAvatar(),
		details: map[string]github.ItemDetails{}, files: map[string]*github.ChangedFiles{},
	}

	var review, authored, assigned []github.Item

	for n := 1; n <= benchOutside; n++ {
		it := pr(n, "CONTRIBUTOR", true)
		it.AuthorLogin = "octo-outsider"
		review = append(review, it)

		head := fmt.Sprintf("%040d", n)
		client.details[it.NodeID] = benchDetails(head)
		client.files[it.Ref().String()] = benchFiles(head)
	}

	for n := benchOutside + 1; n <= benchOutside+benchAuthored; n++ {
		it := pr(n, "OWNER", false)
		authored = append(authored, it)
		client.details[it.NodeID] = benchDetails(fmt.Sprintf("%040d", n))
	}

	for n := benchOutside + benchAuthored + 1; n <= benchOutside+benchAuthored+benchAssigned; n++ {
		it := issue("example-repo", n, "MEMBER")
		assigned = append(assigned, it)
		client.details[it.NodeID] = github.ItemDetails{Comments: 3, Labels: []github.Label{{Name: "bug", Color: "#d73a4a"}}}
	}

	client.inbox = fetched(review, authored, assigned)

	return client
}

// benchDetails are a pull request's details at head.
func benchDetails(head string) github.ItemDetails {
	return github.ItemDetails{
		HeadOID: head, Comments: 4, Additions: 120, Deletions: 30, ChangedFiles: 12, Commits: 3,
		Mergeable: "MERGEABLE", Labels: []github.Label{{Name: "enhancement", Color: "#a2eeef"}},
		Checks:  github.Checks{State: "SUCCESS", Passed: 8, Skipped: 1},
		Reviews: github.Reviews{Decision: "REVIEW_REQUIRED", Reviewers: 2, Reviewed: 1, Approvals: 1},
	}
}

// benchFiles are a pull request's changed files at head: twelve, two of them sensitive.
func benchFiles(head string) *github.ChangedFiles {
	files := make([]github.ChangedFile, 0, 12)
	files = append(files,
		github.ChangedFile{Path: ".github/workflows/ci.yml", Status: "modified"},
		github.ChangedFile{Path: "CLAUDE.md", Status: "modified"},
	)

	for i := range 10 {
		files = append(files, github.ChangedFile{Path: fmt.Sprintf("internal/pkg%d/code.go", i), Status: "modified"})
	}

	return &github.ChangedFiles{Head: head, Total: len(files), Files: files, Complete: true}
}

// benchService returns a Service over a fresh database that has refreshed benchInbox
// until every outside pull request's files are checked: a refresh checks at most
// maxFileChecks of them.
func benchService(b *testing.B) *Service {
	b.Helper()

	svc := NewService(Config{
		Pool: testutil.DB(b), Tokens: &fakeTokens{}, Client: benchInbox(),
		Clock: testutil.NewClock(time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)),
	})

	var snap Snapshot

	for range (benchOutside + maxFileChecks - 1) / maxFileChecks {
		var err error

		snap, err = svc.Refresh(b.Context())
		require.NoError(b, err)
	}

	require.Len(b, snap.ReviewRequested.Items, benchOutside)

	for _, it := range snap.ReviewRequested.Items {
		require.NotNil(b, it.Files)
		require.Equal(b, sensitive.StatusFlagged, it.Files.Status, "#%d", it.Number)
	}

	return svc
}

// BenchmarkSnapshot reads the stored inbox, as the menu bar and the window do each time
// it changes.
func BenchmarkSnapshot(b *testing.B) {
	svc := benchService(b)

	b.ReportAllocs()

	for b.Loop() {
		if _, err := svc.Snapshot(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRefresh runs one refresh against a fake GitHub that answers at once, so it
// times what the app itself does: reading what the last refresh listed, storing the
// answer, and building the new snapshot.
func BenchmarkRefresh(b *testing.B) {
	svc := benchService(b)

	b.ReportAllocs()

	for b.Loop() {
		if _, err := svc.Refresh(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRefreshCycle is what one scheduled refresh costs the app in all: the refresh,
// then the menu bar's and the window's reads of the inbox it stored.
func BenchmarkRefreshCycle(b *testing.B) {
	svc := benchService(b)

	b.ReportAllocs()

	for b.Loop() {
		if _, err := svc.Refresh(b.Context()); err != nil {
			b.Fatal(err)
		}

		for range 2 {
			if _, err := svc.Snapshot(b.Context()); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkStoredFiles reads one pull request's stored file check with the user's
// categories applied, as an inbox card's file list does.
func BenchmarkStoredFiles(b *testing.B) {
	svc := benchService(b)
	snap, err := svc.Snapshot(b.Context())
	require.NoError(b, err)

	id := snap.ReviewRequested.Items[0].ID

	b.ReportAllocs()

	for b.Loop() {
		if _, err = svc.StoredFiles(b.Context(), id); err != nil {
			b.Fatal(err)
		}
	}
}

// benchWatched is how many items the benchmark's routine searches watch: three searches of
// a full page, as a busy org's Dependabot pull requests and issues hold.
const benchWatched = 300

// BenchmarkWatchPass is one steady watch pass, its searches answered at once and nothing
// changed since the last, so it reads no details: what the app itself adds after the
// inbox's store, comparing every watched item with what is stored and writing their rows.
func BenchmarkWatchPass(b *testing.B) {
	svc := benchService(b)
	svc.client.(*fakeClient).watches = map[string]github.WatchResult{}

	searches := make([]Watch, 0, 3)

	for s := range 3 {
		query := fmt.Sprintf("is:pr is:open archived:false org:acme-org label:\"bench-%d\" sort:updated-desc", s)
		searches = append(searches, Watch{Key: query, Query: query, Follow: true})

		items := make([]github.WatchItem, 0, benchWatched/3)
		for n := range benchWatched / 3 {
			number := 1000 + s*benchWatched + n
			head := fmt.Sprintf("%040d", number)
			items = append(items, watched(dependabot(number), head, "SUCCESS"))
			svc.client.(*fakeClient).details[nodeID(number)] = watchedPRDetails(head, "SUCCESS")
		}

		svc.client.(*fakeClient).watches[query] = github.WatchResult{IssueCount: len(items), Items: items}
	}

	pass := func() WatchPass {
		job := &watchJob{login: octoUser, searches: searches, stored: make(chan struct{})}
		job.release()

		report, err := svc.watchPass(b.Context(), job)
		require.NoError(b, err)

		return report
	}

	first := pass()
	require.Equal(b, benchWatched, first.Read, "the first pass reads every new item")

	b.ReportAllocs()

	for b.Loop() {
		if report := pass(); report.Read != 0 {
			b.Fatalf("a steady pass read %d items", report.Read)
		}
	}
}
