package fence

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// The commands and paths an agent's own rules name, worked out from a run's policy, so
// every agent's rules and the hook hold the same lists. An agent's rules are the second
// fence, not the first: Decide holds every hard line whatever they say.

// AllowedCommands returns the commands a fixed preset's agent may run, for an agent mode
// that denies whatever no rule allows: the repository's tools and everyday commands for
// the edit presets, git's reads and the worktree's own git work, gh's reads, the exact
// fetch and push, and the routine's own; gh's reads and GitHub tasks' writes for GitHub
// tasks. Read only runs no command, and the profile's own permissions are the profile's.
func AllowedCommands(p Policy) []Command {
	var out []Command

	switch p.Preset {
	case GitHubTasks:
		out = append(prefixes(ghWords(append(ghReads(), ghTasks()...))...), prefixes(gitWords(gitReads())...)...)
	case EditTest, EditTestPush:
		out = editCommands(p)
	case ReadOnly, Profile:
		return []Command{}
	}

	return append(out, prefixes(p.Allow...)...)
}

// editCommands are what an edit preset's agent may run: the repository's tools, everyday
// commands, git's reads and the worktree's own git work, gh's reads, the item's checkout,
// and the exact fetch and push.
func editCommands(p Policy) []Command {
	out := slices.Concat(prefixes(p.Tools...), prefixes(utilities()...),
		prefixes(gitWords(append(gitReads(), gitEdits()...))...), prefixes(ghWords(ghReads())...))

	var exact []string

	if p.Number > 0 {
		exact = append(exact, "gh pr checkout "+strconv.Itoa(p.Number))
	}

	if f := exactFetch(p); f != "" {
		exact = append(exact, f)
	}

	if p.Push != nil {
		exact = append(exact, p.Push.Commands()...)
	}

	for _, w := range exact {
		out = append(out, Command{Words: w})
	}

	return out
}

// prefixes returns each of words as a command any arguments may follow.
func prefixes(words ...string) []Command {
	out := make([]Command, 0, len(words))
	for _, w := range words {
		out = append(out, Command{Words: w, Prefix: true})
	}

	return out
}

// gitWords returns git's subcommands as commands' words.
func gitWords(subs []string) []string {
	out := make([]string, 0, len(subs))
	for _, sub := range subs {
		out = append(out, "git "+sub)
	}

	return out
}

// ghWords returns gh's commands as their words.
func ghWords(cmds []ghCommand) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c.words())
	}

	return out
}

// DeniedCommands returns the commands no run under p runs, by their first words, for the
// agent's own deny rules, which beat its allows: the network tools, a command run as
// someone else, a shell given a command line, gh's merges, reviews, closes, releases, and
// API, git's options that change where it runs, every push for a preset with none, and
// the routine's own. A push preset denies no push: a deny would beat its one allow, so
// the hook alone refuses the other pushes.
func DeniedCommands(p Policy) []Command {
	var out []Command

	add := func(words ...string) {
		for _, w := range words {
			out = append(out, Command{Words: w, Prefix: true})
		}
	}

	add(networkTools()...)
	add(otherUsers()...)
	add("eval")

	for _, sh := range shells() {
		add(sh + " -c")
	}

	for _, c := range ghNever() {
		add(c.words())
	}

	add("git -C", "git -c", "git --git-dir", "git --work-tree", "git config --global", "git remote add",
		"git remote set-url", "git remote remove", "git remote rename", "git update-ref", "git submodule")

	if p.Push == nil {
		add("git push")
	}

	add(p.Deny...)

	return out
}

// DeniedPaths returns, as absolute patterns in internal/sensitive's syntax rooted at the
// worktree, the paths no file tool of a run that edits writes: the denied categories'
// files at any depth they name, and everything in the git folders.
func DeniedPaths(p Policy) []string {
	var out []string

	for _, wt := range p.Worktrees() {
		for _, raw := range DeniedPatterns() {
			body := strings.Trim(raw, "/")
			anchored := strings.HasPrefix(raw, "/") || strings.Contains(body, "/")

			root := wt + "/"
			if !anchored {
				root += "**/"
			}

			out = appendNew(out, root+body)
			out = appendNew(out, root+body+"/**")
		}

		out = appendNew(out, wt+"/**/.git/**")
	}

	for _, dir := range p.GitDirs {
		out = appendNew(out, dir+"/**")
	}

	return out
}

