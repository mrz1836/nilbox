package updateprefs_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/updateprefs"
)

func TestModes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []updateprefs.Mode{updateprefs.ModeDownload, updateprefs.ModeNotify, updateprefs.ModeOff},
		updateprefs.Modes())
	assert.Equal(t, updateprefs.ModeDownload, updateprefs.DefaultMode)

	for _, m := range updateprefs.Modes() {
		assert.True(t, m.Valid(), m)
		assert.Equal(t, m, updateprefs.Decode(string(m)), "a stored choice reads back as itself")
		assert.Equal(t, m != updateprefs.ModeOff, m.Checks(), m)
	}

	for _, stored := range []string{"", "auto", "DOWNLOAD", " off"} {
		assert.False(t, updateprefs.Mode(stored).Valid(), stored)
		assert.Equal(t, updateprefs.DefaultMode, updateprefs.Decode(stored), "%q reads as the default", stored)
	}
}
