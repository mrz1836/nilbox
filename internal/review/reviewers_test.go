package review

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/uierr"
)

var errDirectory = errors.New("the directory is down")

// octoA is the person most reviewer edits ask.
const octoA = "octo-a"

// peopleReview is #7 as GitHub answers it with who took part: a member opened it, a member
// and a bot reviewed it, two people committed to it, it asks a person and a team, and GitHub
// suggests two people.
func peopleReview() *github.PullReview {
	r := pullReview(headA, github.MergeStateClean)
	r.Update.Item.AuthorType, r.Update.Item.AuthorAssociation = "User", "MEMBER"
	r.Reviews = []github.Review{
		{
			Author: "octo-reviewer", AuthorType: "User", AuthorName: "Octo Reviewer", Association: "MEMBER", State: "APPROVED",
			SubmittedAt: testNow,
		},
		{Author: "octo-drive-by", AuthorType: "User", Association: "NONE", State: "COMMENTED"},
		{Author: "helper", AuthorType: "Bot", Association: "NONE", State: "COMMENTED", SubmittedAt: testNow},
	}
	r.Commits = []github.PullCommit{
		{OID: headA, Author: octoOther, CommittedAt: testNow.Add(-2 * time.Hour)},
		{OID: headA, Author: "Octo-Other", CommittedAt: testNow.Add(-time.Hour)},
		{OID: headA, Author: "octo-pair", CommittedAt: testNow.Add(-time.Hour)},
		{OID: headA, AuthorName: "someone without an account"},
	}
	r.Requested = []github.Person{{Login: "octo-asked", Name: "Octo Asked"}}
	r.RequestedTeams = []github.Team{{Slug: "web", Name: "Web"}}
	r.Suggested = []github.SuggestedReviewer{
		{Login: "octo-suggested", Name: "Octo Suggested", ChangedFiles: true},
		{Login: "octo-commenter", Commented: true},
	}

	return r
}

func TestAReadLearnsWhoWorksOnTheRepository(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review = peopleReview()
	h.refreshed(t)

	assert.Equal(t, []string{"octo-other #7+"}, h.people.seenAs(RoleAuthor), "the author, a member")
	assert.Equal(t, []string{"octo-reviewer #7+", "octo-drive-by #7"}, h.people.seenAs(RoleReviewer),
		"people only, each with their access")
	assert.Equal(t, []string{"octo-other #7", "octo-pair #7"}, h.people.seenAs(RoleCommitter),
		"each committer once, and only those with an account")
	assert.Equal(t, []string{"octo-asked #7"}, h.people.seenAs(RoleRequested))
	assert.Equal(t, []string{"octo-suggested #7", "octo-commenter #7"}, h.people.seenAs(RoleSuggested))
	assert.Equal(t, [][]TeamCandidate{{{Slug: "web", Name: "Web"}}}, h.people.asked, "the teams asked")

	for _, s := range h.people.learned {
		if s.Role == RoleReviewer && s.Login == "octo-drive-by" {
			assert.Equal(t, updatedAt, s.At, "a review without a time is the pull request's")
		}
	}
}

func TestAReadThatCannotLearnStillReads(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review = peopleReview()
	h.people.err = errDirectory

	r := h.refreshed(t)
	assert.NotEmpty(t, r.FetchedAt, "the read stands")
	assert.Contains(t, h.logs.String(), "could not learn who works on the repository")
}

func TestASheetWithoutADirectoryLearnsNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.svc = NewService(Config{
		Pool: h.db, Inbox: h.inbox, GitHub: h.github, Settings: fakeSettings{ignored: h.ignored}, Replies: fakeReplies{},
		Clock: h.clock,
	})
	h.github.review = peopleReview()
	h.refreshed(t)

	got, err := h.svc.ReviewerCandidates(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, []string{"octo-suggested", "octo-commenter"}, candidateLogins(got), "GitHub's suggestions alone")
	assert.False(t, got.Stale, "nothing to read into")

	got, err = h.svc.LearnReviewers(t.Context(), pullID)
	require.NoError(t, err)
	assert.Len(t, got.People, 2)
	assert.Zero(t, h.github.called(callPeople))
}

