package notifyprefs_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/notifyprefs"
)

func TestKinds(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []notifyprefs.Kind{"review_requested", "assigned", "woke"}, notifyprefs.Kinds())

	for _, kind := range notifyprefs.Kinds() {
		assert.True(t, kind.Valid(), kind)
	}

	assert.False(t, notifyprefs.Kind("mentions").Valid())
	assert.False(t, notifyprefs.Kind("").Valid())
}

func TestDecodeBannersOff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stored string
		want   []notifyprefs.Kind
	}{
		{name: "nothing stored: every kind is on", stored: "", want: []notifyprefs.Kind{}},
		{name: "not JSON", stored: "not json", want: []notifyprefs.Kind{}},
		{name: "not a list", stored: `{"woke":true}`, want: []notifyprefs.Kind{}},
		{name: "an empty list", stored: `[]`, want: []notifyprefs.Kind{}},
		{
			name: "known kinds in the order of Kinds, unknown ones dropped", stored: `["woke","mentions","review_requested"]`,
			want: []notifyprefs.Kind{notifyprefs.KindReviewRequested, notifyprefs.KindWoke},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, notifyprefs.DecodeBannersOff(tt.stored))
		})
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

func TestBannersOffRoundTrip(t *testing.T) {
	t.Parallel()

	assert.JSONEq(t, `["review_requested"]`, notifyprefs.EncodeBannersOff([]notifyprefs.Kind{notifyprefs.KindReviewRequested}))
	assert.JSONEq(t, `[]`, notifyprefs.EncodeBannersOff(nil))

	for _, off := range [][]notifyprefs.Kind{{}, {notifyprefs.KindWoke}, notifyprefs.Kinds()} {
		assert.Equal(t, off, notifyprefs.DecodeBannersOff(notifyprefs.EncodeBannersOff(off)))
	}
}
