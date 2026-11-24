package pushwatch

import (
	"context"
	"log/slog"
	"time"

	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/repo"
)

// clone is a clone the watch reads refs in: a folder git can run in, the common git
// folder that holds the refs of the clone and of every worktree it has, and whether that
// folder keeps its refs in reftable rather than a file each.
type clone struct {
	dir      string
	common   string
	reftable bool
}

// indexed is what the index read of one common git folder: the clone, its config file as
// it was when its origin was read, and the GitHub repository its origin points at.
type indexed struct {
	clone

	config   fileSig
	origin   repo.Name
	onGitHub bool
}

// cloneIndex maps each GitHub repository to the clones whose origin it is. Only the
// Watcher's Run reads and writes it.
type cloneIndex struct {
	// known holds each common git folder read, by its path.
	known map[string]indexed

	// byRepo holds the clones of each repository, by repo.Name.Key.
	byRepo map[string][]clone

	// builtAt is when the clones were last listed, zero before they were.
	builtAt time.Time
}

// newCloneIndex returns an index that has listed no clone.
func newCloneIndex() *cloneIndex {
	return &cloneIndex{known: map[string]indexed{}, byRepo: map[string][]clone{}}
}

// of returns the clones whose origin is the repository name.
func (x *cloneIndex) of(name repo.Name) []clone {
	return x.byRepo[name.Key()]
}

// due reports whether the clones are to be listed again at now.
func (x *cloneIndex) due(now time.Time, every time.Duration) bool {
	return x.builtAt.IsZero() || !now.Before(x.builtAt.Add(every))
}

// rebuild lists the clones again, at now, and reads the origin of each one met for the
// first time or whose config file changed since; a clone listed twice, as a linked
// worktree listed beside its clone is, counts once, by its common git folder. A list that
// can't be read keeps the last index, and so does a rebuild cut short by ctx; a clone
// whose origin can't be read is left out until a rebuild reads it.
func (x *cloneIndex) rebuild(ctx context.Context, clones Clones, origins Origins, now time.Time, logger *slog.Logger) {
	x.builtAt = now

	dirs, err := clones.CloneDirs(ctx)
	if err != nil {
		if ctx.Err() == nil {
			logger.WarnContext(ctx, "could not list the clones to watch for pushes", "error", err)
		}

		return
	}

	entries, failed, lastErr := x.readAll(ctx, origins, dirs)
	if ctx.Err() != nil {
		return
	}

	if failed > 0 {
		logger.DebugContext(ctx, "could not read the origin of some clones to watch for pushes", "clones", failed,
			"error", lastErr)
	}

	known := make(map[string]indexed, len(entries))
	byRepo := map[string][]clone{}

	for _, entry := range entries {
		known[entry.common] = entry

		if entry.onGitHub {
			key := entry.origin.Key()
			byRepo[key] = append(byRepo[key], entry.clone)
		}
	}

	x.known, x.byRepo = known, byRepo
}

// readAll reads each clone of dirs, in their order, once per common git folder, and
// returns what it read, how many it couldn't read, and the last of their errors. It stops
// once ctx ends.
func (x *cloneIndex) readAll(ctx context.Context, origins Origins, dirs []string) ([]indexed, int, error) {
	entries := make([]indexed, 0, len(dirs))
	seen := make(map[string]bool, len(dirs))

	var (
		failed  int
		lastErr error
	)

	for _, dir := range dirs {
		if ctx.Err() != nil {
			break
		}

		common := localrepos.CommonDir(dir)
		if common == "" || seen[common] {
			continue
		}

		seen[common] = true

		entry, err := x.read(ctx, origins, dir, common)
		if err != nil {
			failed, lastErr = failed+1, err

			continue
		}

		entries = append(entries, entry)
	}

	return entries, failed, lastErr
}

// read returns what the index knows of the clone at dir, whose common git folder is
// common: what it read before while the config file is the same, so no git runs, and
// otherwise its origin read again. Whether the folder keeps its refs in reftable is
// looked at each time, a stat.
func (x *cloneIndex) read(ctx context.Context, origins Origins, dir, common string) (indexed, error) {
	config := statFile(configPath(common))
	reftable := statFile(tablesPath(common)).exists

	if last, ok := x.known[common]; ok && config.exists && last.config == config {
		last.reftable = reftable

		return last, nil
	}

	origin, onGitHub, err := origins.Origin(ctx, dir)
	if err != nil {
		return indexed{}, err
	}

	return indexed{
		dir: dir, common: common, reftable: reftable, config: config, origin: origin, onGitHub: onGitHub,
	}, nil
}
