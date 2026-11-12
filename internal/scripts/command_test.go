package scripts

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestLauncherArgv(t *testing.T) {
	t.Parallel()

	bin := t.TempDir()
	testutil.WriteExecutable(t, filepath.Join(bin, "zsh"), "")

	dir, home := "/work/example-repo", "/home/user"
	l := launcher{shell: zshPath, magex: "/opt/bin/magex", vars: vars{
		dir: dir, home: home, env: []string{"PATH=" + bin, "TOKEN=abc"},
		inputs: map[string]string{"bump": "minor"},
	}}

	for name, tt := range map[string]struct {
		parts []part
		want  []string
	}{
		"a shell command runs in the login shell": {
			parts: []part{{kind: partShell, command: "magex format:fix && magex lint"}},
			want:  []string{zshPath, "-c", "magex format:fix && magex lint"},
		},
		"a shell task's arguments are quoted onto its line": {
			parts: []part{{kind: partShell, command: "echo", args: []string{"two words", "${workspaceFolderBasename}"}}},
			want:  []string{zshPath, "-c", "echo 'two words' example-repo"},
		},
		"options.shell, absolute, with its arguments": {
			parts: []part{{kind: partShell, command: magexTest, shell: &shellSpec{Executable: "/bin/bash", Args: []string{"-l", "-c"}}}},
			want:  []string{"/bin/bash", "-l", "-c", magexTest},
		},
		"options.shell found on the login shell's PATH, -c by default": {
			parts: []part{{kind: partShell, command: magexTest, shell: &shellSpec{Executable: "zsh"}}},
			want:  []string{filepath.Join(bin, "zsh"), "-c", magexTest},
		},
		"a process with its variables filled in": {
			parts: []part{{kind: partProcess, command: "/usr/bin/true", args: []string{"${workspaceFolder}/out", "${userHome}"}}},
			want:  []string{"/usr/bin/true", "/work/example-repo/out", "/home/user"},
		},
		"a relative program is the clone's": {
			parts: []part{{kind: partProcess, command: "./bin/tool"}},
			want:  []string{"/work/example-repo/bin/tool"},
		},
		"variables through env": {
			parts: []part{{kind: partShell, command: "make", env: [][2]string{{"A", "${env:TOKEN}"}, {"B", "${input:bump}"}}}},
			want:  []string{envProgram, "A=abc", "B=minor", zshPath, "-c", "make"},
		},
		"another folder runs through sh": {
			parts: []part{{kind: partShell, command: "make", cwd: "sub"}},
			want:  []string{chainShell, "-c", "cd -- /work/example-repo/sub && /bin/zsh -c make"},
		},
		"a chain stops at the first failure": {
			parts: []part{
				{kind: partShell, command: "magex lint"},
				{kind: partProcess, command: "/usr/bin/true", cwd: "${workspaceFolder}/sub dir"},
			},
			want: []string{
				chainShell, "-c",
				"cd -- /work/example-repo && /bin/zsh -c 'magex lint' && cd -- '/work/example-repo/sub dir' && /usr/bin/true",
			},
		},
		"a magex target with its arguments": {
			parts: []part{{kind: partMagex, command: "test:race", args: []string{"verbose", "pkg=./..."}}},
			want:  []string{"/opt/bin/magex", "test:race", "verbose", "pkg=./..."},
		},
		"an input in a shell line": {
			parts: []part{{kind: partShell, command: "magex version:bump bump=${input:bump} ${file}"}},
			want:  []string{zshPath, "-c", "magex version:bump bump=minor ${file}"},
		},
	} {
		got, err := l.argv(tt.parts)
		require.NoError(t, err, name)
		assert.Equal(t, tt.want, got, name)
	}

	_, err := l.argv([]part{{kind: partShell, command: "x", shell: &shellSpec{Executable: "no-such-shell"}}})
	require.Error(t, err, "a shell that isn't found")

	_, err = l.argv([]part{{kind: partProcess, command: ""}})
	require.ErrorIs(t, err, ErrCantRun)
}

func TestRunDisplay(t *testing.T) {
	t.Parallel()

	v := vars{dir: "/work/x", env: []string{"TOKEN=secret"}, inputs: map[string]string{"bump": "minor"}}

	sc := script{parts: []part{
		{kind: partShell, command: "deploy --token ${env:TOKEN}", env: [][2]string{{"MODE", "${input:bump}"}}},
		{kind: partShell, command: "echo ${input:bump}"},
	}}
	assert.Equal(t, "MODE=minor deploy --token ${env:TOKEN} && echo minor", runDisplay(sc, v, nil),
		"inputs filled in, never the environment's values")

	magex := script{parts: []part{{kind: partMagex, command: "lint"}}}
	assert.Equal(t, "magex lint fix=true", runDisplay(magex, v, []string{"fix=true"}))
}

func TestShow(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"${workspaceFolder}/out": "${workspaceFolder}/out",
		"a/b.c-d_e=f:g,h+i@j%k":  "a/b.c-d_e=f:g,h+i@j%k",
		"two words":              "'two words'",
		"":                       "''",
		"it's":                   `'it'\''s'`,
		"a;b":                    "'a;b'",
		"${input:two words}":     "'${input:two words}'",
		"=${env:X}":              "'=${env:X}'",
		"café":                   "'café'",
	} {
		assert.Equal(t, want, show(in), in)
	}
}

func TestQuoteArgv(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/usr/bin/env A=b /opt/bin/tool 'two words' ''",
		quoteArgv([]string{envProgram, "A=b", "/opt/bin/tool", "two words", ""}))
	assert.Equal(t, "'/opt/my tools/x' -v", quoteArgv([]string{"/opt/my tools/x", "-v"}))
	assert.Equal(t, "'A=b' x", quoteArgv([]string{"A=b", "x"}), "a program that isn't a path never reads as a variable")
	assert.Equal(t, "'zsh' -c 'magex lint'", quoteArgv([]string{"zsh", "-c", "magex lint"}))
	assert.Empty(t, quoteArgv(nil))
}
