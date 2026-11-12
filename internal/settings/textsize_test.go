package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTextSizePercent(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 100, TextSizeDefault.Percent())
	assert.Equal(t, 115, TextSizeLarge.Percent())
	assert.Equal(t, 130, TextSizeLarger.Percent())
	assert.Equal(t, DefaultTextSize.Percent(), TextSize("huge").Percent(), "an unknown size counts as the default")
}
