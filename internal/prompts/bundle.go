package prompts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/textutil"
)

const (
	// BundleFormat names the prompt file format, so a prompt file can be told from any
	// other JSON file.
	BundleFormat = brand.Slug + ".prompts"

	// BundleVersion is the version of the format this build writes, and the newest it
	// reads. A change that an older build would misread raises it; adding a field does not.
	// Version 2 keeps a model and extra arguments per agent; a version 1 file's are
	// Claude's. Version 3 adds review replies, which an older build would import as agent
	// prompts; a version 1 or 2 file holds agent prompts only.
	BundleVersion = 3

	// maxBundleBytes caps the size of a prompt file Import reads.
	maxBundleBytes = 8 << 20

	// maxBundlePrompts caps how many prompts one import may hold.
	maxBundlePrompts = 1000
)

var (
	// ErrNotBundle is returned for a file that is not a prompt file.
	ErrNotBundle = errors.New("the file isn't a " + brand.Name + " prompts file")

	// ErrNewerBundle is returned for a prompt file a newer build wrote in a format this
	// build cannot read.
	ErrNewerBundle = errors.New("the file was made by a newer version of " + brand.Name +
		"; update " + brand.Name + " to import it")

	// ErrBundleTooLarge is returned for a prompt file too large to import.
	ErrBundleTooLarge = fmt.Errorf("the file is too large: a prompts file holds at most %d prompts", maxBundlePrompts)
)

// Bundle is prompts in portable form. It is the prompt file Export writes and Import
// reads, and the copy of the library the app keeps beside its database (see LibraryFile).
// It names prompts by nickname, never by a database ID, so it reads the same in any
// library, and every field is written out, so a file edited by hand shows every setting a
// prompt has. The format is JSON, where <slug> is brand.Slug (docs/prompt-files.md
// specifies it):
//
//	{
//	  "format": "<slug>.prompts",
//	  "version": 3,
//	  "exportedAt": "2026-11-08T17:00:00Z",
//	  "app": "v0.4.0",
//	  "defaults": {"pr": "Review", "issue": "Work on it", "replies": {"approve": "LGTM"}},
//	  "startersAdded": ["review-pr", "work-on-issue"],
//	  "prompts": [{"name": "Review a pull request", "nickname": "Review", ...}]
//	}
type Bundle struct {
	// Format is BundleFormat.
	Format string `json:"format"`

	// Version is the format's version, BundleVersion when this build wrote it.
	Version int `json:"version"`

	// ExportedAt is when the file was written, as RFC 3339.
	ExportedAt string `json:"exportedAt"`

	// App is the version of the app that wrote the file.
	App string `json:"app"`

	// Defaults names the app's default prompt for each kind of item, by nickname, among
	// the file's prompts.
	Defaults BundleDefaults `json:"defaults"`

	// StartersAdded lists the starter prompts a whole library had been given, so a library
	// restored from the file keeps a starter the user deleted from coming back. It is
	// empty in a file of chosen prompts.
	StartersAdded []string `json:"startersAdded"`

	// Prompts are the prompts.
	Prompts []BundlePrompt `json:"prompts"`
}

// BundleDefaults names the app's default prompt for each kind of item by nickname; ""
// where none of the file's prompts is the default.
type BundleDefaults struct {
	PR    string `json:"pr"`
	Issue string `json:"issue"`

	// Replies names the default reply for each slot by nickname, leaving out a slot whose
	// default is none of the file's prompts.
	Replies map[ReplySlot]string `json:"replies,omitempty"`
}

// setReply names nickname the default reply for slot.
func (d *BundleDefaults) setReply(slot ReplySlot, nickname string) {
	if d.Replies == nil {
		d.Replies = map[ReplySlot]string{}
	}

	d.Replies[slot] = nickname
}

