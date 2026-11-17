package fence

import (
	"os"
	"path/filepath"
	"slices"
)

// The commands the fence knows by name. Each agent's rules allow and deny them its own
// way, and Decide holds every one of them whatever the agent's rules say.

// networkTools are the programs that reach the network on their own, which no preset
// runs: a fence's network is the agent's sandbox's, and the one push.
func networkTools() []string {
	return []string{
		"curl", "wget", "nc", "ncat", "netcat", "socat", "telnet", "ftp", "sftp", "scp", "rsync", "ssh", "http",
		"https", "xh", "aria2c", "lynx", "links", "w3m",
	}
}

// shells are the programs that run a command line of their own with -c, which the fence
// can't see into.
func shells() []string {
	return []string{"sh", "bash", "zsh", "dash", "ksh", "mksh", "fish", "csh", "tcsh", "pwsh", "powershell"}
}

// otherUsers are the programs that run a command as someone else, which no preset runs.
func otherUsers() []string {
	return []string{"sudo", "doas", "su", "pkexec", "runuser", "osascript"}
}

// wrappers are the programs that run the command after their own options: the fence
// decides that command.
func wrappers() []string {
	return []string{
		"env", "nohup", "time", "command", "builtin", "exec", "nice", "timeout", "gtimeout", "xargs", "stdbuf",
		"unbuffer", "caffeinate", "watch", "chronic", "ionice", "taskset", "flock", "arch",
	}
}

// readers are the programs that only read the files they name, so naming an agent or
// tooling file is no write: cat .github/workflows/ci.yml is fine, sed -i on it is not.
func readers() []string {
	return []string{
		"cat", "head", "tail", "less", "more", "grep", "egrep", "fgrep", "rg", "ag", "wc", "ls", "stat", "file",
		"diff", "cmp", "md5", "md5sum", "shasum", "sha1sum", "sha256sum", "jq", "yq", "nl", "od", "hexdump",
		"strings", "realpath", "readlink", "basename", "dirname", "tree", "du", "test", "[",
	}
}

// utilities are the everyday commands an edit preset's agent may run beside the
// repository's tools, which its sandbox holds to the worktree.
func utilities() []string {
	return []string{
		"ls", "cat", "head", "tail", "wc", "grep", "egrep", "rg", "find", "sort", "uniq", "diff", "cmp", "pwd",
		"echo", "printf", "which", "test", "true", "false", "cut", "tr", "sed", "awk", "jq", "tree", "stat",
		"file", "basename", "dirname", "realpath", "mkdir", "touch", "cp", "mv", "rm", "date", "sleep",
		"shasum", "sha256sum", "xxd", "less", "more",
	}
}

// gitReads are the git subcommands that only read, which every preset that runs commands
// allows.
func gitReads() []string {
	return []string{
		"status", "diff", "log", "show", "rev-parse", "rev-list", "ls-files", "ls-tree", "blame", "grep",
		"describe", "merge-base", "cat-file", "shortlog", "show-ref", "for-each-ref", "name-rev", "branch",
		"tag", "remote", "config", "version", "help", "check-ignore", "check-attr", "worktree", "diff-tree",
		"diff-files", "diff-index", "count-objects", "reflog", "var",
	}
}

// gitEdits are the git subcommands that change only the worktree and its own branch,
// which the edit presets allow beside the reads. Every one runs in the sandbox, which keeps
// it to the worktree and its git folder.
func gitEdits() []string {
	return []string{
		"add", "commit", "restore", "checkout", "switch", "fetch", "stash", "rm", "mv", "reset", "clean", "apply",
		"am", "cherry-pick", "revert", "merge", "rebase", "format-patch", "hash-object", "bisect",
	}
}

// gitListOnly are the git subcommands allowed only to list or read: each says what reads.
func gitListOnly() map[string]func(args []string) bool {
	return map[string]func(args []string) bool{
		"branch":   branchLists,
		"tag":      tagLists,
		"remote":   remoteReads,
		"config":   configReads,
		"worktree": func(args []string) bool { return len(args) > 0 && args[0] == "list" },
		"reflog":   func(args []string) bool { return len(args) == 0 || args[0] == "show" || args[0] == "list" },
	}
}

// branchLists reports git branch arguments that only list or show the current branch.
func branchLists(args []string) bool {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case slices.Contains([]string{
			"--show-current", "-a", "--all", "-r", "--remotes", "-l", "--list", "-v", "-vv",
			"--verbose", "--no-color", "--color", "--column", "--no-column",
		}, a):
		case slices.Contains([]string{
			"--contains", "--no-contains", "--merged", "--no-merged", "--points-at", "--sort",
			"--format",
		}, a):
			i++ // its value
		case hasValuePrefix(a, "--contains", "--no-contains", "--merged", "--no-merged", "--points-at", "--sort",
			"--format", "--color"):
		default:
			return false
		}
	}

	return true
}

