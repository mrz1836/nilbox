package scripts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The programs a run starts besides the script's own: env sets a part's variables, and sh
// runs a chain of parts, or a part in a folder other than the clone, one after another,
// stopping at the first that fails. Both are at these paths on macOS and Linux.
const (
	envProgram = "/usr/bin/env"
	chainShell = "/bin/sh"
)

// partKind is how one part of a script runs.
type partKind int

// The kinds of part.
const (
	// partShell is a command line a shell runs: the user's login shell, or a task's own.
	partShell partKind = iota

	// partProcess is a program and its arguments, run with no shell.
	partProcess

	// partMagex is a magex target, with key=value arguments.
	partMagex
)

// part is one command a script runs, with every ${…} as written.
type part struct {
	kind    partKind
	command string
	args    []string

	// cwd is the folder it runs in, "" for the clone; env the variables it sets, by name;
	// and shell a shell task's own shell, or nil for the user's login shell.
	cwd   string
	env   [][2]string
	shell *shellSpec

	// raw is a command line of the user's own, which runs exactly as written, no ${…} in
	// it filled in, in their login shell whatever its family: the app wrote none of it.
	raw bool
}

// shellSpec is the shell a tasks.json shell task names, and the arguments before its
// command line.
type shellSpec struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
}

// script is one script worked out from its source: what the page shows of it, and what it
// runs.
type script struct {
	// entry is its row on the page, without its trust, which the user's records say.
	entry Entry

	// name is its trust record's name, and hash the SHA-256 of its effective form, or ""
	// for a script that needs no trust.
	name string
	hash string

	// place is where the script itself says it runs, or "" for the setting, and asksFirst
	// reports one the user set to ask before each run: only the user's own scripts say.
	// look is the SHA-256 of what one of the user's scripts runs and how (yoursForm), which
	// a run's Before compares, or "" for any other.
	place     integrationprefs.RunsIn
	asksFirst bool
	look      string

	// parts are what it runs, in order, and inputs the values they ask for, by ID.
	parts  []part
	inputs map[string]Input
}

// needsTrust reports a script that needs the user's trust before it runs.
func (s script) needsTrust() bool {
	return s.hash != ""
}

// hashOf returns the SHA-256 of v's JSON, in hex: v's fields in their declared order, and
// a map's keys sorted, so the same form always hashes the same.
func hashOf(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("scripts: hash an unencodable form: %v", err)) // every form is plain data
	}

	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

// variable matches a ${…} variable, as VS Code writes one.
var variable = regexp.MustCompile(`\$\{([^}]*)\}`)

// editorWord says who can't fill in the variables only an editor can.
const editorWord = brand.Name + " can't fill in"

// variableProblem says why the app can't fill in the variable called name, or "" when it
// can: ${workspaceFolder}, ${workspaceFolderBasename}, ${userHome}, ${pathSeparator},
// ${env:NAME}, and ${input:id}, whose input the caller checks.
func variableProblem(name string) string {
	switch {
	case name == "workspaceFolder", name == "workspaceFolderBasename", name == "userHome", name == "pathSeparator",
		strings.HasPrefix(name, "env:"), strings.HasPrefix(name, "input:"):
		return ""
	case strings.HasPrefix(name, "command:"):
		return "Uses ${" + name + "}, an editor command " + brand.Name + " can't run."
	case strings.HasPrefix(name, "config:"):
		return "Uses ${" + name + "}, an editor setting " + brand.Name + " can't read."
	default:
		return "Uses ${" + name + "}, which " + editorWord + "."
	}
}

// vars fills in a script's variables as it runs.
type vars struct {
	// dir is the clone, home the user's home, env the login shell's environment, and
	// inputs the values the user gave, each checked.
	dir    string
	home   string
	env    []string
	inputs map[string]string
}

// expand fills in every variable in s that vars knows, leaving any other as written.
func (v vars) expand(s string) string {
	return variable.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1]

		switch {
		case name == "workspaceFolder":
			return v.dir
		case name == "workspaceFolderBasename":
			return filepath.Base(v.dir)
		case name == "userHome":
			return v.home
		case name == "pathSeparator":
			return string(filepath.Separator)
		case strings.HasPrefix(name, "env:"):
			return shellenv.Lookup(v.env, strings.TrimPrefix(name, "env:"))
		case strings.HasPrefix(name, "input:"):
			if got, ok := v.inputs[strings.TrimPrefix(name, "input:")]; ok {
				return got
			}
		}

		return m
	})
}

