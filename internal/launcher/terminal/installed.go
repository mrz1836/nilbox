package terminal

import (
	"os"
	"path/filepath"
	"slices"
)

// The app bundles the terminals install as. Terminal.app has none here: it comes with the
// system.
const (
	itermBundle = "iTerm.app"
	warpBundle  = "Warp.app"
)

// Choice is one terminal launches can open in, as the setup offers it: its ID, its name,
// and whether it is installed.
type Choice struct {
	ID        ID
	Name      string
	Installed bool
}

// Installed returns each terminal the registry has a driver for, and whether it is
// installed, in the order a choice prefers them (All: iTerm2, Warp, Terminal.app), so the
// first one installed is the one to pick; a driver All doesn't list comes after, in ID
// order. Terminal.app is always installed, since it comes with the system, and iTerm2 and
// Warp are when their bundles are in one of dirs, the folders apps are installed in (core
// passes /Applications and ~/Applications). It reads only whether those bundles are there;
// a registry with no drivers, as on a system without terminal support, offers none.
func (r Registry) Installed(dirs ...string) []Choice {
	ids := r.IDs()
	slices.SortStableFunc(ids, func(a, b ID) int { return rank(a) - rank(b) })

	out := make([]Choice, 0, len(ids))
	for _, id := range ids {
		out = append(out, Choice{ID: id, Name: Name(id), Installed: installed(id, dirs)})
	}

	return out
}

// rank is id's place in All, or a place after every one of them for an ID it doesn't list.
func rank(id ID) int {
	all := All()
	if i := slices.Index(all, id); i >= 0 {
		return i
	}

	return len(all)
}

// installed reports whether the terminal id is installed, as Installed says: Terminal.app
// always is, iTerm2 and Warp are when their bundle is in one of dirs, and a terminal it
// doesn't know never is.
func installed(id ID, dirs []string) bool {
	var bundle string

	switch id {
	case TerminalApp:
		return true
	case ITerm2:
		bundle = itermBundle
	case Warp:
		bundle = warpBundle
	default:
		return false
	}

	for _, dir := range dirs {
		if dir == "" {
			continue
		}

		if info, err := os.Stat(filepath.Join(dir, bundle)); err == nil && info.IsDir() {
			return true
		}
	}

	return false
}
