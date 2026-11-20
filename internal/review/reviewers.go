package review

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The sheet manages a pull request's reviewers: it suggests who to ask from what the app
// learned of who works on the repository (the People directory), with no request, and asks
// or takes requests off with at most two REST requests. It learns for free from every read
// of a pull request's sheet (learn), and, the first time the picker opens in a repository
// and at most weekly after, from one read of the repository's recent pull requests
// (LearnReviewers).

// maxCandidates is the most people ReviewerCandidates lists: as many as one call for their
// pictures looks at (avatars.MaxLogins).
const maxCandidates = 100

// ErrCannotEditReviewers is returned for a reviewer edit of a pull request the user can't
// make: it isn't open, the sheet hasn't read it, or their account has less than triage
// access.
var ErrCannotEditReviewers = errors.New("the pull request's reviewers can't be changed by this account")

// ErrOwnReviewRequest is returned for a reviewer edit that names the user, who asks for
// their own review again, or takes themselves off, with the sheet's own actions.
var ErrOwnReviewRequest = errors.New("a reviewer edit names the signed-in user")

// ErrAuthorReview is returned for a reviewer edit that asks the pull request's author,
// whom GitHub never asks to review their own pull request.
var ErrAuthorReview = errors.New("a reviewer edit asks the pull request's author")

// The roles the sheet hands the People directory, as it names them.
const (
	RoleAuthor    = "author"
	RoleReviewer  = "reviewer"
	RoleCommitter = "committer"
	RoleRequested = "requested"
	RoleSuggested = "suggested"
	RolePicked    = "picked"
)

// PersonSeen is one thing a read saw a person do on one pull request of a repository, for
// the People directory to keep: their login and name ("" when the read gave none), the
// pull request's number, their Role (one of the Role constants), whether GitHub gave them
// access there (an association of OWNER, MEMBER, or COLLABORATOR), and GitHub's time of it,
// zero for now.
type PersonSeen struct {
	Login  string
	Name   string
	Number int
	Role   string
	Access bool
	At     time.Time
}

// KnownPerson is someone the People directory knows works on a repository: how many of its
// pull requests they reviewed, opened, and committed to, and when they were last seen
// there.
type KnownPerson struct {
	Login     string
	Name      string
	Reviewed  int
	Authored  int
	Committed int
	LastAt    time.Time
}

// Directory is who works on a repository, as the People directory knows it: the people,
// the highest ranked first, its teams, when its recent pull requests were last read (zero
// before they were) and how many that read read, whether an organization owns it, and
// whether a read is due (Stale).
type Directory struct {
	People []KnownPerson
	Teams  []TeamCandidate
	ReadAt time.Time
	Pulls  int
	Org    bool
	Stale  bool
}

// People is the local directory of who works on each repository, which the sheet learns
// into and suggests reviewers from, and whose size Settings shows beside Forget, which
// empties it. core adapts *people.Service to it.
type People interface {
	Learn(ctx context.Context, name repo.Name, seen []PersonSeen) error
	SetTeams(ctx context.Context, name repo.Name, teams []TeamCandidate, listed bool) error
	MarkRead(ctx context.Context, name repo.Name, pulls int, org bool) error
	MarkFailed(ctx context.Context, name repo.Name) error
	Ranked(ctx context.Context, name repo.Name) (Directory, error)
	Summary(ctx context.Context) (PeopleSummary, error)
	Forget(ctx context.Context) error
}

// Suggestion is why GitHub suggests someone to review a pull request.
type Suggestion string

// The suggestions.
const (
	// SuggestionNone is someone GitHub doesn't suggest.
	SuggestionNone Suggestion = ""

	// SuggestionChangedFiles is someone whose commits changed the files the pull request
	// changes.
	SuggestionChangedFiles Suggestion = "changed_files"

	// SuggestionCommented is someone who commented in reviews of those files.
	SuggestionCommented Suggestion = "commented"
)

