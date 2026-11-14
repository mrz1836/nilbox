package launchdefaults

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// BackupPart is the launch defaults' part of a backup of the user's setup: every org's and
// repository's launch defaults in portable form, and what importing them does. Core adapts it to a backup.Part; it is a type of its own, apart
// from the Service, so the window, which binds the Service, never reaches it.
type BackupPart struct {
	s *Service
}

// NewBackupPart returns the backup's part over s.
func NewBackupPart(s *Service) *BackupPart {
	return &BackupPart{s: s}
}

// Kept returns every stored launch default as the app keeps them outside its database,
// naming each default prompt by its nickname.
func (bp *BackupPart) Kept(ctx context.Context) ([]Kept, error) {
	s := bp.s

	return keepAll(ctx, s.store, s.prompts)
}

// Known reports whether a prompt nickname will name an agent prompt for kind once an
// import is applied.
type Known func(nickname string, kind item.Kind) bool

// PromptFinder finds, inside an import's transaction, the agent prompt for kind a nickname
// names, and whether there is one.
type PromptFinder func(ctx context.Context, nickname string, kind item.Kind) (int64, bool, error)

// BackupPlan is what importing a backup's launch defaults does, as PlanBackup works it out.
type BackupPlan struct {
	backup.Plan

	// rows are the file's launch defaults as this build would store them, one per org or
	// repository, in the file's order.
	rows []Kept
}

// PlanBackup works out what importing kept, a backup's launch defaults, does under mode,
// changing nothing. Under Merge each of the file's rows replaces this Mac's row for the
// same org or repository, and the other rows stay; under Replace the rows are exactly the
// file's. A setting this build would refuse is left out, a prompt known doesn't find
// inherits, and a row for no valid org or repository, or one that sets nothing, is left
// out, each with a note. A profile is kept by name even when no profile has it here, as a
// restore keeps it: the launch menu warns that it is missing.
func (bp *BackupPart) PlanBackup(ctx context.Context, kept []Kept, mode backup.Mode, known Known) (BackupPlan, error) {
	current, err := bp.Kept(ctx)
	if err != nil {
		return BackupPlan{}, err
	}

	here := map[string]Kept{}
	for _, k := range current {
		here[keyOf(k)] = k
	}

	var out BackupPlan

	seen := map[string]bool{}

	for _, raw := range kept {
		k, ok := out.planRow(raw, known, seen)
		if !ok {
			continue
		}

		k, change := against(here, k)
		out.rows = append(out.rows, k)
		out.Add(backup.Item{Label: k.target(), Change: change})
	}

	if mode == backup.ModeReplace {
		out.planRemoves(current, seen)
	}

	return out, nil
}

// planRemoves lists each of current, the rows here, for an org or repository the file's
// rows, seen, don't hold, as one Replace deletes.
func (p *BackupPlan) planRemoves(current []Kept, seen map[string]bool) {
	for _, k := range current {
		if !seen[keyOf(k)] {
			p.Add(backup.Item{Label: k.target(), Change: backup.ChangeRemove})
		}
	}
}

// planRow works out one of the file's rows: cleaned, its prompts that known doesn't find
// inheriting, and whether it is imported at all, noting what is left out and why. seen
// holds the orgs and repositories already imported, and gets this row's.
func (p *BackupPlan) planRow(raw Kept, known Known, seen map[string]bool) (Kept, bool) {
	k, notes, ok := cleanKept(raw)
	for _, note := range notes {
		p.AddNote(note)
	}

	if !ok {
		return Kept{}, false
	}

	k.PRPrompt = resolvePrompt(&p.Plan, k, k.PRPrompt, item.KindPR, known)
	k.IssuePrompt = resolvePrompt(&p.Plan, k, k.IssuePrompt, item.KindIssue, known)

	switch _, sets := restorableWith(k, promptMark(k.PRPrompt), promptMark(k.IssuePrompt)); {
	case seen[keyOf(k)]:
		p.AddNote(fmt.Sprintf("The file holds the launch defaults for %s twice; the first was imported.", k.target()))

		return Kept{}, false
	case !sets:
		p.AddNote(fmt.Sprintf("The launch defaults for %s were left out: they set nothing.", k.target()))

		return Kept{}, false
	}

	seen[keyOf(k)] = true

	return k, true
}

// against returns k with the casing of the row here for its org or repository, when there
// is one, and what importing it does to that row.
func against(here map[string]Kept, k Kept) (Kept, backup.Change) {
	was, ok := here[keyOf(k)]
	if !ok {
		return k, backup.ChangeAdd
	}

	k.Owner, k.Name = was.Owner, was.Name
	if sameKept(was, k) {
		return k, backup.ChangeSame
	}

	return k, backup.ChangeUpdate
}

// ClearTx deletes every stored launch default inside tx, an import's transaction, as
// Replace does before anything is applied.
func (bp *BackupPart) ClearTx(ctx context.Context, tx *sql.Tx) error {
	if err := bp.s.store.WithTx(tx).q.DeleteAllLaunchDefaults(ctx); err != nil {
		return fmt.Errorf("remove the launch defaults: %w", err)
	}

	return nil
}

// ApplyBackup stores plan's rows inside tx, an import's transaction, each replacing the
// row stored for the same org or repository, its prompts found through find, and returns
// what it changed. A row whose prompts find doesn't find, and that then sets nothing,
// removes the row stored for its org or repository: it inherits every setting.
func (bp *BackupPart) ApplyBackup(ctx context.Context, tx *sql.Tx, plan BackupPlan, find PromptFinder) (backup.Count, error) {
	st := bp.s.store.WithTx(tx)

	for _, k := range plan.rows {
		if err := applyRow(ctx, st, k, find); err != nil {
			return backup.Count{}, err
		}
	}

	return plan.Count, nil
}

