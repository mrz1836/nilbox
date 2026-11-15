package terminal

import (
	"os"
	"path/filepath"
)

// itermBundle is the app bundle iTerm2 installs as.
const itermBundle = "iTerm.app"

// Choice is one terminal launches can open in, as the setup offers it: its ID, its name,
// and whether it is installed.
type Choice struct {
	ID        ID
	Name      string
	Installed bool
}

// Installed returns each terminal the registry has a driver for, in its IDs' order, and
// whether it is installed: Terminal.app always is, since it comes with the system, and
// iTerm2 is when its bundle is in one of dirs, the folders apps are installed in (core
// passes /Applications and ~/Applications). It reads only whether those bundles are there;
// a registry with no drivers, as on a system without terminal support, offers none.
func (r Registry) Installed(dirs ...string) []Choice {
	ids := r.IDs()
	out := make([]Choice, 0, len(ids))

	for _, id := range ids {
		out = append(out, Choice{ID: id, Name: Name(id), Installed: installed(id, dirs)})
	}

	return out
}

// installed reports whether the terminal id is installed, as Installed says.
func installed(id ID, dirs []string) bool {
	switch id {
	case TerminalApp:
		return true
	case ITerm2:
		for _, dir := range dirs {
			if dir == "" {
				continue
			}

			if info, err := os.Stat(filepath.Join(dir, itermBundle)); err == nil && info.IsDir() {
				return true
			}
		}

		return false
	default:
		return false
	}
}
