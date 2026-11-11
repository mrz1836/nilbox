package notifyprefs_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/notifyprefs"
)

func TestKinds(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []notifyprefs.Kind{
		"review_requested", "assigned", "woke", "session_needs_you", "session_finished", "release_done",
		"release_published", "release_failed",
	}, notifyprefs.Kinds())

	for _, kind := range notifyprefs.Kinds() {
		assert.True(t, kind.Valid(), kind)
	}

	assert.False(t, notifyprefs.Kind("mentions").Valid())
	assert.False(t, notifyprefs.Kind("").Valid())
}

func TestDecodeBannersOff(t *testing.T) {
	t.Parallel()

	sessionKinds := []notifyprefs.Kind{notifyprefs.KindSessionNeedsYou, notifyprefs.KindSessionFinished}

	tests := []struct {
		name    string
		off, on string
		want    []notifyprefs.Kind
	}{
		{name: "nothing stored: the inbox's kinds are on, the sessions' off", want: sessionKinds},
		{name: "not JSON", off: "not json", on: "not json", want: sessionKinds},
		{name: "not a list", off: `{"woke":true}`, want: sessionKinds},
		{name: "empty lists", off: `[]`, on: `[]`, want: sessionKinds},
		{
			name: "known kinds in the order of Kinds, unknown ones dropped", off: `["woke","mentions","review_requested"]`,
			want: append([]notifyprefs.Kind{notifyprefs.KindReviewRequested, notifyprefs.KindWoke}, sessionKinds...),
		},
		{
			name: "a session kind turned on", on: `["session_finished","mentions"]`,
			want: []notifyprefs.Kind{notifyprefs.KindSessionNeedsYou},
		},
		{
			name: "each list counts only for the kinds that start its way", off: `["session_finished"]`, on: `["woke"]`,
			want: sessionKinds,
		},
		{
			name: "a release reaching GitHub turned off", off: `["release_done"]`,
			want: append(slices.Clone(sessionKinds), notifyprefs.KindReleaseDone),
		},
		{
			name: "the release workflow's kinds start on, and turn off", off: `["release_failed","release_published"]`,
			want: append(slices.Clone(sessionKinds), notifyprefs.KindReleasePublished, notifyprefs.KindReleaseFailed),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, notifyprefs.DecodeBannersOff(tt.off, tt.on))
		})
	}
}

func TestDefaultOn(t *testing.T) {
	t.Parallel()

	for _, kind := range notifyprefs.Kinds() {
		session := kind == notifyprefs.KindSessionNeedsYou || kind == notifyprefs.KindSessionFinished
		assert.Equal(t, !session, kind.DefaultOn(), kind)
	}
}

func TestSetBanner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		off  []notifyprefs.Kind
		kind notifyprefs.Kind
		on   bool
		want []notifyprefs.Kind
	}{
		{name: "turn one off", off: nil, kind: notifyprefs.KindWoke, on: false, want: []notifyprefs.Kind{notifyprefs.KindWoke}},
		{
			name: "turn another off, in the order of Kinds", off: []notifyprefs.Kind{notifyprefs.KindWoke},
			kind: notifyprefs.KindReviewRequested, on: false,
			want: []notifyprefs.Kind{notifyprefs.KindReviewRequested, notifyprefs.KindWoke},
		},
		{
			name: "turn one back on", off: []notifyprefs.Kind{notifyprefs.KindReviewRequested, notifyprefs.KindWoke},
			kind: notifyprefs.KindWoke, on: true, want: []notifyprefs.Kind{notifyprefs.KindReviewRequested},
		},
		{name: "on that was on", off: []notifyprefs.Kind{}, kind: notifyprefs.KindAssigned, on: true, want: []notifyprefs.Kind{}},
		{
			name: "off that was off", off: []notifyprefs.Kind{notifyprefs.KindAssigned}, kind: notifyprefs.KindAssigned,
			on: false, want: []notifyprefs.Kind{notifyprefs.KindAssigned},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := notifyprefs.SetBanner(tt.off, tt.kind, tt.on)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSetBannerRejectsAnUnknownKind(t *testing.T) {
	t.Parallel()

	_, err := notifyprefs.SetBanner(nil, "mentions", true)
	require.ErrorIs(t, err, notifyprefs.ErrUnknownKind)
	assert.Contains(t, err.Error(), `"mentions"`)
}

func TestBannersRoundTrip(t *testing.T) {
	t.Parallel()

	storedOff, storedOn := notifyprefs.EncodeBanners([]notifyprefs.Kind{notifyprefs.KindReviewRequested})
	assert.JSONEq(t, `["review_requested"]`, storedOff)
	assert.JSONEq(t, `["session_needs_you","session_finished"]`, storedOn, "the sessions' kinds are on: not listed off")

	storedOff, storedOn = notifyprefs.EncodeBanners(nil)
	assert.JSONEq(t, `[]`, storedOff)
	assert.JSONEq(t, `["session_needs_you","session_finished"]`, storedOn)

	for _, off := range [][]notifyprefs.Kind{
		{}, {notifyprefs.KindWoke}, notifyprefs.Kinds(), {notifyprefs.KindSessionFinished}, {notifyprefs.KindReleaseDone},
	} {
		assert.Equal(t, off, notifyprefs.DecodeBannersOff(notifyprefs.EncodeBanners(off)))
	}
}
