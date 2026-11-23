package launchdefaults

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/store"
)

const (
	// KeptFormat names the format of the launch defaults' copy, so it can be told from
	// any other JSON file.
	KeptFormat = brand.Slug + ".launch-defaults"

	// KeptVersion is the version of the format this build writes, and the newest it reads.
	// Version 2 keeps a profile, a model, and extra arguments per agent; a version 1
	// copy's are Claude's.
	KeptVersion = 2

	// maxKeptBytes caps the size of the copy Load reads.
	maxKeptBytes = 8 << 20

	// defaultsTable is the table a backup's launch defaults are read from.
	defaultsTable = "launch_defaults"
)

var (
	// ErrNotKept is returned for a file that is not a copy of the launch defaults.
	ErrNotKept = errors.New("the file isn't a copy of the launch defaults")

	// ErrNewerKept is returned for a copy a newer build wrote in a format this build cannot
	// read.
	ErrNewerKept = errors.New("the copy of the launch defaults was saved by a newer version of " + brand.Name)
)

// Kept is one org's or repository's launch defaults as the app keeps them outside its
// database. It names its default prompts by nickname, not by ID, so it can be matched to
// a prompt library brought back from a backup, whose prompts have new IDs.
type Kept struct {
	Owner    string              `json:"owner"`
	Name     string              `json:"name"`
	Agent    agent.ID            `json:"agent"`
	Profiles map[agent.ID]string `json:"profiles"`

	// PRPrompt and IssuePrompt are the nicknames of the default prompts; "" inherits.
	PRPrompt    string `json:"prPrompt"`
	IssuePrompt string `json:"issuePrompt"`

	// ChecksPrompt, ConflictsPrompt, ChangesPrompt, and RereviewPrompt are the nicknames of
	// the default prompt for a pull request whose checks fail, that conflicts with its base,
	// that a reviewer asked to change, and that the user is asked to review again; "" (left
	// out) inherits. A copy without them, as an older build's, inherits.
	ChecksPrompt    string `json:"checksPrompt,omitempty"`
	ConflictsPrompt string `json:"conflictsPrompt,omitempty"`
	ChangesPrompt   string `json:"changesPrompt,omitempty"`
	RereviewPrompt  string `json:"rereviewPrompt,omitempty"`

	// PromptUUIDs names each default prompt by its UUID too, by its slot ("pr", "issue",
	// "checks", "conflicts", "changes", "rereview"), so a restore or an import finds it
	// whatever its nickname became; left out when it names none, and in an older build's
	// copy.
	PromptUUIDs map[string]string `json:"promptUUIDs,omitempty"`

	Models    map[agent.ID]string   `json:"models"`
	Effort    string                `json:"effort"`
	PlanMode  *bool                 `json:"planMode"`
	Worktree  *bool                 `json:"worktree"`
	ExtraArgs map[agent.ID][]string `json:"extraArgs"`

	// AutopilotOutside is whether Autopilot may run agents on outside contributors' items
	// here: "off" or "on", left out to inherit. A copy without it, as an older build's,
	// inherits.
	AutopilotOutside Outside `json:"autopilotOutside,omitempty"`
}

// keptV1 is the part of a version 1 copy that version 2 keeps per agent: one profile,
// model, and list of extra arguments, all Claude's.
type keptV1 struct {
	Profile   string   `json:"profile"`
	Model     string   `json:"model"`
	ExtraArgs []string `json:"extraArgs"`
}

// claudeOnly returns a version 1 copy's settings as version 2 keeps them.
func (v keptV1) claudeOnly() (profiles, models map[agent.ID]string, args map[agent.ID][]string) {
	profiles = agent.CloneModels(map[agent.ID]string{agent.Claude: v.Profile})
	models = agent.CloneModels(map[agent.ID]string{agent.Claude: v.Model})
	args = agent.CloneArgs(map[agent.ID][]string{agent.Claude: v.ExtraArgs})

	return profiles, models, args
}

