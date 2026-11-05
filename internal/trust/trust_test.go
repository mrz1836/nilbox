package trust_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/trust"
)

func TestTrustedMatrix(t *testing.T) {
	associations := map[string]bool{
		"OWNER":                  true,
		"MEMBER":                 true,
		"COLLABORATOR":           true,
		"CONTRIBUTOR":            false,
		"FIRST_TIME_CONTRIBUTOR": false,
		"FIRST_TIMER":            false,
		"MANNEQUIN":              false,
		"NONE":                   false,
		"":                       false,
		"SOMETHING_NEW":          false,
	}

	for assoc, trustedAssoc := range associations {
		for _, isPR := range []bool{true, false} {
			for _, crossRepo := range []bool{true, false} {
				name := fmt.Sprintf("%q/pr=%t/cross=%t", assoc, isPR, crossRepo)

				// A fork pull request is never trusted; otherwise the association decides.
				want := trustedAssoc && (!isPR || !crossRepo)

				assert.Equal(t, want, trust.Trusted(assoc, isPR, crossRepo), name)
			}
		}
	}
}

func TestTrustedNormalizesCase(t *testing.T) {
	assert.True(t, trust.Trusted(" owner ", true, false))
	assert.True(t, trust.Trusted("Member", false, false))
	assert.False(t, trust.Trusted("contributor", false, false))
}
