package avatars

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// dependabot is a bot's login as GitHub's GraphQL names it, and a login a person could have.
const dependabot = "dependabot"

// Bot pictures on the avatar host, as GitHub gives a bot author's.
const (
	dependabotURL = "https://avatars.githubusercontent.com/in/102?s=40&v=4"
	helperURL     = "https://avatars.githubusercontent.com/in/103?s=40&v=4"
)

// keptLogins returns the keys of the kept pictures, in order.
func keptLogins(t *testing.T, r *rig) []string {
	t.Helper()

	return testutil.Column[string](t, r.db, "SELECT login FROM avatars ORDER BY login")
}

func TestPicturesForServesPeopleAndBotsTogether(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	got, err := r.svc.PicturesFor(t.Context(), []string{octoUser, "Octo-Other"},
		[]Bot{{Login: "Dependabot", URL: dependabotURL}, {Login: "helper-bot", URL: helperURL}})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		octoUser:          dataURL(octoUser, 1),
		"Octo-Other":      dataURL("octo-other", 1),
		"Dependabot[bot]": dataURL(dependabotURL, 1),
		"helper-bot[bot]": dataURL(helperURL, 1),
	}, got, "people by login as given, bots by login as given with [bot] after it")
	assert.Equal(t, 1, r.fetcher.times(dependabotURL), "a bot's picture comes from its URL")
	assert.Zero(t, r.fetcher.times(dependabot), "never by its login")
	assert.Equal(t, []string{"dependabot[bot]", "helper-bot[bot]", "octo-other", octoUser}, keptLogins(t, r))

	// A later look within the week is served from what was kept, whichever way it's asked.
	r.clock.Advance(refreshAfter - time.Minute)

	got, err = r.svc.PicturesFor(t.Context(), nil, []Bot{{Login: "DEPENDABOT", URL: dependabotURL}})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"DEPENDABOT[bot]": dataURL(dependabotURL, 1)}, got)
	assert.Equal(t, 1, r.fetcher.times(dependabotURL))

	// Pictures asks for people alone, as before.
	got, err = r.svc.Pictures(t.Context(), []string{octoUser})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{octoUser: dataURL(octoUser, 1)}, got)
}

func TestABotNeverTakesThePictureOfAPersonNamedTheSame(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	got, err := r.svc.PicturesFor(t.Context(), []string{dependabot}, []Bot{{Login: dependabot, URL: dependabotURL}})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		dependabot:        dataURL(dependabot, 1),
		"dependabot[bot]": dataURL(dependabotURL, 1),
	}, got)
	assert.Equal(t, []string{dependabot, "dependabot[bot]"}, keptLogins(t, r), "kept apart")

	// Each is served its own from what was kept.
	got, err = r.svc.PicturesFor(t.Context(), []string{dependabot}, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{dependabot: dataURL(dependabot, 1)}, got)
}

func TestPicturesForSkipsABotWithNoURL(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	got, err := r.svc.PicturesFor(t.Context(), nil, []Bot{{Login: dependabot}, {URL: helperURL}})
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
	assert.Zero(t, r.fetcher.times(""))
	assert.Zero(t, r.fetcher.times(helperURL), "a bot with no login is skipped too")
	assert.Zero(t, keptRows(t, r.db))
}

func TestPicturesForRefusesAURLOffTheAvatarHostAndBacksOff(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	offHost := "https://example.com/in/102.png"

	got, err := r.svc.PicturesFor(t.Context(), []string{octoUser}, []Bot{{Login: dependabot, URL: offHost}})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{octoUser: dataURL(octoUser, 1)}, got, "the refused bot gets no picture")
	assert.Equal(t, 1, r.fetcher.times(offHost))

	// It isn't asked again within retryAfter, as any failed download.
	r.clock.Advance(retryAfter - time.Second)

	got, err = r.svc.PicturesFor(t.Context(), nil, []Bot{{Login: dependabot, URL: offHost}})
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, 1, r.fetcher.times(offHost))

	r.clock.Advance(time.Second)

	_, err = r.svc.PicturesFor(t.Context(), nil, []Bot{{Login: dependabot, URL: offHost}})
	require.NoError(t, err)
	assert.Equal(t, 2, r.fetcher.times(offHost))

	// The client itself refuses the URL before it sends anything.
	svc := NewService(Config{Pool: testutil.DB(t), Fetcher: github.NewClient("test")})

	got, err = svc.PicturesFor(t.Context(), nil, []Bot{{Login: dependabot, URL: offHost}})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestMaxLoginsCountsPeopleAndBotsTogether(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	people := make([]string, 0, MaxLogins-2)
	for i := range MaxLogins - 2 {
		people = append(people, "user-"+strconv.Itoa(i))
	}

	bots := make([]Bot, 0, 5)
	for i := range 5 {
		bots = append(bots, Bot{Login: "bot-" + strconv.Itoa(i), URL: helperURL + "&b=" + strconv.Itoa(i)})
	}

	got, err := r.svc.PicturesFor(t.Context(), people, bots)
	require.NoError(t, err)
	assert.Len(t, got, MaxLogins)
	assert.Contains(t, got, "bot-1[bot]")
	assert.NotContains(t, got, "bot-2[bot]", "the people come first, then the bots, up to MaxLogins in all")
}

func TestABotsPictureAWeekOldIsDownloadedAgain(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	bots := []Bot{{Login: dependabot, URL: dependabotURL}}

	_, err := r.svc.PicturesFor(t.Context(), nil, bots)
	require.NoError(t, err)

	r.clock.Advance(refreshAfter)

	got, err := r.svc.PicturesFor(t.Context(), nil, bots)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"dependabot[bot]": dataURL(dependabotURL, 2)}, got, "the new picture")
	assert.Equal(t, 2, r.fetcher.times(dependabotURL))

	// A refresh that fails keeps the old picture.
	r.fetcher.failing(dependabotURL, true)
	r.clock.Advance(refreshAfter)

	got, err = r.svc.PicturesFor(t.Context(), nil, bots)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"dependabot[bot]": dataURL(dependabotURL, 2)}, got, "the picture kept")
}
