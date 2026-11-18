package fence

import (
	"path/filepath"
	"slices"
	"strings"
)

// The fence's reader also tells the agent sessions' observers when a command an agent ran
// changed something on GitHub, so the inbox reads the session's item again while the
// session still works, rather than only once it stops. It reads a line the way it decides
// one; a line it can't read tells nothing, and the read after the session stops working
// stays the backstop.

// ghPRChanges and ghIssueChanges are the gh pr and gh issue actions that change the item on
// GitHub.
func ghPRChanges() []string {
	return []string{"merge", "review", "close", "reopen", "comment", "edit", "ready", "update-branch"}
}

func ghIssueChanges() []string {
	return []string{"close", "reopen", "comment", "edit"}
}

// ChangesGitHub reports whether the command line runs a command that changes something on
// GitHub: gh pr merge, review, close, reopen, comment, edit, ready, or update-branch; gh
// issue close, reopen, comment, or edit; gh api with a method other than GET, with fields
// and no method, or a GraphQL mutation; or git push. It looks past the wrapper programs
// (env, timeout, xargs, …) and into the line a shell's -c runs, as a Codex command arrives
// (/bin/zsh -lc '…'), and reports false for a line it can't read.
func ChangesGitHub(line string) bool {
	return changesGitHubAt(line, 0)
}

// changesGitHubAt is ChangesGitHub for a line nested depth shells deep.
func changesGitHubAt(line string, depth int) bool {
	s, err := readShellAt(line, depth)
	if err != nil {
		return false
	}

	return slices.ContainsFunc(s.commands, func(c simple) bool {
		return len(c.words) > 0 && commandChangesGitHub(texts(c.words), depth)
	})
}

// commandChangesGitHub reports whether a simple command, by its words, the program first,
// changes GitHub: itself, or, for a wrapper, the command among its words.
func commandChangesGitHub(words []string, depth int) bool {
	if !slices.Contains(wrappers(), filepath.Base(words[0])) {
		return programChangesGitHub(words, depth)
	}

	for i := 1; i < len(words); i++ {
		if programChangesGitHub(words[i:], depth) {
			return true
		}
	}

	return false
}

// programChangesGitHub reports whether the command words run changes GitHub, by its
// program alone.
func programChangesGitHub(words []string, depth int) bool {
	name := filepath.Base(words[0])

	switch {
	case name == "gh":
		return ghChangesGitHub(words)
	case name == "git":
		return gitSubcommand(words) == "push"
	case slices.Contains(shells(), name):
		return shellChangesGitHub(words[1:], depth)
	default:
		return false
	}
}

// gitValueOptions are git's own options that take the word after them as their value.
func gitValueOptions() []string {
	return []string{"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--super-prefix", "--config-env", "--exec-path"}
}

// gitSubcommand returns a git command's subcommand, by its words, past git's own options
// and their values (git -C dir -c name=value push), or "" with none.
func gitSubcommand(words []string) string {
	for i := 1; i < len(words); i++ {
		switch w := words[i]; {
		case slices.Contains(gitValueOptions(), w):
			i++
		case strings.HasPrefix(w, "-"):
		default:
			return w
		}
	}

	return ""
}

// ghChangesGitHub reports a gh command, by its words, that changes GitHub.
func ghChangesGitHub(words []string) bool {
	cmd := ghCommandOf(words)

	switch cmd.group {
	case ghPR:
		return slices.Contains(ghPRChanges(), cmd.action)
	case ghIssue:
		return slices.Contains(ghIssueChanges(), cmd.action)
	case "api":
		return apiChangesGitHub(words[2:])
	default:
		return false
	}
}

// apiChangesGitHub reports whether gh api, with args, writes: its method, when it names
// one, says (GET reads, whatever fields it sends as its query); else a GraphQL request
// writes when it holds a mutation, and any other when it sends fields, which gh posts.
func apiChangesGitHub(args []string) bool {
	req := apiRequestOf(args)

	switch {
	case req.method != "":
		return !strings.EqualFold(req.method, "GET")
	case req.endpoint == "graphql":
		return slices.ContainsFunc(args, func(a string) bool { return strings.Contains(a, "mutation") })
	default:
		return req.fields
	}
}

// apiRequest is what gh api's arguments ask for: the method it names ("" for none), whether
// it sends a field or a body, and its endpoint, the first word that isn't an option.
type apiRequest struct {
	method   string
	fields   bool
	endpoint string
}

// apiRequestOf reads gh api's arguments.
func apiRequestOf(args []string) apiRequest {
	var req apiRequest

	for i := 0; i < len(args); i++ {
		a := args[i]

		switch {
		case (a == "-X" || a == "--method") && i+1 < len(args):
			req.method = args[i+1]
			i++
		case strings.HasPrefix(a, "--method="):
			req.method = strings.TrimPrefix(a, "--method=")
		case strings.HasPrefix(a, "-X"):
			req.method = strings.TrimPrefix(a, "-X")
		case isAPIField(a):
			req.fields = true
		case req.endpoint == "" && !strings.HasPrefix(a, "-"):
			req.endpoint = a
		}
	}

	return req
}

// isAPIField reports one of gh api's options that sends a field or a body: -f, -F,
// --field, --raw-field, or --input, alone or holding its value.
func isAPIField(a string) bool {
	for _, long := range []string{"--field", "--raw-field", "--input"} {
		if a == long || strings.HasPrefix(a, long+"=") {
			return true
		}
	}

	return strings.HasPrefix(a, "-f") || strings.HasPrefix(a, "-F")
}

// shellChangesGitHub reports whether a shell, by its arguments, runs a line with -c that
// changes GitHub.
func shellChangesGitHub(args []string, depth int) bool {
	for i, a := range args {
		if shellCommandFlag(a) && i+1 < len(args) {
			return changesGitHubAt(args[i+1], depth+1)
		}
	}

	return false
}
