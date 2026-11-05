package prompts

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/agentflags"
	"github.com/mrz1836/nilbox/internal/prompts/model"
)

// Preference keys the library keeps in the prefs table.
const (
	// prefSeeded lists, as a JSON array, the starter keys already added once.
	prefSeeded = "prompts.seeded"

	// prefDefaultPR and prefDefaultIssue hold the app's default prompt ID per kind.
	prefDefaultPR    = "prompts.default.pr"
	prefDefaultIssue = "prompts.default.issue"
)

// upsertPrefSQL stores one preference, replacing any earlier value.
const upsertPrefSQL = `INSERT INTO prefs (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`

// usageSQL reads the launch defaults that name a prompt.
const usageSQL = `SELECT owner, name, pr_prompt_id, issue_prompt_id FROM launch_defaults
WHERE pr_prompt_id IS NOT NULL OR issue_prompt_id IS NOT NULL ORDER BY owner, name`

// Lookup returns the stored prompt with id, without its usage.
func Lookup(ctx context.Context, db *gorm.DB, id int64) (Prompt, error) {
	row, err := loadRow(db.WithContext(ctx), id)
	if err != nil {
		return Prompt{}, err
	}

	return fromRow(row, Usage{}), nil
}

// All returns every stored prompt, without usage, sorted by nickname.
func All(ctx context.Context, db *gorm.DB) ([]Prompt, error) {
	rows, err := loadRows(db.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	out := make([]Prompt, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromRow(row, Usage{}))
	}

	return out, nil
}

// LoadDefaults returns the app's default prompt for each kind. A stored default that is
// gone or no longer applies falls back to the first prompt, by nickname, that applies.
func LoadDefaults(ctx context.Context, db *gorm.DB) (DefaultPrompts, error) {
	db = db.WithContext(ctx)

	stored, err := readDefaultPrefs(db)
	if err != nil {
		return DefaultPrompts{}, err
	}

	rows, err := loadRows(db)
	if err != nil {
		return DefaultPrompts{}, err
	}

	return DefaultPrompts{
		PR:    pickDefault(rows, KindPR, stored.PR),
		Issue: pickDefault(rows, KindIssue, stored.Issue),
	}, nil
}

// Template parses the prompt's text, failing when it has errors.
func (p Prompt) Template() (*Template, error) {
	t, diags := Parse(p.Body, p.AppliesTo)
	if t != nil {
		return t, nil
	}

	for _, d := range diags {
		if d.Severity == SeverityError {
			return nil, fmt.Errorf("%w: %q: %s", ErrInvalidTemplate, p.Nickname, d.Message)
		}
	}

	return nil, fmt.Errorf("%w: %q", ErrInvalidTemplate, p.Nickname)
}

// Seed adds each starter prompt this build ships that was never added before, and makes
// the review and work-on-issue starters the default prompts where no default is set yet.
// A starter that was added once is never added again, so one the user deleted stays
// deleted. A starter whose nickname is taken gets a free one.
func Seed(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seeded, err := readSeeded(tx)
		if err != nil {
			return err
		}

		if seeded, err = seedMissing(tx, seeded); err != nil {
			return err
		}

		encoded, err := json.Marshal(seeded)
		if err != nil {
			return fmt.Errorf("record the starter prompts: %w", err)
		}

		return setPref(tx, prefSeeded, string(encoded))
	})
}

// seedMissing adds every starter not in seeded and returns seeded with their keys.
func seedMissing(tx *gorm.DB, seeded []string) ([]string, error) {
	defaults, err := readDefaultPrefs(tx)
	if err != nil {
		return nil, err
	}

	for _, st := range Starters() {
		if slices.Contains(seeded, st.Key) {
			continue
		}

		if err = seedStarter(tx, st, defaults); err != nil {
			return nil, err
		}

		seeded = append(seeded, st.Key)
	}

	return seeded, nil
}

// seedStarter adds one starter, and makes it the default for its kind when it is the
// review or work-on-issue starter and no default is set.
func seedStarter(tx *gorm.DB, st Starter, defaults DefaultPrompts) error {
	nickname, err := freeNickname(tx, st.Nickname)
	if err != nil {
		return err
	}

	in := st.input()
	in.Nickname = nickname

	row := toRow(in, st.ExtraArgs)
	row.StarterKey = st.Key

	if err = tx.Create(&row).Error; err != nil {
		return fmt.Errorf("add the %q starter prompt: %w", st.Name, err)
	}

	switch {
	case st.Key == StarterReviewPR && defaults.PR == 0:
		return setPref(tx, prefDefaultPR, strconv.FormatInt(row.ID, 10))
	case st.Key == StarterWorkOnIssue && defaults.Issue == 0:
		return setPref(tx, prefDefaultIssue, strconv.FormatInt(row.ID, 10))
	default:
		return nil
	}
}