// File is a file a fenced run keeps in its own folder: its name there, and its bytes.
type File struct {
	Name string
	Data []byte
}

// WriteRun writes a fenced run's files into its folder, which it makes if it is missing:
// the policy, the agent's own files, and for a sandboxed run the folder its sandbox check
// writes its proof into. The folder is the user's alone (0700) and each file too (0600),
// and no file is written through a link.
func WriteRun(dir string, f Fence, files ...File) error {
	if !filepath.IsAbs(dir) {
		return ErrSpec
	}

	if err := privateDir(dir); err != nil {
		return err
	}

	policy, err := f.Policy.Encode()
	if err != nil {
		return err
	}

	for _, file := range append([]File{{Name: PolicyFile, Data: policy}}, files...) {
		if file.Name == "" || file.Name != filepath.Base(file.Name) || strings.HasPrefix(file.Name, ".") {
			return ErrSpec
		}

		if err = writePrivate(filepath.Join(dir, file.Name), file.Data); err != nil {
			return err
		}
	}

	if f.Policy.Canary != nil {
		return privateDir(filepath.Join(dir, ProofDir))
	}

	return nil
}

// privateDir makes a folder only the user can open, refusing a link in its place.
func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return ErrSpec
	}

	return os.Chmod(dir, 0o700) //nolint:gosec // a folder, which needs its search bit to be opened; the user alone has it
}

// writePrivate writes a file only the user can read, never through a link.
func writePrivate(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600) //nolint:gosec // a file of the run's own folder
	if err != nil {
		return err
	}

	if _, err = f.Write(data); err != nil {
		_ = f.Close()

		return err
	}

	if err = f.Chmod(0o600); err != nil {
		_ = f.Close()

		return err
	}

	return f.Close()
}

// SandboxState is what a sandboxed run's check says of its sandbox.
type SandboxState string

// The states.
const (
	// SandboxHeld is a check that ran and was refused its second file: the sandbox held.
	SandboxHeld SandboxState = "held"

	// SandboxLeaked is a check whose second file was written: the sandbox didn't hold.
	SandboxLeaked SandboxState = "leaked"

	// SandboxUnchecked is a run whose check never ran, so none of its commands did.
	SandboxUnchecked SandboxState = "unchecked"

	// SandboxNone is a run with no sandbox to check.
	SandboxNone SandboxState = "none"
)

// Sandboxed reads what a run's sandbox check left in its folder, as its end is judged.
func Sandboxed(p Policy, fs FS) SandboxState {
	if fs == nil {
		fs = OSFS{}
	}

	switch {
	case p.Canary == nil:
		return SandboxNone
	case fs.Exists(p.Canary.Probe):
		return SandboxLeaked
	case fs.Exists(p.Canary.Proof):
		return SandboxHeld
	default:
		return SandboxUnchecked
	}
}

// SandboxWords says what a sandboxed run's sandbox lets its commands do, as AgentNote tells
// the agent, for a sandbox that keeps to Sandbox's folders and hosts (Claude Code's).
const SandboxWords = "Commands run in a sandbox that writes only in this worktree and the build caches, and " +
	"reaches only GitHub and the package registries."

// AgentNote is what the agent is told of its fence, for the unattended note: its preset, and
// for a sandboxed run, what its sandbox lets commands do (SandboxWords) and the check to
// run before any other command.
func (f Fence) AgentNote() string {
	return f.NoteWith(SandboxWords)
}

// NoteWith is AgentNote with sandbox saying what the agent's own sandbox lets commands do.
func (f Fence) NoteWith(sandbox string) string {
	var b strings.Builder

	b.WriteString("Your tools are fenced: " + f.Policy.Preset.Label() + ".")

	if f.Policy.Push != nil {
		b.WriteString(" The one push allowed is `" + f.Policy.Push.Commands()[0] + "`, exactly as written.")
	} else if f.Policy.Preset.Edits() {
		b.WriteString(" Push nothing.")
	}

	if f.Policy.Canary != nil {
		b.WriteString(" " + sandbox + " Before any other command, run `" + f.Policy.Canary.Command() +
			"` on its own: it checks the sandbox, and its second file is meant to fail with \"Operation not permitted\".")
	}

	return b.String()
}
