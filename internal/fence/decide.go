package fence

import (
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Call is one tool call an agent is about to make, in the fence's own terms.
type Call struct {
	// Tool is the agent's name for the tool, such as Bash, Edit, or Read.
	Tool string

	// Command is a command tool's command line.
	Command string

	// Path is the file or folder a file tool reads or writes; Pattern a search's glob.
	Path    string
	Pattern string

	// Dir is the folder the call runs in.
	Dir string
}

// Verdict is the fence's answer to a call: denied, with why, or let through to the agent's
// own rules, which still decide it.
type Verdict struct {
	Deny   bool
	Reason string
}

// deny refuses a call, saying why.
func deny(format string, args ...any) Verdict {
	return Verdict{Deny: true, Reason: fmt.Sprintf(format, args...)}
}

// The tools the policy knows, by the names agents give them.
const (
	toolBash = "Bash"
)

// commandTools are the tools that run a command line.
func commandTools() []string { return []string{toolBash, "Monitor", "PowerShell", "BashOutput"} }

// writeTools are the tools that write a file.
func writeTools() []string { return []string{"Edit", "Write", "MultiEdit", "NotebookEdit"} }

// readTools are the tools that read files.
func readTools() []string { return []string{"Read", "Grep", "Glob", "LS", "NotebookRead"} }

// networkToolNames are the tools that reach the network, and the ones that start work
// elsewhere or later, beyond the run: none is ever let through.
func networkToolNames() []string {
	return []string{
		"WebFetch", "WebSearch", "RemoteTrigger", "CronCreate", "CronDelete", "ScheduleWakeup", "EnterWorktree",
		"ExitWorktree",
	}
}

// Decide is the fence's policy: whether a tool call is denied, and why. It is pure but
// for what fs says of the files on disk (nil reads none). A call it lets through still
// goes through the agent's own rules: the fence only ever narrows them.
func Decide(p Policy, c Call, fs FS) Verdict {
	if fs == nil {
		fs = noFS{}
	}

	dir := cmp.Or(c.Dir, p.Worktree)

	if v := p.canaryGate(c, fs); v.Deny {
		return v
	}

	if p.isCanary(c) {
		// The check writes outside the worktree on purpose: the sandbox, not the fence,
		// must refuse its second file.
		return Verdict{}
	}

	switch {
	case slices.Contains(commandTools(), c.Tool):
		if !p.Preset.RunsCommands() {
			return deny("%s runs no commands", p.Preset.Label())
		}

		return p.decideCommand(c.Command, dir, fs)
	case slices.Contains(writeTools(), c.Tool):
		return p.decideWrite(c.Path, dir, fs)
	case slices.Contains(readTools(), c.Tool):
		return p.decideRead(c, dir, fs)
	case slices.Contains(networkToolNames(), c.Tool):
		return deny("%s isn't allowed: a fenced agent uses no network tools, and starts nothing beyond its run", c.Tool)
	case strings.HasPrefix(c.Tool, "mcp__"):
		return deny("MCP tools aren't allowed in a fenced run")
	default:
		return Verdict{}
	}
}

// canaryGate holds a sandboxed run's commands until the sandbox check has run, and
// refuses every call once the check shows the sandbox didn't hold.
func (p Policy) canaryGate(c Call, fs FS) Verdict {
	if p.Canary == nil {
		return Verdict{}
	}

	if fs.Exists(p.Canary.Probe) {
		return deny("The sandbox didn't hold: a command wrote %s, outside the worktree. Stop here, change nothing more, "+
			"and say in your summary that the sandbox didn't apply.", p.Canary.Probe)
	}

	if !slices.Contains(commandTools(), c.Tool) || p.isCanary(c) {
		return Verdict{}
	}

	if !fs.Exists(p.Canary.Proof) {
		return deny("Before any other command, run exactly this, on its own: %s\nIt checks that commands run in the "+
			"sandbox: its second file is meant to fail with \"Operation not permitted\". Then go on.", p.Canary.Command())
	}

	return Verdict{}
}

// isCanary reports the sandbox check's own command.
func (p Policy) isCanary(c Call) bool {
	return p.Canary != nil && c.Tool == toolBash && strings.TrimSpace(c.Command) == p.Canary.Command()
}

// decideWrite decides a file tool's write.
func (p Policy) decideWrite(path, dir string, fs FS) Verdict {
	if !p.Preset.Edits() {
		return deny("%s edits no files", p.Preset.Label())
	}

	if path == "" {
		return deny("the fence can't tell which file this writes")
	}

	abs := p.absPath(path, dir)

	if why := p.readDeniedPath(abs, fs); why != "" {
		return deny("%s %s", path, why)
	}

	if why := p.writeDenied(abs, fs, true); why != "" {
		return deny("%s %s", path, why)
	}

	return Verdict{}
}

// decideRead decides a file tool's read.
func (p Policy) decideRead(c Call, dir string, fs FS) Verdict {
	for _, path := range []string{c.Path, globRoot(c.Pattern)} {
		if path == "" {
			continue
		}

		if why := p.readDeniedPath(p.absPath(path, dir), fs); why != "" {
			return deny("%s %s", path, why)
		}
	}

	if c.Path == "" && c.Pattern == "" {
		if why := p.readDeniedPath(dir, fs); why != "" {
			return deny("%s %s", dir, why)
		}
	}

	return Verdict{}
}

// globRoot returns the folder a glob pattern starts in, the part before its first glob
// character, or "" for a pattern relative to the search's own folder.
func globRoot(pattern string) string {
	i := strings.IndexAny(pattern, "*?[{")
	if i >= 0 {
		pattern = pattern[:i]
	}

	if !strings.HasPrefix(pattern, "/") && !strings.HasPrefix(pattern, "~") {
		return ""
	}

	return pattern
}

// trailerPattern matches an AI co-author trailer or sign-off in a commit message.
var trailerPattern = regexp.MustCompile(`(?i)co-authored-by:[^\n]*(claude|anthropic|openai|codex|copilot)|` +
	`noreply@anthropic\.com|generated (with|by) \[?(claude|codex)`)

// decideCommand decides a command line, every command it runs.
func (p Policy) decideCommand(line, dir string, fs FS) Verdict {
	trimmed := strings.TrimSpace(line)

	if p.Push != nil && slices.Contains(p.Push.Commands(), trimmed) {
		return Verdict{}
	}

	if f := exactFetch(p); f != "" && trimmed == f {
		return Verdict{}
	}

	s, err := readShell(line)
	if err != nil {
		return deny("the fence can't read this command line, so it doesn't run; write it more plainly")
	}

	for _, c := range s.commands {
		if v := p.decideSimple(c, s, line, dir, fs); v.Deny {
			return v
		}
	}

	return Verdict{}
}

// decideSimple decides one simple command of a line.
func (p Policy) decideSimple(c simple, s script, line, dir string, fs FS) Verdict {
	for _, a := range c.assigns {
		if v := assignVerdict(a.text); v.Deny {
			return v
		}
	}

	for _, r := range c.redirects {
		if v := p.pathVerdict(r.target.text, dir, fs, r.writes()); v.Deny {
			return v
		}
	}

	if len(c.words) == 0 {
		return Verdict{}
	}

	prog := c.words[0]
	if prog.dynamic {
		return deny("%q names its program through a variable, a substitution, or a glob, so the fence can't tell what "+
			"runs; name the program itself", prog.text)
	}

	if v := p.denyListed(c.words); v.Deny {
		return v
	}

	return p.decideProgram(c, s, line, dir, fs)
}

// decideProgram decides a simple command by its program.
func (p Policy) decideProgram(c simple, s script, line, dir string, fs FS) Verdict {
	words := texts(c.words)
	name := filepath.Base(words[0])

	if v := hardVerdict(name, words); v.Deny {
		return v
	}

	if v := exportVerdict(name, words[1:]); v.Deny {
		return v
	}

	switch {
	case name == "git":
		return p.decideGit(words, line, dir, fs)
	case name == "gh":
		return p.decideGH(words, s, dir, fs)
	case p.Preset == GitHubTasks:
		return deny("GitHub tasks runs only gh and git's reads, not %s", name)
	}

	if v := p.decideInner(words, line, dir, fs); v.Deny {
		return v
	}

	return p.argsVerdict(c, name, dir, fs)
}

// hardRule is a program no preset runs, or runs with an option the fence can't see past.
type hardRule struct {
	applies func(name string, args []string) bool
	reason  func(name string) string
}

// hardRules are the programs no preset runs: the network tools, a command run as someone
// else, eval, a shell given a command line of its own, and the wrappers that split one.
func hardRules() []hardRule {
	has := func(flags ...string) func(string, []string) bool {
		return func(_ string, args []string) bool {
			return slices.ContainsFunc(args, func(a string) bool {
				return slices.ContainsFunc(flags, func(f string) bool { return a == f || strings.HasPrefix(a, f+"=") })
			})
		}
	}

	return []hardRule{
		{
			applies: func(n string, _ []string) bool { return slices.Contains(networkTools(), n) },
			reason: func(n string) string {
				return n + " isn't allowed: a fenced agent reaches the network only through git, gh, and the repository's own tools"
			},
		},
		{
			applies: func(n string, _ []string) bool { return slices.Contains(otherUsers(), n) },
			reason:  func(n string) string { return n + " isn't allowed: a fenced agent runs everything as itself" },
		},
		{
			applies: func(n string, _ []string) bool { return n == "eval" },
			reason:  func(string) string { return "eval isn't allowed: the fence can't see what it runs" },
		},
		{
			applies: func(n string, args []string) bool {
				return slices.Contains(shells(), n) && slices.ContainsFunc(args, shellCommandFlag)
			},
			reason: func(n string) string {
				return n + " -c isn't allowed: the fence can't see the command line a shell is given; run the commands themselves"
			},
		},
		{
			applies: func(n string, args []string) bool {
				return n == "env" && (has("--split-string")(n, args) || slices.ContainsFunc(args, isSplitFlag))
			},
			reason: func(string) string { return "env -S isn't allowed: the fence can't see the command line it splits" },
		},
		{
			applies: func(n string, args []string) bool { return n == "flock" && has("-c", "--command")(n, args) },
			reason:  func(string) string { return "flock -c isn't allowed: the fence can't see the command line it runs" },
		},
	}
}

// isSplitFlag reports env's -S, alone or holding its string.
func isSplitFlag(a string) bool {
	return strings.HasPrefix(a, "-S")
}

// hardVerdict refuses a program no preset runs (hardRules).
func hardVerdict(name string, words []string) Verdict {
	for _, rule := range hardRules() {
		if rule.applies(name, words[1:]) {
			return deny("%s", rule.reason(name))
		}
	}

	return Verdict{}
}

// exportVerdict refuses a program that sets a variable git or gh reads (assignVerdict).
func exportVerdict(name string, args []string) Verdict {
	if !slices.Contains([]string{"export", "env", "declare", "typeset", "readonly", "local"}, name) {
		return Verdict{}
	}

	for _, a := range args {
		if v := assignVerdict(a); v.Deny {
			return v
		}
	}

	return Verdict{}
}

// shellCommandFlag reports a shell option that takes a command line: -c, alone or among
// other short options (-lc, -ec).
func shellCommandFlag(w string) bool {
	return w == "-c" || w == "--command" || len(w) > 1 && w[0] == '-' && w[1] != '-' && strings.ContainsRune(w[1:], 'c')
}

// assignVerdict refuses a variable that changes what git or gh does: where it finds its
// config, its repository, its credentials, or which program it runs.
func assignVerdict(assign string) Verdict {
	name, _, ok := strings.Cut(assign, "=")
	if !ok || !validName(name) {
		return Verdict{}
	}

	upper := strings.ToUpper(name)
	if strings.HasPrefix(upper, "GIT_") || strings.HasPrefix(upper, "GH_") || upper == "GITHUB_TOKEN" {
		return deny("setting %s isn't allowed: it changes what git or gh does", name)
	}

	return Verdict{}
}

// decideInner decides the commands a wrapper runs (xargs, env, timeout, …) or find runs
// with -exec: each git, gh, or program no preset runs that appears among its words.
func (p Policy) decideInner(words []string, line, dir string, fs FS) Verdict {
	if name := filepath.Base(words[0]); !slices.Contains(wrappers(), name) && name != "find" {
		return Verdict{}
	}

	for i := 1; i < len(words); i++ {
		if v := p.decideAt(words[i:], line, dir, fs); v.Deny {
			return v
		}
	}

	return Verdict{}
}

// decideAt decides the command a wrapper's words may run from their first.
func (p Policy) decideAt(words []string, line, dir string, fs FS) Verdict {
	name := filepath.Base(words[0])

	if v := hardVerdict(name, words); v.Deny {
		return v
	}

	switch name {
	case "git":
		return p.decideGit(words, line, dir, fs)
	case "gh":
		return p.decideGH(words, script{compound: true}, dir, fs)
	default:
		return Verdict{}
	}
}

// denyListed refuses a command the routine denies, by its first words.
func (p Policy) denyListed(words []word) Verdict {
	line := strings.Join(texts(words), " ")

	for _, d := range p.Deny {
		if line == d || strings.HasPrefix(line, d+" ") {
			return deny("the routine doesn't allow %s", d)
		}
	}

	return Verdict{}
}

// argsVerdict decides the files a command names: none private to the user, and, when the
// command writes, none git's own or the repository's agent, workflow, or tooling files.
func (p Policy) argsVerdict(c simple, name, dir string, fs FS) Verdict {
	words := texts(c.words)
	writes := !slices.Contains(readers(), name)

	if name == "find" {
		writes = slices.ContainsFunc(words, func(w string) bool {
			return slices.Contains([]string{"-delete", "-exec", "-execdir", "-ok", "-okdir", "-fprint", "-fprint0", "-fprintf", "-fls"}, w)
		})
	}

	for _, w := range c.words[1:] {
		for _, cand := range pathCandidates(w.text) {
			if v := p.pathVerdict(cand, dir, fs, writes); v.Deny {
				return v
			}
		}
	}

	return Verdict{}
}

// pathCandidates returns the paths a word may name: the word, and a --flag=value's value.
func pathCandidates(w string) []string {
	if w == "" || w == "-" || w == "--" {
		return nil
	}

	out := []string{}
	if !strings.HasPrefix(w, "-") {
		out = append(out, w)
	}

	if _, v, ok := strings.Cut(w, "="); ok && strings.HasPrefix(w, "-") && v != "" {
		out = append(out, v)
	}

	return out
}

// pathVerdict decides one path a command names.
func (p Policy) pathVerdict(path, dir string, fs FS, writes bool) Verdict {
	if path == "" || strings.Contains(path, "://") {
		return Verdict{}
	}

	abs := p.absPath(path, dir)

	if why := p.readDeniedPath(abs, fs); why != "" {
		return deny("%s %s", path, why)
	}

	if !writes {
		return Verdict{}
	}

	if why := p.writeDenied(abs, fs, false); why != "" {
		return deny("%s %s", path, why)
	}

	return Verdict{}
}

// texts returns the words' texts.
func texts(words []word) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, w.text)
	}

	return out
}

// isItem reports an argument naming the run's item: its number, #number, or its URL.
func (p Policy) isItem(arg string) bool {
	n := strconv.Itoa(p.Number)

	if p.Number <= 0 {
		return false
	}

	if arg == n || arg == "#"+n {
		return true
	}

	base := "https://github.com/" + strings.ToLower(p.Repo) + "/"
	lower := strings.ToLower(strings.TrimSuffix(arg, "/"))

	return lower == base+"pull/"+n || lower == base+"issues/"+n
}

// digits reports a string of ASCII digits.
func digits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}