// tagLists reports git tag arguments that only list.
func tagLists(args []string) bool {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case slices.Contains([]string{"-l", "--list", "-n", "--column", "--no-column"}, a):
		case slices.Contains([]string{
			"--contains", "--no-contains", "--merged", "--no-merged", "--points-at", "--sort",
			"--format",
		}, a):
			i++
		case hasValuePrefix(a, "--contains", "--no-contains", "--merged", "--no-merged", "--points-at", "--sort",
			"--format", "-n"):
		default:
			return false
		}
	}

	return true
}

// remoteReads reports git remote arguments that only read: none, -v, show, get-url.
func remoteReads(args []string) bool {
	if len(args) == 0 {
		return true
	}

	return slices.Contains([]string{"-v", "--verbose", "show", "get-url"}, args[0])
}

// configReads reports git config arguments that only read.
func configReads(args []string) bool {
	if len(args) == 0 {
		return false
	}

	for _, a := range args {
		if slices.Contains([]string{
			"--get", "--get-all", "--get-regexp", "--list", "-l", "--show-origin",
			"--show-scope", "--name-only", "get", "list",
		}, a) {
			return true
		}
	}

	return false
}

// hasValuePrefix reports an argument of the form <flag>=value for one of flags.
func hasValuePrefix(arg string, flags ...string) bool {
	for _, f := range flags {
		if len(arg) > len(f) && arg[:len(f)+1] == f+"=" {
			return true
		}
	}

	return false
}

// gitGlobalDenied are git's own options, before the subcommand, that change what git
// runs, where it runs, or which config it reads: none is allowed, so no option can
// disguise a push or plant a command.
func gitGlobalDenied() []string {
	return []string{
		"-C", "-c", "--git-dir", "--work-tree", "--exec-path", "--namespace", "--config-env",
		"--super-prefix", "--list-cmds",
	}
}

// gitArgsDenied are options of any git subcommand that run a program the line doesn't
// name.
func gitArgsDenied() []string {
	return []string{"--upload-pack", "--receive-pack", "--exec", "--extcmd", "--tool", "--ext-diff", "--textconv"}
}

// ghCommand is one gh command: its group and its action ("" for a group with none).
type ghCommand struct {
	group, action string
}

// ghReads are the gh commands that only read, which every preset that runs commands
// allows.
func ghReads() []ghCommand {
	return []ghCommand{
		{ghPR, "checks"},
		{ghPR, "view"},
		{ghPR, "diff"},
		{ghPR, "list"},
		{ghPR, "status"},
		{ghRun, "view"},
		{ghRun, "list"},
		{ghIssue, "view"},
		{ghIssue, "list"},
		{ghIssue, "status"},
		{ghRepo, "view"},
		{"workflow", "view"},
		{"workflow", "list"},
		{"label", "list"},
		{"status", ""},
	}
}

// ghTasks are the gh commands GitHub tasks only adds, each on the run's own item alone:
// a comment, a label added or removed, and failed jobs run again.
func ghTasks() []ghCommand {
	return []ghCommand{{ghPR, "comment"}, {ghPR, "edit"}, {ghIssue, "comment"}, {ghIssue, "edit"}, {ghRun, "rerun"}}
}

// ghNever are the gh commands no preset runs, the profile's own included: merging,
// reviewing, closing, releasing, and the API, through which any of them could be done,
// beside the account's own settings and secrets. The app's own steps merge and post
// reviews, after its own checks.
func ghNever() []ghCommand {
	return []ghCommand{
		{ghPR, "merge"},
		{ghPR, "review"},
		{ghPR, "close"},
		{ghPR, "reopen"},
		{ghIssue, "close"},
		{ghIssue, "delete"},
		{ghIssue, "transfer"},
		{ghIssue, "reopen"},
		{"api", ""},
		{"release", ""},
		{"auth", ""},
		{"secret", ""},
		{"variable", ""},
		{"ssh-key", ""},
		{"gpg-key", ""},
		{"extension", ""},
		{"alias", ""},
		{"config", ""},
		{ghRepo, "delete"},
		{ghRepo, "edit"},
		{ghRepo, "rename"},
		{ghRepo, "archive"},
		{ghRepo, "unarchive"},
		{ghRepo, "create"},
		{ghRepo, "fork"},
		{ghRepo, "sync"},
		{ghRepo, "deploy-key"},
		{ghRepo, "set-default"},
		{"workflow", "run"},
		{"workflow", "enable"},
		{"workflow", "disable"},
		{"cache", "delete"},
		{ghRun, "cancel"},
		{ghRun, "delete"},
		{"codespace", ""},
		{"gist", ""},
		{"ruleset", ""},
		{"label", "create"},
		{"label", "edit"},
		{"label", "delete"},
		{"label", "clone"},
		{"project", ""},
		{"org", ""},
		{"attestation", ""},
		{"agent-task", ""},
		{"copilot", ""},
	}
}