// keptFile is the document File writes.
type keptFile struct {
	Format   string `json:"format"`
	Version  int    `json:"version"`
	SavedAt  string `json:"savedAt"`
	App      string `json:"app"`
	Defaults []Kept `json:"defaults"`
}

// keptHeader is the part of the document Load reads before it knows the version.
type keptHeader struct {
	Format   string            `json:"format"`
	Version  int               `json:"version"`
	Defaults []json.RawMessage `json:"defaults"`
}

// File is the copy of the launch defaults the app keeps in a plain file beside its
// database. The database stays their home; the file is written whole after every change
// to them, and after every change to the prompt library, since it names prompts by
// nickname and a prompt may have been renamed. When the database is lost, damaged, or set
// aside, Restore brings the launch defaults back from it.
type File struct {
	path  string
	app   string
	clock clock.Clock

	// mu makes each Save read the defaults and write the file before the next one starts,
	// so the file always ends up holding the latest defaults.
	mu sync.Mutex
}

// NewFile returns the copy at path, which records app as the version that wrote it and
// stamps it from c (nil for the system clock).
func NewFile(path, app string, c clock.Clock) *File {
	return &File{path: path, app: app, clock: clock.Or(c)}
}

// Path returns where the file is.
func (f *File) Path() string {
	return f.path
}

// Save writes every stored launch default st holds to the file, replacing it in one
// step, naming each default prompt by its nickname in library.
func (f *File) Save(ctx context.Context, st *Store, library PromptLookup) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	kept, err := keepAll(ctx, st, library)
	if err != nil {
		return err
	}

	data, err := EncodeKept(kept, f.app, f.clock.Now())
	if err != nil {
		return err
	}

	return appdata.WriteFile(f.path, data)
}

// keepAll returns every stored launch default st holds as Kept, naming each default prompt
// by its nickname in library.
func keepAll(ctx context.Context, st *Store, library PromptLookup) ([]Kept, error) {
	all, err := st.All(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]Kept, 0, len(all))

	for _, d := range all {
		k, keepErr := keep(ctx, d, library)
		if keepErr != nil {
			return nil, keepErr
		}

		out = append(out, k)
	}

	return out, nil
}

// EncodeKept writes kept in the format of the launch defaults' copy, saved at now by the
// app version app, as indented JSON ending in a newline.
func EncodeKept(kept []Kept, app string, now time.Time) ([]byte, error) {
	doc := keptFile{
		Format: KeptFormat, Version: KeptVersion, SavedAt: clock.Format(now), App: app,
		Defaults: append(make([]Kept, 0, len(kept)), kept...),
	}

	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("write the launch defaults: %w", err)
	}

	return buf.Bytes(), nil
}

// Load reads the file. A file that is not there is an error matching fs.ErrNotExist. Its
// lists are never nil.
func (f *File) Load() ([]Kept, error) {
	data, err := appdata.ReadFile(f.path, maxKeptBytes)
	if err != nil {
		return nil, err
	}

	return DecodeKept(data)
}

// DecodeKept reads launch defaults in the format of their copy, of any version this build
// reads. Its lists are never nil.
func DecodeKept(data []byte) ([]Kept, error) {
	if len(data) > maxKeptBytes {
		return nil, fmt.Errorf("%w: it is too large", ErrNotKept)
	}

	var doc keptHeader
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotKept, err)
	}

	switch {
	case doc.Format != KeptFormat || doc.Version < 1:
		return nil, ErrNotKept
	case doc.Version > KeptVersion:
		return nil, ErrNewerKept
	}

	out := make([]Kept, 0, len(doc.Defaults))

	for _, raw := range doc.Defaults {
		k, decodeErr := decodeKept(raw, doc.Version)
		if decodeErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrNotKept, decodeErr)
		}

		out = append(out, k)
	}

	return out, nil
}

