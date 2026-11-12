package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsDark(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		theme      Theme
		systemDark bool
		want       bool
	}{
		{ThemeSystem, true, true},
		{ThemeSystem, false, false},
		{ThemeDark, false, true},
		{ThemeDark, true, true},
		{ThemeLight, true, false},
		{ThemeLight, false, false},
		{"unknown", true, true},
		{"", false, false},
	} {
		assert.Equal(t, tc.want, IsDark(tc.theme, tc.systemDark), "%q with the system dark: %t", tc.theme, tc.systemDark)
	}
}
