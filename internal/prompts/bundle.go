package prompts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

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
	BundleVersion = 1

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
//	  "version": 1,
//	  "exportedAt": "2026-11-08T17:00:00Z",
//	  "app": "v0.4.0",
//	  "defaults": {"pr": "Review", "issue": "Work on it"},
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
}

// BundlePrompt is one prompt in portable form: its text, its names, and its launch
// settings, as the library stores them.
type BundlePrompt struct {
	Name      string    `json:"name"`
	Nickname  string    `json:"nickname"`
	AppliesTo AppliesTo `json:"appliesTo"`
	Body      string    `json:"body"`

	// Model and Effort set --model and --effort; "" inherits.
	Model  string `json:"model"`
	Effort string `json:"effort"`

	// PlanMode and Worktree turn plan mode and the worktree on or off; nil inherits.
	PlanMode *bool `json:"planMode"`
	Worktree *bool `json:"worktree"`

	// ExtraArgs are added after the profile's and the launch defaults' arguments.
	ExtraArgs []string `json:"extraArgs"`

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

		if p.ID == lib.Defaults.PR {
			b.Defaults.PR = p.Nickname
		}

		if p.ID == lib.Defaults.Issue {
			b.Defaults.Issue = p.Nickname
		}
	}

	return b
}

// bundlePrompt returns a stored prompt in portable form.
func bundlePrompt(p Prompt) BundlePrompt {
	return BundlePrompt{
		Name: p.Name, Nickname: p.Nickname, AppliesTo: p.AppliesTo, Body: p.Body, Model: p.Model, Effort: p.Effort,
		PlanMode: p.PlanMode, Worktree: p.Worktree, ExtraArgs: slices.Clone(textutil.NonNil(p.ExtraArgs)),
		StarterKey: p.StarterKey, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

// encodeBundle writes b as indented JSON ending in a newline. Characters HTML treats
// specially stay as they are, so a prompt's text reads in the file as it was written.
func encodeBundle(b Bundle) ([]byte, error) {
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

	for i := range b.Prompts {
		b.Prompts[i].ExtraArgs = textutil.NonNil(b.Prompts[i].ExtraArgs)
	}

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