// decodeKept reads one org's or repository's launch defaults from a copy of the given
// version. Its maps are never nil.
func decodeKept(raw json.RawMessage, version int) (Kept, error) {
	var k Kept

	if version == 1 {
		var old struct {
			keptV1

			Owner       string `json:"owner"`
			Name        string `json:"name"`
			Agent       string `json:"agent"`
			PRPrompt    string `json:"prPrompt"`
			IssuePrompt string `json:"issuePrompt"`
			Effort      string `json:"effort"`
			PlanMode    *bool  `json:"planMode"`
			Worktree    *bool  `json:"worktree"`
		}

		if err := json.Unmarshal(raw, &old); err != nil {
			return Kept{}, err
		}

		k = Kept{
			Owner: old.Owner, Name: old.Name, Agent: agent.ID(old.Agent), PRPrompt: old.PRPrompt,
			IssuePrompt: old.IssuePrompt, Effort: old.Effort, PlanMode: old.PlanMode, Worktree: old.Worktree,
		}
		k.Profiles, k.Models, k.ExtraArgs = old.claudeOnly()

		return k, nil
	}

	if err := json.Unmarshal(raw, &k); err != nil {
		return Kept{}, err
	}

	k.Profiles, k.Models, k.ExtraArgs = agent.CloneModels(k.Profiles), agent.CloneModels(k.Models),
		agent.CloneArgs(k.ExtraArgs)

	return k, nil
}

// keep returns stored defaults as Kept, naming their prompts by nickname.
func keep(ctx context.Context, d Defaults, library PromptLookup) (Kept, error) {
	k := Kept{
		Owner: d.Owner, Name: d.Name, Agent: d.Agent, Profiles: agent.CloneModels(d.Profiles),
		Models: agent.CloneModels(d.Models), Effort: d.Effort, PlanMode: d.PlanMode, Worktree: d.Worktree,
		ExtraArgs: agent.CloneArgs(d.ExtraArgs), AutopilotOutside: d.AutopilotOutside,
	}

	ids := promptIDs{
		pr: d.PRPromptID, issue: d.IssuePromptID, checks: d.ChecksPromptID, conflicts: d.ConflictsPromptID,
		changes: d.ChangesPromptID, rereview: d.RereviewPromptID,
	}

	for _, p := range promptsOf(&k, &ids) {
		found, err := lookup(ctx, library, *p.id)
		if err != nil {
			return Kept{}, err
		}

		*p.nickname = found.Nickname
		setUUID(&k, p.key, found.UUID)
	}

	return k, nil
}

// lookup returns the prompt with id, or none for nil or a prompt that is gone.
func lookup(ctx context.Context, library PromptLookup, id *int64) (prompts.Prompt, error) {
	if id == nil {
		return prompts.Prompt{}, nil
	}

	p, err := library.Lookup(ctx, *id)

	switch {
	case errors.Is(err, prompts.ErrPromptNotFound):
		return prompts.Prompt{}, nil
	case err != nil:
		return prompts.Prompt{}, err
	default:
		return p, nil
	}
}

// setUUID names u the UUID of k's default prompt in the slot key; "" names none.
func setUUID(k *Kept, key, u string) {
	if u == "" {
		return
	}

	if k.PromptUUIDs == nil {
		k.PromptUUIDs = map[string]string{}
	}

	k.PromptUUIDs[key] = u
}

// defaultsColumns are the columns a backup's launch defaults are read from: both the
// per-agent ones and the single ones builds before them kept, which were Claude's. A
// backup an older build wrote may lack some, and each one missing inherits.
func defaultsColumns() []string {
	return []string{
		"owner", "name", "agent", "profile", "profiles", "pr_prompt_id", "issue_prompt_id", "checks_prompt_id",
		"conflicts_prompt_id", "changes_prompt_id", "rereview_prompt_id", "model", "models", "effort", "plan_mode",
		"worktree", "extra_args", "agent_args", "autopilot_outside",
	}
}

