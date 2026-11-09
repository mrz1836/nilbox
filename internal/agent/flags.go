package agent

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// maxModelRunes caps a model name: the longest real names are well under it.
	maxModelRunes = 100

	// endOfFlags ends an agent's options: every argument after it is positional.
	endOfFlags = "--"
)

// Why a reserved flag can't be added, completing the sentence "<flag> ...".
const (
	reasonModel       = "is set by the Model setting"
	reasonEffort      = "is set by the Effort setting"
	reasonPermissions = "can't be added: permissions come from the profile and the outside-contributor rule"
	reasonTerminal    = "can't be added: sessions run in your terminal"
	reasonTrust       = "the launch trusts the folder it starts in"
	reasonCommand     = "can't be added: it runs another command instead of a session, with none of the launch's settings"
)

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

	// ErrEndOfFlags is returned for a "--" in extra arguments: everything after it would
	// be read as the prompt.
	ErrEndOfFlags = errors.New(`"--" can't be added: everything after it would be read as the prompt`)
)

// ReservedFlagError is returned for an extra flag the launcher sets itself. Its message
// says which setting owns the flag.
type ReservedFlagError struct {
	// Flag is the argument as written, such as "--model" or "-w", or for a config
	// override, the option and its key, such as "-c model".
	Flag string

	// Reason completes the sentence "<flag> ...": what to do instead.
	Reason string

	// Access reports a flag that changes the session's permissions.
	Access bool
}

// Error reads like "--model is set by the Model setting".
func (e *ReservedFlagError) Error() string {
	return e.Flag + " " + e.Reason
}

// Is makes a ReservedFlagError match ErrReservedFlag.
func (e *ReservedFlagError) Is(target error) bool {
	return target == ErrReservedFlag
}

// reservedFlag is a flag the launcher owns, with its short form if it has one. access
// marks a permission switch, which only the launcher's trust policy chooses.
type reservedFlag struct {
	long   string
	short  string
	reason string
	access bool
}

// commonReserved returns the flags every agent reserves: the launcher makes the worktree
// a session starts in itself.
func commonReserved() []reservedFlag {
	return []reservedFlag{
		{long: "--worktree", short: "-w", reason: "is set by the Worktree setting"},
	}
}

// EffortLevels returns the effort levels the agent with id accepts, lowest first, or nil
// for an agent this build does not know.
func EffortLevels(id ID) []string {
	s, err := specFor(id)
	if err != nil {
		return nil
	}

	return values(s.info().EffortLevels)
}

// AnyEffortLevels returns every effort level some agent accepts, lowest first.
func AnyEffortLevels() []string {
	var out []string

	for _, a := range All() {
		for _, level := range values(a.EffortLevels) {
			if !slices.Contains(out, level) {
				out = append(out, level)
			}
		}
	}

	order := effortOrder()

	slices.SortStableFunc(out, func(a, b string) int { return order[a] - order[b] })

	return out
}

// effortOrder ranks every effort level, lowest first.
func effortOrder() map[string]int {
	order := map[string]int{}
	for i, level := range []string{"minimal", "low", "medium", "high", "xhigh", "max", "ultra"} {
		order[level] = i
	}

	return order
}

// ValidateEffort accepts "" (the agent's default) or one of the agent's EffortLevels.
func ValidateEffort(id ID, effort string) error {
	levels := EffortLevels(id)
	if effort == "" || slices.Contains(levels, effort) {
		return nil
	}

	return fmt.Errorf("%w %q: choose one of %s", ErrInvalidEffort, effort, strings.Join(levels, ", "))
}

