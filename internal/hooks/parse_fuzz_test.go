package hooks

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// hookName is what a hook type's name looks like in a status line.
var hookName = regexp.MustCompile(`^[a-z-]+$`)

// FuzzParseEnv checks that any .env file reads as settings each named by a trimmed name,
// with no line break, that doesn't start a comment, and that a setting written with its
// value in quotes, any value on one line, reads back as that value.
func FuzzParseEnv(f *testing.F) {
	for _, seed := range []struct {
		data, key, value string
	}{
		{"", "ENABLE_GO_PRE_COMMIT", "true"},
		{"# comment\nA=1\n  B = two # note\nC='quoted # kept'\nD=\"x\nE\n=no-name\n", "GO_PRE_COMMIT_FUZZ", "  spaced  "},
		{"K=\"\nK2='\nK3=\"a'\r\n\tK4\t=\tv\t#c\n", "WITH SPACE", "\"inner\""},
		{"\xff=\xfe\n#=x\n", "é", "#"},
	} {
		f.Add([]byte(seed.data), seed.key, seed.value)
	}

	f.Fuzz(func(t *testing.T, data []byte, key, value string) {
		for name, setting := range parseEnv(data) {
			assert.NotEmpty(t, name)
			assert.Equal(t, strings.TrimSpace(name), name)
			assert.False(t, strings.HasPrefix(name, "#"), "%q", name)
			assert.NotContains(t, name+setting, "\n")
		}

		if key != strings.TrimSpace(key) || key == "" || strings.HasPrefix(key, "#") ||
			strings.ContainsAny(key, "=\n") || strings.Contains(value, "\n") {
			return
		}

		assert.Equal(t, map[string]string{key: value}, parseEnv([]byte(key+"=\""+value+"\"\n")))
	})
}

// FuzzReadStatusText checks that any `go-pre-commit status --no-color` output either
// fails as a status that failed or said nothing known, or reads as hook types in a state
// the scan knows, and that a hook type's line, with any message a terminal shows as it
// is, reads back as the state its message says.
func FuzzReadStatusText(f *testing.F) {
	for _, seed := range []struct {
		out, name, message string
	}{
		{"", "pre-commit", "Go pre-commit hook installed and ready"},
		{"✓   ✓ pre-commit: Go pre-commit hook installed but outdated\nPre-commit system is enabled\n", "commit-msg", "Different hook installed"},
		{"Pre-commit system is disabled\n\x1b[32m✓\x1b[0m   ✓ pre-push: something new\n", "pre-push", "installed and ready (warning: not executable)"},
		{"No hooks are currently installed\r\nError: no config\n", "x", "Error: anything"},
		{"⚠ ✗ Bad: x\n✗\t✗ a-b: Different hook\n", "a-b", "trailing space "},
	} {
		f.Add([]byte(seed.out), seed.name, seed.message)
	}

	f.Fuzz(func(t *testing.T, out []byte, name, message string) {
		rep, err := readStatusText(out)
		if err != nil {
			assert.True(t, errors.Is(err, errStatusFailed) || errors.Is(err, errStatusOutput), "%v", err)
			assert.Nil(t, rep.hooks)
		}

		checkStatusReport(t, rep, statusReady, statusOutdated, statusOther, "")

		if !hookName.MatchString(name) || message == "" || textutil.TerminalLine(message) != message {
			return
		}

		got, err := readStatusText([]byte("Pre-commit system is enabled\n✓   ✓ " + name + ": " + message + "\n"))
		require.NoError(t, err)
		assert.Equal(t, map[string]string{name: hookStatus(message)}, got.hooks)
	})
}

// FuzzReadStatusJSON checks that any `go-pre-commit status --json` document either fails
// or reads as hook types in a state the scan knows, and that a document go-pre-commit
// writes reads back as each hook type's state, one not executable read as ready or
// outdated, and one in any other state left out.
func FuzzReadStatusJSON(f *testing.F) {
	for _, seed := range []struct {
		out, name, state string
		outdated, on     bool
	}{
		{"", "pre-commit", statusReady, false, true},
		{`{"enabled":true,"hooks":[{"name":"pre-commit","state":"ready"},{"name":"x","state":"missing"}]}`, "commit-msg", statusNotExecutable, true, false},
		{`{"enabled":false,"hooks":null}`, "pre-push", "error", false, true},
		{`{"error":{"code":"no_config","message":"no config"}}`, "", statusOther, true, true},
		{`{"hooks":[]}`, "é", statusOutdated, false, false},
	} {
		f.Add([]byte(seed.out), seed.name, seed.state, seed.outdated, seed.on)
	}

	f.Fuzz(func(t *testing.T, out []byte, name, state string, outdated, on bool) {
		rep, err := readStatusJSON(out)
		if err != nil {
			assert.Nil(t, rep.hooks)
		}

		checkStatusReport(t, rep, statusReady, statusOutdated, statusOther)

		if !utf8.ValidString(name) {
			return
		}

		doc, err := json.Marshal(map[string]any{
			"enabled": on, "hooks": []map[string]any{{"name": name, "state": state, "outdated": outdated}},
		})
		require.NoError(t, err)

		got, err := readStatusJSON(doc)
		require.NoError(t, err)
		assert.Equal(t, !on, got.disabled)
		assert.Equal(t, statusJSONWant(name, state, outdated), got.hooks)
	})
}

// statusJSONWant is the hook types readStatusJSON reads from a document of one hook type,
// name, in state.
func statusJSONWant(name, state string, outdated bool) map[string]string {
	switch {
	case state == statusReady || state == statusOutdated || state == statusOther:
		return map[string]string{name: state}
	case state == statusNotExecutable && outdated:
		return map[string]string{name: statusOutdated}
	case state == statusNotExecutable:
		return map[string]string{name: statusReady}
	default:
		return map[string]string{}
	}
}

// checkStatusReport checks that every hook type rep names is in one of states.
func checkStatusReport(t *testing.T, rep statusReport, states ...string) {
	t.Helper()

	for name, state := range rep.hooks {
		assert.Contains(t, states, state, "%s", name)
	}
}
