package devtools_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/devtools"
)

func TestFeedKeepsTheLastEntries(t *testing.T) {
	t.Parallel()

	feed := devtools.NewFeed[int](3)
	assert.Empty(t, feed.Last(0))

	for i := range 10 {
		feed.Add(i)
	}

	assert.Equal(t, []int{7, 8, 9}, feed.Last(0), "the oldest go once it is full")
	assert.Equal(t, []int{8, 9}, feed.Last(2))
	assert.Equal(t, []int{7, 8, 9}, feed.Last(50))
	assert.Equal(t, 3, feed.Len())

	feed.Add(10)
	assert.Equal(t, []int{8, 9, 10}, feed.Last(0))
}

func TestFeedKeepsOneAtLeast(t *testing.T) {
	t.Parallel()

	feed := devtools.NewFeed[string](0)
	feed.Add("a")
	feed.Add("b")

	assert.Equal(t, []string{"b"}, feed.Last(0))
}

func TestFeedFollowers(t *testing.T) {
	t.Parallel()

	feed := devtools.NewFeed[int](2)
	feed.Add(1)

	entries, stop := feed.Follow()
	other, stopOther := feed.Follow()

	feed.Add(2)

	assert.Equal(t, 2, <-entries, "a follower hears what comes after it started")
	assert.Equal(t, 2, <-other)

	stop()
	stop()

	_, open := <-entries
	assert.False(t, open, "stopping closes the follower, once")

	feed.Add(3)
	assert.Equal(t, 3, <-other, "the others keep hearing")

	stopOther()
}

func TestFeedClear(t *testing.T) {
	t.Parallel()

	feed := devtools.NewFeed[int](2)
	assert.Equal(t, 0, feed.Clear())

	for i := range 5 {
		feed.Add(i)
	}

	entries, stop := feed.Follow()
	defer stop()

	assert.Equal(t, 2, feed.Clear(), "it counts only what it keeps")
	assert.Empty(t, feed.Last(0))
	assert.Equal(t, 0, feed.Len())

	feed.Add(5)
	assert.Equal(t, 5, <-entries, "its followers go on following")
	assert.Equal(t, []int{5}, feed.Last(0))
}

func TestFeedNeverWaitsOnASlowFollower(t *testing.T) {
	t.Parallel()

	feed := devtools.NewFeed[int](10)

	entries, stop := feed.Follow()
	defer stop()

	for i := range 1000 {
		feed.Add(i)
	}

	require.Len(t, entries, 256, "the follower holds what it has room for and misses the rest")
	assert.Equal(t, 0, <-entries)
	assert.Equal(t, []int{990, 991, 992, 993, 994, 995, 996, 997, 998, 999}, feed.Last(0))
}
