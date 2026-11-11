package scripts

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// maxFile bounds a source file the app reads: t3.json, tasks.json, or one file under
// magefiles/.
const maxFile = 1 << 20

// releaseMarker is what a command that bumps a version holds: Release does that, with its
// checks.
const releaseMarker = "version:bump"

// t3Doc is what the app reads of a t3.json: its scripts' names, commands, and icons.
type t3Doc struct {
	Scripts []struct {
		Name    string `json:"name"`
		Command string `json:"command"`
		Icon    string `json:"icon"`
	} `json:"scripts"`
}

// t3Form is a t3.json script's effective form, whose hash its trust is.
type t3Form struct {
	Source  Source `json:"source"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

// readT3 reads the scripts in the t3.json of the clone at dir, in its order. found
// reports the file is there; an error says why it couldn't be read.
func readT3(dir string) (out []script, found bool, err error) {
	data, found, err := readSource(dir, t3File)
	if err != nil || !found {
		return nil, found, err
	}

	var doc t3Doc
	if err = decodeJSONC(data, &doc); err != nil {
		return nil, true, invalidJSON(t3File, err)
	}

	seen := map[string]bool{}

	for _, s := range doc.Scripts {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}

		p := part{kind: partShell, command: s.Command}
		sc := script{
			entry: Entry{
				Ref: ref(SourceT3, name), Source: SourceT3, Name: name, Command: s.Command, Icon: s.Icon,
				Runnable: true, Release: strings.Contains(s.Command, releaseMarker), Inputs: []Input{},
			},
			name: name, hash: hashOf(t3Form{Source: SourceT3, Name: name, Command: s.Command}),
			parts: []part{p}, inputs: map[string]Input{},
		}

		switch {
		case seen[name]:
			sc.entry.Runnable, sc.entry.Why = false, "Another script in "+t3File+" has this name."
		case strings.TrimSpace(s.Command) == "":
			sc.entry.Runnable, sc.entry.Why = false, "Has no command to run."
		}

		seen[name] = true

		out = append(out, sc)
	}

	return out, true, nil
}

// readSource reads the file at rel in the clone at dir: found reports it is there, and
// anything but a regular file, a file over maxFile, or one that can't be read, is an error.
func readSource(dir, rel string) (data []byte, found bool, err error) {
	data, err = readLimited(filepath.Join(dir, filepath.FromSlash(rel)), rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}

	return data, true, err
}

// readLimited reads the regular file at path, which the error calls name, refusing one
// over maxFile. A file that isn't regular, such as a pipe, which would block the read, is
// refused before it is opened.
func readLimited(path, name string) ([]byte, error) {
	info, err := os.Stat(path)

	switch {
	case err != nil:
		return nil, readFailed(name, err)
	case !info.Mode().IsRegular():
		return nil, uierr.New(name+" isn't a file.", fmt.Errorf("%w: %s isn't a file", ErrCantRun, name))
	}

	f, err := os.Open(path) //nolint:gosec // a file of the clone the page names
	if err != nil {
		return nil, readFailed(name, err)
	}

	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxFile+1))

	switch {
	case err != nil:
		return nil, readFailed(name, err)
	case len(data) > maxFile:
		return nil, uierr.New(fmt.Sprintf("%s is over %d MB, too large to read.", name, maxFile>>20),
			fmt.Errorf("%w: %s is too large", ErrCantRun, name))
	}

	return data, nil
}

// readFailed words a file that couldn't be read, naming it as the page does, without the
// folder it is in.
func readFailed(name string, err error) error {
	reason := err.Error()
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		reason = pe.Err.Error()
	}

	return uierr.New(textutil.WithReason("Couldn't read "+name, reason), fmt.Errorf("read %s: %w", name, err))
}

// invalidJSON words a source file that isn't valid JSON.
func invalidJSON(name string, err error) error {
	return uierr.New(textutil.WithReason(name+" isn't valid JSON", err.Error()), fmt.Errorf("decode %s: %w", name, err))
}

// ref names a script within its clone: its source and its name.
func ref(src Source, name string) string {
	return string(src) + ":" + name
}

// parseRef reads a script's ref back.
func parseRef(r string) (Source, string, bool) {
	src, name, ok := strings.Cut(r, ":")
	if !ok || name == "" || !Source(src).Valid() {
		return "", "", false
	}

	return Source(src), name, true
}