// listWithUsage returns every prompt with its usage, sorted by nickname.
func listWithUsage(ctx context.Context, db *gorm.DB) ([]Prompt, error) {
	db = db.WithContext(ctx)

	rows, err := loadRows(db)
	if err != nil {
		return nil, err
	}

	usage, err := loadUsage(db)
	if err != nil {
		return nil, err
	}

	out := make([]Prompt, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromRow(row, usage[row.ID]))
	}

	return out, nil
}

// loadRow reads one prompt row.
func loadRow(db *gorm.DB, id int64) (model.Prompt, error) {
	var rows []model.Prompt
	if err := db.Where("id = ?", id).Limit(1).Find(&rows).Error; err != nil {
		return model.Prompt{}, fmt.Errorf("read the prompt: %w", err)
	}

	if len(rows) == 0 {
		return model.Prompt{}, fmt.Errorf("%w (id %d)", ErrPromptNotFound, id)
	}

	return rows[0], nil
}

// loadRows reads every prompt row, sorted by nickname (ignoring case), then by ID.
func loadRows(db *gorm.DB) ([]model.Prompt, error) {
	var rows []model.Prompt
	if err := db.Order("nickname COLLATE NOCASE").Order("id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read the prompts: %w", err)
	}

	return rows, nil
}

// usageRow is one row of usageSQL.
type usageRow struct {
	Owner         string `gorm:"column:owner"`
	Name          string `gorm:"column:name"`
	PRPromptID    *int64 `gorm:"column:pr_prompt_id"`
	IssuePromptID *int64 `gorm:"column:issue_prompt_id"`
}

// loadUsage reads where every prompt is a default: the app's default prompts, and the
// org and repository launch defaults.
func loadUsage(db *gorm.DB) (map[int64]Usage, error) {
	defaults, err := readDefaultPrefs(db)
	if err != nil {
		return nil, err
	}

	var rows []usageRow
	if err = db.Raw(usageSQL).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read where the prompts are used: %w", err)
	}

	usage := map[int64]Usage{}

	addDefault := func(id int64, kind string) {
		if id != 0 {
			u := usage[id]
			u.Defaults = append(u.Defaults, kind)
			u.Kinds = appendUnique(u.Kinds, kind)
			usage[id] = u
		}
	}

	addDefault(defaults.PR, KindPR)
	addDefault(defaults.Issue, KindIssue)

	for _, r := range rows {
		addPlace(usage, r, r.PRPromptID, KindPR)
		addPlace(usage, r, r.IssuePromptID, KindIssue)
	}

	return usage, nil
}

// addPlace records that a launch default uses the prompt id for kind.
func addPlace(usage map[int64]Usage, r usageRow, id *int64, kind string) {
	if id == nil {
		return
	}

	u := usage[*id]
	if r.Name == "" {
		u.Orgs = appendUnique(u.Orgs, r.Owner)
	} else {
		u.Repos = appendUnique(u.Repos, r.Owner+"/"+r.Name)
	}

	u.Kinds = appendUnique(u.Kinds, kind)
	usage[*id] = u
}

// usageOf returns one prompt's usage.
func usageOf(db *gorm.DB, id int64) (Usage, error) {
	usage, err := loadUsage(db)
	if err != nil {
		return Usage{}, err
	}

	return usage[id], nil
}

// appendUnique appends s unless list already holds it.
func appendUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}

	return append(list, s)
}

// nicknameTaken reports whether a prompt other than id has nickname, ignoring case.
func nicknameTaken(db *gorm.DB, id int64, nickname string) (bool, error) {
	var count int64

	err := db.Model(&model.Prompt{}).Where("nickname = ? AND id <> ?", nickname, id).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("check the nickname: %w", err)
	}

	return count > 0, nil
}

// freeNickname returns base when no prompt has it, or else base followed by the lowest
// free number, as in "Review copy 2", cut so it stays within the nickname limit.
func freeNickname(db *gorm.DB, base string) (string, error) {
	base = truncateRunes(base, maxNicknameRunes)

	for n := 1; ; n++ {
		candidate := base

		if n > 1 {
			suffix := " " + strconv.Itoa(n)
			candidate = truncateRunes(base, maxNicknameRunes-utf8.RuneCountInString(suffix)) + suffix
		}

		taken, err := nicknameTaken(db, 0, candidate)
		if err != nil || !taken {
			return candidate, err
		}
	}
}

