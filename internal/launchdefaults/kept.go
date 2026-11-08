package launchdefaults

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
)

const (
	// KeptFormat names the format of the launch defaults' copy, so it can be told from
	// any other JSON file.
	KeptFormat = brand.Slug + ".launch-defaults"

	// KeptVersion is the version of the format this build writes, and the newest it reads.
	KeptVersion = 1

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
	Owner   string `json:"owner"`
	Name    string `json:"name"`
	Agent   string `json:"agent"`
	Profile string `json:"profile"`

	// PRPrompt and IssuePrompt are the nicknames of the default prompts; "" inherits.
	PRPrompt    string `json:"prPrompt"`
	IssuePrompt string `json:"issuePrompt"`

	Model     string   `json:"model"`
	Effort    string   `json:"effort"`
	PlanMode  *bool    `json:"planMode"`
	Worktree  *bool    `json:"worktree"`
	ExtraArgs []string `json:"extraArgs"`
}

// keptFile is the document File writes.
type keptFile struct {
	Format   string `json:"format"`
	Version  int    `json:"version"`
	SavedAt  string `json:"savedAt"`
	App      string `json:"app"`
	Defaults []Kept `json:"defaults"`
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

	all, err := st.All(ctx)
	if err != nil {
		return err
	}

	doc := keptFile{
		Format: KeptFormat, Version: KeptVersion, SavedAt: clock.Format(f.clock.Now()), App: f.app,
		Defaults: make([]Kept, 0, len(all)),
	}

	for _, d := range all {
		k, keepErr := keep(ctx, d, library)
		if keepErr != nil {
			return keepErr
		}

		doc.Defaults = append(doc.Defaults, k)
	}

	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if err = enc.Encode(doc); err != nil {
		return fmt.Errorf("write the launch defaults: %w", err)
	}

	return appdata.WriteFile(f.path, buf.Bytes())
}

// Load reads the file. A file that is not there is an error matching fs.ErrNotExist. Its
// lists are never nil.
func (f *File) Load() ([]Kept, error) {
	data, err := appdata.ReadFile(f.path, maxKeptBytes)
	if err != nil {
		return nil, err
	}

	var doc keptFile
	if err = json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotKept, err)
	}

	switch {
	case doc.Format != KeptFormat || doc.Version < 1:
		return nil, ErrNotKept
	case doc.Version > KeptVersion:
		return nil, ErrNewerKept
	}

	for i := range doc.Defaults {
		doc.Defaults[i].ExtraArgs = textutil.NonNil(doc.Defaults[i].ExtraArgs)
	}

	return textutil.NonNil(doc.Defaults), nil
}

// keep returns stored defaults as Kept, naming their prompts by nickname.
func keep(ctx context.Context, d Defaults, library PromptLookup) (Kept, error) {
	pr, err := nickname(ctx, library, d.PRPromptID)
	if err != nil {
		return Kept{}, err
	}

	issue, err := nickname(ctx, library, d.IssuePromptID)
	if err != nil {
		return Kept{}, err
	}

	return Kept{
		Owner: d.Owner, Name: d.Name, Agent: d.Agent, Profile: d.Profile, PRPrompt: pr, IssuePrompt: issue,
		Model: d.Model, Effort: d.Effort, PlanMode: d.PlanMode, Worktree: d.Worktree,
		ExtraArgs: slices.Clone(textutil.NonNil(d.ExtraArgs)),
	}, nil
}

// nickname returns the nickname of the prompt with id, or "" for nil or a prompt that is
// gone.
func nickname(ctx context.Context, library PromptLookup, id *int64) (string, error) {
	if id == nil {
		return "", nil
	}

	p, err := library.Lookup(ctx, *id)

	switch {
	case errors.Is(err, prompts.ErrPromptNotFound):
		return "", nil
	case err != nil:
		return "", err
	default:
		return p.Nickname, nil
	}
}

// defaultsColumns are the columns a backup's launch defaults are read from. A backup an
// older build wrote may lack some, and each one missing inherits.
func defaultsColumns() []string {
	return []string{
		"owner", "name", "agent", "profile", "pr_prompt_id", "issue_prompt_id", "model", "effort", "plan_mode",
		"worktree", "extra_args",
	}
}