// Marker is a file or folder in a clone that says which tools work on it, so an edit
// preset's agent may run them.
type Marker struct {
	Name  string
	Tools []string
}

// Markers returns the markers, in the order their tools are named.
func Markers() []Marker {
	return []Marker{
		{Name: "go.mod", Tools: []string{"go", "gofmt", "govulncheck", "golangci-lint", "staticcheck"}},
		{Name: "magefiles", Tools: []string{"magex", "mage"}},
		{Name: ".mage.yaml", Tools: []string{"magex", "mage"}},
		{Name: ".github/env", Tools: []string{"go-pre-commit"}},
		{Name: ".github/.env.base", Tools: []string{"go-pre-commit"}},
		{Name: "pnpm-lock.yaml", Tools: []string{"pnpm", "node", "npx"}},
		{Name: "package-lock.json", Tools: []string{"npm", "node", "npx"}},
		{Name: "yarn.lock", Tools: []string{"yarn", "node", "npx"}},
		{Name: "bun.lock", Tools: []string{"bun"}},
		{Name: "Makefile", Tools: []string{"make"}},
		{Name: "Cargo.toml", Tools: []string{"cargo", "rustc", "rustfmt"}},
		{Name: "pyproject.toml", Tools: []string{"python3", "pip", "uv", "pytest", "ruff"}},
		{Name: "requirements.txt", Tools: []string{"python3", "pip"}},
		{Name: ".pre-commit-config.yaml", Tools: []string{"pre-commit"}},
	}
}

// RepoTools returns the tools the clone at dir's files say an agent works with, each
// once, in the markers' order. It only looks whether each marker is there (os.Lstat), as
// readiness does, and runs nothing in the clone.
func RepoTools(dir string) []string {
	var out []string

	for _, m := range Markers() {
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(m.Name))); err != nil {
			continue
		}

		for _, tool := range m.Tools {
			if !slices.Contains(out, tool) {
				out = append(out, tool)
			}
		}
	}

	return out
}

// Domains are the hosts a sandboxed command may reach: GitHub, and the package registries
// the markers' tools fetch from.
func Domains() []string {
	return []string{
		"github.com", "api.github.com", "codeload.github.com", "objects.githubusercontent.com",
		"raw.githubusercontent.com", "release-assets.githubusercontent.com",
		"proxy.golang.org", "sum.golang.org", "index.golang.org", "golang.org", "go.dev",
		"registry.npmjs.org", "registry.yarnpkg.com", "repo.yarnpkg.com",
		"pypi.org", "files.pythonhosted.org",
		"crates.io", "index.crates.io", "static.crates.io",
	}
}

// homeCaches are the folders under home where the markers' tools keep their caches, which
// a sandboxed command may write: a build that can't write its cache fails.
func homeCaches() []string {
	return []string{
		"Library/Caches/go-build", "go/pkg/mod", "go/pkg/sumdb", "Library/Caches/golangci-lint",
		"Library/Caches/staticcheck", "Library/pnpm", "Library/Caches/pnpm", ".local/share/pnpm", ".npm",
		"Library/Caches/Yarn", ".yarn/berry", ".bun/install/cache", "Library/Caches/pip", ".cache/uv", ".cache/pip", ".cache/pre-commit", ".cache/go-build",
		".cargo/registry", ".cargo/git", ".magefile", "Library/Caches/pre-commit",
	}
}

// homeSecrets are the folders and files under home that hold the user's credentials,
// which no tool call reads and no sandboxed command opens.
func homeSecrets() []string {
	return []string{
		".ssh", ".aws", ".gnupg", ".config/gh", ".netrc", ".git-credentials", ".docker/config.json", ".kube",
		".config/gcloud", ".azure", ".password-store", "Library/Keychains", ".npmrc", ".pypirc",
		".cargo/credentials", ".cargo/credentials.toml", ".gem/credentials", ".config/hub", ".terraform.d",
		".vault-token", ".zsh_history", ".bash_history",
	}
}
