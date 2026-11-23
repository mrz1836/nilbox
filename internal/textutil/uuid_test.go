package textutil_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// v4 is a version 4 UUID with the RFC 9562 variant, in its canonical form.
var v4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewUUID(t *testing.T) {
	t.Parallel()

	a, b := textutil.NewUUID(), textutil.NewUUID()

	assert.Regexp(t, v4, a)
	assert.Regexp(t, v4, b)
	assert.NotEqual(t, a, b)
}