// expandInputs fills in only the ${input:…} variables of s, for a run's display form,
// which never shows what the user's environment holds.
func (v vars) expandInputs(s string) string {
	return variable.ReplaceAllStringFunc(s, func(m string) string {
		if id, ok := strings.CutPrefix(m[2:len(m)-1], "input:"); ok {
			if got, has := v.inputs[id]; has {
				return got
			}
		}

		return m
	})
}

// step is one part as it runs: its argv, and the folder it runs in.
type step struct {
	argv []string
	dir  string
}

// launcher works out what runs: shell is the user's login shell, and magex the copy of
// magex in use.
type launcher struct {
	shell string
	magex string
	vars  vars
}

// argv works out the command a script's parts run as. One part in the clone runs as
// itself; a chain, or a part in another folder, runs through sh, each part changing into
// its folder first and shell-quoted, one after another, stopping at the first that fails.
func (l launcher) argv(parts []part) ([]string, error) {
	steps := make([]step, 0, len(parts))

	for _, p := range parts {
		s, err := l.step(p)
		if err != nil {
			return nil, err
		}

		steps = append(steps, s)
	}

	if len(steps) == 1 && steps[0].dir == l.vars.dir {
		return steps[0].argv, nil
	}

	lines := make([]string, 0, len(steps))
	for _, s := range steps {
		lines = append(lines, "cd -- "+quote(s.dir)+" && "+quoteArgv(s.argv))
	}

	return []string{chainShell, "-c", strings.Join(lines, " && ")}, nil
}

// step works out one part's argv and folder: its command, run through env when it sets
// variables.
func (l launcher) step(p part) (step, error) {
	dir := l.dir(p)

	argv, err := l.command(p, dir)
	if err != nil {
		return step{}, err
	}

	if len(p.env) > 0 {
		if strings.ContainsRune(argv[0], '=') {
			return step{}, uierr.New(envProgramWhy, fmt.Errorf("%w: %s", ErrCantRun, argv[0]))
		}

		set := []string{envProgram}
		for _, kv := range p.env {
			set = append(set, kv[0]+"="+l.vars.expand(kv[1]))
		}

		argv = append(set, argv...)
	}

	return step{argv: argv, dir: dir}, nil
}

// envProgramWhy is why a part that sets variables can't run a program whose path holds
// an =: env, which sets them, would read that program as one more variable.
const envProgramWhy = "Sets variables for a program whose path holds an =, which env would read as one more variable."

// dir returns the folder a part runs in: the clone, or its cwd, from the clone when
// relative.
func (l launcher) dir(p part) string {
	if p.cwd == "" {
		return l.vars.dir
	}

	dir := l.vars.expand(p.cwd)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(l.vars.dir, dir)
	}

	return filepath.Clean(dir)
}

// command returns a part's own argv, run in dir.
func (l launcher) command(p part, dir string) ([]string, error) {
	switch p.kind {
	case partMagex:
		return append([]string{l.magex, p.command}, p.args...), nil
	case partProcess:
		prog, err := l.program(l.vars.expand(p.command), dir)
		if err != nil {
			return nil, err
		}

		return append([]string{prog}, l.expandAll(p.args)...), nil
	case partShell:
	}

	if p.raw {
		return []string{l.ownShell(), "-c", p.command}, nil
	}

	line := commandLine(l.vars.expand(p.command), l.expandAll(p.args))
	if p.shell == nil {
		return []string{l.lineShell(), "-c", line}, nil
	}

	exe, err := l.program(l.vars.expand(p.shell.Executable), dir)
	if err != nil {
		return nil, err
	}

	if !posixShell(exe) && len(p.args) > 0 {
		return nil, uierr.New(shellWhy(exe), fmt.Errorf("%w: %s quotes differently", ErrCantRun, exe))
	}

	return append(append([]string{exe}, l.expandAll(shellArgs(exe, p.shell.Args))...), line), nil
}

