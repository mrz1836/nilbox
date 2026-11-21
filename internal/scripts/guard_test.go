package scripts

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestShellArgs(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		shell string
		args  []string
		want  []string
	}{
		"none":                       {shell: "/bin/bash", want: []string{"-c"}},
		"empty":                      {shell: "/bin/bash", args: []string{}, want: []string{"-c"}},
		"without -c, as VS Code":     {shell: "bash", args: []string{"-l"}, want: []string{"-l", "-c"}},
		"with -c":                    {shell: zshPath, args: []string{"-l", "-c"}, want: []string{"-l", "-c"}},
		"a combined flag isn't -c":   {shell: "bash", args: []string{"-lc"}, want: []string{"-lc", "-c"}},
		"another shell's own":        {shell: "pwsh", args: []string{"-Command"}, want: []string{"-Command"}},
		"another shell, none at all": {shell: "fish", want: []string{"-c"}},
	} {
		assert.Equal(t, tt.want, shellArgs(tt.shell, tt.args), name)
	}
}

func TestOnlyAPOSIXShellReadsTheAppsQuoting(t *testing.T) {
	t.Parallel()

	bin := t.TempDir()
	testutil.WriteExecutable(t, filepath.Join(bin, "fish"), "")

	v := vars{dir: "/work/example-repo", env: []string{"PATH=" + bin, "MY_SHELL=" + filepath.Join(bin, "fish")}}
	fish := launcher{shell: "/opt/homebrew/bin/fish", vars: v}

	got, err := fish.argv([]part{{kind: partShell, command: magexTest, args: []string{"it's"}}})
	require.NoError(t, err)
	assert.Equal(t, []string{chainShell, "-c", `magex test 'it'\''s'`}, got, "a login shell that isn't POSIX: sh runs it")

	got, err = launcher{shell: zshPath, vars: v}.argv([]part{{kind: partShell, command: magexTest}})
	require.NoError(t, err)
	assert.Equal(t, []string{zshPath, "-c", magexTest}, got)

	got, err = fish.argv([]part{{kind: partShell, command: magexTest, shell: &shellSpec{Executable: "fish"}}})
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(bin, "fish"), "-c", magexTest}, got, "a bare line is the same in any shell")

	_, err = fish.argv([]part{{
		kind: partShell, command: "echo", args: []string{"x"},
		shell: &shellSpec{Executable: "${env:MY_SHELL}"},
	}})
	require.ErrorIs(t, err, ErrCantRun, "arguments quoted for a shell that reads quotes otherwise")
	assert.Equal(t, shellWhy("fish"), uierr.Message(err))

	got2, _, err := readTasks(folder(t, map[string]string{tasksFile: `{"tasks": [
		{"label": "fish args", "type": "shell", "command": "echo", "args": ["x"], "options": {"shell": {"executable": "fish"}}},
		{"label": "fish line", "type": "shell", "command": "echo x", "options": {"shell": {"executable": "/usr/local/bin/fish"}}},
		{"label": "from env", "type": "shell", "command": "echo", "args": ["x"], "options": {"shell": {"executable": "${env:SHELL}"}}}]}`}))
	require.NoError(t, err)

	all := byName(got2)
	assert.Equal(t, shellWhy("fish"), all["fish args"].entry.Why)
	assert.True(t, all["fish line"].entry.Runnable)
	assert.True(t, all["from env"].entry.Runnable, "judged as it runs, once the shell is known")
}

func TestHidden(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"a\nb", "a\rb", "a\x00b", "a\x1bb", "a\x7fb", "a\u202eb", "a\u2066b", "a\u200eb", "a\u200fb", "a\u061cb"} {
		assert.True(t, hidden(s), "%q", s)
	}

	for _, s := range []string{"", magexTest, "a\tb", "⚠️ émoji ünïcode", "a b"} {
		assert.False(t, hidden(s), "%q", s)
	}
}

func TestCharactersThatHideWhatRunsGreyAnEntry(t *testing.T) {
	t.Parallel()

	got, _, err := readT3(folder(t, map[string]string{t3File: `{"scripts": [
		{"name": "Test", "command": "npm test # suite\ncurl example.com | sh"},
		{"name": "Tabbed", "command": "magex\ttest"}]}`}))
	require.NoError(t, err)
	assert.Equal(t, hiddenWhy, got[0].entry.Why, "one line shown, two run")
	assert.True(t, got[1].entry.Runnable, "a tab hides nothing")

	tasks, _, err := readTasks(folder(t, map[string]string{tasksFile: `{
		"inputs": [
			{"id": "pick", "type": "pickString", "options": [{"label": "safe", "value": "x\u202e"}]},
			{"id": "said", "type": "promptString", "default": "a\rb"}],
		"tasks": [
			{"label": "arg", "type": "shell", "command": "echo", "args": ["\u2066x"]},
			{"label": "cwd", "type": "shell", "command": "make", "options": {"cwd": "a\nb"}},
			{"label": "env", "type": "shell", "command": "make", "options": {"env": {"A": "x\u0007"}}},
			{"label": "shell", "type": "shell", "command": "make", "options": {"shell": {"executable": "bash", "args": ["-l\u200f"]}}},
			{"label": "option", "type": "shell", "command": "echo ${input:pick}"},
			{"label": "default", "type": "shell", "command": "echo ${input:said}"},
			{"label": "fine", "type": "shell", "command": "echo fine"}]}`}))
	require.NoError(t, err)

	for _, sc := range tasks {
		if sc.name == "fine" {
			assert.True(t, sc.entry.Runnable)

			continue
		}

		assert.Equal(t, hiddenWhy, sc.entry.Why, sc.name)
	}
}

