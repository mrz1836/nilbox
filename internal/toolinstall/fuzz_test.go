package toolinstall

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// FuzzParseChecksums checks that any checksums file lists, for any archive name, only
// whole fields of its own lines, no more than it has lines, and that a line written the
// way a release writes one, the archive's name with or without the binary-mode "*",
// lists that checksum once more.
func FuzzParseChecksums(f *testing.F) {
	const sum = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	for _, seed := range []struct {
		data, archive string
		binary        bool
	}{
		{"", "mage-x_1.30.2_darwin_arm64.tar.gz", false},
		{sum + "  mage-x_1.30.2_darwin_arm64.tar.gz\n" + sum + " *other.tar.gz\n", "mage-x_1.30.2_darwin_arm64.tar.gz", true},
		{sum + "  a.tar.gz extra\n\n  \n" + sum + "\ta.tar.gz\r\n", "a.tar.gz", false},
		{"**a.tar.gz " + sum + "\n" + sum + " **a.tar.gz", "*a.tar.gz", true},
		{strings.Repeat("x", bufio.MaxScanTokenSize+1) + "\n" + sum + "  a.tar.gz\n", "a.tar.gz", false},
	} {
		f.Add([]byte(seed.data), seed.archive, seed.binary)
	}

	f.Fuzz(func(t *testing.T, data []byte, archive string, binary bool) {
		listed := parseChecksums(data, archive)
		assert.LessOrEqual(t, len(listed), strings.Count(string(data), "\n")+1)

		for _, checksum := range listed {
			assert.NotEmpty(t, checksum)
			assert.Equal(t, []string{checksum}, strings.Fields(checksum), "a checksum is one whole field")
		}

		if len(data) >= bufio.MaxScanTokenSize || len(strings.Fields(archive)) != 1 || archive != strings.TrimSpace(archive) ||
			strings.HasPrefix(archive, "*") {
			return
		}

		name := archive
		if binary {
			name = "*" + archive
		}

		again := parseChecksums([]byte(string(data)+"\n"+"0123  "+name+"\n"), archive)
		assert.Equal(t, append(listed, "0123"), again)
	})
}
