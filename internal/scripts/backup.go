package scripts

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/scripts/internal/queries"
	"github.com/mrz1836/nilbox/internal/textutil"
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
// everything Save stores but its ID, which is this Mac's alone.
type PortableScript struct {
	// UUID names the script for good (UserScript.UUID); a file an older build wrote has
	// none.
	UUID string `json:"uuid,omitempty"`

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
		UUID: u.UUID, Name: u.Name, Command: u.Command, Icon: u.Icon, Scope: u.Scope, Repos: slices.Clone(u.Repos),
		RunsIn: u.RunsIn, Confirm: u.Confirm,
	}
}

// script returns p as a script to save, with no ID.
func (p PortableScript) script() UserScript {
	return UserScript{
		UUID: p.UUID, Name: p.Name, Command: p.Command, Icon: p.Icon, Scope: p.Scope, Repos: slices.Clone(p.Repos),
		RunsIn: p.RunsIn, Confirm: p.Confirm,
	}
}

// same reports whether u and p are the same script, whatever u's ID and UUID.
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
// a word on why. A script with the UUID of one here is that script: the same, it stays as
// it is; changed since, Replace saves the file's over it, keeping its UUID, and Merge,
// which never changes the user's own, adds the file's as a copy. Otherwise Merge skips a
// script identical to one here and appends the others in the file's order; Replace makes
// the scripts exactly the file's, in its order, keeping each one here identical to one of
// them as it is.
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

		m := matchScript(current, matched, p, mode == backup.ModeReplace)
		item := backup.Item{Label: p.Name, Change: m.change, Detail: p.Command}

		if m.change == backup.ChangeUpdate && current[m.at].Name != p.Name {
			item.Detail = "was “" + current[m.at].Name + "”"
		}

		out.Add(item)
	}

	if mode == backup.ModeReplace {
		planRemoves(&out.Plan, current, matched)
	}

	return out, nil
}

// scriptMatch is what one of a file's scripts is to the scripts here: the one at at it
// matches, kept (ChangeSame) or saved over (ChangeUpdate), or none (at -1), as it is added
// (ChangeAdd).
type scriptMatch struct {
	at     int
	change backup.Change
}

// matchScript matches p, one of a file's scripts, to a script of current that matched
// doesn't hold yet, which it then marks matched, as a plan and its apply both do. The one
// with p's UUID is p, kept when identical and, under replace, saved over when not; under
// Merge, one changed since isn't matched, and p is added as a copy with a UUID of its own.
// Otherwise p matches the first identical script, whatever its UUID, or none.
func matchScript(current []UserScript, matched map[int64]bool, p PortableScript, replace bool) scriptMatch {
	if u, ok := textutil.CanonicalUUID(strings.TrimSpace(p.UUID)); ok {
		i := slices.IndexFunc(current, func(c UserScript) bool { return !matched[c.ID] && c.UUID == u })

		switch {
		case i >= 0 && p.same(current[i]):
			matched[current[i].ID] = true

			return scriptMatch{at: i, change: backup.ChangeSame}
		case i >= 0 && replace:
			matched[current[i].ID] = true

			return scriptMatch{at: i, change: backup.ChangeUpdate}
		}
	}

	if i := slices.IndexFunc(current, func(c UserScript) bool { return !matched[c.ID] && p.same(c) }); i >= 0 {
		matched[current[i].ID] = true

		return scriptMatch{at: i, change: backup.ChangeSame}
	}

	return scriptMatch{at: -1, change: backup.ChangeAdd}
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
// changed, matching each of the file's scripts to the scripts tx holds as PlanBackup does.
// Merge appends each script that matches none here, in order; Replace saves each one that
// matches a script here changed since over it, deletes each script here none matched, adds
// the rest, and puts them all in the file's order.
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

	for _, r := range rows {
		im.current = append(im.current, fromRow(r))
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
// it, and the scripts they are, those one of the file's matched, the order it stores, and
// what it did.
type scriptImport struct {
	yours   yourStore
	q       *queries.Queries
	rows    []queries.UserScript
	current []UserScript
	replace bool

	matched map[int64]bool
	order   []queries.UserScript
	count   backup.Count
}

// place stores p as matchScript matches it: the row it matches stays as it is, or is saved
// over with p under Replace (put in the file's order under Replace either way), and
// otherwise p is added at the end.
func (im *scriptImport) place(ctx context.Context, p PortableScript) error {
	m := matchScript(im.current, im.matched, p, im.replace)

	switch m.change {
	case backup.ChangeSame:
		im.count = im.count.With(backup.ChangeSame)

		if im.replace {
			im.order = append(im.order, im.rows[m.at])
		}

		return nil
	case backup.ChangeUpdate:
		u := p.script()
		u.ID = im.rows[m.at].ID

		if _, err := im.yours.updateWith(ctx, im.q, u); err != nil {
			return err
		}

		im.count = im.count.With(backup.ChangeUpdate)
		im.order = append(im.order, im.rows[m.at])

		return nil
	case backup.ChangeAdd, backup.ChangeRemove:
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
