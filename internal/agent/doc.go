// Package agent knows the coding tools a launch can start and checks the options a launch
// passes to them, so every place that stores those options (prompts and launch defaults)
// and the launcher that passes them apply the same rules.
//
// Only Claude Code ships today. Launch defaults already store a tool per org or
// repository ("" meaning the app's default), so another tool can be added here without
// changing what is stored.
//
// It knows the agent's effort levels, what a model name may look like, which flags the
// launcher sets itself (and so may not be added by hand), and how to read extra flags
// typed on one line, quoted the way a shell would, into one argument each. CheckOverrides
// checks the three together the way the prompt and launch-default editors show them, as
// FieldErrors.
package agent