// BundlePrompt is one prompt in portable form: its text, its names, and its launch
// settings, as the library stores them.
type BundlePrompt struct {
	Name      string    `json:"name"`
	Nickname  string    `json:"nickname"`
	AppliesTo AppliesTo `json:"appliesTo"`
	Body      string    `json:"body"`

	// Purpose is PurposeAgent or PurposeReply; a file older than version 3 holds agent
	// prompts only, and reads as "".
	Purpose Purpose `json:"purpose"`

	// Action is a reply's review, and "" for an agent prompt.
	Action ReplyAction `json:"action"`

	// Models is each agent's model; an agent with no entry inherits.
	Models map[agent.ID]string `json:"models"`

	// Effort is the effort level of every agent; "" inherits.
	Effort string `json:"effort"`

	// PlanMode and Worktree turn plan mode and the worktree on or off; nil inherits.
	PlanMode *bool `json:"planMode"`
	Worktree *bool `json:"worktree"`

	// ExtraArgs is each agent's extra arguments, added after its profile's and the launch
	// defaults' arguments.
	ExtraArgs map[agent.ID][]string `json:"extraArgs"`

	// StarterKey names the starter the prompt came from, or "".
	StarterKey string `json:"starterKey"`

	// CreatedAt and UpdatedAt are when the prompt was added and last saved, as RFC 3339,
	// or "" when not known.
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// newBundle returns the prompts in lib with ids, or every prompt when ids is empty, as a
// Bundle written at now by the app version app. startersAdded goes in only with the
// whole library.
func newBundle(lib Library, startersAdded []string, ids []int64, now time.Time, app string) Bundle {
	b := Bundle{
		Format:        BundleFormat,
		Version:       BundleVersion,
		ExportedAt:    clock.Format(now),
		App:           app,
		StartersAdded: []string{},
		Prompts:       []BundlePrompt{},
	}

	if len(ids) == 0 {
		b.StartersAdded = slices.Clone(textutil.NonNil(startersAdded))
	}

	for _, p := range lib.Prompts {
		if len(ids) > 0 && !slices.Contains(ids, p.ID) {
			continue
		}

		b.Prompts = append(b.Prompts, bundlePrompt(p))
		b.Defaults.name(lib.Defaults, p)
	}

	return b
}

// name records p by nickname wherever defaults makes it a default.
func (d *BundleDefaults) name(defaults DefaultPrompts, p Prompt) {
	if p.ID == defaults.PR {
		d.PR = p.Nickname
	}

	if p.ID == defaults.Issue {
		d.Issue = p.Nickname
	}

	for _, slot := range defaults.Replies.Slots(p.ID) {
		d.setReply(slot, p.Nickname)
	}
}

// bundlePrompt returns a stored prompt in portable form.
func bundlePrompt(p Prompt) BundlePrompt {
	return BundlePrompt{
		Name: p.Name, Nickname: p.Nickname, AppliesTo: p.AppliesTo, Body: p.Body, Purpose: purposeOf(p.Purpose),
		Action: actionOf(p), Models: agent.CloneModels(p.Models),
		Effort: p.Effort, PlanMode: p.PlanMode, Worktree: p.Worktree, ExtraArgs: agent.CloneArgs(p.ExtraArgs),
		StarterKey: p.StarterKey, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

// UnmarshalJSON reads a prompt in either version of the format: version 2's models and
// extraArgs objects, keyed by agent, or version 1's single model and extraArgs list, which
// were Claude's.
func (p *BundlePrompt) UnmarshalJSON(data []byte) error {
	type plain BundlePrompt

	var raw struct {
		plain

		// Model is version 1's model, and ExtraArgs either version's extra arguments.
		Model     string          `json:"model"`
		ExtraArgs json.RawMessage `json:"extraArgs"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*p = BundlePrompt(raw.plain)
	p.Models = agent.CloneModels(p.Models)

	if model := raw.Model; model != "" && p.Models[agent.Claude] == "" {
		p.Models[agent.Claude] = model
	}

	args, err := decodeBundleArgs(raw.ExtraArgs)
	if err != nil {
		return fmt.Errorf("extraArgs: %w", err)
	}

	p.ExtraArgs = args

	return nil
}

// decodeBundleArgs reads extraArgs: an object of each agent's arguments, or version 1's
// list, which was Claude's. Missing and null read as none.
func decodeBundleArgs(raw json.RawMessage) (map[agent.ID][]string, error) {
	trimmed := bytes.TrimSpace(raw)

	switch {
	case len(trimmed) == 0 || string(trimmed) == "null":
		return map[agent.ID][]string{}, nil
	case trimmed[0] == '[':
		var list []string
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return nil, err
		}

		return agent.CloneArgs(map[agent.ID][]string{agent.Claude: list}), nil
	default:
		var args map[agent.ID][]string
		if err := json.Unmarshal(trimmed, &args); err != nil {
			return nil, err
		}

		return agent.CloneArgs(args), nil
	}
}

// EncodeBundle writes b as indented JSON ending in a newline, as Export and the library
// file write it. Characters HTML treats
// specially stay as they are, so a prompt's text reads in the file as it was written.
func EncodeBundle(b Bundle) ([]byte, error) {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if err := enc.Encode(b); err != nil {
		return nil, fmt.Errorf("write the prompts: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeBundle reads a prompt file. It refuses a file that is not one, one too large to
// import, and one in a newer format than this build reads. Fields it does not know are
// ignored, so a file a newer build wrote in the same format still reads. Its lists are
// never nil.
func DecodeBundle(data []byte) (Bundle, error) {
	if len(data) > maxBundleBytes {
		return Bundle{}, ErrBundleTooLarge
	}

	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return Bundle{}, fmt.Errorf("%w: %w", ErrNotBundle, err)
	}

	switch {
	case b.Format != BundleFormat || b.Version < 1:
		return Bundle{}, ErrNotBundle
	case b.Version > BundleVersion:
		return Bundle{}, ErrNewerBundle
	case len(b.Prompts) > maxBundlePrompts:
		return Bundle{}, ErrBundleTooLarge
	}

	b.StartersAdded = textutil.NonNil(b.StartersAdded)
	b.Prompts = textutil.NonNil(b.Prompts)

	return b, nil
}

// readBundleFile reads the prompt file at path.
func readBundleFile(path string) (Bundle, error) {
	data, err := appdata.ReadFile(path, maxBundleBytes)

	switch {
	case errors.Is(err, appdata.ErrTooLarge):
		return Bundle{}, ErrBundleTooLarge
	case errors.Is(err, appdata.ErrNotAFile):
		return Bundle{}, fmt.Errorf("%w: %w", ErrNotBundle, err)
	case err != nil:
		return Bundle{}, err
	}

	return DecodeBundle(data)
}