// candidateLogins lists the people offered, in order.
func candidateLogins(r Reviewers) []string {
	out := make([]string, 0, len(r.People))
	for _, p := range r.People {
		out = append(out, p.Login)
	}

	return out
}

func TestReviewerCandidatesPutGitHubsSuggestionsFirst(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review = peopleReview()
	h.github.review.Suggested = append(h.github.review.Suggested,
		github.SuggestedReviewer{Login: octoOther}, github.SuggestedReviewer{Login: octoUser},
		github.SuggestedReviewer{Login: "helper[bot]"})
	h.refreshed(t)

	h.people.dir = Directory{
		People: []KnownPerson{
			{Login: "Octo-Suggested", Reviewed: 2, LastAt: testNow},
			{Login: "octo-member", Name: "Octo Member", Reviewed: 12, Authored: 3, Committed: 1, LastAt: testNow},
			{Login: octoUser, Reviewed: 40},
			{Login: octoOther, Authored: 9},
		},
		Teams:  []TeamCandidate{{Slug: "web", Name: "Web"}, {Slug: ""}},
		ReadAt: testNow, Pulls: 50,
	}

	before := len(h.github.calls.Since())

	got, err := h.svc.ReviewerCandidates(t.Context(), pullID)
	require.NoError(t, err)

	assert.Equal(t, Reviewers{
		People: []ReviewerCandidate{
			{
				Login: "octo-suggested", Name: "Octo Suggested", Suggestion: SuggestionChangedFiles, Reviewed: 2,
				LastAt: clock.Format(testNow),
			},
			{Login: "octo-commenter", Suggestion: SuggestionCommented},
			{Login: "octo-member", Name: "Octo Member", Reviewed: 12, Authored: 3, Committed: 1, LastAt: clock.Format(testNow)},
		},
		Teams:  []TeamCandidate{{Slug: "web", Name: "Web"}},
		Pulls:  50,
		ReadAt: clock.Format(testNow),
	}, got, "never the author, the user, or a bot, and each person once, with what the directory knows")
	assert.Len(t, h.github.calls.Since(), before, "no request")
}

func TestReviewerCandidatesBeforeTheSheetReadThePullRequest(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	got, err := h.svc.ReviewerCandidates(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, Reviewers{People: []ReviewerCandidate{}, Teams: []TeamCandidate{}, Stale: true}, got)
}

func TestReviewerCandidatesAreCapped(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for i := range maxCandidates + 20 {
		h.people.dir.People = append(h.people.dir.People, KnownPerson{Login: "octo-" + strconv.Itoa(i)})
	}

	got, err := h.svc.ReviewerCandidates(t.Context(), pullID)
	require.NoError(t, err)
	assert.Len(t, got.People, maxCandidates)
}

func TestReviewerCandidatesFailWithTheDirectory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.people.err = errDirectory

	_, err := h.svc.ReviewerCandidates(t.Context(), pullID)
	require.ErrorIs(t, err, errDirectory)

	h.inbox.pulled = errDirectory

	_, err = h.svc.ReviewerCandidates(t.Context(), pullID)
	require.ErrorIs(t, err, errDirectory)
}

// repoPeople is what the read of acme-org/example-repo's recent pull requests answers: an
// organization's, two pull requests.
func repoPeople() *github.RepoPeople {
	return &github.RepoPeople{Org: true, Pulls: []github.RepoPull{
		{
			Number: 12, UpdatedAt: updatedAt,
			Author: github.Participant{Login: "octo-member", Name: "Octo Member", Association: "MEMBER"},
			Reviews: []github.Participant{
				{Login: "octo-collaborator", Association: "COLLABORATOR", At: testNow},
				{Login: "octo-outsider", Association: "NONE"},
			},
			Requested: []github.Person{{Login: "octo-asked"}},
			Teams:     []github.Team{{Slug: "api", Name: "API"}},
		},
		{Number: 11, UpdatedAt: updatedAt, Author: github.Participant{Login: "octo-contributor", Association: "CONTRIBUTOR"}},
		{Number: 10, UpdatedAt: updatedAt},
	}}
}

func TestLearnReviewersReadsTheRepositoryOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.repoPeople = repoPeople()
	h.github.repoTeams = []github.Team{{Slug: "web", Name: "Web"}}

	got, err := h.svc.LearnReviewers(t.Context(), pullID)
	require.NoError(t, err)

	assert.Equal(t, []string{"octo-member #12+", "octo-contributor #11"}, h.people.seenAs(RoleAuthor))
	assert.Equal(t, []string{"octo-collaborator #12+", "octo-outsider #12"}, h.people.seenAs(RoleReviewer))
	assert.Equal(t, []string{"octo-asked #12"}, h.people.seenAs(RoleRequested))
	assert.Equal(t, [][]TeamCandidate{{{Slug: "web", Name: "Web"}}}, h.people.listed, "the repository's teams list")
	assert.Equal(t, [][]TeamCandidate{{{Slug: "api", Name: "API"}}}, h.people.asked, "and the teams its pull requests asked")
	assert.Equal(t, []string{"3 true"}, h.people.reads)
	assert.False(t, got.Stale)
	assert.Equal(t, 3, got.Pulls)
	assert.Equal(t, 1, h.github.called(callPeople))
	assert.Equal(t, 1, h.github.called(callTeams))
	assert.Equal(t, 1, h.inbox.peeks, "through the inbox's token")

	for _, s := range h.people.learned {
		if s.Login == "octo-outsider" {
			assert.Equal(t, updatedAt, s.At, "a review without a time is its pull request's")
		}
	}

	_, err = h.svc.LearnReviewers(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, 1, h.github.called(callPeople), "read again only once it is due")
}

func TestLearnReviewersOfAUsersRepositoryAsksForNoTeams(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.repoPeople = repoPeople()
	h.github.repoPeople.Org = false

	_, err := h.svc.LearnReviewers(t.Context(), pullID)
	require.NoError(t, err)
	assert.Zero(t, h.github.called(callTeams))
	assert.Empty(t, h.people.listed)
	assert.Equal(t, []string{"3 false"}, h.people.reads)
}

func TestLearnReviewersKeepsThePeopleWhenTheTeamsAreHidden(t *testing.T) {
	t.Parallel()

	for _, teamsErr := range []error{github.ErrTeamsHidden, errFakeRead} {
		t.Run(teamsErr.Error(), func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.github.repoPeople = repoPeople()
			h.github.teamsErr = teamsErr

			_, err := h.svc.LearnReviewers(t.Context(), pullID)
			require.NoError(t, err)
			assert.Empty(t, h.people.listed, "the teams list stays as it was")
			assert.Equal(t, [][]TeamCandidate{{{Slug: "api", Name: "API"}}}, h.people.asked)
			assert.Equal(t, []string{"3 true"}, h.people.reads)
		})
	}
}

func TestLearnReviewersRecordsAFailedRead(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.peopleErr = uierr.New("GitHub is down.", errFakeRead)

	_, err := h.svc.LearnReviewers(t.Context(), pullID)
	require.ErrorIs(t, err, errFakeRead)
	assert.Equal(t, "Couldn't learn who works on acme-org/example-repo: GitHub is down.", uierr.Message(err))
	assert.Equal(t, 1, h.people.failed)
	assert.Empty(t, h.people.reads)

	_, err = h.svc.LearnReviewers(t.Context(), pullID)
	require.NoError(t, err, "held off after a failure")
	assert.Equal(t, 1, h.github.called(callPeople))
}

func TestLearnReviewersFailsWithTheDirectory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.repoPeople = repoPeople()
	h.people.err = errDirectory

	_, err := h.svc.LearnReviewers(t.Context(), pullID)
	require.ErrorIs(t, err, errDirectory)

	h.inbox.pulled = errDirectory

	_, err = h.svc.LearnReviewers(t.Context(), pullID)
	require.ErrorIs(t, err, errDirectory)
}

func TestLearnReviewersReadsARepositoryOnceAtATime(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		h.github.repoPeople = repoPeople()
		h.github.peopleWait = make(chan struct{})

		var wg sync.WaitGroup

		errs := make([]error, 2)

		for i := range errs {
			wg.Go(func() {
				_, errs[i] = h.svc.LearnReviewers(t.Context(), pullID)
			})
		}

		synctest.Wait()
		close(h.github.peopleWait)
		wg.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		assert.Equal(t, 1, h.github.called(callPeople), "the second joined the first")
	})
}