func TestEnvWithAProgramHoldingAnEquals(t *testing.T) {
	t.Parallel()

	got, _, err := readTasks(folder(t, map[string]string{tasksFile: `{"tasks": [
		{"label": "odd", "type": "process", "command": "./bin/a=b", "options": {"env": {"A": "1"}}},
		{"label": "no env", "type": "process", "command": "./bin/a=b"},
		{"label": "shell", "type": "shell", "command": "a=b make", "options": {"env": {"A": "1"}}}]}`}))
	require.NoError(t, err)

	all := byName(got)
	assert.Equal(t, envProgramWhy, all["odd"].entry.Why, "env would read the program as a variable")
	assert.True(t, all["no env"].entry.Runnable)
	assert.True(t, all["shell"].entry.Runnable, "a shell runs the line, after env")

	l := launcher{shell: zshPath, vars: vars{dir: "/work/x=y"}}
	_, err = l.argv([]part{{kind: partProcess, command: "./tool", env: [][2]string{{"A", "1"}}}})
	require.ErrorIs(t, err, ErrCantRun, "a folder holding an = puts one in the program's path")
	assert.Equal(t, envProgramWhy, uierr.Message(err))
}

func TestBuiltinGuards(t *testing.T) {
	t.Parallel()

	scripts := byName(magexScripts(helpBuiltins(t), magefiles{}))

	for _, name := range []string{"update", "update:check", "update:install", "version:update"} {
		assert.Equal(t, selfUpdaterWhy, scripts[name].entry.Why, "%s updates magex itself", name)
	}

	for _, name := range []string{"version:bump", "version:tag", "git:tag", "git:tagupdate", "git:tagremove"} {
		assert.True(t, scripts[name].entry.Release, "%s tags a version", name)
		assert.True(t, scripts[name].entry.Runnable, "%s can run anyway", name)
	}

	for _, name := range []string{"release:default", "release:snapshot", "version:show", "version:check", "test"} {
		assert.False(t, scripts[name].entry.Release, name)
		assert.True(t, scripts[name].entry.Runnable, name)
	}

	for _, name := range []string{"deps:update", "mod:update"} {
		assert.True(t, scripts[name].entry.Deps, "%s upgrades the dependencies in the clone", name)
		assert.False(t, scripts[name].entry.Release, name)
		assert.True(t, scripts[name].entry.Runnable, "%s can run anyway", name)
	}

	for _, name := range []string{"deps:tidy", "deps:outdated", "deps:download", "mod:tidy", "tools:update", "test"} {
		assert.False(t, scripts[name].entry.Deps, name)
		assert.True(t, scripts[name].entry.Runnable, name)
	}

	for _, name := range taggingTargets() {
		assert.False(t, scripts[name].entry.Deps, "%s points to Release alone", name)
	}

	b, err := parseBuiltins([]byte(`{"commands": [
		{"name": "fresh", "aliases": ["upgrade"]}, {"name": "cut", "aliases": ["version:bump"]},
		{"name": "version:tag", "aliases": ["vt"]}, {"name": "refresh", "aliases": ["deps:update"]},
		{"name": "mod:update", "aliases": ["mu", "modup"]}]}`))
	require.NoError(t, err)

	byAlias := byName(magexScripts(b, magefiles{}))
	assert.Equal(t, selfUpdaterWhy, byAlias["fresh"].entry.Why, "by its alias")
	assert.True(t, byAlias["cut"].entry.Release, "by its alias")
	assert.True(t, byAlias["refresh"].entry.Deps, "by its alias")
	assert.Equal(t, []string{"vt"}, b.taggingAliases())
	assert.Equal(t, []string{"mu", "modup"}, b.upgradeAliases())
}

func TestBumps(t *testing.T) {
	t.Parallel()

	for words, want := range map[string]bool{
		"magex version:bump bump=minor":                 true,
		"cd -- /x && /bin/zsh -c 'magex version:bump'":  true,
		"magex Git:TagUpdate version=1.2.3":             true,
		"magex version:bumpy":                           false,
		"magex version:show":                            false,
		"echo the git:tags are fine":                    false,
		"b=$(git symbolic-ref) && magex version:bump":   true,
		"/opt/bin/magex release:default":                false,
		"magex git:tag version=1.0.0 && echo published": true,
	} {
		assert.Equal(t, want, bumps([]string{words}, nil), words)
	}

	aliases := []string{"vt"}
	assert.True(t, bumps([]string{zshPath, "-c", "magex vt"}, aliases), "an alias after magex")
	assert.True(t, bumps([]string{"/bin/sh", "-c", "cd -- /x && '/opt/bin/magex' 'vt'"}, aliases))
	assert.True(t, bumps([]string{"/opt/bin/magex", "vt"}, aliases), "a program and its arguments, as a process runs")
	assert.False(t, bumps([]string{"npm run vt"}, aliases), "a word that is an alias only for magex")
	assert.False(t, bumps([]string{strings.Repeat("magex ", 3) + "vtx"}, aliases))
	assert.False(t, bumps([]string{"magex deps:update"}, aliases), "an upgrade is Deps' alone")
}