// ReadSnapshot reads the launch defaults in the database file at path, whichever build
// wrote it, without changing it. promptNickname names the prompt each of its prompt IDs
// stood for in that database, "" for one it does not know.
func ReadSnapshot(ctx context.Context, path string, promptNickname func(id int64) string) ([]Kept, error) {
	snap, err := store.OpenSnapshot(ctx, path)
	if err != nil {
		return nil, err
	}

	defer func() { _ = snap.Close() }()

	rows, err := snap.Rows(ctx, defaultsTable, defaultsColumns()...)
	if err != nil {
		return nil, err
	}

	named := func(row store.Row, column string) string {
		if id, ok := row.Int(column); ok {
			return promptNickname(id)
		}

		return ""
	}

	out := make([]Kept, 0, len(rows))

	for _, row := range rows {
		out = append(out, Kept{
			Owner: row.Text("owner"), Name: row.Text("name"), Agent: row.Text("agent"), Profile: row.Text("profile"),
			PRPrompt: named(row, "pr_prompt_id"), IssuePrompt: named(row, "issue_prompt_id"),
			Model: row.Text("model"), Effort: row.Text("effort"), PlanMode: row.Bool("plan_mode"),
			Worktree: row.Bool("worktree"), ExtraArgs: row.Strings("extra_args"),
		})
	}

	return out, nil
}

// Restore stores kept launch defaults in a database that holds none, as after the
// database was set aside, damaged, or deleted, all of them or, when a write fails, none.
// It returns how many it stored, and stores nothing when any launch defaults are stored
// already. promptID finds the prompt a nickname names now; a nickname it does not find
// inherits. A profile is kept by name even when no profile has it now, as on a Mac
// without that config directory yet: the launch menu warns that it is missing, and it
// works again once the profile is back, where dropping it would lose the user's choice. A
// setting this build would refuse is left out, as is a row for no valid org or
// repository.
func Restore(ctx context.Context, st *Store, kept []Kept, promptID func(nickname string) (int64, bool)) (int, error) {
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
		if _, err = st.save(ctx, in.input, in.args); err != nil {
			return 0, err
		}
	}

	return len(inputs), nil
}

// restoring is kept defaults ready to store: the Input and its extra arguments.
type restoring struct {
	input Input
	args  []string
}

// restorable returns kept defaults as this build would store them, and whether there is
// anything to store: a row for a valid org or repository that sets something.
func restorable(k Kept, promptID func(nickname string) (int64, bool)) (restoring, bool) {
	in := Input{
		Owner: strings.TrimSpace(k.Owner), Name: strings.TrimSpace(k.Name), Agent: strings.TrimSpace(k.Agent),
		Profile: strings.TrimSpace(k.Profile), Model: strings.TrimSpace(k.Model), Effort: strings.TrimSpace(k.Effort),
		PlanMode: k.PlanMode, Worktree: k.Worktree,
	}

	if !repo.ValidOwner(in.Owner) || (in.Name != "" && !repo.ValidRepo(in.Name)) {
		return restoring{}, false
	}

	if _, err := agent.Lookup(in.Agent); err != nil {
		in.Agent = ""
	}

	if agent.ValidateModel(in.Model) != nil {
		in.Model = ""
	}

	if agent.ValidateEffort(in.Effort) != nil {
		in.Effort = ""
	}

	args := slices.Clone(textutil.NonNil(k.ExtraArgs))
	if agent.CheckExtra(args) != nil {
		args = []string{}
	}

	in.ExtraFlags = agent.Join(args)
	in.PRPromptID = idFor(k.PRPrompt, promptID)
	in.IssuePromptID = idFor(k.IssuePrompt, promptID)

	return restoring{input: in, args: args}, !in.Empty()
}

// idFor returns the ID promptID finds for a nickname, or nil for "" or one it does not
// find.
func idFor(nickname string, promptID func(nickname string) (int64, bool)) *int64 {
	if nickname == "" {
		return nil
	}

	id, ok := promptID(nickname)
	if !ok {
		return nil
	}

	return &id
}