// editable is a harness that has read #7, which asks octo-asked and the team ops to review.
func editable(t *testing.T) *harness {
	t.Helper()

	h := newHarness(t)
	h.refreshed(t)
	h.github.reviewers = github.ReviewRequests{Users: []string{"octo-asked"}, Teams: []string{"ops"}}

	return h
}

func TestEditReviewersSendsAtMostTwoRequests(t *testing.T) {
	t.Parallel()

	h := editable(t)

	res, err := h.svc.EditReviewers(t.Context(), ReviewersRequest{
		ItemID: pullID, Add: []string{octoA, " octo-b ", "OCTO-A", ""}, AddTeams: []string{"web"},
		Remove: []string{"octo-asked"}, RemoveTeams: []string{"ops"},
	})
	require.NoError(t, err)

	assert.Equal(t, ReviewersResult{Sent: 2}, res)
	assert.Equal(t, []string{callAsk, callUnask}, h.github.requests(), "one request to ask, one to take off")
	assert.Equal(t, github.ReviewRequests{Users: []string{octoA, "octo-b"}, Teams: []string{"web"}}, h.github.reviewers)

	require.Len(t, h.inbox.acted, 1)
	assert.True(t, h.inbox.acted[0].RequestsEdited)
	assert.Equal(t, github.ReviewRequests{Users: []string{octoA, "octo-b"}, Teams: []string{"web"}},
		h.inbox.acted[0].ReviewRequests, "the inbox records who GitHub says it asks after the last step")
	assert.Equal(t, []string{"octo-a #7", "octo-b #7"}, h.people.seenAs(RolePicked), "the people the user asked")
	assert.Equal(t, 1, h.github.called(callReview), "no read after the edit")
}

func TestEditReviewersSendsOnlyWhatChanges(t *testing.T) {
	t.Parallel()

	h := editable(t)

	res, err := h.svc.EditReviewers(t.Context(), ReviewersRequest{ItemID: pullID, RemoveTeams: []string{"ops"}})
	require.NoError(t, err)
	assert.Equal(t, ReviewersResult{Sent: 1}, res)
	assert.Equal(t, []string{callUnask}, h.github.requests())
	assert.Empty(t, h.people.seenAs(RolePicked))

	res, err = h.svc.EditReviewers(t.Context(), ReviewersRequest{
		ItemID: pullID, Add: []string{octoA}, Remove: []string{"Octo-A"},
	})
	require.NoError(t, err)
	assert.Equal(t, ReviewersResult{}, res, "one both added and taken off is sent not at all")
	assert.Equal(t, []string{callUnask}, h.github.requests())
}

func TestEditReviewersNeverSendsAStepTwice(t *testing.T) {
	t.Parallel()

	h := editable(t)
	h.github.unaskBadToken = true

	res, err := h.svc.EditReviewers(t.Context(), ReviewersRequest{
		ItemID: pullID, Add: []string{octoA}, Remove: []string{"octo-asked"},
	})
	require.NoError(t, err)

	assert.Equal(t, ReviewersResult{Sent: 2}, res)
	assert.Equal(t, 1, h.github.called(callAsk), "the request GitHub made isn't sent again with the new token")
	assert.Equal(t, 2, h.github.called(callUnask))
	assert.Equal(t, 2, h.inbox.tries)
	assert.Equal(t, github.ReviewRequests{Users: []string{octoA}, Teams: []string{"ops"}}, h.inbox.acted[0].ReviewRequests)
}

func TestEditReviewersReportsAFailureAfterAStep(t *testing.T) {
	t.Parallel()

	h := editable(t)
	h.github.unaskErr = uierr.New("GitHub refused the change.", errFakeRead)

	res, err := h.svc.EditReviewers(t.Context(), ReviewersRequest{
		ItemID: pullID, Add: []string{octoA}, Remove: []string{"octo-asked"},
	})
	require.NoError(t, err)
	assert.Equal(t, ReviewersResult{Sent: 1, Failed: "GitHub refused the change."}, res)

	require.Len(t, h.inbox.acted, 1)
	assert.True(t, h.inbox.acted[0].RequestsEdited, "what GitHub made is recorded")
	assert.Equal(t, []string{"octo-a #7"}, h.people.seenAs(RolePicked))
}

