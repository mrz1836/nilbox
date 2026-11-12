package localrepos

import (
	"context"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// CloneRoot reads the clone root as set now, lists the clones directly under it, reading
// no git, and reads which GitHub repository a clone's origin is, if any: what a page that
// takes a clone by its folder reads. Core adapts the settings and Clones to it.
type CloneRoot interface {
	Root(ctx context.Context) (string, error)
	Dirs(ctx context.Context, root string) ([]string, error)
	Origin(ctx context.Context, dir string) (name repo.Name, onGitHub bool, err error)
}

// Listing is the clone root as set when it was read, and the clones under it.
type Listing struct {
	Root string
	Dirs []string
}

// Picked is the clone a request names: its folder, absolute, as the listing holds it, and
// its origin's repository on GitHub, or the zero Name.
type Picked struct {
	Dir  string
	Name repo.Name
}

// ByFolder finds the clone a page's request names by its folder, as the page shows it,
// with ~: only a clone directly under the clone root is one. Hooks and Repo scripts share
// it, each with its own errors.
type ByFolder struct {
	// Clones reads the clone root, the clones under it, and their origins.
	Clones CloneRoot

	// Home is the user's home folder, which ~ stands for.
	Home string

	// Invalid is the page's error for a request it can't read, which a folder that can't
	// be read as a path is wrapped in, and NoClone its error for a folder that isn't a
	// clone under the clone root. Both are required.
	Invalid, NoClone error

	// Known, when set, returns the repository the page already holds for the clone at dir
	// (the zero Name for an origin that isn't on GitHub) and whether it holds one, as the
	// Hooks page's last scan does; a clone it doesn't hold has its origin read.
	Known func(dir string) (repo.Name, bool)
}

// List reads the clone root as set now and lists the clones under it.
func (b ByFolder) List(ctx context.Context) (Listing, error) {
	root, err := b.Clones.Root(ctx)
	if err != nil {
		return Listing{}, fmt.Errorf("read the clone root: %w", err)
	}

	dirs, err := b.Clones.Dirs(ctx, root)
	if err != nil {
		return Listing{}, fmt.Errorf("list the clones: %w", err)
	}

	return Listing{Root: root, Dirs: dirs}, nil
}

// At lists the clones under the clone root as set now and finds the one shown names
// (see Pick).
func (b ByFolder) At(ctx context.Context, shown string) (Picked, error) {
	l, err := b.List(ctx)
	if err != nil {
		return Picked{}, err
	}

	return b.Pick(ctx, shown, l)
}

// Pick finds the clone shown names among l's: the folder itself, else one that is the
// same through a link. Its repository is the one Known holds for it, else its origin's.
// A folder that isn't a clone under l's root is refused in words the window shows: "~/x
// isn't a clone in ~/projects."
func (b ByFolder) Pick(ctx context.Context, shown string, l Listing) (Picked, error) {
	dir, err := paths.ExpandHome(shown, b.Home)
	if err != nil {
		return Picked{}, fmt.Errorf("%w: the folder %q: %w", b.Invalid, shown, err)
	}

	i := slices.Index(l.Dirs, dir)
	if i < 0 {
		i = slices.IndexFunc(l.Dirs, func(d string) bool { return paths.Same(d, dir) })
	}

	if i < 0 {
		return Picked{}, uierr.New(fmt.Sprintf("%s isn't a clone in %s.", paths.Tilde(dir, b.Home),
			paths.Tilde(l.Root, b.Home)), fmt.Errorf("%w: %s", b.NoClone, dir))
	}

	p := Picked{Dir: l.Dirs[i]}

	if b.Known != nil {
		if name, known := b.Known(p.Dir); known {
			p.Name = name

			return p, nil
		}
	}

	if name, onHub, originErr := b.Clones.Origin(ctx, p.Dir); originErr == nil && onHub {
		p.Name = name
	}

	return p, nil
}
