package terminal

// TermProgramEnv is the environment variable each of the terminals sets in every shell it
// starts, naming itself, which an agent started there inherits from its tab's shell. Show
// in terminal reads it from a session's agent process, as it reads Warp's focus link, to
// learn which terminal the session runs in now: one the user resumed themselves may run in
// another terminal than the one its launch recorded.
const TermProgramEnv = "TERM_PROGRAM"

// FromTermProgram returns the terminal whose shells TERM_PROGRAM names with value: Warp's
// WarpTerminal, iTerm2's iTerm.app, or Terminal.app's Apple_Terminal, matched exactly. Any
// other value, such as the one tmux sets in its own shells or an empty one, is no terminal
// of these, and reports false. It is pure: reading the variable is the caller's job.
func FromTermProgram(value string) (ID, bool) {
	switch value {
	case "iTerm.app":
		return ITerm2, true
	case "WarpTerminal":
		return Warp, true
	case "Apple_Terminal":
		return TerminalApp, true
	default:
		return "", false
	}
}
