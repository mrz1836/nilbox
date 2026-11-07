package notifyprefs_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/notifyprefs"
)

func TestKinds(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"review_requested", "assigned", "woke"}, notifyprefs.Kinds())
}

func TestDecodeBannersOff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stored string
		want   []string
	}{
		{name: "nothing stored: every kind is on", stored: "", want: []string{}},
		{name: "not JSON", stored: "not json", want: []string{}},
		{name: "not a list", stored: `{"woke":true}`, want: []string{}},
		{name: "an empty list", stored: `[]`, want: []string{}},
		{
			name: "known kinds in the order of Kinds, unknown ones dropped", stored: `["woke","mentions","review_requested"]`,
			want: []string{notifyprefs.KindReviewRequested, notifyprefs.KindWoke},
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
		off  []string
		kind string
		on   bool
		want []string
	}{
		{name: "turn one off", off: nil, kind: notifyprefs.KindWoke, on: false, want: []string{notifyprefs.KindWoke}},
		{
			name: "turn another off, in the order of Kinds", off: []string{notifyprefs.KindWoke},
			kind: notifyprefs.KindReviewRequested, on: false,
			want: []string{notifyprefs.KindReviewRequested, notifyprefs.KindWoke},
		},
		{
			name: "turn one back on", off: []string{notifyprefs.KindReviewRequested, notifyprefs.KindWoke},
			kind: notifyprefs.KindWoke, on: true, want: []string{notifyprefs.KindReviewRequested},
		},
		{name: "on that was on", off: []string{}, kind: notifyprefs.KindAssigned, on: true, want: []string{}},
		{
			name: "off that was off", off: []string{notifyprefs.KindAssigned}, kind: notifyprefs.KindAssigned,
			on: false, want: []string{notifyprefs.KindAssigned},
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

	assert.JSONEq(t, `["review_requested"]`, notifyprefs.EncodeBannersOff([]string{notifyprefs.KindReviewRequested}))
	assert.JSONEq(t, `[]`, notifyprefs.EncodeBannersOff(nil))

	for _, off := range [][]string{{}, {notifyprefs.KindWoke}, notifyprefs.Kinds()} {
		assert.Equal(t, off, notifyprefs.DecodeBannersOff(notifyprefs.EncodeBannersOff(off)))
	}
}
