//go:build !darwin

package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRegistryEmptyOffDarwin(t *testing.T) {
	t.Parallel()

	r := DefaultRegistry(nil)

	assert.Empty(t, r.IDs())

	_, err := r.Get("iterm2")
	require.ErrorIs(t, err, ErrUnsupportedOS)
}