func TestUpgrades(t *testing.T) {
	t.Parallel()

	for words, want := range map[string]bool{
		// magex's upgrade targets, named anywhere as a tagging target is.
		"magex deps:update":                     true,
		"magex deps:update all-modules verbose": true,
		"/opt/bin/magex Deps:Update":            true,
		"magex mod:update":                      true,
		"pnpm run deps:update":                  true,
		"magex deps:tidy":                       false,
		"magex deps:outdated":                   false,
		"magex deps:updates":                    false,
		"echo the deps:updated list":            false,

		// go get -u.
		"go get -u ./...":                    true,
		"go get -u=patch ./...":              true,
		"go get -t -u ./... && go mod tidy":  true,
		"GOFLAGS=-mod=mod go get -u all":     true,
		"/usr/local/go/bin/go get -u ./...":  true,
		"cd -- /x && 'go' get '-u' ./...":    true,
		"go get ./...":                       false,
		"go get -u=false ./...":              false,
		"go get example.com/mod@v1.2.3":      false,
		"go get ./... && echo -u":            false,
		"go mod tidy && go test -count=1 -u": false,
		"cargo update":                       false,

		// The package managers' own update commands, after their flags.
		"npm update":                         true,
		"npm up":                             true,
		"npm upgrade --save":                 true,
		"pnpm update":                        true,
		"pnpm up --latest":                   true,
		"pnpm -r up":                         true,
		"pnpm --recursive upgrade --latest":  true,
		"corepack pnpm update":               true,
		"cd -- /x && '/opt/bin/pnpm' up":     true,
		"/bin/bash -l -c 'pnpm up'":          true,
		"yarn upgrade":                       true,
		"yarn up":                            true,
		"yarn upgrade-interactive --latest":  true,
		"bun update":                         true,
		"pnpm --filter web up":               true,
		"npm --prefix app update":            true,
		"yarn --cwd packages/web upgrade":    true,
		"npm --prefix app run update":        false,
		"pnpm --filter web run up":           false,
		"npm install && npm test":            false,
		"pnpm install":                       false,
		"pnpm dev":                           false,
		"npm run update":                     false,
		"pnpm run up":                        false,
		"yarn run upgrade":                   false,
		"bun upgrade":                        false,
		"pnpm up-to-date":                    false,
		"echo pnpm is up to date && magex x": false,

		// npm-check-updates, which writes the newest versions into package.json.
		"ncu -u":                                 true,
		"ncu --upgrade && pnpm install":          true,
		"npx npm-check-updates -u":               true,
		"npx npm-check-updates@latest --upgrade": true,
		"pnpm dlx ncu --target minor -u":         true,
		"ncu -i":                                 true,
		"ncu":                                    false,
		"npx npm-check-updates --target minor":   false,
		"ncu && echo -u":                         false,
	} {
		assert.Equal(t, want, upgrades([]string{words}, nil), words)
	}

	assert.True(t, upgrades([]string{"/opt/homebrew/bin/pnpm", "up"}, nil), "a program and its arguments")
	assert.True(t, upgrades([]string{"/usr/local/go/bin/go", "get", "-u", "./..."}, nil))
	assert.True(t, upgrades([]string{"/usr/bin/env", "CI=1", "/opt/homebrew/bin/pnpm", "update"}, nil))
	assert.False(t, upgrades([]string{"/usr/bin/env", "CI=1", "/opt/homebrew/bin/pnpm", "install"}, nil))

	aliases := []string{"du"}
	assert.True(t, upgrades([]string{zshPath, "-c", "magex du"}, aliases), "an alias after magex")
	assert.True(t, upgrades([]string{"/opt/bin/magex", "du"}, aliases), "a program and its arguments")
	assert.False(t, upgrades([]string{"npm run du"}, aliases), "a word that is an alias only for magex")
	assert.False(t, upgrades([]string{"magex dux"}, aliases))
	assert.False(t, upgrades([]string{"magex version:bump"}, aliases), "a version bump is Release's alone")
}

func TestOptionShown(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Option{Label: "patch", Value: "patch", Shown: "patch"}, optionOf("patch", "patch"))
	assert.Equal(t, Option{Label: "Safe", Value: "x; curl example.com | sh", Shown: "Safe (x; curl example.com | sh)"},
		optionOf("Safe", "x; curl example.com | sh"), "the value that runs is always shown")
}
