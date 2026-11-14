package scripts

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/scripts/internal/queries"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// BackupPart is the user's own scripts' part of a backup of the user's setup: the scripts in
// portable form, and what importing them does. Core adapts it to a backup.Part; it is a type of its own, apart
// from the Service, so the window, which binds the Service, never reaches it.
type BackupPart struct {
	s *Service
}

// NewBackupPart returns the backup's part over s.
func NewBackupPart(s *Service) *BackupPart {
	return &BackupPart{s: s}
}

// PortableScript is one of the user's own scripts in portable form, as a backup holds it:
// everything Save stores but its ID.
type PortableScript struct {
	Name    string                  `json:"name"`
	Command string                  `json:"command"`
	Icon    Icon                    `json:"icon"`
	Scope   Scope                   `json:"scope"`
	Repos   []string                `json:"repos"`
	RunsIn  integrationprefs.RunsIn `json:"runsIn"`
	Confirm bool                    `json:"confirm"`
}

// PortableScripts is the user's own scripts in portable form, in their order.
type PortableScripts struct {
	Scripts []PortableScript `json:"scripts"`
}

// portable returns u in portable form.
func portable(u UserScript) PortableScript {
	return PortableScript{
		Name: u.Name, Command: u.Command, Icon: u.Icon, Scope: u.Scope, Repos: slices.Clone(u.Repos), RunsIn: u.RunsIn,
		Confirm: u.Confirm,
	}
}

// script returns p as a script to save, with no ID.
func (p PortableScript) script() UserScript {
	return UserScript{
		Name: p.Name, Command: p.Command, Icon: p.Icon, Scope: p.Scope, Repos: slices.Clone(p.Repos), RunsIn: p.RunsIn,
		Confirm: p.Confirm,
	}
}

// same reports whether u and p are the same script, whatever u's ID.
func (p PortableScript) same(u UserScript) bool {
	q := portable(u)

	return p.Name == q.Name && p.Command == q.Command && p.Icon == q.Icon && p.Scope == q.Scope &&
		slices.Equal(p.Repos, q.Repos) && p.RunsIn == q.RunsIn && p.Confirm == q.Confirm
}

// PortableScripts returns the user's own scripts in portable form, in their order.
func (bp *BackupPart) PortableScripts(ctx context.Context) (PortableScripts, error) {
	all, err := bp.s.yours.all(ctx)
	if err != nil {
		return PortableScripts{}, err
	}

	out := PortableScripts{Scripts: make([]PortableScript, 0, len(all))}
	for _, u := range all {
		out.Scripts = append(out.Scripts, portable(u))
	}

	return out, nil
}

// ScriptsPlan is what importing a backup's scripts does, as PlanBackup works it out.
type ScriptsPlan struct {
	backup.Plan

	mode backup.Mode

	// scripts are the file's scripts as Save would store them, in the file's order.
	scripts []PortableScript
}

// PlanBackup works out what importing in, a backup's scripts, does under mode, changing
// nothing. Each is checked as Save checks it, and one Save would refuse is left out, with
// a word on why. Merge skips a script identical to one here and appends the others in the
// file's order; Replace makes the scripts exactly the file's, in its order, keeping each
// one here identical to one of them as it is.
func (bp *BackupPart) PlanBackup(ctx context.Context, in PortableScripts, mode backup.Mode) (ScriptsPlan, error) {
	current, err := bp.s.yours.all(ctx)
	if err != nil {
		return ScriptsPlan{}, err
	}

	out := ScriptsPlan{mode: mode}
	matched := map[int64]bool{}

	for _, p := range in.Scripts {
		u, checkErr := checked(p.script())
		if checkErr != nil {
			out.AddNote(fmt.Sprintf("The script “%s” was left out. %s", p.Name, uierr.Message(checkErr)))

			continue
		}

		p = portable(u)
		out.scripts = append(out.scripts, p)

		change := backup.ChangeAdd

		if i := slices.IndexFunc(current, func(c UserScript) bool { return !matched[c.ID] && p.same(c) }); i >= 0 {
			matched[current[i].ID], change = true, backup.ChangeSame
		}

		out.Add(backup.Item{Label: p.Name, Change: change, Detail: p.Command})
	}

	if mode == backup.ModeReplace {
		planRemoves(&out.Plan, current, matched)
	}

	return out, nil
}

// planRemoves lists each of current that matched doesn't hold as one Replace deletes.
func planRemoves(p *backup.Plan, current []UserScript, matched map[int64]bool) {
	for _, c := range current {
		if !matched[c.ID] {
			p.Add(backup.Item{Label: c.Name, Change: backup.ChangeRemove, Detail: c.Command})
		}
	}
}

// ApplyBackup stores plan's scripts inside tx, an import's transaction, and returns what it
// changed. Merge appends each script not identical to one here, in order; Replace deletes
// each script here not identical to one of the file's, adds the rest, and puts them all in
// the file's order.
func (bp *BackupPart) ApplyBackup(ctx context.Context, tx *sql.Tx, plan ScriptsPlan) (backup.Count, error) {
	if bp.s.yours.q == nil {
		return backup.Count{}, ErrNotSetUp
	}

	q := bp.s.yours.q.WithTx(tx)

	rows, err := q.ListUserScripts(ctx)
	if err != nil {
		return backup.Count{}, fmt.Errorf("read your scripts: %w", err)
	}

	im := scriptImport{
		yours: bp.s.yours, q: q, rows: rows, replace: plan.mode == backup.ModeReplace, matched: map[int64]bool{},
	}

	if !im.replace {
		im.order = rows
	}

	for _, p := range plan.scripts {
		if err = im.place(ctx, p); err != nil {
			return backup.Count{}, err
		}
	}

	if im.replace {
		if err = im.dropUnmatched(ctx); err != nil {
			return backup.Count{}, err
		}
	}

	return im.count, renumber(ctx, q, im.order)
}

// scriptImport is an import of the user's scripts as it is applied: the rows stored before
// it, those an identical script of the file's matched, the order it stores, and what it
// did.
type scriptImport struct {
	yours   yourStore
	q       *queries.Queries
	rows    []queries.UserScript
	replace bool

	matched map[int64]bool
	order   []queries.UserScript
	count   backup.Count
}

// place stores p: the row it is identical to stays as it is (put in the file's order under
// Replace), and otherwise p is added at the end.
func (im *scriptImport) place(ctx context.Context, p PortableScript) error {
	i := slices.IndexFunc(im.rows, func(r queries.UserScript) bool { return !im.matched[r.ID] && p.same(fromRow(r)) })
	if i >= 0 {
		im.matched[im.rows[i].ID] = true
		im.count = im.count.With(backup.ChangeSame)

		if im.replace {
			im.order = append(im.order, im.rows[i])
		}

		return nil
	}

	row, err := im.yours.insert(ctx, im.q, p.script(), int64(len(im.rows)+len(im.order)))
	if err != nil {
		return err
	}

	im.count = im.count.With(backup.ChangeAdd)
	im.order = append(im.order, row)

	return nil
}

// dropUnmatched deletes each row no script of the file's matched, as Replace does.
func (im *scriptImport) dropUnmatched(ctx context.Context) error {
	for _, r := range im.rows {
		if im.matched[r.ID] {
			continue
		}

		if _, err := im.q.DeleteUserScript(ctx, r.ID); err != nil {
			return fmt.Errorf("delete your script %d: %w", r.ID, err)
		}

		im.count = im.count.With(backup.ChangeRemove)
	}

	return nil
}