// readSeeded reads the starter keys already added.
func readSeeded(db *gorm.DB) ([]string, error) {
	raw, err := readPref(db, prefSeeded)
	if err != nil || raw == "" {
		return nil, err
	}

	var keys []string
	if err = json.Unmarshal([]byte(raw), &keys); err != nil {
		return nil, fmt.Errorf("read the starter prompts added: %w", err)
	}

	return keys, nil
}

// readDefaultPrefs reads the stored default prompt IDs; 0 where none is stored.
func readDefaultPrefs(db *gorm.DB) (DefaultPrompts, error) {
	pr, err := readPref(db, prefDefaultPR)
	if err != nil {
		return DefaultPrompts{}, err
	}

	issue, err := readPref(db, prefDefaultIssue)
	if err != nil {
		return DefaultPrompts{}, err
	}

	prID, _ := strconv.ParseInt(pr, 10, 64)       // a missing or bad value means no default
	issueID, _ := strconv.ParseInt(issue, 10, 64) // a missing or bad value means no default

	return DefaultPrompts{PR: prID, Issue: issueID}, nil
}

// pickDefault returns the stored default for kind when it exists and applies, else the
// first prompt that applies, else 0.
func pickDefault(rows []model.Prompt, kind string, stored int64) int64 {
	var first int64

	for _, row := range rows {
		if !AppliesTo(row.AppliesTo, kind) {
			continue
		}

		if row.ID == stored {
			return stored
		}

		if first == 0 {
			first = row.ID
		}
	}

	return first
}

// defaultPrefKey returns the preference holding the default prompt for kind.
func defaultPrefKey(kind string) (string, error) {
	switch kind {
	case KindPR:
		return prefDefaultPR, nil
	case KindIssue:
		return prefDefaultIssue, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidKind, kind)
	}
}

// readPref reads one preference, "" when it is unset.
func readPref(db *gorm.DB, key string) (string, error) {
	var values []string
	if err := db.Raw("SELECT value FROM prefs WHERE key = ?", key).Scan(&values).Error; err != nil {
		return "", fmt.Errorf("read setting %s: %w", key, err)
	}

	if len(values) == 0 {
		return "", nil
	}

	return values[0], nil
}

// setPref stores one preference.
func setPref(db *gorm.DB, key, value string) error {
	if err := db.Exec(upsertPrefSQL, key, value, time.Now().UTC()).Error; err != nil {
		return fmt.Errorf("save setting %s: %w", key, err)
	}

	return nil
}

// fromRow converts a row and its usage to a Prompt. Nil lists become empty ones.
func fromRow(row model.Prompt, usage Usage) Prompt {
	args := append([]string{}, row.ExtraArgs...)

	p := Prompt{
		ID: row.ID, Name: row.Name, Nickname: row.Nickname, AppliesTo: row.AppliesTo, Body: row.Body,
		Model: row.Model, Effort: row.Effort, PlanMode: row.PlanMode, Worktree: row.Worktree,
		ExtraArgs: args, ExtraFlags: agentflags.Join(args), StarterKey: row.StarterKey,
		Usage:     normalizeUsage(usage),
		UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
	}

	if st, ok := starterFor(row.StarterKey); ok {
		p.Modified = !matchesStarter(row, st)
	}

	return p
}

// normalizeUsage sorts a usage's lists and turns nil lists into empty ones.
func normalizeUsage(u Usage) Usage {
	for _, list := range []*[]string{&u.Defaults, &u.Orgs, &u.Repos, &u.Kinds} {
		if *list == nil {
			*list = []string{}
		}

		sort.Strings(*list)
	}

	return u
}

// matchesStarter reports whether a row still has its starter's title, text, and
// settings. The nickname may differ: Reset keeps it when another prompt took it.
func matchesStarter(row model.Prompt, st Starter) bool {
	return row.Name == st.Name && row.AppliesTo == st.AppliesTo && row.Body == st.Body &&
		row.Model == st.Model && row.Effort == st.Effort && sameBool(row.PlanMode, st.PlanMode) &&
		sameBool(row.Worktree, st.Worktree) && slices.Equal([]string(row.ExtraArgs), st.ExtraArgs)
}

// sameBool reports whether two optional settings are equal.
func sameBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return *a == *b
}