// ReviewerCandidate is someone the picker offers to ask for a review.
type ReviewerCandidate struct {
	Login string `json:"login"`
	Name  string `json:"name"`

	// Suggestion is why GitHub suggests them for this pull request, or SuggestionNone.
	Suggestion Suggestion `json:"suggestion"`

	// Reviewed, Authored, and Committed count the repository's pull requests they reviewed,
	// opened, and committed to, of those the app knows.
	Reviewed  int `json:"reviewed"`
	Authored  int `json:"authored"`
	Committed int `json:"committed"`

	// LastAt is when they were last seen working on the repository, as RFC 3339, or "".
	LastAt string `json:"lastAt"`
}

// TeamCandidate is a team the picker offers to ask for a review: its slug, which a request
// names it by, and its display name, or "".
type TeamCandidate struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// Reviewers is who the picker offers to ask for a review of a pull request.
type Reviewers struct {
	// People are the people, GitHub's suggestions first in its order, then those who work
	// on the repository, the highest ranked first: never the pull request's author, the
	// signed-in user, or a bot, at most 100, never nil.
	People []ReviewerCandidate `json:"people"`

	// Teams are the repository's teams the app knows, never nil.
	Teams []TeamCandidate `json:"teams"`

	// Pulls is how many pull requests the last read of the repository read, ReadAt when it
	// was, as RFC 3339, or "" before one was, and Stale whether one is due (LearnReviewers).
	Pulls  int    `json:"pulls"`
	ReadAt string `json:"readAt"`
	Stale  bool   `json:"stale"`
}

// ReviewersRequest asks to change who the pull request with ItemID asks to review: the
// people to ask (Add) and to take off (Remove), by login, and the teams, by slug.
type ReviewersRequest struct {
	ItemID      int64    `json:"itemId"`
	Add         []string `json:"add"`
	Remove      []string `json:"remove"`
	AddTeams    []string `json:"addTeams"`
	RemoveTeams []string `json:"removeTeams"`
}

// ReviewersResult is what a reviewer edit made of a pull request on GitHub: Sent counts the
// requests GitHub made, and Failed says why the one after them failed, or "" when every one
// went.
type ReviewersResult struct {
	Sent   int    `json:"sent"`
	Failed string `json:"failed"`
}

// ReviewerCandidates returns who the picker offers to ask for a review of the pull request
// with id, from the sheet's cache and the People directory alone, with no request: GitHub's
// suggestions for it first, as the sheet last read them, then the people who work on the
// repository.
func (s *Service) ReviewerCandidates(ctx context.Context, id int64) (Reviewers, error) {
	pull, err := s.pull(ctx, id)
	if err != nil {
		return Reviewers{}, err
	}

	c, err := s.cache.load(ctx, id, 0)
	if err != nil {
		return Reviewers{}, err
	}

	dir, err := s.directory(ctx, pull.ref().Repo)
	if err != nil {
		return Reviewers{}, err
	}

	var suggested []contextSuggestion
	if c != nil {
		suggested = c.context.Suggested
	}

	return candidates(pull, suggested, dir), nil
}

// directory reads who works on the repository name, or an empty directory, never due, when
// the sheet has no People directory.
func (s *Service) directory(ctx context.Context, name repo.Name) (Directory, error) {
	if s.people == nil {
		return Directory{People: []KnownPerson{}, Teams: []TeamCandidate{}}, nil
	}

	return s.people.Ranked(ctx, name)
}

// candidates merges GitHub's suggestions for pull with who the directory dir says works on
// its repository, leaving out its author, the signed-in user, and bots.
func candidates(pull StoredPull, suggested []contextSuggestion, dir Directory) Reviewers {
	out := Reviewers{People: []ReviewerCandidate{}, Teams: []TeamCandidate{}, Pulls: dir.Pulls, Stale: dir.Stale}
	if !dir.ReadAt.IsZero() {
		out.ReadAt = clock.Format(dir.ReadAt)
	}

	known := make(map[string]KnownPerson, len(dir.People))
	for _, p := range dir.People {
		known[strings.ToLower(p.Login)] = p
	}

	list := offered{pull: pull, listed: map[string]bool{}, people: []ReviewerCandidate{}}

	for _, sug := range suggested {
		c := candidateOf(known[strings.ToLower(sug.Login)])
		c.Login, c.Suggestion, c.Name = sug.Login, sug.suggestion(), cmp.Or(c.Name, sug.Name)
		list.offer(c)
	}

	for _, p := range dir.People {
		list.offer(candidateOf(p))
	}

	out.People = list.people

	for _, t := range dir.Teams {
		if t.Slug != "" {
			out.Teams = append(out.Teams, t)
		}
	}

	return out
}

