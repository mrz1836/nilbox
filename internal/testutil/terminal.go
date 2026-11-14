package testutil

// TerminalScript is what a fake terminal's shell runs for a run in the terminal, the
// command in its arguments: what the script the app writes for such a run does under
// script(1), in plain sh, so a test needs no pseudo-terminal. It records the shell's PID
// in $PID_FILE, runs the command with its output added to $LOG_FILE, and writes its exit
// status to $EXIT_FILE.tmp, then renames that into $EXIT_FILE: the files the runner
// follows the run through. With NO_EXIT_FILE set, the shell ends without the exit status,
// as a terminal closed just as the command ended leaves it.
//
// Run it as /bin/sh -c TerminalScript sh <command> <arguments>....
const TerminalScript = `printf '%s\n' "$$" > "$PID_FILE"
"$@" >> "$LOG_FILE" 2>&1
status=$?
if [ -n "${NO_EXIT_FILE:-}" ]; then exit "$status"; fi
printf '%s\n' "$status" > "$EXIT_FILE.tmp" && mv "$EXIT_FILE.tmp" "$EXIT_FILE"
exit "$status"`