// lineShell is the shell a repository's command line runs in: the user's login shell when
// it is of the POSIX family, whose quoting the app writes and t3.json and tasks.json
// assume, else sh.
func (l launcher) lineShell() string {
	if posixShell(l.shell) {
		return l.shell
	}

	return chainShell
}

// ownShell is the shell a line of the user's own runs in: their login shell as it is,
// fish and tcsh included, since the app quotes nothing in it; sh only when the login shell
// isn't an absolute path.
func (l launcher) ownShell() string {
	if filepath.IsAbs(l.shell) {
		return l.shell
	}

	return chainShell
}

// expandAll fills in the variables of every word.
func (l launcher) expandAll(words []string) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, l.vars.expand(w))
	}

	return out
}

// program finds the program name names: an absolute path as it is, a relative path from
// dir, and a bare name on the login shell's PATH.
func (l launcher) program(name, dir string) (string, error) {
	switch {
	case name == "":
		return "", fmt.Errorf("%w: no program to run", ErrCantRun)
	case filepath.IsAbs(name):
		return name, nil
	case strings.ContainsRune(name, filepath.Separator):
		return filepath.Join(dir, name), nil
	}

	found, err := execpath.Search{Path: shellenv.Lookup(l.vars.env, "PATH"), Home: l.vars.home}.Find(name)
	if err != nil {
		return "", fmt.Errorf("find %s: %w", name, err)
	}

	return found, nil
}

// shellArgs returns the arguments a task's shell takes before the command line, as VS Code
// gives them: for a POSIX shell, its own with -c after them unless they hold it; for any
// other, its own, or -c when it names none.
func shellArgs(shell string, args []string) []string {
	switch {
	case posixShell(shell) && !slices.Contains(args, "-c"):
		return append(slices.Clone(args), "-c")
	case len(args) == 0:
		return []string{"-c"}
	default:
		return args
	}
}

// commandLine joins a shell task's command and its arguments as VS Code does: the command
// as written, and each argument quoted when the shell would read it as more than itself.
func commandLine(command string, args []string) string {
	words := append([]string{command}, quoteAll(args)...)

	return strings.TrimSpace(strings.Join(words, " "))
}

// display is how the page shows a part, with every ${…} as written.
func (p part) display() string {
	var words []string

	switch p.kind {
	case partMagex:
		words = append([]string{"magex", p.command}, quoteAll(p.args)...)
	case partProcess:
		words = append([]string{show(p.command)}, showAll(p.args)...)
	case partShell:
		line := strings.TrimSpace(strings.Join(append([]string{p.command}, showAll(p.args)...), " "))
		words = []string{line}

		if p.shell != nil {
			words = append(append([]string{show(p.shell.Executable)}, showAll(shellArgs(p.shell.Executable, p.shell.Args))...), show(line))
		}
	}

	prefix := ""
	if p.cwd != "" {
		prefix = "cd " + show(p.cwd) + " && "
	}

	for _, kv := range p.env {
		prefix += kv[0] + "=" + show(kv[1]) + " "
	}

	return prefix + strings.Join(words, " ")
}

// displayParts is how the page shows a script's parts: one after another.
func displayParts(parts []part) string {
	shown := make([]string, 0, len(parts))
	for _, p := range parts {
		shown = append(shown, p.display())
	}

	return strings.Join(shown, " && ")
}

// quote quotes s for a POSIX shell, unless it needs none.
func quote(s string) string {
	return quoteUnless(s, "")
}

// show quotes s as the page shows it: like quote, but a ${…} stays readable.
func show(s string) string {
	return quoteUnless(s, "${}")
}

// quoteUnless quotes s for a POSIX shell unless every character is a letter, a digit, or
// one that reads as itself, or one of safe.
func quoteUnless(s, safe string) string {
	plain := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_./=:,+@%"+safe, r)
	}

	if s != "" && !strings.ContainsFunc(s, func(r rune) bool { return !plain(r) }) {
		return s
	}

	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quoteAll quotes each word for a shell.
func quoteAll(words []string) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, quote(w))
	}

	return out
}

// showAll quotes each word as the page shows it.
func showAll(words []string) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, show(w))
	}

	return out
}

// quoteArgv writes argv out as one shell command line.
func quoteArgv(argv []string) string {
	return strings.Join(quoteAll(argv), " ")
}
