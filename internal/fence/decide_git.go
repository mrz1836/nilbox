package fence

import (
	"slices"
	"strings"
)

// The git and gh commands, as decideProgram hands them over: words, the program first.

// The gh groups the fence names.
const (
	ghPR    = "pr"
	ghIssue = "issue"
	ghRun   = "run"
	ghRepo  = "repo"
)

// decideGit decides a git command: no option that changes where git runs or what config
// it reads, none that runs a program the line doesn't name, the one push alone, only the
// subcommands the preset allows, no AI trailer in a commit, and the paths it names.
func (p Policy) decideGit(words []string, line, dir string, fs FS) Verdict {
	i, v := gitGlobals(words)
	if v.Deny || i >= len(words) {
		return v
	}

	sub, args := words[i], words[i+1:]

	if v = gitArgsVerdict(sub, args); v.Deny {
		return v
	}

	if sub == "push" {
		return p.pushVerdict()
	}

	if v = p.gitSubVerdict(sub, args); v.Deny {
		return v
	}

	if v = p.commitVerdict(sub, args, line, dir, fs); v.Deny {
		return v
	}

	return p.gitPathsVerdict(sub, args, dir, fs)
}

// gitGlobals reads git's own options, before the subcommand, and returns where the
// subcommand is, refusing one that changes where git runs or what it reads.
func gitGlobals(words []string) (int, Verdict) {
	i := 1

	for ; i < len(words) && strings.HasPrefix(words[i], "-"); i++ {
		if opt, refused := gitGlobalRefused(words[i]); refused {
			return i, deny("git %s isn't allowed: it changes where git runs or what it reads", opt)
		}
	}

	return i, Verdict{}
}

// gitGlobalRefused reports one of git's own options the fence refuses, as -C dir, -Cdir,
// or --git-dir=dir.
func gitGlobalRefused(word string) (string, bool) {
	opt, _, _ := strings.Cut(word, "=")
	if slices.Contains(gitGlobalDenied(), opt) {
		return opt, true
	}

	if len(opt) > 2 && opt[1] != '-' && (opt[:2] == "-C" || opt[:2] == "-c") {
		return opt[:2], true
	}

	return "", false
}

// gitArgsVerdict refuses a subcommand's option that runs a program the line doesn't name.
func gitArgsVerdict(sub string, args []string) Verdict {
	for _, a := range args {
		opt, _, _ := strings.Cut(a, "=")
		if slices.Contains(gitArgsDenied(), opt) {
			return deny("git %s %s isn't allowed: it runs a program the command doesn't name", sub, opt)
		}
	}

	return Verdict{}
}

// pushVerdict refuses every push but the exact one.
func (p Policy) pushVerdict() Verdict {
	if p.Push == nil {
		return deny("%s pushes nothing", p.Preset.Label())
	}

	return deny("the fence allows one push, exactly as written and on its own: %s", p.Push.Commands()[0])
}

// gitSubVerdict refuses a git subcommand the preset doesn't allow, a listing subcommand
// used to change something, and a branch made in the clone.
func (p Policy) gitSubVerdict(sub string, args []string) Verdict {
	if v := p.gitSubAllowed(sub); v.Deny {
		return v
	}

	if reads, ok := gitListOnly()[sub]; ok && !reads(args) {
		return deny("git %s may only list or read here: it would change the clone's branches, tags, remotes, "+
			"worktrees, or config", sub)
	}

	if (sub == "checkout" || sub == "switch") && slices.ContainsFunc(args, makesBranch) {
		return deny("git %s may not make a branch: the worktree has its own", sub)
	}

	return Verdict{}
}

// gitSubAllowed refuses a git subcommand the preset doesn't run.
func (p Policy) gitSubAllowed(sub string) Verdict {
	switch {
	case slices.Contains(gitReads(), sub), p.Preset.Edits() && slices.Contains(gitEdits(), sub):
		return Verdict{}
	case p.Preset.Edits():
		return deny("git %s isn't allowed in a fenced run", sub)
	default:
		return deny("under %s, git only reads: git %s isn't allowed", p.Preset.Label(), sub)
	}
}

// makesBranch reports a checkout or switch option that makes a branch.
func makesBranch(a string) bool {
	return slices.Contains([]string{"-b", "-B", "-c", "-C", "--orphan"}, a)
}

