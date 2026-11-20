package assignees

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/repo"
)

// testNow is the fixed time the tests' people were seen at.
var testNow = time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test value

// exampleRepo is the repository the tests' items are in.
var exampleRepo = repo.Name{Owner: "acme-org", Repo: "example-repo"} //nolint:gochecknoglobals // a fixed test value

// viewer is the signed-in user of the tests' items, and octoA the person most tests know.
const (
	viewer = "octo-me"
	octoA  = "octo-a"
)

// testItem is an issue opened by octo-author, with two participants.
func testItem() Item {
	return Item{
		Repo: exampleRepo, Viewer: viewer, Author: "octo-author", AuthorIsPerson: true,
		Participants: []string{"octo-commenter", "Octo-Author"},
	}
}

// candidateLogins lists the candidates' logins, in order.
func candidateLogins(people []Candidate) []string {
	out := make([]string, 0, len(people))
	for _, c := range people {
		out = append(out, c.Login)
	}

	return out
}

// sourcesOf lists the candidates' sources, by login.
func sourcesOf(people []Candidate) map[string]Source {
	out := make(map[string]Source, len(people))
	for _, c := range people {
		out[c.Login] = c.Source
	}

	return out
}

func TestRankOffersEachSourceInTurn(t *testing.T) {
	t.Parallel()

	k := Known{
		Assignable: []Assignable{
			{Login: "octo-listed", Listed: true},
			{Login: "octo-old", AssignedAt: testNow.Add(-48 * time.Hour)},
			{Login: "octo-recent", Name: "Octo Recent", AssignedAt: testNow, Listed: true},
			{Login: "octo-worker", Listed: true},
		},
		WorksHere: []Worker{{Login: "octo-worker", Reviewed: 4, LastAt: testNow}, {Login: "octo-me"}},
		Org:       []Worker{{Login: "octo-org", Authored: 2, LastAt: testNow, Where: "acme-org/web"}},
	}

	got := rank(testItem(), k, []Person{{Login: "octo-found", Name: "Octo Found"}, {Login: "octo-listed"}}, "")

	assert.Equal(t, []string{
		"octo-me", "octo-author", "octo-commenter", "octo-recent", "octo-old", "octo-worker", "octo-found", "octo-listed",
		"octo-org",
	}, candidateLogins(got), "each person once, in the first source that names them")
	assert.Equal(t, map[string]Source{
		"octo-me": SourceYou, "octo-author": SourceAuthor, "octo-commenter": SourceParticipant,
		"octo-recent": SourceAssignedBefore, "octo-old": SourceAssignedBefore, "octo-worker": SourceWorksHere,
		"octo-found": SourceFound, "octo-listed": SourceFound, "octo-org": SourceOrg,
	}, sourcesOf(got))

	assert.True(t, got[0].Mine)
	assert.False(t, got[1].Mine)
	assert.Equal(t, Candidate{
		Login: "octo-recent", Name: "Octo Recent", Source: SourceAssignedBefore, AssignedAt: "2026-11-09T17:00:00Z",
	}, got[3])
	assert.Equal(t, 4, got[5].Reviewed, "what they did here")
	assert.Equal(t, "Octo Found", got[6].Name)
	assert.Equal(t, Candidate{
		Login: "octo-org", Source: SourceOrg, Authored: 2, LastAt: "2026-11-09T17:00:00Z", Where: "acme-org/web",
	}, got[8], "what they did where they were last seen")
}

func TestRankLeavesOutBotsAndWhatIsNoLogin(t *testing.T) {
	t.Parallel()

	it := Item{Repo: exampleRepo, Author: "dependabot[bot]", AuthorIsPerson: true, Participants: []string{"-bad-", ""}}
	k := Known{
		Assignable: []Assignable{{Login: "renovate[bot]", Listed: true}, {Login: octoA, Listed: true}},
		WorksHere:  []Worker{{Login: octoA}},
	}

	assert.Equal(t, []string{octoA}, candidateLogins(rank(it, k, nil, "")), "no bot, no viewer, and no login GitHub can't issue")

	it.Author, it.AuthorIsPerson = "octo-author", false
	assert.Equal(t, []string{octoA}, candidateLogins(rank(it, k, nil, "")), "an author that is no person")
}

func TestACompleteListLeavesOutWhoIsNotOnIt(t *testing.T) {
	t.Parallel()

	k := Known{
		Complete: true,
		Assignable: []Assignable{
			{Login: "octo-listed", Listed: true},
			{Login: "octo-gone", AssignedAt: testNow},
		},
		WorksHere: []Worker{{Login: "octo-listed"}, {Login: "octo-reader"}},
		Org:       []Worker{{Login: "octo-org"}},
	}

	got := rank(testItem(), k, nil, "")

	assert.Equal(t, []string{"octo-me", "octo-author", "octo-commenter", "octo-listed"}, candidateLogins(got),
		"the item's own people stay; whom GitHub didn't list, and the org, go")
	assert.Equal(t, SourceWorksHere, got[3].Source)
}

func TestRankKeepsWhoMatchesAQuery(t *testing.T) {
	t.Parallel()

	k := Known{
		Assignable: []Assignable{
			{Login: "ann-lee", Listed: true},
			{Login: "bob", Name: "Annie Bob", Listed: true},
			{Login: "joanne", Listed: true},
			{Login: "carl", Listed: true},
		},
	}

	got := rank(Item{Repo: exampleRepo}, k, []Person{{Login: "carl"}}, " AN ")

	assert.Equal(t, []string{"ann-lee", "bob", "carl", "joanne"}, candidateLogins(got),
		"a login or a name's word that starts with it first, then one that holds it, and whom the search found always")
	assert.Equal(t, 0, matchRank("lee", "ann-lee"), "a word of the login")
	assert.Equal(t, 0, matchRank("org", "acme-org/web"))
	assert.Equal(t, -1, matchRank("zed", "ann", ""))
}

func TestRankIsCapped(t *testing.T) {
	t.Parallel()

	k := Known{}
	for i := range maxCandidates + 20 {
		k.Assignable = append(k.Assignable, Assignable{Login: fmt.Sprintf("octo-%03d", i), Listed: true})
	}

	got := rank(testItem(), k, nil, "")
	assert.Len(t, got, maxCandidates)
	assert.Equal(t, "octo-me", got[0].Login, "the item's own people first")
}
