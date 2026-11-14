package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/scripts"
	"github.com/mrz1836/nilbox/internal/settings"
)

// newBackup builds the service that backs the user's setup up and brings it back, over
// the owners of each category: the settings (and, apart, the repositories they hold), the
// prompt library, the launch defaults, and the user's own scripts.
func newBackup(pool *sql.DB, c *Core, promptStore *prompts.Store, opts Options, logger *slog.Logger) *backup.Service {
	prefs := settings.NewBackupPart(c.Settings)

	return backup.NewService(backup.Config{
		Pool: pool,
		Parts: map[backup.Section]backup.Part{
			backup.SectionSettings:     backupSettings{settings: prefs},
			backup.SectionRepositories: backupRepos{settings: prefs},
			backup.SectionPrompts:      backupPrompts{prompts: prompts.NewBackupPart(c.Prompts)},
			backup.SectionLaunchDefaults: backupDefaults{
				defaults: launchdefaults.NewBackupPart(c.LaunchDefaults), prompts: promptStore, app: opts.Version,
				clock: clock.Or(opts.Clock),
			},
			backup.SectionScripts: backupScripts{scripts: scripts.NewBackupPart(c.Scripts)},
		},
		Dir: opts.Paths.Backups, App: opts.Version, Home: opts.Home, Clock: opts.Clock, Logger: logger,
	})
}

// backupSettings adapts the settings to a backup's settings section.
type backupSettings struct {
	settings *settings.BackupPart
}

// Export implements backup.Part.
func (b backupSettings) Export(ctx context.Context) (json.RawMessage, int, error) {
	p, err := b.settings.Portable(ctx)
	if err != nil {
		return nil, 0, err
	}

	raw, err := backup.EncodeSection(p)

	return raw, b.settings.PortableItems(p), err
}

// Prepare implements backup.Part.
func (b backupSettings) Prepare(ctx context.Context, raw json.RawMessage, _ backup.How) (backup.Prepared, error) {
	var p settings.Portable
	if err := decodeSection(raw, &p, "settings"); err != nil {
		return backup.Prepared{}, err
	}

	plan, err := b.settings.PlanPortable(ctx, p)
	if err != nil {
		return backup.Prepared{}, err
	}

	return backup.Prepared{Plan: plan.Plan, State: plan}, nil
}

// Apply implements backup.Part.
func (b backupSettings) Apply(ctx context.Context, tx *sql.Tx, p backup.Prepared, _ backup.Links) (backup.Count, error) {
	return b.settings.ApplyPortable(ctx, tx, stateOf[settings.PortablePlan](p))
}

// Done implements backup.Part.
func (b backupSettings) Done(ctx context.Context, p backup.Prepared) {
	b.settings.Announce(ctx, stateOf[settings.PortablePlan](p))
}

// backupRepos adapts the settings' repositories to a backup's repositories section.
type backupRepos struct {
	settings *settings.BackupPart
}

// Export implements backup.Part.
func (b backupRepos) Export(ctx context.Context) (json.RawMessage, int, error) {
	r, err := b.settings.PortableRepos(ctx)
	if err != nil {
		return nil, 0, err
	}

	raw, err := backup.EncodeSection(r)

	return raw, 1 + len(*r.Locations) + len(*r.Pinned) + len(*r.Muted), err
}

// Prepare implements backup.Part.
func (b backupRepos) Prepare(ctx context.Context, raw json.RawMessage, how backup.How) (backup.Prepared, error) {
	var r settings.PortableRepos
	if err := decodeSection(raw, &r, "repositories"); err != nil {
		return backup.Prepared{}, err
	}

	plan, err := b.settings.PlanRepos(ctx, r, how.Mode)
	if err != nil {
		return backup.Prepared{}, err
	}

	return backup.Prepared{Plan: plan.Plan, State: plan}, nil
}

// Apply implements backup.Part.
func (b backupRepos) Apply(ctx context.Context, tx *sql.Tx, p backup.Prepared, _ backup.Links) (backup.Count, error) {
	return b.settings.ApplyPortable(ctx, tx, stateOf[settings.PortablePlan](p))
}

// Done implements backup.Part.
func (b backupRepos) Done(ctx context.Context, p backup.Prepared) {
	b.settings.Announce(ctx, stateOf[settings.PortablePlan](p))
}

// backupPrompts adapts the prompt library to a backup's prompts section, which is a
// prompt file.
type backupPrompts struct {
	prompts *prompts.BackupPart
}

// Export implements backup.Part.
func (b backupPrompts) Export(ctx context.Context) (json.RawMessage, int, error) {
	bundle, err := b.prompts.Bundle(ctx)
	if err != nil {
		return nil, 0, err
	}

	data, err := prompts.EncodeBundle(bundle)

	return data, len(bundle.Prompts), err
}

// Prepare implements backup.Part. Under Replace, a prompt a launch default names is kept
// unless the import replaces the launch defaults too.
func (b backupPrompts) Prepare(ctx context.Context, raw json.RawMessage, how backup.How) (backup.Prepared, error) {
	bundle, err := prompts.DecodeBundle(raw)
	if err != nil {
		return backup.Prepared{}, err
	}

	clears := how.Mode == backup.ModeReplace && how.Takes(backup.SectionLaunchDefaults)

	plan, err := b.prompts.PlanBackup(ctx, bundle, how.Mode, clears)
	if err != nil {
		return backup.Prepared{}, err
	}

	return backup.Prepared{Plan: plan.Plan, Prompts: plan.Prompts, State: plan}, nil
}

// Apply implements backup.Part.
func (b backupPrompts) Apply(ctx context.Context, tx *sql.Tx, p backup.Prepared, links backup.Links) (backup.Count, error) {
	return b.prompts.ApplyBackup(ctx, tx, stateOf[prompts.BackupPlan](p), links)
}