// offered is the people the picker offers for pull, as candidates adds them: each once, at
// most maxCandidates, never its author, the signed-in user, or a bot.
type offered struct {
	pull   StoredPull
	listed map[string]bool
	people []ReviewerCandidate
}

// offer adds c, unless it is left out.
func (o *offered) offer(c ReviewerCandidate) {
	key := strings.ToLower(c.Login)
	if c.Login == "" || o.listed[key] || len(o.people) == maxCandidates || strings.EqualFold(c.Login, o.pull.AuthorLogin) ||
		strings.EqualFold(c.Login, o.pull.Viewer) || isBotLogin(c.Login) {
		return
	}

	o.listed[key] = true
	o.people = append(o.people, c)
}

// candidateOf is someone the directory knows, as the picker offers them.
func candidateOf(p KnownPerson) ReviewerCandidate {
	return ReviewerCandidate{
		Login: p.Login, Name: p.Name, Reviewed: p.Reviewed, Authored: p.Authored, Committed: p.Committed,
		LastAt: stamp(p.LastAt),
	}
}

// isBotLogin reports a login only a bot has: one GitHub's REST API spells with "[bot]".
func isBotLogin(login string) bool {
	return strings.HasSuffix(strings.ToLower(login), "[bot]")
}

// LearnReviewers reads who works on the repository of the pull request with id, when a read
// is due (Reviewers.Stale), and returns ReviewerCandidates with what it learned: one GraphQL
// request of one point for the repository's most recently updated pull requests, and, for
// an organization's repository, one REST request for its teams, whose refusal leaves the
// teams the app saw asked. One read runs at a time per repository, so the picker opened on
// two of its pull requests at once reads it once. A read that fails is recorded, so none is
// due for 30 minutes, and is the error; one not due sends nothing.
func (s *Service) LearnReviewers(ctx context.Context, id int64) (Reviewers, error) {
	pull, err := s.pull(ctx, id)
	if err != nil {
		return Reviewers{}, err
	}

	if s.people != nil {
		name := pull.ref().Repo

		_, err = s.learns.Do(ctx, name.Key(), func(ctx context.Context) (struct{}, error) {
			return struct{}{}, s.seed(ctx, id, name)
		})
		if err != nil {
			return Reviewers{}, err
		}
	}

	return s.ReviewerCandidates(ctx, id)
}

// seed reads who works on the repository name through the pull request with id, when a
// read is due, and keeps it in the directory.
func (s *Service) seed(ctx context.Context, id int64, name repo.Name) error {
	dir, err := s.people.Ranked(ctx, name)
	if err != nil {
		return err
	}

	if !dir.Stale {
		return nil
	}

	var (
		got    *github.RepoPeople
		teams  []github.Team
		listed bool
	)

	err = s.inbox.Peek(ctx, id, func(ctx context.Context, tok github.Token, t Target) error {
		read, readErr := s.github.FetchRepoPeople(ctx, tok, t.NodeID)
		if readErr != nil {
			return readErr
		}

		got, teams, listed = read, nil, false

		if read.Org {
			teams, listed = s.repoTeams(ctx, tok, name)
		}

		return nil
	})
	if err != nil {
		if markErr := s.people.MarkFailed(ctx, name); markErr != nil {
			s.logger.DebugContext(ctx, "could not record the failed read of who works on the repository", "error", markErr)
		}

		return uierr.New(fmt.Sprintf("Couldn't learn who works on %s: %s", name, uierr.Message(err)), err)
	}

	return s.keepSeed(ctx, name, got, teams, listed)
}

// repoTeams reads the teams of the repository name; listed is false when GitHub wouldn't
// list them or the read failed, which is logged: the teams the app saw asked stand.
func (s *Service) repoTeams(ctx context.Context, tok github.Token, name repo.Name) ([]github.Team, bool) {
	teams, err := s.github.FetchRepoTeams(ctx, tok, name)
	if err != nil {
		if !errors.Is(err, github.ErrTeamsHidden) {
			s.logger.DebugContext(ctx, "could not read the repository's teams", "error", err)
		}

		return nil, false
	}

	return teams, true
}

