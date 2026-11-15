//go:build darwin

package localrepos

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMountPointsReadTheMountTable(t *testing.T) {
	t.Parallel()

	assert.Contains(t, mountPoints(), "/", "the startup disk is always mounted")
}
