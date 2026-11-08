package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/store"
)

// kept is what the app keeps for the user outside its database, so it outlives a
// database that is set aside, damaged, or deleted: the prompt library and the launch
// defaults, each beside the plain-file copy the app keeps of it. Agent profiles are not
// among them: discovery finds them in the home directory on every start, and nothing in
// the app changes one.
type kept struct {
	prompts      *prompts.Store
	promptFile   *prompts.LibraryFile
	defaults     *launchdefaults.Store
	defaultsFile *launchdefaults.File
	logger       *slog.Logger
}

// newKept returns the kept stores over pool, with their copies at the paths opts gives.
func newKept(pool *sql.DB, opts Options, logger *slog.Logger) *kept {
	return &kept{
		prompts:      prompts.NewStore(pool, opts.Clock),
		promptFile:   prompts.NewLibraryFile(opts.Paths.Prompts, opts.Version, opts.Clock),
		defaults:     launchdefaults.NewStore(pool, opts.Clock),
		defaultsFile: launchdefaults.NewFile(opts.Paths.LaunchDefaults, opts.Version, opts.Clock),
		logger:       logger,
	}
}

// restored is what restore brought back.
type restored struct {
	prompts  prompts.Restored
	defaults int
}

// info returns what the window tells the user about what was brought back, or nil when
// nothing was.
func (r restored) info(home string) *appinfo.Restored {
	if r.prompts.From == "" || r.prompts.Count+r.defaults == 0 {
		return nil
	}

	return &appinfo.Restored{
		Prompts: r.prompts.Count, LaunchDefaults: r.defaults, From: paths.Tilde(r.prompts.From, home),
	}
}

// restore brings back the prompt library when the database holds a new one, as after it
// was set aside, damaged, or deleted, and then the launch defaults from the same place,
// matched to the restored prompts: from the database set aside, when reset says one was,
// or else from the copies kept beside the database. What cannot be brought back is
// logged, never fatal: the app starts with the starters, and the user can still restore
// the prompts from a backup by hand.
func (k *kept) restore(ctx context.Context, reset *store.Reset) restored {
	setAside := ""
	if reset != nil {
		setAside = reset.Path
	}

	var out restored

	var err error
	if out.prompts, err = prompts.Restore(ctx, k.prompts, setAside, k.promptFile); err != nil {
		k.logger.WarnContext(ctx, "could not bring back all of the prompt library", "error", err)
	}

	if out.prompts.From == "" {
		return out
	}

	if out.defaults, err = k.restoreDefaults(ctx, out.prompts); err != nil {
		k.logger.WarnContext(ctx, "could not bring back the launch defaults", "error", err)
	}

	k.logger.InfoContext(ctx, "brought back the prompt library and launch defaults",
		"prompts", out.prompts.Count, "launch_defaults", out.defaults, "from", out.prompts.From)

	return out
}

// restoreDefaults brings back the launch defaults from where the prompts came from, each
// prompt they name matched to its restored prompt by nickname, and returns how many came
// back.
func (k *kept) restoreDefaults(ctx context.Context, from prompts.Restored) (int, error) {
	list, err := k.readDefaults(ctx, from)
	if err != nil || len(list) == 0 {
		return 0, err
	}

	return launchdefaults.Restore(ctx, k.defaults, list, func(nickname string) (int64, bool) {
		id, ok := from.IDs[nickname]

		return id, ok
	})
}

// readDefaults reads the launch defaults kept where the prompts came from: the database
// set aside, whose prompt IDs from.Nicknames names, or else, and when that database has
// none it can read, the launch defaults' copy. A copy that is not there holds none.
func (k *kept) readDefaults(ctx context.Context, from prompts.Restored) ([]launchdefaults.Kept, error) {
	var snapErr error

	if from.Nicknames != nil {
		list, err := launchdefaults.ReadSnapshot(ctx, from.From, func(id int64) string { return from.Nicknames[id] })
		if err == nil {
			return list, nil
		}

		snapErr = fmt.Errorf("read the launch defaults in the database set aside: %w", err)
	}

	list, err := k.defaultsFile.Load()

	switch {
	case err == nil:
		return list, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, snapErr
	default:
		return nil, errors.Join(snapErr, fmt.Errorf("read the copy of the launch defaults: %w", err))
	}
}

// keep writes each copy that is missing, as on the first start of a build that keeps
// it, or every copy when the data was just restored. A copy is otherwise left as the last
// change wrote it, so a database that came back without the user's data never overwrites
// the copy that still holds it.
func (k *kept) keep(ctx context.Context, restored bool) {
	k.keepFile(ctx, k.promptFile.Path(), restored, "prompt library", func() error {
		return k.promptFile.Save(ctx, k.prompts)
	})
	k.keepFile(ctx, k.defaultsFile.Path(), restored, "launch defaults", func() error {
		return k.defaultsFile.Save(ctx, k.defaults, k.prompts)
	})
}

// keepFile runs save when the copy at path is missing, or restored is set. A failure is
// logged: the copy is written again after the next change.
func (k *kept) keepFile(ctx context.Context, path string, restored bool, what string, save func() error) {
	_, err := os.Stat(path)
	if err == nil && !restored {
		return
	}

	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		k.logger.WarnContext(ctx, "could not check the copy of the "+what, "path", path, "error", err)

		return
	}

	if err = save(); err != nil {
		k.logger.WarnContext(ctx, "could not save the copy of the "+what, "path", path, "error", err)
	}
}

// saveDefaults rewrites the launch defaults' copy after a change to the prompt library,
// since it names prompts by nickname and one may have been renamed. A failure is logged.
func (k *kept) saveDefaults(ctx context.Context) {
	if err := k.defaultsFile.Save(ctx, k.defaults, k.prompts); err != nil {
		k.logger.WarnContext(ctx, "could not save the copy of the launch defaults",
			"path", k.defaultsFile.Path(), "error", err)
	}
}