// commitVerdict refuses a commit whose message, on the line or in the file it names,
// carries an AI co-author trailer: attribution stays off.
func (p Policy) commitVerdict(sub string, args []string, line, dir string, fs FS) Verdict {
	const message = "the commit message carries an AI co-author trailer or sign-off; write the message without it"

	if sub != "commit" {
		return Verdict{}
	}

	if trailerPattern.MatchString(line) {
		return deny(message)
	}

	for _, file := range messageFiles(args) {
		if data, ok := fs.ReadHead(p.absPath(file, dir), 64<<10); ok && trailerPattern.Match(data) {
			return deny(message)
		}
	}

	return Verdict{}
}

// messageFiles returns the files a commit reads its message or template from, but stdin.
func messageFiles(args []string) []string {
	var out []string

	for i := range args {
		if f := messageFile(args, i); f != "" && f != "-" {
			out = append(out, f)
		}
	}

	return out
}

// messageFile returns the file args[i] names as a commit's message or template, or "".
func messageFile(args []string, i int) string {
	a := args[i]

	switch {
	case slices.Contains([]string{"-F", "--file", "-t", "--template"}, a) && i+1 < len(args):
		return args[i+1]
	case strings.HasPrefix(a, "--file=") || strings.HasPrefix(a, "--template="):
		_, f, _ := strings.Cut(a, "=")

		return f
	case strings.HasPrefix(a, "-F") && len(a) > 2:
		return a[2:]
	default:
		return ""
	}
}

// gitPathsVerdict decides the paths a git subcommand names, as a write unless it only
// reads.
func (p Policy) gitPathsVerdict(sub string, args []string, dir string, fs FS) Verdict {
	writes := !slices.Contains(gitReads(), sub)

	for _, a := range args {
		for _, cand := range pathCandidates(a) {
			if v := p.pathVerdict(cand, dir, fs, writes); v.Deny {
				return v
			}
		}
	}

	return Verdict{}
}

// ghCommandOf returns a gh command's group and action.
func ghCommandOf(words []string) ghCommand {
	var cmd ghCommand

	if len(words) > 1 {
		cmd.group = words[1]
	}

	if len(words) > 2 && !strings.HasPrefix(words[2], "-") {
		cmd.action = words[2]
	}

	return cmd
}

// decideGH decides a gh command: never one no preset runs, under a fixed preset only on
// its own and only the preset's, and a write only to the run's own item.
func (p Policy) decideGH(words []string, s script, dir string, fs FS) Verdict {
	cmd := ghCommandOf(words)

	if v := ghNeverVerdict(cmd); v.Deny || p.Preset == Profile {
		return v
	}

	if s.compound {
		return deny("run gh on its own, with no pipe, other command, redirection, or substitution in the line")
	}

	return p.ghPresetVerdict(cmd, words, dir, fs)
}

// ghNeverVerdict refuses a gh command no preset runs.
func ghNeverVerdict(cmd ghCommand) Verdict {
	for _, never := range ghNever() {
		if never.group == cmd.group && (never.action == "" || never.action == cmd.action) {
			return deny("gh %s isn't allowed: a fenced agent never merges, reviews, closes, releases, or calls GitHub's "+
				"API; the routine's own steps do what it allows", cmd.words()[len("gh "):])
		}
	}

	return Verdict{}
}

// ghPresetVerdict lets through gh's reads, GitHub tasks' writes on the item, and an edit
// preset's checkout of the item, and refuses every other gh command.
func (p Policy) ghPresetVerdict(cmd ghCommand, words []string, dir string, fs FS) Verdict {
	switch {
	case slices.Contains(ghReads(), cmd):
		return Verdict{}
	case p.Preset == GitHubTasks && slices.Contains(ghTasks(), cmd):
		return p.ghTaskVerdict(cmd, words[3:], dir, fs)
	case p.Preset.Edits() && cmd == ghCommand{ghPR, "checkout"}:
		_, v := p.ghTarget(cmd, words[3:])

		return v
	default:
		return deny("gh %s isn't allowed under %s", strings.TrimSpace(cmd.group+" "+cmd.action), p.Preset.Label())
	}
}

// ghFlagSpec is what a gh write may be given: its flags that take a value, and those
// that don't.
type ghFlagSpec struct {
	values   []string
	switches []string
}

