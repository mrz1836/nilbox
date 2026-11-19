package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestTheRoutinesTransferAndProfilesRefuseUntilCoreFillsThemIn(t *testing.T) {
	t.Parallel()

	transfer := &routinePromptTransfer{}

	_, _, err := transfer.ExportPrompts(t.Context(), []string{"Review"})
	require.ErrorIs(t, err, errNotBuilt)

	_, err = transfer.PreviewPrompts(t.Context(), json.RawMessage(`{}`))
	require.ErrorIs(t, err, errNotBuilt)

	_, err = transfer.ImportPrompts(t.Context(), json.RawMessage(`{}`))
	require.ErrorIs(t, err, errNotBuilt)

	_, err = (&routineProfiles{}).Profiles(t.Context())
	require.ErrorIs(t, err, errNotBuilt)

	got, err := (&templateSignals{}).TemplateSignals(t.Context(), time.Now())
	require.NoError(t, err, "with nothing to read, nothing suggests a template")
	assert.Equal(t, autopilot.TemplateSignals{}, got)
}

func TestARoutinesPromptsTheLibraryHoldsAsTheyAreArriveTheSame(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))
	transfer := &routinePromptTransfer{prompts: prompts.NewRoutinePrompts(c.Prompts)}

	raw, n, err := transfer.ExportPrompts(t.Context(), []string{"review"})
	require.NoError(t, err)
	assert.Equal(t, 1, n, "the prompt is found by its nickname, ignoring case")

	arrivals, err := transfer.PreviewPrompts(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, []autopilot.PromptArrival{{Nickname: "Review", Arrives: autopilot.ArrivesSame}}, arrivals,
		"the library's own copy is the same")

	added, err := transfer.ImportPrompts(t.Context(), raw)
	require.NoError(t, err)
	assert.Empty(t, added, "nothing is added over a prompt of that nickname")

	none, n, err := transfer.ExportPrompts(t.Context(), []string{"No such prompt"})
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Nil(t, none, "an export that names no prompt of the library carries none")

	_, err = transfer.PreviewPrompts(t.Context(), json.RawMessage(`{"format": 42}`))
	require.Error(t, err, "a file's prompts that can't be read preview nothing")
}

func TestTheGallerysReadsFailWhenTheirStoresCantBeRead(t *testing.T) {
	t.Parallel()

	closed := testutil.ClosedDB(t)

	_, err := (&templateSignals{inbox: inbox.NewStore(closed, nil)}).TemplateSignals(t.Context(), time.Now())
	require.Error(t, err)

	agents := &routineProfiles{profiles: profiles.NewService(profiles.NewStore(closed, nil), t.TempDir(), testutil.Finder{})}

	_, err = agents.Profiles(t.Context())
	require.Error(t, err, "profiles that can't be read map nothing")
}
