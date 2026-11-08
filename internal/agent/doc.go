// Package agent knows the coding agents a launch can start, Claude Code and OpenAI Codex,
// and checks the options a launch passes them, so every place that stores those options
// (profiles, prompts, and launch defaults) and the launcher that passes them apply the
// same rules.
//
// Each agent is a backend spec: its command, the environment variable that points it at
// a profile's config directory, the entries that mark such a directory, its effort levels,
// model suggestions, and permission modes, the flags the launcher sets itself (and so may
// not be added by hand), and how a launch's model, effort, access, extra directories, and
// session name become its command-line flags. Adding a backend adds a spec here; the
// launcher, the profiles, and the editors read everything else from it.
//
// It also reads extra flags typed on one line, quoted the way a shell would, into one
// argument each. CheckOverrides and CheckSettings check a model, an effort level, and
// extra flags the way the prompt and launch-default editors show them, as FieldErrors.
//
// The package is pure: it reads no files and runs nothing.
package agent