// Done implements backup.Part.
func (b backupPrompts) Done(ctx context.Context, _ backup.Prepared) {
	b.prompts.BackupDone(ctx)
}

// backupDefaults adapts the launch defaults to a backup's launch defaults section, which
// is their copy's format, naming prompts by nickname: each is found among the prompts the
// import brings, else in the library.
type backupDefaults struct {
	defaults *launchdefaults.BackupPart
	prompts  *prompts.Store
	app      string
	clock    clock.Clock
}

// Export implements backup.Part.
func (b backupDefaults) Export(ctx context.Context) (json.RawMessage, int, error) {
	kept, err := b.defaults.Kept(ctx)
	if err != nil {
		return nil, 0, err
	}

	data, err := launchdefaults.EncodeKept(kept, b.app, b.clock.Now())

	return data, len(kept), err
}

// Prepare implements backup.Part.
func (b backupDefaults) Prepare(ctx context.Context, raw json.RawMessage, how backup.How) (backup.Prepared, error) {
	kept, err := launchdefaults.DecodeKept(raw)
	if err != nil {
		return backup.Prepared{}, err
	}

	lib, err := b.prompts.All(ctx)
	if err != nil {
		return backup.Prepared{}, err
	}

	known := func(nickname string, kind item.Kind) bool {
		if k, ok := how.Prompts[nickname]; ok {
			return !k.Reply && prompts.AppliesTo(k.AppliesTo).Covers(kind)
		}

		// Replace leaves the library only the prompts the file brings.
		if how.Prompts != nil && how.Mode == backup.ModeReplace {
			return false
		}

		p, ok := named(lib, nickname)

		return ok && launchable(p, kind)
	}

	plan, err := b.defaults.PlanBackup(ctx, kept, how.Mode, known)
	if err != nil {
		return backup.Prepared{}, err
	}

	return backup.Prepared{Plan: plan.Plan, State: plan}, nil
}

// Clear implements backup.Clearer.
func (b backupDefaults) Clear(ctx context.Context, tx *sql.Tx) error {
	return b.defaults.ClearTx(ctx, tx)
}

// Apply implements backup.Part.
func (b backupDefaults) Apply(ctx context.Context, tx *sql.Tx, p backup.Prepared, links backup.Links) (backup.Count, error) {
	st := b.prompts.WithTx(tx)

	find := func(ctx context.Context, nickname string, kind item.Kind) (int64, bool, error) {
		if id, ok := links[nickname]; ok {
			found, err := st.Lookup(ctx, id)
			if err != nil {
				return 0, false, err
			}

			return id, launchable(found, kind), nil
		}

		lib, err := st.All(ctx)
		if err != nil {
			return 0, false, err
		}

		found, ok := named(lib, nickname)

		return found.ID, ok && launchable(found, kind), nil
	}

	return b.defaults.ApplyBackup(ctx, tx, stateOf[launchdefaults.BackupPlan](p), find)
}

// Done implements backup.Part.
func (b backupDefaults) Done(ctx context.Context, _ backup.Prepared) {
	b.defaults.BackupDone(ctx)
}

// backupScripts adapts the user's own scripts to a backup's scripts section.
type backupScripts struct {
	scripts *scripts.BackupPart
}

// Export implements backup.Part.
func (b backupScripts) Export(ctx context.Context) (json.RawMessage, int, error) {
	all, err := b.scripts.PortableScripts(ctx)
	if err != nil {
		return nil, 0, err
	}

	raw, err := backup.EncodeSection(all)

	return raw, len(all.Scripts), err
}

// Prepare implements backup.Part.
func (b backupScripts) Prepare(ctx context.Context, raw json.RawMessage, how backup.How) (backup.Prepared, error) {
	var in scripts.PortableScripts
	if err := decodeSection(raw, &in, "scripts"); err != nil {
		return backup.Prepared{}, err
	}

	plan, err := b.scripts.PlanBackup(ctx, in, how.Mode)
	if err != nil {
		return backup.Prepared{}, err
	}

	return backup.Prepared{Plan: plan.Plan, State: plan}, nil
}

// Apply implements backup.Part.
func (b backupScripts) Apply(ctx context.Context, tx *sql.Tx, p backup.Prepared, _ backup.Links) (backup.Count, error) {
	return b.scripts.ApplyBackup(ctx, tx, stateOf[scripts.ScriptsPlan](p))
}

// Done implements backup.Part. Repo scripts reads the scripts as it lists a clone's, so
// nothing needs telling.
func (backupScripts) Done(context.Context, backup.Prepared) {}

// decodeSection reads a backup's section into v, worded as the section what.
func decodeSection(raw json.RawMessage, v any, what string) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%w: its %s can't be read: %w", backup.ErrNotBackup, what, err)
	}

	return nil
}

// stateOf returns the part's own state its Prepare kept, the zero T when there is none.
func stateOf[T any](p backup.Prepared) T {
	state, _ := p.State.(T) // a part always gets back the state its own Prepare kept

	return state
}

// named returns the prompt in lib whose nickname is nickname, ignoring case, as the
// library's nicknames are unique.
func named(lib []prompts.Prompt, nickname string) (prompts.Prompt, bool) {
	for _, p := range lib {
		if strings.EqualFold(p.Nickname, nickname) {
			return p, true
		}
	}

	return prompts.Prompt{}, false
}

// launchable reports whether p can be a launch default's prompt for kind: an agent prompt
// that applies to it.
func launchable(p prompts.Prompt, kind item.Kind) bool {
	return !p.IsReply() && p.AppliesTo.Covers(kind)
}
