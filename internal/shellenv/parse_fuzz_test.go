package shellenv

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzParse checks that any output of the login shell either holds no environment
// between its markers, or none worth keeping, or reads as variables that each have a
// name, with no line break or NUL in it, before an =, and that an environment printed
// between two markers, whatever a startup file prints around them, reads back as it was.
func FuzzParse(f *testing.F) {
	const marker = "ENV0123456789abcdef0123456789abcdef"

	for _, seed := range []struct {
		out, before, after, key, value string
	}{
		{"", "", "", "PATH", "/usr/bin:/bin"},
		{marker + "HOME=/srv/home\x00PATH=/bin\x00" + marker, "motd\n", "bye\n", "EMPTY", ""},
		{"x" + marker + "=no-name\x00NO_EQUALS\x00BAD\nKEY=1\x00" + marker + "y", "", "", "MULTI", "line\none"},
		{marker, marker, "", "A", "=b=c"},
		{marker + marker, "\x00", "\xff", "é", "\xfe"},
	} {
		f.Add([]byte(seed.out), seed.before, seed.after, seed.key, seed.value)
	}

	f.Fuzz(func(t *testing.T, out []byte, before, after, key, value string) {
		env, err := parse(out, marker)
		if err != nil {
			assert.True(t, errors.Is(err, ErrNoMarkers) || errors.Is(err, ErrNoVariables), "%v", err)
			assert.Nil(t, env)
		}

		for _, kv := range env {
			name, _, ok := strings.Cut(kv, "=")
			assert.True(t, ok, "%q", kv)
			assert.NotEmpty(t, name)
			assert.NotContains(t, name, "\n")
			assert.NotContains(t, kv, "\x00")
		}

		// The marker shares no prefix with its own suffix, so it is found only where it
		// was printed whole.
		if key == "" || strings.ContainsAny(key, "=\n\x00") || strings.Contains(value, "\x00") ||
			strings.Contains(before+"\x00"+after+"\x00"+key+"="+value, marker) {
			return
		}

		vars := []string{key + "=" + value, "SHELL=/bin/zsh"}
		printed := before + marker + strings.Join(vars, "\x00") + "\x00" + marker + after

		got, err := parse([]byte(printed), marker)
		require.NoError(t, err)
		assert.Equal(t, vars, got)
	})
}