// applyRow stores k in st, its prompts found through find.
func applyRow(ctx context.Context, st *Store, k Kept, find PromptFinder) error {
	pr, err := findPrompt(ctx, find, k.PRPrompt, item.KindPR)
	if err != nil {
		return err
	}

	issue, err := findPrompt(ctx, find, k.IssuePrompt, item.KindIssue)
	if err != nil {
		return err
	}

	in, sets := restorableWith(k, pr, issue)
	if sets {
		_, err = st.save(ctx, in.input, in.checked)

		return err
	}

	if err = st.remove(ctx, in.input.Owner, in.input.Name); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	return nil
}

// BackupDone rewrites the copy of the launch defaults once a backup's are committed.
func (bp *BackupPart) BackupDone(ctx context.Context) {
	bp.s.saved(ctx)
}

// cleanKept returns kept defaults as this build would store them, with a note for each
// setting it left out, and whether they are for a valid org or repository. Their prompts
// stay named by nickname.
func cleanKept(k Kept) (Kept, []string, bool) {
	var notes []string

	k.Owner, k.Name = strings.TrimSpace(k.Owner), strings.TrimSpace(k.Name)
	where := k.target()

	if !repo.ValidOwner(k.Owner) || (k.Name != "" && !repo.ValidRepo(k.Name)) {
		return Kept{}, []string{fmt.Sprintf("The launch defaults for %q were left out: that isn't an org or a repository.",
			where)}, false
	}

	if k.Agent = agent.ID(strings.TrimSpace(string(k.Agent))); k.Agent != agent.Inherit && !agent.Known(k.Agent) {
		notes = append(notes, fmt.Sprintf("%s: its coding agent, %q, was left out: this version doesn't know it.", where,
			k.Agent))
		k.Agent = agent.Inherit
	}

	if k.Effort = strings.TrimSpace(k.Effort); agent.ValidateEffortAny(k.Effort) != nil {
		notes = append(notes, fmt.Sprintf("%s: its effort, %q, was left out: it isn't one of the levels.", where, k.Effort))
		k.Effort = ""
	}

	profiles, checked := restorableAgents(k)

	notes = append(notes, dropped(where, "profile", trimmed(k.Profiles), profiles)...)
	notes = append(notes, dropped(where, "model", trimmed(k.Models), checked.Models)...)

	for _, id := range slices.Sorted(maps.Keys(k.ExtraArgs)) {
		if _, ok := checked.Args[id]; !ok && len(k.ExtraArgs[id]) > 0 {
			notes = append(notes, fmt.Sprintf("%s: its extra flags for %s were left out.", where, agentWord(id)))
		}
	}

	k.Profiles, k.Models, k.ExtraArgs = profiles, checked.Models, agent.CloneArgs(checked.Args)
	k.PRPrompt, k.IssuePrompt = strings.TrimSpace(k.PRPrompt), strings.TrimSpace(k.IssuePrompt)

	return k, notes, true
}

// dropped notes each of an agent's settings what that was in given and left out of kept.
func dropped(where, what string, given, kept map[agent.ID]string) []string {
	var notes []string

	for _, id := range slices.Sorted(maps.Keys(given)) {
		if _, ok := kept[id]; !ok {
			notes = append(notes, fmt.Sprintf("%s: its %s for %s, %q, was left out.", where, what, agentWord(id), given[id]))
		}
	}

	return notes
}

// resolvePrompt returns nickname, k's default prompt for kind, when known finds it, and
// otherwise "", noting that it inherits.
func resolvePrompt(p *backup.Plan, k Kept, nickname string, kind item.Kind, known Known) string {
	if nickname == "" || known(nickname, kind) {
		return nickname
	}

	p.AddNote(fmt.Sprintf("%s: its prompt for %s, “%s”, isn't here, so it inherits.", k.target(), kind.Plural(), nickname))

	return ""
}

// findPrompt returns the ID find finds for nickname, or nil for "" or one it doesn't find.
func findPrompt(ctx context.Context, find PromptFinder, nickname string, kind item.Kind) (*int64, error) {
	if nickname == "" {
		return nil, nil //nolint:nilnil // no prompt is no error
	}

	id, ok, err := find(ctx, nickname, kind)
	if err != nil || !ok {
		return nil, err
	}

	return &id, nil
}

// promptMark is a stand-in prompt ID for a nickname that names one, or nil, for telling
// whether a row sets anything before its prompts are found.
func promptMark(nickname string) *int64 {
	if nickname == "" {
		return nil
	}

	mark := int64(1)

	return &mark
}

// sameKept reports whether two kept defaults set the same.
func sameKept(a, b Kept) bool {
	norm := func(k Kept) Kept {
		k.Profiles, k.Models, k.ExtraArgs = trimmed(k.Profiles), trimmed(k.Models), agent.CloneArgs(k.ExtraArgs)

		return k
	}

	return reflect.DeepEqual(norm(a), norm(b))
}

// target names the org or repository k is for.
func (k Kept) target() string {
	return target(k.Owner, k.Name)
}

// keyOf is the key k's org or repository matches by, ignoring case.
func keyOf(k Kept) string {
	return repo.Name{Owner: k.Owner, Repo: k.Name}.Key()
}

// agentWord names an agent, or quotes its ID when this build doesn't know it.
func agentWord(id agent.ID) string {
	if a, err := agent.Lookup(id); err == nil {
		return a.Name
	}

	return fmt.Sprintf("%q", id)
}
