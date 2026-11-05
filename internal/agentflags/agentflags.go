// Package agentflags checks the options a launch passes to the coding agent, so every
// place that stores them (prompts and launch defaults) and the launcher that passes them
// apply the same rules.
//
// It knows the agent's effort levels, what a model name may look like, which flags the
// launcher sets itself (and so may not be added by hand), and how to read extra flags
// typed on one line, quoted the way a shell would, into one argument each.
package agentflags

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxModelRunes caps a model name: the longest real names are well under it.
const maxModelRunes = 100

var (
	// ErrInvalidEffort is returned for an effort level the agent does not accept.
	ErrInvalidEffort = errors.New("unknown effort level")

	// ErrInvalidModel is returned for a model name the agent could misread.
	ErrInvalidModel = errors.New("invalid model name")

	// ErrReservedFlag is returned for an extra flag the launcher sets itself.
	ErrReservedFlag = errors.New("reserved flag")

	// ErrEmptyFlag is returned for an empty extra argument.
	ErrEmptyFlag = errors.New("empty argument")

	// ErrNULByte is returned for an argument holding a NUL byte, which no process
	// argument can carry.
	ErrNULByte = errors.New("value contains a NUL byte")

	// ErrTilde is returned for an argument that starts with ~: launch scripts quote every
	// argument, so no shell expands it.
	ErrTilde = errors.New("~ is not expanded")

	// ErrUnterminatedQuote is returned by Split for a quote that is never closed.
	ErrUnterminatedQuote = errors.New("unterminated quote")
)

// ReservedFlagError is returned for an extra flag the launcher sets itself. Its message
// says which setting owns the flag.
type ReservedFlagError struct {
	// Flag is the argument as written, such as "--model" or "-w".
	Flag string

	// Reason completes the sentence "<flag> ...": what to do instead.
	Reason string
}

// Error reads like "--model is set by the Model setting".
func (e *ReservedFlagError) Error() string {
	return e.Flag + " " + e.Reason
}

// Is makes a ReservedFlagError match ErrReservedFlag.
func (e *ReservedFlagError) Is(target error) bool {
	return target == ErrReservedFlag
}

// reservedFlag is a flag the launcher owns, with its short form if it has one.
type reservedFlag struct {
	long   string
	short  string
	reason string
}

// reservedFlags returns the flags extra arguments may not set: the ones the launcher
// sets from their own settings, the permission switches, which only the launcher's trust
// policy chooses, and the ones that would end the session before it opens.
func reservedFlags() []reservedFlag {
	const (
		permissions = "can't be added: permissions come from the profile and the outside-contributor rule"
		terminal    = "can't be added: sessions run in your terminal"
	)

	return []reservedFlag{
		{long: "--model", reason: "is set by the Model setting"},
		{long: "--effort", reason: "is set by the Effort setting"},
		{long: "--worktree", short: "-w", reason: "is set by the Worktree setting"},
		{long: "--permission-mode", reason: permissions},
		{long: "--dangerously-skip-permissions", reason: permissions},
		{long: "--allow-dangerously-skip-permissions", reason: permissions},
		{long: "--name", short: "-n", reason: "can't be added: each session is named for you"},
		{long: "--print", short: "-p", reason: terminal},
		{long: "--background", reason: terminal},
		{long: "--bg", reason: terminal},
	}
}

// EffortLevels returns the values the agent accepts for --effort, lowest first.
func EffortLevels() []string {
	return []string{"low", "medium", "high", "xhigh", "max"}
}

// ValidateEffort accepts "" (the agent's default) or one of EffortLevels.
func ValidateEffort(effort string) error {
	if effort == "" || slices.Contains(EffortLevels(), effort) {
		return nil
	}

	return fmt.Errorf("%w %q: choose one of %s", ErrInvalidEffort, effort, strings.Join(EffortLevels(), ", "))
}

// ValidateModel accepts "" (the agent's default) or a model alias or full name: one word
// of printable characters, at most 100 long, that does not start with "-", so the agent
// can never read it as a flag.
func ValidateModel(model string) error {
	switch {
	case model == "":
		return nil
	case strings.HasPrefix(model, "-"):
		return fmt.Errorf("%w: %q starts with -", ErrInvalidModel, model)
	case utf8.RuneCountInString(model) > maxModelRunes:
		return fmt.Errorf("%w: longer than %d characters", ErrInvalidModel, maxModelRunes)
	case strings.ContainsFunc(model, func(r rune) bool { return unicode.IsSpace(r) || !unicode.IsPrint(r) }):
		return fmt.Errorf("%w: %q holds a space or control character", ErrInvalidModel, model)
	default:
		return nil
	}
}

// CheckExtra checks extra arguments added by a prompt or a launch default. Each must be
// non-empty and free of NUL bytes, must not start with ~ or give a flag a value that
// does (--add-dir=~/x), since nothing expands it, and must not set a flag the launcher
// owns (see ReservedFlagError).
func CheckExtra(args []string) error {
	for _, arg := range args {
		if err := checkArg(arg); err != nil {
			return err
		}
	}

	return nil
}

// checkArg checks one extra argument.
func checkArg(arg string) error {
	switch {
	case arg == "":
		return ErrEmptyFlag
	case strings.ContainsRune(arg, 0):
		return ErrNULByte
	case strings.HasPrefix(arg, "~") || strings.Contains(arg, "=~"):
		return fmt.Errorf("%w in %q: write the full path", ErrTilde, arg)
	}

	if flag, ok := reserved(arg); ok {
		return &ReservedFlagError{Flag: displayFlag(arg), Reason: flag.reason}
	}

	return nil
}

// reserved returns the reserved flag arg sets, in the "--flag", "--flag=value", "-f", or
// "-f=value" form.
func reserved(arg string) (reservedFlag, bool) {
	for _, flag := range reservedFlags() {
		for _, name := range []string{flag.long, flag.short} {
			if name != "" && (arg == name || strings.HasPrefix(arg, name+"=")) {
				return flag, true
			}
		}
	}

	return reservedFlag{}, false
}

// displayFlag returns the flag part of an argument, without any "=value".
func displayFlag(arg string) string {
	name, _, _ := strings.Cut(arg, "=")

	return name
}
