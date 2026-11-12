package scripts

import (
	"encoding/json"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSource writes data as the file rel of the clone at dir, for a fuzz test that reads
// it back.
func writeSource(t *testing.T, dir, rel string, data []byte) {
	t.Helper()

	path := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

// checkScripts checks what a source's reader made of a file: each script from src, under
// its own ref and trimmed name, runnable exactly when nothing says why not, with a hash and
// inputs, and the same scripts when the file is read again.
func checkScripts(t *testing.T, src Source, out, again []script) {
	t.Helper()

	assert.Equal(t, out, again, "the same file reads the same every time")

	for _, sc := range out {
		e := sc.entry
		assert.Equal(t, src, e.Source)
		assert.Equal(t, ref(src, e.Name), e.Ref)
		assert.NotEmpty(t, e.Name)
		assert.Equal(t, strings.TrimSpace(e.Name), e.Name)
		assert.Equal(t, e.Why == "", e.Runnable, "%s: %q", e.Name, e.Why)
		assert.Len(t, sc.hash, 64)
		assert.NotNil(t, e.Inputs)
	}
}

// FuzzReadT3 checks that any t3.json either fails as invalid JSON or reads as scripts
// with distinct runnable names, the same every time, and that a script written into one
// reads back with its trimmed name and its command, runnable unless it has none or holds
// a character that hides what it runs.
func FuzzReadT3(f *testing.F) {
	dir := f.TempDir()

	for _, seed := range []struct {
		data, name, command string
	}{
		{"", "Dev", "pnpm dev"},
		{`{"scripts":[{"name":"Dev","command":"pnpm dev","icon":"play"},{"name":"Dev","command":"x"},{"name":" ","command":"y"}]}`, " Test ", ""},
		{"\ufeff// comment\n{\"scripts\": [{\"name\": \"a\", \"command\": \"echo \\u202e\"},],}", "b", "echo\nrm"},
		{`{"scripts":{"not":"a list"}}`, "é", "  "},
		{`[1, 2`, "x", "\t"},
	} {
		f.Add([]byte(seed.data), seed.name, seed.command)
	}

	f.Fuzz(func(t *testing.T, data []byte, name, command string) {
		writeSource(t, dir, t3File, data)

		out, found, err := readT3(dir)
		assert.True(t, found)

		if err != nil {
			assert.Nil(t, out)

			return
		}

		again, _, err := readT3(dir)
		require.NoError(t, err)
		checkScripts(t, SourceT3, out, again)

		var runnable []string

		for _, sc := range out {
			if sc.entry.Runnable {
				assert.NotContains(t, runnable, sc.entry.Name, "a name runs once")
				runnable = append(runnable, sc.entry.Name)
			}
		}

		checkT3RoundTrip(t, dir, name, command)
	})
}

// checkT3RoundTrip checks that a t3.json of one script, name and command, reads back as
// that script.
func checkT3RoundTrip(t *testing.T, dir, name, command string) {
	t.Helper()

	if strings.TrimSpace(name) == "" || !utf8.ValidString(name) || !utf8.ValidString(command) {
		return
	}

	data, err := json.Marshal(map[string]any{"scripts": []map[string]string{{"name": name, "command": command}}})
	require.NoError(t, err)
	writeSource(t, dir, t3File, data)

	out, _, err := readT3(dir)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, strings.TrimSpace(name), out[0].entry.Name)
	assert.Equal(t, command, out[0].entry.Command)
	assert.Equal(t, strings.TrimSpace(command) != "" && !hidden(command), out[0].entry.Runnable)
}

// FuzzReadTasks checks that any tasks.json either fails as invalid JSON or reads as no
// more than maxTasks scripts, the same every time, however its tasks depend on one
// another, and that a shell task written into one reads back under its trimmed label.
func FuzzReadTasks(f *testing.F) {
	dir := f.TempDir()

	for _, seed := range []struct {
		data, label, command string
	}{
		{"", "build", "go build ./..."},
		{`{"version":"2.0.0","tasks":[{"label":"a","dependsOn":["b"]},{"label":"b","dependsOn":"a"},{"label":"a"}]}`, " test ", "go test"},
		{`{"tasks":[{"label":"x","type":"process","command":"go","args":["${input:pick}"],"options":{"env":{"A=B":"1"}}}],` +
			`"inputs":[{"id":"pick","type":"pickString","options":["one",{"label":"Two","value":"2"}]}]}`, "x", "${file}"},
		{`// tasks` + "\n" + `{"tasks":[{"label":"s","command":{"value":"echo","quoting":"strong"},"dependsOn":{"x":1},},]}`, "é", "echo \u202e"},
		{`{"tasks":[{"label":7},"text",null,{"label":"bg","isBackground":true,"command":"tail -f log","dependsOrder":"sequence"}]}`, "-", ""},
	} {
		f.Add([]byte(seed.data), seed.label, seed.command)
	}

	f.Fuzz(func(t *testing.T, data []byte, label, command string) {
		writeSource(t, dir, tasksFile, data)

		out, found, err := readTasks(dir)
		assert.True(t, found)

		if err != nil {
			assert.Nil(t, out)

			return
		}

		assert.LessOrEqual(t, len(out), maxTasks)

		again, _, err := readTasks(dir)
		require.NoError(t, err)
		checkScripts(t, SourceTasks, out, again)

		if strings.TrimSpace(label) == "" || !utf8.ValidString(label) || !utf8.ValidString(command) {
			return
		}

		doc, err := json.Marshal(map[string]any{
			"version": "2.0.0", "tasks": []map[string]string{{"label": label, "type": "shell", "command": command}},
		})
		require.NoError(t, err)
		writeSource(t, dir, tasksFile, doc)

		out, _, err = readTasks(dir)
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, strings.TrimSpace(label), out[0].entry.Name)
	})
}

