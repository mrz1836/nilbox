package fence

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// The hook mode's flag. The protocol is versioned in the flag's name, so a run that
// outlives an update calls the new build's hook in the protocol the run was started with.
// Version 1 is Claude Code's PreToolUse hook: the tool call as JSON on stdin, and a call
// refused with exit status 2 and its reason on stderr.
const (
	// HookFlag asks for the hook mode, protocol version 1, with the run's folder as its
	// value: --fence-hook-v1=<run folder>.
	HookFlag = "--fence-hook-v1"

	// hookFlagPrefix starts the hook mode's flag in every protocol version, so a build
	// refuses a version it doesn't know rather than start as the app.
	hookFlagPrefix = "--fence-hook"

	// hookDenied is the exit status that refuses a call.
	hookDenied = 2

	// maxHookInput caps the tool call read from stdin: a Write's content can be large.
	maxHookInput = 32 << 20
)

// HookArg returns the hook mode's argument for a run's folder.
func HookArg(runDir string) string {
	return HookFlag + "=" + runDir
}

// IsHookCall reports arguments that ask for the hook mode, in any protocol version.
func IsHookCall(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, hookFlagPrefix) {
			return true
		}
	}

	return false
}

// hookInput is the part of Claude Code's PreToolUse input the fence reads.
type hookInput struct {
	Event     string          `json:"hook_event_name"`
	Tool      string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	Cwd       string          `json:"cwd"`
}

// toolInput is the part of a tool's input the fence reads, by the field names Claude
// Code's tools use.
type toolInput struct {
	Command      string `json:"command"`
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	Path         string `json:"path"`
	Pattern      string `json:"pattern"`
}

// RunHook runs the hook mode when args ask for it, and returns the status the process
// exits with and true; for any other arguments it returns 0 and false and reads nothing.
// It reads the tool call from stdin and the policy from the run's folder, and lets the
// call through with status 0 and no output, or refuses it with status 2 and the reason on
// stderr. It refuses every call it can't read, and every call under a policy it can't
// read, so a fence never fails open. It reads nothing else and writes nothing.
func RunHook(args []string, stdin io.Reader, stderr io.Writer) (int, bool) {
	if !IsHookCall(args) {
		return 0, false
	}

	v := hookVerdict(args, stdin)
	if !v.Deny {
		return 0, true
	}

	_, _ = fmt.Fprintln(stderr, v.Reason)

	return hookDenied, true
}

// hookVerdict decides the hook mode's call.
func hookVerdict(args []string, stdin io.Reader) Verdict {
	runDir, err := hookRunDir(args)
	if err != nil {
		return deny("The fence refused this call: %v.", err)
	}

	in, err := readHookInput(stdin)
	if err != nil {
		return deny("The fence refused this call: %v.", err)
	}

	if in.Event != "" && in.Event != "PreToolUse" {
		return Verdict{}
	}

	p, err := ReadPolicy(runDir)
	if err != nil {
		return deny("The fence refused this call: %v.", err)
	}

	return Decide(p, in.call(), OSFS{})
}

// errHookCall is a hook call the fence can't read: its arguments or its tool call.
var errHookCall = errors.New("the fence can't read this call")

// hookRunDir returns the run's folder the arguments name, refusing a protocol this build
// doesn't know.
func hookRunDir(args []string) (string, error) {
	for _, a := range args {
		if !strings.HasPrefix(a, hookFlagPrefix) {
			continue
		}

		dir, ok := strings.CutPrefix(a, HookFlag+"=")
		if !ok {
			return "", fmt.Errorf("%w: this build doesn't know the fence protocol of %q", errHookCall,
				strings.SplitN(a, "=", 2)[0])
		}

		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("%w: the run's folder isn't an absolute path", errHookCall)
		}

		return filepath.Clean(dir), nil
	}

	return "", fmt.Errorf("%w: no run's folder was given", errHookCall)
}

// decodedCall is a hook input with its tool's input read.
type decodedCall struct {
	hookInput

	input toolInput
}

// readHookInput reads the tool call from stdin.
func readHookInput(stdin io.Reader) (decodedCall, error) {
	data, err := io.ReadAll(io.LimitReader(stdin, maxHookInput+1))
	if err != nil {
		return decodedCall{}, fmt.Errorf("%w: its tool call couldn't be read", errHookCall)
	}

	if len(data) > maxHookInput {
		return decodedCall{}, fmt.Errorf("%w: its tool call is too large to check", errHookCall)
	}

	var in decodedCall
	if err = json.Unmarshal(data, &in.hookInput); err != nil || in.Tool == "" {
		return decodedCall{}, fmt.Errorf("%w: its tool call isn't one the fence knows", errHookCall)
	}

	raw := bytes.TrimSpace(in.ToolInput)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return in, nil
	}

	if err = json.Unmarshal(raw, &in.input); err != nil {
		return decodedCall{}, fmt.Errorf("%w: its tool's input isn't one the fence knows", errHookCall)
	}

	return in, nil
}

// call returns the decoded input as the policy's Call.
func (in decodedCall) call() Call {
	path := in.input.FilePath
	if path == "" {
		path = cmp.Or(in.input.NotebookPath, in.input.Path)
	}

	c := Call{Tool: in.Tool, Command: in.input.Command, Path: path, Dir: in.Cwd}

	if in.Tool == "Glob" {
		// Grep's pattern is a regular expression to search for, not a path.
		c.Pattern = in.input.Pattern
	}

	return c
}
