package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeToastSeconds(t *testing.T) {
	t.Parallel()

	for stored, want := range map[string]int{
		"5": 5, "12": 12, "37": 37, "60": 60,
		"": DefaultToastSeconds, "4": DefaultToastSeconds, "61": DefaultToastSeconds, "-12": DefaultToastSeconds,
		"0": DefaultToastSeconds, "12.5": DefaultToastSeconds, " 12": DefaultToastSeconds, "soon": DefaultToastSeconds,
	} {
		assert.Equal(t, want, decodeToastSeconds(stored), "stored %q", stored)
	}
}

func TestToastSecondsBoundsAreKept(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	for _, seconds := range []int{MinToastSeconds, MaxToastSeconds} {
		got, err := svc.SetToastSeconds(t.Context(), seconds)
		require.NoError(t, err)
		assert.Equal(t, seconds, got.ToastSeconds)

		again, err := svc.Get(t.Context())
		require.NoError(t, err)
		assert.Equal(t, seconds, again.ToastSeconds)
	}
}

func TestTheWindowTimesToastsAsTheSettingsDo(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "components", "Toast.tsx"))
	require.NoError(t, err)

	// Before the settings load, and on the slider in Settings, the window uses its own
	// copies of these, so they must be the ones the settings keep to.
	for name, value := range map[string]int{
		"minToastSeconds": MinToastSeconds, "maxToastSeconds": MaxToastSeconds,
		"defaultToastSeconds": DefaultToastSeconds,
	} {
		want := fmt.Sprintf("export const %s = %d\n", name, value)
		assert.Contains(t, string(source), want, "frontend/src/components/Toast.tsx must set %s as the settings do", name)
	}
}