// ValidateEffortAny accepts "" or a level some agent accepts, for an effort shared by
// every agent, as prompts and launch defaults store it.
func ValidateEffortAny(effort string) error {
	levels := AnyEffortLevels()
	if effort == "" || slices.Contains(levels, effort) {
		return nil
	}

	return fmt.Errorf("%w %q: choose one of %s", ErrInvalidEffort, effort, strings.Join(levels, ", "))
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

// CheckExtra checks extra arguments added by a profile, a prompt, or a launch default for
// the agent with id. Each must be non-empty and free of NUL bytes, must not start with ~
// or give a flag a value that does (--add-dir=~/x), since nothing expands it, must not be
// "--", and must not set a flag the launcher owns (see ReservedFlagError), including a
// config override of a value the launcher sets, such as Codex's -c model=<name>.
func CheckExtra(id ID, args []string) error {
	s, err := specFor(id)
	if err != nil {
		return err
	}

	for i, arg := range args {
		if err = checkArg(s, arg); err != nil {
			return err
		}

		if err = checkConfig(s, args, i); err != nil {
			return err
		}
	}

	return nil
}

// CheckAccess returns the first of args that sets a permission switch of the agent with
// id, as a ReservedFlagError, or nil. Permissions come only from the launcher's own
// policy, so an outside contributor's item can never be launched with more access than
// it allows, whatever a profile's extra arguments say.
func CheckAccess(id ID, args []string) error {
	s, err := specFor(id)
	if err != nil {
		return err
	}

	for i := range args {
		if err = accessArg(s, args, i); err != nil {
			return err
		}
	}

	return nil
}

// accessArg returns a ReservedFlagError when args[i] sets one of the agent's permission
// switches, directly or through a config override.
func accessArg(s spec, args []string, i int) error {
	if err := command(s, args[i]); err != nil {
		return err
	}

	if flag, ok := reserved(s, args[i]); ok && flag.access {
		return &ReservedFlagError{Flag: displayFlag(args[i]), Reason: flag.reason, Access: true}
	}

	err := checkConfig(s, args, i)
	if refused, ok := errors.AsType[*ReservedFlagError](err); ok && refused.Access {
		return err
	}

	return nil
}

// command returns a ReservedFlagError when arg is one of the agent's commands (see
// spec.commands): the agent would run that command, which takes none of the session's
// flags, permissions included, rather than start a session.
func command(s spec, arg string) error {
	if strings.HasPrefix(arg, "-") || !slices.Contains(s.commands(), arg) {
		return nil
	}

	return &ReservedFlagError{Flag: arg, Reason: reasonCommand, Access: true}
}

// checkArg checks one extra argument.
func checkArg(s spec, arg string) error {
	if err := command(s, arg); err != nil {
		return err
	}

	switch {
	case arg == "":
		return ErrEmptyFlag
	case strings.ContainsRune(arg, 0):
		return ErrNULByte
	case strings.HasPrefix(arg, "~") || strings.Contains(arg, "=~"):
		return fmt.Errorf("%w in %q: write the full path", ErrTilde, arg)
	case arg == endOfFlags:
		return ErrEndOfFlags
	}

	if flag, ok := reserved(s, arg); ok {
		return &ReservedFlagError{Flag: displayFlag(arg), Reason: flag.reason, Access: flag.access}
	}

	return nil
}

// reserved returns the reserved flag arg sets, in the "--flag", "--flag=value", "-f", or
// "-f=value" form, or, for an agent that reads its options the way Codex does, "-fvalue".
func reserved(s spec, arg string) (reservedFlag, bool) {
	attached := s.configFlags() != nil

	for _, flag := range s.reserved() {
		if setsFlag(arg, flag.long) || setsFlag(arg, flag.short) || (attached && attachedShort(arg, flag.short)) {
			return flag, true
		}
	}

	return reservedFlag{}, false
}

// setsFlag reports whether arg is the option name, alone or with "=value".
func setsFlag(arg, name string) bool {
	return name != "" && (arg == name || strings.HasPrefix(arg, name+"="))
}

// attachedShort reports whether arg is the short option with its value attached, as in
// "-mo3".
func attachedShort(arg, short string) bool {
	return short != "" && len(arg) > len(short) && !strings.HasPrefix(arg, "--") && strings.HasPrefix(arg, short)
}

// checkConfig checks the config override at args[i], if it is one: "-c key=value",
// "-ckey=value", "-c=key=value", "--config key=value", or "--config=key=value". A key the
// launcher sets is refused.
func checkConfig(s spec, args []string, i int) error {
	flag, value, ok := configValue(s.configFlags(), args, i)
	if !ok {
		return nil
	}

	key, _, _ := strings.Cut(value, "=")
	key = strings.TrimSpace(key)

	reason := s.reservedConfig(key)
	if reason == "" {
		return nil
	}

	return &ReservedFlagError{Flag: flag + " " + key, Reason: reason, Access: reason == reasonPermissions}
}

// configValue returns the option and the key=value a config override at args[i] sets, if
// it is one. A short option may hold its value directly, as in -cmodel=x.
func configValue(flags, args []string, i int) (flag, value string, ok bool) {
	arg := args[i]

	for _, flag = range flags {
		switch {
		case arg == flag:
			if i+1 < len(args) {
				return flag, args[i+1], true
			}

			return "", "", false
		case strings.HasPrefix(arg, flag+"="):
			return flag, arg[len(flag)+1:], true
		case !strings.HasPrefix(flag, "--") && !strings.HasPrefix(arg, "--") && strings.HasPrefix(arg, flag):
			return flag, arg[len(flag):], true
		}
	}

	return "", "", false
}

// displayFlag returns the flag part of an argument, without any "=value".
func displayFlag(arg string) string {
	name, _, _ := strings.Cut(arg, "=")

	return name
}
