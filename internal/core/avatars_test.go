package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestOneAvatarsServiceServesTheSheetsAndTheCards(t *testing.T) {
	t.Parallel()

	var logs testutil.Logs

	opts := testOptions(t)
	opts.Logger = logs.Logger()
	c := bootstrap(t, opts)

	require.NotNil(t, c.Avatars)

	// A login no user could have is refused before anything is sent, and a refused
	// download isn't tried again for a while by the service that tried it. So the issue
	// sheet's Avatars, asked after the bound service, tries nothing again only when the two
	// are one service.
	got, err := c.Avatars.Pictures(t.Context(), []string{"helper[bot]"})
	require.NoError(t, err)
	assert.Empty(t, got)

	got, err = c.Issues.Avatars(t.Context(), []string{"helper[bot]"})
	require.NoError(t, err)
	assert.Empty(t, got)

	assert.Equal(t, 1, strings.Count(logs.String(), "download a profile picture"),
		"the sheets and the cards share one service, which tried the picture once")
}