// ghTaskFlags are the flags each of GitHub tasks' writes may be given.
func ghTaskFlags() map[ghCommand]ghFlagSpec {
	repoFlags := []string{"--repo", "-R"}
	comment := ghFlagSpec{values: append([]string{"--body", "-b", "--body-file", "-F"}, repoFlags...), switches: []string{"--edit-last"}}
	edit := ghFlagSpec{values: append([]string{"--add-label", "--remove-label"}, repoFlags...)}

	return map[ghCommand]ghFlagSpec{
		{ghPR, "comment"}: comment, {ghIssue, "comment"}: comment, {ghPR, "edit"}: edit, {ghIssue, "edit"}: edit,
		{ghRun, "rerun"}:   {values: append([]string{"--job", "-j"}, repoFlags...), switches: []string{"--failed", "--debug", "-d"}},
		{ghPR, "checkout"}: {values: append([]string{"--branch", "-b"}, repoFlags...), switches: []string{"--detach", "--force", "-f"}},
	}
}

// ghTaskVerdict decides one of GitHub tasks' writes.
func (p Policy) ghTaskVerdict(cmd ghCommand, args []string, dir string, fs FS) Verdict {
	flags, v := p.ghTarget(cmd, args)
	if v.Deny {
		return v
	}

	if v = ghTaskFlagsVerdict(cmd, flags); v.Deny {
		return v
	}

	return p.bodyFileVerdict(flags, dir, fs)
}

// ghTaskFlagsVerdict refuses a rerun of every job, and an edit of anything but labels.
func ghTaskFlagsVerdict(cmd ghCommand, flags map[string]string) Verdict {
	switch {
	case cmd.group == ghRun && !hasFlag(flags, "--failed"):
		return deny("gh run rerun runs only the failed jobs again here: add --failed")
	case cmd.action == "edit" && !hasFlag(flags, "--add-label") && !hasFlag(flags, "--remove-label"):
		return deny("gh %s edit changes only labels here: --add-label or --remove-label", cmd.group)
	default:
		return Verdict{}
	}
}

// bodyFileVerdict refuses a comment's body read from a file private to the user.
func (p Policy) bodyFileVerdict(flags map[string]string, dir string, fs FS) Verdict {
	for _, name := range []string{"--body-file", "-F"} {
		file, ok := flags[name]
		if !ok || file == "-" {
			continue
		}

		if why := p.readDeniedPath(p.absPath(file, dir), fs); why != "" {
			return deny("%s %s", file, why)
		}
	}

	return Verdict{}
}

// hasFlag reports a flag given.
func hasFlag(flags map[string]string, name string) bool {
	_, ok := flags[name]

	return ok
}

// ghTarget reads a gh write's arguments into its flags, refusing a flag it doesn't take
// and one aimed at anything but the run's own item and repository.
func (p Policy) ghTarget(cmd ghCommand, args []string) (map[string]string, Verdict) {
	flags, positional, v := readGHArgs(cmd, args)
	if v.Deny {
		return nil, v
	}

	for _, name := range []string{"--repo", "-R"} {
		if r, ok := flags[name]; ok && !strings.EqualFold(r, p.Repo) {
			return nil, deny("gh %s %s may only reach this run's repository, %s", cmd.group, cmd.action, p.Repo)
		}
	}

	return flags, p.ghPositionalVerdict(cmd, positional)
}

// readGHArgs reads a gh write's arguments: its flags, by the names it takes, and its
// positional arguments.
func readGHArgs(cmd ghCommand, args []string) (flags map[string]string, positional []string, v Verdict) {
	spec := ghTaskFlags()[cmd]
	flags = map[string]string{}

	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value, hasValue := strings.Cut(a, "=")

		switch {
		case !strings.HasPrefix(a, "-"):
			positional = append(positional, a)
		case slices.Contains(spec.switches, a):
			flags[a] = ""
		case slices.Contains(spec.values, name) && hasValue:
			flags[name] = value
		case slices.Contains(spec.values, a) && i+1 < len(args):
			flags[a] = args[i+1]
			i++
		default:
			return nil, nil, deny("gh %s %s isn't allowed with %s here", cmd.group, cmd.action, name)
		}
	}

	return flags, positional, Verdict{}
}

// ghPositionalVerdict refuses a write's positional arguments unless they name the run's
// item, or for a rerun, one run.
func (p Policy) ghPositionalVerdict(cmd ghCommand, positional []string) Verdict {
	if cmd.group == ghRun {
		if len(positional) > 1 || len(positional) == 1 && !digits(positional[0]) {
			return deny("gh run rerun takes one run ID here")
		}

		return Verdict{}
	}

	if len(positional) != 1 || !p.isItem(positional[0]) {
		return deny("gh %s %s may only reach this run's own item, #%d; name it", cmd.group, cmd.action, p.Number)
	}

	return Verdict{}
}
