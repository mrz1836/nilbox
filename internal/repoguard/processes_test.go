package repoguard_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// processStarterExceptions are the production paths besides internal/execpath allowed to
// start a process directly: Codex's app-server proxy, which outlives the call that starts
// it; the startup measurement, which runs the app itself; the test support; and the build
// targets. A path ending in "/" covers the folder.
func processStarterExceptions() []string {
	return []string{
		"internal/execpath/",
		"internal/sessions/observe/codex/proxy.go",
		"internal/measure/",
		"internal/testutil/",
		"magefiles/",
	}
}

// goLiteral matches one Go string literal on a line, interpreted or raw.
var goLiteral = regexp.MustCompile("\"(?:[^\"\\\\]|\\\\.)*\"|`[^`]*`")

// processStarter matches a call that starts a process. Built from parts so this file
// does not match itself, though test files are skipped anyway.
var processStarter = regexp.MustCompile(`\b(?:exec\.Command|exec\.CommandContext|os\.StartProcess|syscall\.ForkExec|` +
	`syscall\.` + `Exec)\(`)

// commentLine reports a line that is only a comment.
func commentLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "//")
}

// argvOrCommand returns a matcher for a program run with sub: an argv of program then
// sub as two string literals, or a string literal holding "program sub " as a command a
// shell would run. With sub empty, the argv is the program's literal alone and the command
// is "program ". Comment lines never match.
func argvOrCommand(program, sub string) func(line string) bool {
	argv := regexp.MustCompile(`(^|[\s,({])"` + regexp.QuoteMeta(program) + `"`)
	command := regexp.MustCompile(`\b` + regexp.QuoteMeta(program) + `\s`)

	if sub != "" {
		argv = regexp.MustCompile(`"` + regexp.QuoteMeta(program) + `",\s*"` + regexp.QuoteMeta(sub) + `"`)
		command = regexp.MustCompile(`\b` + regexp.QuoteMeta(program) + `\s+` + regexp.QuoteMeta(sub) + `\s`)
	}

	return func(line string) bool {
		if commentLine(line) {
			return false
		}

		if argv.MatchString(line) {
			return true
		}

		return slices.ContainsFunc(goLiteral.FindAllString(line, -1), command.MatchString)
	}
}

// goInstall matches a run of go install; sudo matches a run of sudo.
func goInstall() func(string) bool { return argvOrCommand("go", "ins"+"tall") }

func sudo() func(string) bool { return argvOrCommand("su"+"do", "") }

// productionMatches returns "file:line: text" for every line of a non-test Go file that
// match accepts, skipping files under the exempt paths (a path ending in "/" covers its
// folder).
func productionMatches(t *testing.T, root string, match func(string) bool, exempt ...string) []string {
	t.Helper()

	var found []string

	for _, rel := range goFiles(t, root) {
		if strings.HasSuffix(rel, "_test.go") || exempted(rel, exempt) {
			continue
		}

		if text, ok := readText(t, root, rel); ok {
			found = append(found, hits(rel, text, match)...)
		}
	}

	return found
}

// exempted reports whether rel is one of paths, or under one ending in "/".
func exempted(rel string, paths []string) bool {
	for _, p := range paths {
		if rel == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(rel, p)) {
			return true
		}
	}

	return false
}

func TestNoGoInstall(t *testing.T) {
	t.Parallel()

	assert.Empty(t, productionMatches(t, moduleRoot(t), goInstall()),
		"the app installs tools from their GitHub releases, verified against their checksums, never with go install")
}

func TestNoSudo(t *testing.T) {
	t.Parallel()

	assert.Empty(t, productionMatches(t, moduleRoot(t), sudo()), "the app never runs anything with sudo")
}

func TestOnlyExecpathStartsProcesses(t *testing.T) {
	t.Parallel()

	match := func(line string) bool { return !commentLine(line) && processStarter.MatchString(line) }

	assert.Empty(t, productionMatches(t, moduleRoot(t), match, processStarterExceptions()...),
		"programs start through internal/execpath (Exec.Run, or Start for a long-lived one), so the guards on what "+
			"the app runs can't be dodged by building an argv elsewhere")
}

func TestGoInstallPattern(t *testing.T) {
	t.Parallel()

	match := goInstall()

	for line, want := range map[string]bool{
		`exec.CommandContext(ctx, "go", "ins` + `tall", pkg)`: true,
		`args := []string{"go",  "ins` + `tall", "x@latest"}`: true,
		`cmd := "go ins` + `tall example.com/tool@latest"`:    true,
		"script := `go  ins" + "tall ./...`":                  true,
		`run(ctx, "pnpm", "ins` + `tall")`:                    false,
		`source := "go ins` + `tall"`:                         false,
		`// never go ins` + `tall anything`:                   false,
		`run(ctx, "go", "build", "./...")`:                    false,
		`msg := "lego ins` + `tall kit"`:                      false,
	} {
		assert.Equal(t, want, match(line), line)
	}
}

func TestSudoPattern(t *testing.T) {
	t.Parallel()

	match := sudo()

	for line, want := range map[string]bool{
		`exec.Command("su` + `do", "rm", path)`: true,
		`cmd := "su` + `do rm -rf /tmp/x"`:      true,
		`// never su` + `do`:                    false,
		`text := "pseudo code"`:                 false,
		`name := "su` + `doers"`:                false,
	} {
		assert.Equal(t, want, match(line), line)
	}
}

func TestProcessStarterPattern(t *testing.T) {
	t.Parallel()

	for line, want := range map[string]bool{
		`cmd := exec.Command(bin, "x")`:                  true,
		`cmd := exec.CommandContext(ctx, path, args...)`: true,
		`p, err := os.StartProcess(path, argv, attr)`:    true,
		`pid, err := syscall.ForkExec(path, argv, attr)`: true,
		`err := syscall.` + `Exec(path, argv, env)`:      true,
		`r := execpath.Exec{}`:                           false,
		`path, err := exec.LookPath("git")`:              false,
		`var ee *exec.ExitError`:                         false,
	} {
		assert.Equal(t, want, processStarter.MatchString(line), line)
	}

	assert.True(t, commentLine("	// exec.Command( is only used in execpath"))
	assert.True(t, exempted("internal/execpath/group_unix.go", processStarterExceptions()))
	assert.True(t, exempted("internal/sessions/observe/codex/proxy.go", processStarterExceptions()))
	assert.False(t, exempted("internal/sessions/observe/codex/client.go", processStarterExceptions()))
	assert.False(t, exempted("internal/execpathx/run.go", processStarterExceptions()))
}
