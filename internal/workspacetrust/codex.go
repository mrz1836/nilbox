package workspacetrust

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/mrz1836/nilbox/internal/logging"
)

// Codex's config file, its default directory, and the keys a grant writes.
const (
	// codexConfigName is Codex's config file in its config directory (CODEX_HOME).
	codexConfigName = "config.toml"

	// codexDefaultDir is the default profile's config directory under home.
	codexDefaultDir = ".codex"

	// codexTrustLevel is a project's trust key, and codexTrusted the value that trusts it.
	codexTrustLevel = "trust_level"
	codexTrusted    = "trusted"
)

// ErrNotTOML is returned when Codex's config file cannot be read as TOML, so a grant
// leaves it alone.
var ErrNotTOML = errors.New("the config is not valid TOML")

// Claude marks directories trusted in Claude Code's config (see Grant).
type Claude struct {
	// Home is the user's home directory, where the default profile's config is.
	Home string
}

// Trust marks each of dirs trusted in the Claude config a session with configDir reads.
func (c Claude) Trust(ctx context.Context, configDir string, dirs []string) error {
	file := ConfigFile(configDir, c.Home)
	if file == "" {
		return nil
	}

	for _, dir := range dirs {
		if _, err := Grant(ctx, file, dir); err != nil {
			return err
		}
	}

	return nil
}

// Codex marks directories trusted in Codex's config, so a session started there does not
// stop at Codex's question about trusting the folder.
//
// Codex keeps each project's trust in $CODEX_HOME/config.toml, as
//
//	[projects."<path>"]
//	trust_level = "trusted"
//
// A grant only ever appends such a table for a directory the file says nothing about, so
// the user's comments, order, and formatting are kept byte for byte. It writes nothing
// for a directory with an entry of its own, trusted or not (an untrusted one is the
// user's choice, and is logged), or when the file is not valid TOML. The file is replaced
// in one rename, with its mode kept, after it is read again just before the write.
type Codex struct {
	// Home is the user's home directory, where the default profile's config directory
	// (~/.codex) is.
	Home string

	// Logger hears what a grant left alone. Nil discards.
	Logger *slog.Logger
}

// Trust marks each of dirs trusted in the config.toml of configDir, or of ~/.codex for the
// default profile (""). Nothing is written when the config directory does not exist yet,
// as for a profile Codex has never run with, which it sets up on its own.
func (c Codex) Trust(ctx context.Context, configDir string, dirs []string) error {
	dir := configDir
	if dir == "" {
		if c.Home == "" {
			return nil
		}

		dir = filepath.Join(c.Home, codexDefaultDir)
	}

	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%w: Codex config directory %q", ErrRelativePath, dir)
	}

	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	keys := make([]string, 0, len(dirs))

	for _, d := range dirs {
		if !filepath.IsAbs(d) {
			return fmt.Errorf("%w: %q", ErrRelativePath, d)
		}

		key, err := filepath.EvalSymlinks(d)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", d, err)
		}

		keys = append(keys, key)
	}

	_, err := c.grant(ctx, filepath.Join(dir, codexConfigName), keys)

	return err
}

// grant appends a trusted entry for each of keys the config at file lacks, and reports
// whether it changed the file. A config that changed while it was read is read again
// once; one that keeps changing is left alone.
func (c Codex) grant(ctx context.Context, file string, keys []string) (bool, error) {
	log := logging.Or(c.Logger)

	target, err := filepath.EvalSymlinks(file)
	if errors.Is(err, fs.ErrNotExist) {
		target, err = file, nil
	}

	if err != nil {
		return false, fmt.Errorf("resolve %s: %w", file, err)
	}

	for range 2 {
		changed, settled, attemptErr := c.attempt(ctx, log, file, target, keys)
		if attemptErr != nil || settled {
			return changed, attemptErr
		}
	}

	log.WarnContext(ctx, "Codex's config kept changing, so no folder was trusted in it", "path", file)

	return false, nil
}

// attempt reads the config at target, adds the entries it lacks, and replaces it, unless
// it changed in the meantime. settled reports that no further attempt is needed: the file
// was written, needed nothing, or is left alone.
func (c Codex) attempt(ctx context.Context, log *slog.Logger, file, target string, keys []string) (changed, settled bool, err error) {
	data, mode, err := readConfig(target)
	if err != nil {
		return false, true, err
	}

	out, changed, err := codexGrant(data, keys, func(key, level string) {
		log.WarnContext(ctx, "left a Codex project's own trust setting as it is", "project", key, "trust_level", level)
	})

	switch {
	case errors.Is(err, ErrNotTOML):
		log.WarnContext(ctx, "Codex's config could not be read, so no folder was trusted in it", "path", file, "error", err)

		return false, true, nil
	case err != nil || !changed:
		return false, true, err
	}

	again, _, err := readConfig(target)
	if err != nil {
		return false, true, err
	}

	if !bytes.Equal(again, data) {
		return false, false, nil
	}

	if err = replaceFile(target, out, mode); err != nil {
		return false, true, fmt.Errorf("write %s: %w", file, err)
	}

	return true, true, nil
}

// codexGrant returns the config with a trusted entry appended for each key it has no
// entry for, and whether that changed anything. explicit hears each key with an entry of
// its own that is not trusted, which is left as it is. It returns ErrNotTOML when the
// config, or the config with the entries added, is not valid TOML.
func codexGrant(data []byte, keys []string, explicit func(key, level string)) ([]byte, bool, error) {
	projects, err := codexProjects(data)
	if err != nil {
		return nil, false, err
	}

	missing := missingKeys(projects, keys, explicit)
	if len(missing) == 0 {
		return data, false, nil
	}

	out := bytes.Clone(data)
	if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, '\n')
	}

	for _, key := range missing {
		out = append(out, "\n[projects."+tomlString(key)+"]\n"+codexTrustLevel+" = "+strconv.Quote(codexTrusted)+"\n"...)
	}

	check, err := codexProjects(out)
	if err != nil {
		return nil, false, err
	}

	for _, key := range missing {
		if trustLevel(check[key]) != codexTrusted {
			return nil, false, fmt.Errorf("%w: the entry for %s did not read back", ErrNotTOML, key)
		}
	}

	return out, true, nil
}

// missingKeys returns the keys, each once and in order, that projects has no entry for.
// explicit hears each key with an entry of its own that is not trusted.
func missingKeys(projects map[string]any, keys []string, explicit func(key, level string)) []string {
	var missing []string

	seen := map[string]bool{}

	for _, key := range keys {
		if seen[key] {
			continue
		}

		seen[key] = true

		entry, ok := projects[key]

		switch {
		case !ok:
			missing = append(missing, key)
		case trustLevel(entry) != codexTrusted:
			explicit(key, trustLevel(entry))
		}
	}

	return missing
}

// codexProjects reads the config's projects table: each project's entry, by path. A
// config with none has an empty one.
func codexProjects(data []byte) (map[string]any, error) {
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotTOML, err)
	}

	raw, ok := doc["projects"]
	if !ok {
		return map[string]any{}, nil
	}

	projects, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: projects is not a table", ErrNotTOML)
	}

	return projects, nil
}

// trustLevel returns a project entry's trust_level, or "" when it has none.
func trustLevel(entry any) string {
	table, ok := entry.(map[string]any)
	if !ok {
		return ""
	}

	level, _ := table[codexTrustLevel].(string)

	return level
}

// tomlString writes s as a TOML basic string: quoted, with backslashes, quotes, and
// control characters escaped.
func tomlString(s string) string {
	var b strings.Builder

	b.WriteByte('"')

	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}

	b.WriteByte('"')

	return b.String()
}