// keepSeed keeps what a read of the repository name's recent pull requests found: who did
// what on each, its teams when GitHub listed them (listed), the teams each asked, and the
// read itself.
func (s *Service) keepSeed(ctx context.Context, name repo.Name, got *github.RepoPeople, teams []github.Team, listed bool) error {
	seen := make([]PersonSeen, 0, 2*len(got.Pulls))

	var asked []TeamCandidate

	for _, p := range got.Pulls {
		seen = append(seen, seenOnPull(p)...)

		for _, t := range p.Teams {
			asked = append(asked, TeamCandidate(t))
		}
	}

	if err := s.people.Learn(ctx, name, seen); err != nil {
		return err
	}

	if listed {
		if err := s.people.SetTeams(ctx, name, teamCandidates(teams), true); err != nil {
			return err
		}
	}

	if len(asked) > 0 {
		if err := s.people.SetTeams(ctx, name, asked, false); err != nil {
			return err
		}
	}

	return s.people.MarkRead(ctx, name, len(got.Pulls), got.Org)
}

// seenOnPull is what a read of a repository's recent pull requests saw on p: who opened it,
// who reviewed it, and who it asks to review.
func seenOnPull(p github.RepoPull) []PersonSeen {
	var out []PersonSeen

	if p.Author.Login != "" {
		out = append(out, PersonSeen{
			Login: p.Author.Login, Name: p.Author.Name, Number: p.Number, Role: RoleAuthor,
			Access: hasAccess(p.Author.Association), At: p.UpdatedAt,
		})
	}

	for _, r := range p.Reviews {
		at := r.At
		if at.IsZero() {
			at = p.UpdatedAt
		}

		out = append(out, PersonSeen{
			Login: r.Login, Name: r.Name, Number: p.Number, Role: RoleReviewer, Access: hasAccess(r.Association), At: at,
		})
	}

	for _, r := range p.Requested {
		out = append(out, PersonSeen{Login: r.Login, Name: r.Name, Number: p.Number, Role: RoleRequested, At: p.UpdatedAt})
	}

	return out
}

// teamCandidates converts the teams GitHub listed.
func teamCandidates(teams []github.Team) []TeamCandidate {
	out := make([]TeamCandidate, 0, len(teams))
	for _, t := range teams {
		out = append(out, TeamCandidate(t))
	}

	return out
}

// hasAccess reports an association with a repository that gives access to it: its owner,
// a member of the organization that owns it, or a collaborator.
func hasAccess(association string) bool {
	switch association {
	case "OWNER", "MEMBER", "COLLABORATOR":
		return true
	default:
		return false
	}
}

// learn keeps what review, a read of pull, saw of who works on its repository: who opened
// it, who reviewed it, who committed to it, who and which teams it asks to review, and whom
// GitHub suggests; and who it is assigned to, as assigned there. People only. A failure is
// logged, and never fails the read.
func (s *Service) learn(ctx context.Context, pull StoredPull, review *github.PullReview) {
	if review == nil || !review.Update.Found {
		return
	}

	s.finder.Seen(ctx, pull.ref().Repo, review.Assignees.Logins)

	if s.people == nil {
		return
	}

	name, it := pull.ref().Repo, review.Update.Item
	seen := seenOnRead(it, review)

	if err := s.people.Learn(ctx, name, seen); err != nil {
		s.logger.DebugContext(ctx, "could not learn who works on the repository", "error", err)

		return
	}

	if len(review.RequestedTeams) == 0 {
		return
	}

	if err := s.people.SetTeams(ctx, name, teamCandidates(review.RequestedTeams), false); err != nil {
		s.logger.DebugContext(ctx, "could not learn the repository's teams", "error", err)
	}
}

// typeUser is the GraphQL type of an author that is a person.
const typeUser = "User"