func TestEditReviewersFailsBeforeAnyStep(t *testing.T) {
	t.Parallel()

	h := editable(t)
	h.github.askErr = uierr.New("GitHub didn't ask octo-a.", errFakeRead)

	res, err := h.svc.EditReviewers(t.Context(), ReviewersRequest{
		ItemID: pullID, Add: []string{octoA}, Remove: []string{"octo-asked"},
	})
	require.ErrorIs(t, err, errFakeRead)
	assert.Equal(t, ReviewersResult{}, res)
	assert.Equal(t, []string{callAsk}, h.github.requests(), "it stops at the first step that fails")
	assert.Empty(t, h.people.seenAs(RolePicked))
}

func TestEditReviewersRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(h *harness)
		req   ReviewersRequest
		want  error
		words string
	}{
		{
			name: "not read", setup: func(*harness) {}, req: ReviewersRequest{Add: []string{octoA}},
			want: ErrCannotEditReviewers, words: "Wait for the pull request to load from GitHub, then try again.",
		},
		{
			name: "without triage",
			setup: func(h *harness) {
				h.refreshed(t)
				h.inbox.pull.ViewerPermission = "READ"
			},
			req:  ReviewersRequest{Add: []string{octoA}},
			want: ErrCannotEditReviewers, words: "Your account can't change this pull request's reviewers: that takes triage access.",
		},
		{
			name: "the user asked", setup: func(h *harness) { h.refreshed(t) }, req: ReviewersRequest{Add: []string{"OCTO-USER"}},
			want: ErrOwnReviewRequest, words: "Ask for your own review with Re-request my review.",
		},
		{
			name: "the user taken off", setup: func(h *harness) { h.refreshed(t) },
			req:  ReviewersRequest{Remove: []string{octoUser}},
			want: ErrOwnReviewRequest, words: "Take yourself off with Remove me as reviewer.",
		},
		{
			name: "the author asked", setup: func(h *harness) { h.refreshed(t) }, req: ReviewersRequest{Add: []string{octoOther}},
			want: ErrAuthorReview, words: "GitHub doesn't ask octo-other to review their own pull request.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			tt.setup(h)

			tt.req.ItemID = pullID
			_, err := h.svc.EditReviewers(t.Context(), tt.req)
			require.ErrorIs(t, err, tt.want)
			assert.Equal(t, tt.words, uierr.Message(err))
			assert.Zero(t, h.github.called(callAsk)+h.github.called(callUnask), "nothing is sent")
		})
	}

	h := newHarness(t)
	h.inbox.pulled = errDirectory

	_, err := h.svc.EditReviewers(t.Context(), ReviewersRequest{ItemID: pullID, Add: []string{octoA}})
	require.ErrorIs(t, err, errDirectory)
}

func TestTheSheetSaysWhetherTheUserMayEditItsReviewers(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	r, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)
	assert.False(t, r.CanEditReviewers, "not before the sheet read it")

	assert.True(t, h.refreshed(t).CanEditReviewers, "open, read, and with write access")

	h.inbox.pull.ViewerPermission = "TRIAGE"

	r, err = h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)
	assert.True(t, r.CanEditReviewers, "triage requests reviews")

	h.inbox.pull.ViewerPermission = "READ"

	r, err = h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)
	assert.False(t, r.CanEditReviewers)

	assert.False(t, canEditReviewers(storedPull(), &cached{context: pullContext{State: github.StateMerged}}))
}

func TestTheReviewersSayWhichIsABot(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review = peopleReview()

	r := h.refreshed(t)

	bots := map[string]bool{}
	for _, e := range r.Reviews {
		bots[e.Author] = e.Bot
	}

	assert.Equal(t, map[string]bool{"octo-reviewer": false, "octo-drive-by": false, "helper": true}, bots)
}

func TestTheCacheKeepsGitHubsSuggestions(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review = peopleReview()
	h.refreshed(t)

	c, err := h.svc.cache.load(t.Context(), pullID, 0)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, []contextSuggestion{
		{Login: "octo-suggested", Name: "Octo Suggested", ChangedFiles: true},
		{Login: "octo-commenter", Commented: true},
	}, c.context.Suggested)
	assert.Equal(t, SuggestionNone, contextSuggestion{Login: octoA}.suggestion())
}
