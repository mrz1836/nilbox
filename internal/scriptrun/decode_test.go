package scriptrun

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecode(t *testing.T) {
	t.Parallel()

	type intent struct {
		Version string   `json:"version"`
		Steps   []string `json:"steps"`
	}

	got, err := Decode[intent](map[string]any{"version": "v1.4.3", "steps": []any{"tag", "push"}})
	require.NoError(t, err)
	assert.Equal(t, intent{Version: "v1.4.3", Steps: []string{"tag", "push"}}, got, "as the history decodes it")

	got, err = Decode[intent](nil)
	require.NoError(t, err)
	assert.Zero(t, got, "nothing recorded")

	got, err = Decode[intent](map[string]any{"version": "v1.4.3", "steps": "tag"})
	require.ErrorContains(t, err, "json: cannot unmarshal string", "a value of another shape")
	assert.Zero(t, got, "nothing half read")

	_, err = Decode[intent](func() {})
	require.ErrorContains(t, err, "json: unsupported type", "a value JSON can't hold")
}