// seenOnRead is what a read of the pull request it saw of who works on its repository.
func seenOnRead(it github.Item, review *github.PullReview) []PersonSeen {
	var out []PersonSeen

	at := it.UpdatedAt

	if it.AuthorLogin != "" && it.AuthorType == typeUser {
		out = append(out, PersonSeen{
			Login: it.AuthorLogin, Number: it.Number, Role: RoleAuthor, Access: hasAccess(it.AuthorAssociation), At: at,
		})
	}

	out = append(out, reviewersSeen(it, review.Reviews)...)
	out = append(out, committersSeen(it, review.Commits)...)

	for _, p := range review.Requested {
		out = append(out, PersonSeen{Login: p.Login, Name: p.Name, Number: it.Number, Role: RoleRequested, At: at})
	}

	for _, p := range review.Suggested {
		out = append(out, PersonSeen{Login: p.Login, Name: p.Name, Number: it.Number, Role: RoleSuggested, At: at})
	}

	return out
}

// reviewersSeen are the people of reviews, each reviewer's latest review of it, with their
// access, at when they submitted it, or its last update for one that doesn't say.
func reviewersSeen(it github.Item, reviews []github.Review) []PersonSeen {
	var out []PersonSeen

	for _, r := range reviews {
		if r.AuthorType != typeUser {
			continue
		}

		when := r.SubmittedAt
		if when.IsZero() {
			when = it.UpdatedAt
		}

		out = append(out, PersonSeen{
			Login: r.Author, Name: r.AuthorName, Number: it.Number, Role: RoleReviewer, Access: hasAccess(r.Association),
			At: when,
		})
	}

	return out
}

// committersSeen are the people commits name, each once, at their first commit's time.
func committersSeen(it github.Item, commits []github.PullCommit) []PersonSeen {
	var out []PersonSeen

	committed := map[string]bool{}

	for _, c := range commits {
		key := strings.ToLower(c.Author)
		if c.Author == "" || committed[key] {
			continue
		}

		committed[key] = true

		out = append(out, PersonSeen{Login: c.Author, Number: it.Number, Role: RoleCommitter, At: c.CommittedAt})
	}

	return out
}

// canEditReviewers reports whether the sheet may change who pull asks to review, as c, its
// cache, read it: it is open, the sheet has read it, and the user's account has triage
// access or more, as GitHub's roles give the right to request reviews.
func canEditReviewers(pull StoredPull, c *cached) bool {
	if c == nil {
		return false
	}

	state := c.context.State

	return (state == "" || state == github.StateOpen) && github.CanTriage(pull.ViewerPermission)
}

// EditReviewers changes who the pull request with req.ItemID asks to review as req asks:
// one REST request asks every person and team added, and one takes every request taken off,
// through the inbox's Actor (the item's lock, the account check, and a token borrowed again
// once when GitHub refuses one, which never sends a step twice). A review request depends
// on no commit, so it is not pinned to the head, but it needs the sheet to have read the
// pull request, open, and the user's account to have triage access or more. A name given
// twice is sent once, and one both added and taken off not at all; the user and the pull
// request's author are refused, before anything is sent. GitHub's answers say who the pull
// request asks after each step, which the inbox records on its card, so the sheet rebuilds
// with no read. Once GitHub made a step, the result counts it and says why the next failed,
// while a failure before any is the error. Each person asked counts as picked by the user in
// the People directory, so the people the user asks most rank first.
func (s *Service) EditReviewers(ctx context.Context, req ReviewersRequest) (ReviewersResult, error) {
	pull, err := s.pull(ctx, req.ItemID)
	if err != nil {
		return ReviewersResult{}, err
	}

	c, err := s.cache.load(ctx, req.ItemID, 0)
	if err != nil {
		return ReviewersResult{}, err
	}

	if !canEditReviewers(pull, c) {
		return ReviewersResult{}, cannotEditReviewers(pull, c)
	}

	add, remove := namesApart(namesOnce(req.Add), namesOnce(req.Remove))
	addTeams, removeTeams := namesApart(namesOnce(req.AddTeams), namesOnce(req.RemoveTeams))

	if err = checkReviewers(pull, add, remove); err != nil {
		return ReviewersResult{}, err
	}

	want := github.ReviewRequests{Users: add, Teams: addTeams}
	drop := github.ReviewRequests{Users: remove, Teams: removeTeams}

	if len(add)+len(addTeams)+len(remove)+len(removeTeams) == 0 {
		return ReviewersResult{}, nil
	}

	done := map[string]bool{}

	var acted Acted

	err = s.inbox.Act(ctx, req.ItemID, func(ctx context.Context, tok github.Token, t Target) (Acted, error) {
		sendErr := sendReviewers(ctx, s.github, tok, t, want, drop, done, &acted)

		return acted, sendErr
	})

	if done["+"] {
		s.picked(ctx, pull, add)
	}

	switch sent := len(done); {
	case err == nil:
		return ReviewersResult{Sent: sent}, nil
	case sent == 0:
		return ReviewersResult{}, err
	default:
		return ReviewersResult{Sent: sent, Failed: uierr.Message(err)}, nil
	}
}

