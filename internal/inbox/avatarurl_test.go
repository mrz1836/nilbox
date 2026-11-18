package inbox

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

// Bot pictures on the avatar host, as GitHub gives a bot author's.
const (
	botPicture      = "https://avatars.githubusercontent.com/in/102?s=40&v=4"
	botPictureLater = "https://avatars.githubusercontent.com/in/102?s=40&v=5"
)

// pictured is pull request number, opened by Dependabot, with its picture URL as an inbox
// read brings it.
func pictured(number int, url string) github.Item {
	it := dependabot(number)
	it.AuthorAvatarURL = url

	return it
}

// storedPicture returns the picture URL stored for the item with a number.
func (h *harness) storedPicture(t *testing.T, number int) string {
	t.Helper()

	got := h.column(t, `SELECT author_avatar_url FROM items WHERE number = ?`, number)
	require.Len(t, got, 1)

	return got[0]
}

func TestARefreshStoresABotAuthorsPictureURL(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	snap := h.seed(t, []github.Item{pictured(15, botPicture), pr(12, "CONTRIBUTOR", false)}, nil, nil)

	assert.Equal(t, botPicture, h.storedPicture(t, 15))
	assert.Empty(t, h.storedPicture(t, 12), "a person's picture is asked for by login")

	bot, person := anyItem(t, snap, 15), anyItem(t, snap, 12)
	assert.Equal(t, botPicture, bot.AuthorAvatarURL, "the snapshot carries it")
	assert.True(t, bot.AuthorIsBot)
	assert.Empty(t, person.AuthorAvatarURL)

	// A routine's Watching list reads it as the lists do.
	rows, err := h.svc.ItemsByID(t.Context(), []int64{bot.ID, person.ID})
	require.NoError(t, err)
	require.Len(t, rows, 2)

	byNumber := map[int]string{rows[0].Number: rows[0].AuthorAvatarURL, rows[1].Number: rows[1].AuthorAvatarURL}
	assert.Equal(t, map[int]string{15: botPicture, 12: ""}, byNumber)

	// The next refresh writes what GitHub says now.
	again := h.seed(t, []github.Item{pictured(15, botPictureLater), pr(12, "CONTRIBUTOR", false)}, nil, nil)
	assert.Equal(t, botPictureLater, anyItem(t, again, 15).AuthorAvatarURL)
}

func TestAnItemsOwnReadKeepsItsPictureURLCurrent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	snap := h.seed(t, []github.Item{pictured(1, botPicture)}, nil, nil)
	id := anyItem(t, snap, 1).ID

	read := func(it github.Item) Snapshot {
		t.Helper()

		it.State = github.StateOpen
		h.client.items = map[string]*github.ItemUpdate{
			nodeID(1): {Found: true, Item: it, Details: prDetails(), RateLimit: itemRate()},
		}

		got, err := h.svc.RefreshItem(t.Context(), id)
		require.NoError(t, err)

		return got
	}

	snap = read(pictured(1, botPictureLater))
	assert.Equal(t, botPictureLater, anyItem(t, snap, 1).AuthorAvatarURL, "the read's URL is stored")

	// A read of the same author that brought no URL, as a routine's watched read applied
	// again over a refresh, keeps the stored one.
	snap = read(pictured(1, ""))
	assert.Equal(t, botPictureLater, anyItem(t, snap, 1).AuthorAvatarURL)

	// An author who is no longer the bot (a deleted app's items name a ghost account)
	// has no bot picture.
	ghost := pr(1, "NONE", false)
	ghost.AuthorLogin, ghost.AuthorType = "ghost", "User"
	snap = read(ghost)
	assert.Empty(t, anyItem(t, snap, 1).AuthorAvatarURL)
	assert.Empty(t, h.storedPicture(t, 1))
}

func TestAWatchedRefreshKeepsABotsPictureURL(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		// A whole refresh lists #7 and stores its picture, then the inbox's searches let
		// it go while a routine's search still watches it.
		h.seed(t, []github.Item{pr(1, "OWNER", false), pictured(7, botPicture)}, nil, nil)
		synctest.Wait()
		require.Equal(t, botPicture, h.storedPicture(t, 7))

		h.next()
		h.source.set(Watch{Key: depQuery, Query: depQuery, Follow: true})
		h.client.details[nodeID(1)] = prDetails()
		h.client.details[nodeID(7)] = watchedPRDetails(headB, "SUCCESS")

		moved := dependabot(7)
		moved.Title = "Bump example-dep, again"
		moved.UpdatedAt = moved.UpdatedAt.Add(time.Hour)
		h.client.watches = map[string]github.WatchResult{depQuery: answer(watched(moved, headB, "SUCCESS"))}

		h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)
		synctest.Wait()

		got := h.column(t, `SELECT title FROM items WHERE number = 7`)
		require.Equal(t, []string{"Bump example-dep, again"}, got, "the pass stored the card it read")
		assert.Equal(t, botPicture, h.storedPicture(t, 7),
			"a routine's search doesn't ask for the picture, so it leaves the stored one")
	})
}