// ReadSnapshot reads the launch defaults in the database file at path, whichever build
// wrote it, without changing it. promptOf names the prompt each of its prompt IDs stood
// for in that database, by its nickname and its UUID, "" for what it does not know.
func ReadSnapshot(ctx context.Context, path string, promptOf func(id int64) (nickname, uuid string)) ([]Kept, error) {
	snap, err := store.OpenSnapshot(ctx, path)
	if err != nil {
		return nil, err
	}

	defer func() { _ = snap.Close() }()

	rows, err := snap.Rows(ctx, defaultsTable, defaultsColumns()...)
	if err != nil {
		return nil, err
	}

	out := make([]Kept, 0, len(rows))

	for _, row := range rows {
		k := Kept{
			Owner: row.Text("owner"), Name: row.Text("name"), Agent: agent.ID(row.Text("agent")),
			Profiles: prompts.SnapshotModels(row, "profiles", "profile"),
			Models:   prompts.SnapshotModels(row, "models", "model"), Effort: row.Text("effort"),
			PlanMode: row.Bool("plan_mode"), Worktree: row.Bool("worktree"),
			ExtraArgs:        prompts.SnapshotArgs(row, "agent_args", "extra_args"),
			AutopilotOutside: outsideOf(new(row.Text("autopilot_outside"))),
		}

		for _, p := range promptsOf(&k, new(promptIDs)) {
			if id, ok := row.Int(p.key + "_prompt_id"); ok {
				nickname, u := promptOf(id)
				*p.nickname = nickname
				setUUID(&k, p.key, u)
			}
		}

		out = append(out, k)
	}

	return out, nil
}

