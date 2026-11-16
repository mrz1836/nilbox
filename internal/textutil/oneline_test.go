package textutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/textutil"
)

func TestOneLine(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"":                          "",
		"  plain  ":                 "plain",
		"two\nlines\r\n\tand a tab": "two lines and a tab",
		"a \x00 control":            "a control",
	} {
		assert.Equal(t, want, textutil.OneLine(in), "%q", in)
	}

	assert.Equal(t, "bidi gnp.exe", textutil.OneLine("bidi "+string(rune(0x202E))+"gnp.exe"+string(rune(0x202C))),
		"bidirectional-text controls are removed")
}
