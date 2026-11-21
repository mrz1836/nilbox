package toolsigns_test

import (
	"path"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/toolsigns"
)

func TestMageXSigns(t *testing.T) {
	t.Parallel()

	signs := toolsigns.MageX()
	assert.Equal(t, []string{".github/env/10-mage-x.env", ".mage.yaml", "magefiles"}, signs)

	for _, sign := range signs {
		assert.Equal(t, path.Clean(sign), sign, "a clean path from the clone's top, with slashes")
		assert.False(t, path.IsAbs(sign), sign)
	}

	signs[0] = "changed"

	assert.NotEqual(t, "changed", toolsigns.MageX()[0], "each call returns a list of its own")
}
