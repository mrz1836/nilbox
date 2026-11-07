package terminal

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// awkwardScriptPath has a space, a single quote, a double quote, and a backslash.
const awkwardScriptPath = `/tmp/launch dir/it's "x"\y.command`

// unescapeAppleScript reverses appleScriptString.
func unescapeAppleScript(s string) string {
	return strings.NewReplacer(`\\`, `\`, `\"`, `"`).Replace(s)
}

func TestITermAppleScript(t *testing.T) {
	want := `set wasRunning to application "iTerm" is running
tell application "iTerm"
	activate
	if wasRunning and (count of windows) > 0 then
		tell current window to create tab with default profile
	else
		repeat 20 times
			if (count of windows) > 0 then exit repeat
			delay 0.1
		end repeat
		if (count of windows) = 0 then
			create window with default profile
		end if
	end if
	tell current session of current window to write text "'/tmp/launch dir/it'\\''s \"x\"\\y.command'"
end tell
`

	assert.Equal(t, want, itermAppleScript(awkwardScriptPath))
}

func TestITermAppleScriptTypesTheQuotedPath(t *testing.T) {
	paths := []string{
		awkwardScriptPath,
		"/tmp/data/Library/Application Support/App/launch/pr-7-20260110T120000Z-0a1b2c3d.command",
		"/tmp/$(id)/`whoami`/#7.command",
		"/tmp/café 🚀/a.command",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			script := itermAppleScript(path)

			const prefix = `write text "`

			start := strings.Index(script, prefix)
			require.GreaterOrEqual(t, start, 0)

			rest := script[start+len(prefix):]
			end := strings.Index(rest, "\"\nend tell")
			require.GreaterOrEqual(t, end, 0)

			typed := unescapeAppleScript(rest[:end])
			assert.Equal(t, shQuote(path), typed, "iTerm2 types the shell-quoted path")

			out, err := runSh(t, os.Environ(), "-c", "printf %s "+typed)
			require.NoError(t, err)
			assert.Equal(t, path, string(out), "the user's shell reads back exactly the path")
		})
	}
}

func TestAppleScriptString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "plain", want: "plain"},
		{input: `a "b"`, want: `a \"b\"`},
		{input: `a\b`, want: `a\\b`},
		{input: `\"`, want: `\\\"`},
		{input: "it's", want: "it's"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := appleScriptString(tt.input)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.input, unescapeAppleScript(got))
		})
	}
}