// Restore stores kept launch defaults in a database that holds none, as after the
// database was set aside, damaged, or deleted, all of them or, when a write fails, none.
// It returns how many it stored, and stores nothing when any launch defaults are stored
// already. promptID finds the prompt a default names now, by its UUID first, else its
// nickname; one it does not find inherits. A profile is kept by name even when no profile has it now, as on a Mac
// without that config directory yet: the launch menu warns that it is missing, and it
// works again once the profile is back, where dropping it would lose the user's choice. A
// setting this build would refuse is left out, as is a row for no valid org or
// repository.
func Restore(ctx context.Context, st *Store, kept []Kept, promptID PromptMatcher) (int, error) {
	inputs := make([]restoring, 0, len(kept))

	for _, k := range kept {
		if in, ok := restorable(k, promptID); ok {
			inputs = append(inputs, in)
		}
	}

	stored := 0

	err := st.inTx(ctx, func(tx *Store) error {
		var err error

		stored, err = storeAll(ctx, tx, inputs)

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("restore the launch defaults: %w", err)
	}

	return stored, nil
}

// storeAll stores inputs, when st holds no launch defaults, and returns how many it
// stored.
func storeAll(ctx context.Context, st *Store, inputs []restoring) (int, error) {
	existing, err := st.All(ctx)
	if err != nil || len(existing) > 0 {
		return 0, err
	}

	for _, in := range inputs {
		if _, err = st.save(ctx, in.input, in.checked); err != nil {
			return 0, err
		}
	}

	return len(inputs), nil
}

// restoring is kept defaults ready to store: the Input and its checked models and extra
// arguments.
type restoring struct {
	input   Input
	checked agent.Checked
}

// promptIDs are the default prompts a row names, each nil to inherit: each kind's and each
// situation's.
type promptIDs struct {
	pr, issue, checks, conflicts, changes, rereview *int64
}

// PromptMatcher finds the prompt a kept default names now, by its UUID first, else its
// nickname, and whether there is one.
type PromptMatcher func(uuid, nickname string) (int64, bool)

// keptPrompt is one of the default prompts a Kept names: its slot's key, which keys its
// UUID, where the Kept names it by nickname, where a promptIDs holds its ID, the kind of
// item it must be a prompt for (a situation's is a pull request's), and the words a note
// names it with.
type keptPrompt struct {
	key      string
	nickname *string
	id       **int64
	kind     item.Kind
	what     string
}

// promptsOf returns each of the default prompts k names, each kind's and then each
// situation's, with where ids holds its ID.
func promptsOf(k *Kept, ids *promptIDs) []keptPrompt {
	situation := func(slot prompts.DefaultSlot) string { return "its prompt " + slot.When() }

	return []keptPrompt{
		{"pr", &k.PRPrompt, &ids.pr, item.KindPR, "its prompt for " + item.KindPR.Plural()},
		{"issue", &k.IssuePrompt, &ids.issue, item.KindIssue, "its prompt for " + item.KindIssue.Plural()},
		{"checks", &k.ChecksPrompt, &ids.checks, item.KindPR, situation(prompts.DefaultSlotChecksFailing)},
		{"conflicts", &k.ConflictsPrompt, &ids.conflicts, item.KindPR, situation(prompts.DefaultSlotConflicts)},
		{"changes", &k.ChangesPrompt, &ids.changes, item.KindPR, situation(prompts.DefaultSlotChangesRequested)},
		{"rereview", &k.RereviewPrompt, &ids.rereview, item.KindPR, situation(prompts.DefaultSlotRereview)},
	}
}

// restorable returns kept defaults as this build would store them, and whether there is
// anything to store: a row for a valid org or repository that sets something.
func restorable(k Kept, promptID PromptMatcher) (restoring, bool) {
	var ids promptIDs

	for _, p := range promptsOf(&k, &ids) {
		*p.id = idFor(k.PromptUUIDs[p.key], *p.nickname, promptID)
	}

	return restorableWith(k, ids)
}

// restorableWith returns kept defaults as restorable does, with the default prompts ids.
func restorableWith(k Kept, ids promptIDs) (restoring, bool) {
	in := Input{
		Owner: strings.TrimSpace(k.Owner), Name: strings.TrimSpace(k.Name),
		Agent: agent.ID(strings.TrimSpace(string(k.Agent))), Effort: strings.TrimSpace(k.Effort),
		PlanMode: k.PlanMode, Worktree: k.Worktree, AutopilotOutside: outsideOf(new(string(k.AutopilotOutside))),
	}

	if !repo.ValidOwner(in.Owner) || (in.Name != "" && !repo.ValidRepo(in.Name)) {
		return restoring{}, false
	}

	if !agent.Known(in.Agent) {
		in.Agent = agent.Inherit
	}

	if agent.ValidateEffortAny(in.Effort) != nil {
		in.Effort = ""
	}

	var checked agent.Checked

	in.Profiles, checked = restorableAgents(k)
	in.Models, in.ExtraFlags = checked.Models, agent.JoinArgs(checked.Args)
	in.PRPromptID, in.IssuePromptID = ids.pr, ids.issue
	in.ChecksPromptID, in.ConflictsPromptID, in.ChangesPromptID, in.RereviewPromptID = ids.checks, ids.conflicts,
		ids.changes, ids.rereview

	return restoring{input: in, checked: checked}, !in.Empty()
}

// restorableAgents returns kept defaults' profiles, models, and extra arguments as this
// build would store them: only an agent this build knows, a model it would take, and
// flags the editor would accept.
func restorableAgents(k Kept) (map[agent.ID]string, agent.Checked) {
	names := keepKnown(trimmed(k.Profiles), func(agent.ID, string) bool { return true })
	models := keepKnown(trimmed(k.Models), func(_ agent.ID, model string) bool { return agent.ValidateModel(model) == nil })
	args := keepKnown(agent.CloneArgs(k.ExtraArgs), func(id agent.ID, list []string) bool {
		return agent.CheckExtra(id, list) == nil
	})

	return names, agent.Checked{Models: models, Args: args}
}

// keepKnown returns the entries of an agent this build knows that ok accepts; never nil.
func keepKnown[V any](m map[agent.ID]V, ok func(agent.ID, V) bool) map[agent.ID]V {
	out := make(map[agent.ID]V, len(m))

	for id, v := range m {
		if agent.Known(id) && ok(id, v) {
			out[id] = v
		}
	}

	return out
}

// idFor returns the ID promptID finds for a default named by uuid and nickname, or nil for
// one that names none or one it does not find.
func idFor(uuid, nickname string, promptID PromptMatcher) *int64 {
	if nickname == "" && uuid == "" {
		return nil
	}

	id, ok := promptID(uuid, nickname)
	if !ok {
		return nil
	}

	return &id
}