// FuzzParseBuiltins checks that any answer to help:commands json=true either fails or
// lists distinct, trimmed, lowercased targets, each known by its name, and that a target
// it lists, under any name, is known by that name lowercased and by each alias.
func FuzzParseBuiltins(f *testing.F) {
	for _, seed := range []struct {
		out, name, alias string
	}{
		{"", "build", "b"},
		{`{"commands":[{"name":"Build","namespace":"build","aliases":["B"]},{"name":"build"},{"name":"  "}]}`, "Lint", "l"},
		{"Loading...\n" + `{"commands":[{"name":"test:race","namespace":"Test"}]}`, "version:bump", ""},
		{`{"commands":[]}`, "  spaced  ", "ALIAS"},
		{`{"commands":"none"}`, "é", "\xff"},
	} {
		f.Add([]byte(seed.out), seed.name, seed.alias)
	}

	f.Fuzz(func(t *testing.T, out []byte, name, alias string) {
		b, err := parseBuiltins(out)
		if err == nil {
			checkBuiltins(t, b)
		}

		if strings.TrimSpace(name) == "" || !utf8.ValidString(name) || !utf8.ValidString(alias) {
			return
		}

		doc, err := json.Marshal(map[string]any{
			"commands": []map[string]any{{"name": name, "namespace": "ns", "aliases": []string{alias}}},
		})
		require.NoError(t, err)

		b, err = parseBuiltins(doc)
		require.NoError(t, err)

		lower := strings.ToLower(strings.TrimSpace(name))
		assert.Equal(t, []builtin{{Name: lower, Namespace: "ns", Aliases: []string{alias}}}, b.list)
		assert.True(t, b.shadows(lower))
		assert.True(t, b.shadows(strings.ToLower(alias)))
		assert.True(t, b.shadows("ns"))
	})
}

// checkBuiltins checks a list of magex's targets: at least one, each named once, trimmed
// and lowercased, and known by its name.
func checkBuiltins(t *testing.T, b builtins) {
	t.Helper()

	require.NotEmpty(t, b.list)

	seen := map[string]bool{}

	for _, c := range b.list {
		assert.NotEmpty(t, c.Name)
		assert.Equal(t, strings.ToLower(strings.TrimSpace(c.Name)), c.Name)
		assert.False(t, seen[c.Name], "%s listed twice", c.Name)
		assert.True(t, b.names[c.Name])

		seen[c.Name] = true
	}
}

// FuzzParseTargets checks that any Go file under magefiles/ reads as targets in name
// order, each named once, lowercased, as a function or a namespace's method, and that a
// target function and a namespace's method written into one read back under the names
// magex runs them by.
func FuzzParseTargets(f *testing.F) {
	path := filepath.Join(f.TempDir(), "magefile.go")

	for _, seed := range []struct {
		src, method string
	}{
		{"", "Race"},
		{"package main\n\n// Build builds it.\nfunc Build() error { return nil }\n\nfunc Lint(ctx context.Context, fix bool, n *int) {}\n", "Run"},
		{"package main\n\ntype Test mg.Namespace\n\nfunc (Test) Unit(d time.Duration) error { return nil }\n\nfunc (*Test) Race() {}\nfunc (t Other) X() {}\n", "Ünicode"},
		{"package main\n\nfunc () Orphan() {}\nfunc Bad(a, b context.Context) {}\nfunc Two() (int, error) { return 0, nil }\n", "lower"},
		{"package main\n\ntype (\n\tA mg.Namespace\n\tB = mg.Namespace\n)\n\n/* block */\nfunc (A) Go() {}\n", "_"},
		{"not go at all {", "Build"},
	} {
		f.Add([]byte(seed.src), seed.method)
	}

	f.Fuzz(func(t *testing.T, src []byte, method string) {
		require.NoError(t, os.WriteFile(path, src, 0o600))

		targets, err := parseTargets([]string{path})
		require.NoError(t, err)
		checkTargets(t, targets)

		if !token.IsIdentifier(method) || !token.IsExported(method) {
			return
		}

		written := "package main\n\nimport (\n\t\"context\"\n\n\t\"github.com/magefile/mage/mg\"\n)\n\n" +
			"type Ns mg.Namespace\n\n// Build builds it.\nfunc Build() error { return nil }\n\n" +
			"func (Ns) " + method + "(ctx context.Context, n int) {}\n"
		require.NoError(t, os.WriteFile(path, []byte(written), 0o600))

		targets, err = parseTargets([]string{path})
		require.NoError(t, err)

		want := []customTarget{
			{name: "build", description: "Build builds it."},
			{name: "ns:" + strings.ToLower(method), params: true},
		}
		slices.SortFunc(want, func(a, b customTarget) int { return strings.Compare(a.name, b.name) })
		assert.Equal(t, want, targets)
	})
}

// checkTargets checks a magefile's targets: in name order, each once, lowercased, and a
// function's name or a namespace's and its method's.
func checkTargets(t *testing.T, targets []customTarget) {
	t.Helper()

	assert.True(t, slices.IsSortedFunc(targets, func(a, b customTarget) int { return strings.Compare(a.name, b.name) }))

	for i, target := range targets {
		assert.Equal(t, strings.ToLower(target.name), target.name)
		assert.LessOrEqual(t, strings.Count(target.name, ":"), 1)

		if i > 0 {
			assert.NotEqual(t, targets[i-1].name, target.name)
		}
	}
}