// cannotEditReviewers says why the user can't change who pull asks to review, as c, its
// cache, read it.
func cannotEditReviewers(pull StoredPull, c *cached) error {
	switch {
	case c == nil:
		return uierr.New("Wait for the pull request to load from GitHub, then try again.",
			fmt.Errorf("%w: item %d not read", ErrCannotEditReviewers, pull.ID))
	case !github.CanTriage(pull.ViewerPermission):
		return uierr.New("Your account can't change this pull request's reviewers: that takes triage access.",
			fmt.Errorf("%w: permission %q", ErrCannotEditReviewers, pull.ViewerPermission))
	default:
		return uierr.New("This pull request is no longer open, so its reviewers can't change.",
			fmt.Errorf("%w: state %q", ErrCannotEditReviewers, c.context.State))
	}
}

// checkReviewers refuses a reviewer edit of pull that names the signed-in user, or asks its
// author.
func checkReviewers(pull StoredPull, add, remove []string) error {
	is := func(login string) func(string) bool {
		return func(name string) bool { return login != "" && strings.EqualFold(name, login) }
	}

	switch {
	case slices.ContainsFunc(add, is(pull.Viewer)):
		return uierr.New("Ask for your own review with Re-request my review.",
			fmt.Errorf("%w: %s added", ErrOwnReviewRequest, pull.Viewer))
	case slices.ContainsFunc(remove, is(pull.Viewer)):
		return uierr.New("Take yourself off with Remove me as reviewer.",
			fmt.Errorf("%w: %s taken off", ErrOwnReviewRequest, pull.Viewer))
	case slices.ContainsFunc(add, is(pull.AuthorLogin)):
		return uierr.New(fmt.Sprintf("GitHub doesn't ask %s to review their own pull request.", pull.AuthorLogin),
			fmt.Errorf("%w: %s", ErrAuthorReview, pull.AuthorLogin))
	default:
		return nil
	}
}

// sendReviewers sends each request of a reviewer edit GitHub hasn't made yet, by its key in
// done, in order: "+" asks everyone in want, and "-" takes everyone in drop off. It stops at
// the first that fails. acted holds, across tries, that the requests were edited and who the
// pull request asks after the last step GitHub made.
func sendReviewers(
	ctx context.Context, gh GitHub, tok github.Token, t Target, want, drop github.ReviewRequests, done map[string]bool,
	acted *Acted,
) error {
	steps := []struct {
		key  string
		who  github.ReviewRequests
		send func(context.Context, github.Token, item.Ref, github.ReviewRequests) (*github.Edited, error)
	}{
		{key: "+", who: want, send: gh.RequestReviewers},
		{key: "-", who: drop, send: gh.RemoveReviewRequests},
	}

	for _, step := range steps {
		if done[step.key] || len(step.who.Users)+len(step.who.Teams) == 0 {
			continue
		}

		edited, err := step.send(ctx, tok, t.Ref, step.who)
		if err != nil {
			return err
		}

		done[step.key] = true
		acted.RequestsEdited, acted.ReviewRequests = true, edited.ReviewRequests
	}

	return nil
}

// picked keeps the people the user asked to review pull as picked by them, in the People
// directory, now. A failure is logged.
func (s *Service) picked(ctx context.Context, pull StoredPull, logins []string) {
	if s.people == nil || len(logins) == 0 {
		return
	}

	seen := make([]PersonSeen, 0, len(logins))
	for _, login := range logins {
		seen = append(seen, PersonSeen{Login: login, Number: pull.Number, Role: RolePicked})
	}

	if err := s.people.Learn(ctx, pull.ref().Repo, seen); err != nil {
		s.logger.DebugContext(ctx, "could not keep the reviewers the user asked", "error", err)
	}
}
