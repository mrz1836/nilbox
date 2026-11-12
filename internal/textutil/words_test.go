package textutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/textutil"
)

func TestCounted(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1 file", textutil.Counted(1, "file", "files"))
	assert.Equal(t, "0 files", textutil.Counted(0, "file", "files"))
	assert.Equal(t, "3 files", textutil.Counted(3, "file", "files"))
	assert.Equal(t, "-1 files", textutil.Counted(-1, "file", "files"))
	assert.Equal(t, "2 required checks are failing.",
		textutil.Counted(2, "required check is failing.", "required checks are failing."), "a noun can carry its verb")
}

func TestSeries(t *testing.T) {
	t.Parallel()

	assert.Empty(t, textutil.Series(nil))
	assert.Empty(t, textutil.Series([]string{}))
	assert.Equal(t, "a", textutil.Series([]string{"a"}))
	assert.Equal(t, "a and b", textutil.Series([]string{"a", "b"}))
	assert.Equal(t, "a, b, and c", textutil.Series([]string{"a", "b", "c"}))
	assert.Equal(t, "a, b, c, and 2 more", textutil.Series([]string{"a", "b", "c", "2 more"}))

	names := []string{"a", "b", "c"}
	textutil.Series(names)
	assert.Equal(t, []string{"a", "b", "c"}, names, "the names are left as they were")
}
