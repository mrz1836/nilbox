// Package statusprefs holds the catalog of the status pages the app checks (GitHub's,
// Claude's, and OpenAI's) and the user's choices of what to watch on them: which providers
// are on, and which of each one's parts count. Settings writes the choices, under the keys
// declared here, and the service status check (internal/svcstatus) obeys them.
//
// The choices are stored as changes from the defaults, as notifyprefs stores the banners:
// the providers turned off, the default parts unticked, and the other parts ticked. A part
// a later build adds with Default set is then watched without the user doing anything, and
// a provider's own component ids never reach the stored choices: a part is named by the
// app's own PartID, so a provider that issues new ids needs only a catalog edit.
//
// It is a leaf: it imports only prefval, never the store, the GitHub client, or a
// service, so settings can write the choices without depending on what checks the pages.
package statusprefs

import (
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// The preferences the choices are stored under, each a JSON array. Settings writes them.
const (
	// KeyProvidersOff lists the providers the user turned off. A provider is on unless
	// listed, so one added later starts on.
	KeyProvidersOff = "status.providersOff"

	// KeyPartsOff lists the parts that start ticked (Part.Default) and that the user
	// unticked.
	KeyPartsOff = "status.partsOff"

	// KeyPartsOn lists the parts that start unticked and that the user ticked.
	KeyPartsOn = "status.partsOn"
)

var (
	// ErrUnknownProvider is returned for a ProviderID that is not in the catalog.
	ErrUnknownProvider = errors.New("unknown status page")

	// ErrUnknownPart is returned for a PartID that is not in the catalog.
	ErrUnknownPart = errors.New("unknown part of a status page")
)

// ProviderID names a provider whose status page the app checks.
type ProviderID string

// The providers.
const (
	// ProviderGitHub is GitHub, www.githubstatus.com.
	ProviderGitHub ProviderID = "github"

	// ProviderClaude is Anthropic's Claude, status.claude.com.
	ProviderClaude ProviderID = "claude"

	// ProviderOpenAI is OpenAI, status.openai.com, Codex's.
	ProviderOpenAI ProviderID = "openai"
)

// Format is the shape of a provider's status page's summary.
type Format string

// The formats.
const (
	// FormatStatuspage is Atlassian Statuspage's summary.json: every component, and every
	// incident with the components it names.
	FormatStatuspage Format = "statuspage"

	// FormatIncidentIO is incident.io's documented widget API: only the incidents and the
	// maintenance under way, each with the components it affects.
	FormatIncidentIO Format = "incidentio"
)

// PartID is the app's own stable name for a part of a provider, such as github.git. It is
// what the choices store, whatever ids the provider's page uses.
type PartID string

// Part is a part of a provider the user can watch, such as GitHub's Pull Requests.
type Part struct {
	// ID is the app's own name for it.
	ID PartID `json:"id"`

	// Name is what the window calls it.
	Name string `json:"name"`

	// Group is the group the provider's page lists it under, which the window groups the
	// parts by, or "" for none.
	Group string `json:"group"`

	// Default reports whether it is watched until the user unticks it.
	Default bool `json:"default"`

	// PageIDs are the ids the provider's page gives the component, which a reading is
	// matched by first.
	PageIDs []string `json:"-"`

	// PageName is the component's name on the page, which a reading is matched by when no
	// id matches (with Group, when it isn't ""), or "" when it is Name.
	PageName string `json:"-"`
}

// MatchName is the name a component on the page is matched by when no id matches.
func (p Part) MatchName() string {
	if p.PageName != "" {
		return p.PageName
	}

	return p.Name
}

// Provider is a provider whose status page the app checks.
type Provider struct {
	// ID is its name in the choices.
	ID ProviderID `json:"id"`

	// Name is what the window calls it.
	Name string `json:"name"`

	// PageURL is its status page, which the window opens.
	PageURL string `json:"pageUrl"`

	// Parts are the parts the user can watch, in the order the window lists them.
	Parts []Part `json:"parts"`

	// Host and Path are where its summary is read from (always https), and Format its
	// shape. Host must be one of the GitHub client's status hosts.
	Host   string `json:"-"`
	Path   string `json:"-"`
	Format Format `json:"-"`
}

// Catalog returns the providers whose status pages the app checks, in the order the window
// lists them, each with the parts that could matter to the app's user: those the app
// itself or the coding agents it launches depend on, and the rest of what each page lists
// that a user may care about. The defaults are what the app depends on. A fresh copy is
// returned each call.
func Catalog() []Provider {
	return []Provider{github(), claude(), openai()}
}

// github is GitHub's status page: its eleven real components, without the placeholder
// that points to the page itself.
func github() Provider {
	part := func(id PartID, name, pageID string, def bool) Part {
		return Part{ID: id, Name: name, Default: def, PageIDs: []string{pageID}}
	}

	return Provider{
		ID: ProviderGitHub, Name: "GitHub", PageURL: "https://www.githubstatus.com/",
		Host: "www.githubstatus.com", Path: "/api/v2/summary.json", Format: FormatStatuspage,
		Parts: []Part{
			part("github.git", "Git Operations", "8l4ygp009s5s", true),
			part("github.api", "API Requests", "brv1bkgrwx7q", true),
			part("github.issues", "Issues", "kr09ddfgbfsf", true),
			part("github.pulls", "Pull Requests", "hhtssxt0f5v2", true),
			part("github.actions", "Actions", "br0l2tvcx85d", true),
			part("github.webhooks", "Webhooks", "4230lsnqdsld", false),
			part("github.packages", "Packages", "st3j38cctv9l", false),
			part("github.pages", "Pages", "vg70hn9s2tyj", false),
			part("github.copilot", "Copilot", "pjmpxvq2cmr2", false),
			part("github.copilot-models", "Copilot AI Model Providers", "cnnb39dkkk82", false),
			part("github.codespaces", "Codespaces", "h2ftsgbw7kmk", false),
		},
	}
}

// claude is Claude's status page: its nine real components, the cloud platforms'
// included, without the group that only repeats its members.
func claude() Provider {
	part := func(id PartID, name, pageName, pageID string, def bool) Part {
		return Part{ID: id, Name: name, PageName: pageName, Default: def, PageIDs: []string{pageID}}
	}

	return Provider{
		ID: ProviderClaude, Name: "Claude", PageURL: "https://status.claude.com/",
		Host: "status.claude.com", Path: "/api/v2/summary.json", Format: FormatStatuspage,
		Parts: []Part{
			part("claude.api", "Claude API", "Claude API (api.anthropic.com)", "k8w3r06qmzrp", true),
			part("claude.code", "Claude Code", "", "yyzkbfz2thpt", true),
			part("claude.ai", "claude.ai", "", "rwppv331jlwc", false),
			part("claude.console", "Claude Console", "Claude Console (platform.claude.com)", "0qbwn08sd68x", false),
			part("claude.cowork", "Claude Cowork", "", "bpp5gb3hpjcl", false),
			part("claude.aws", "Claude Platform on AWS", "", "w14q5j51spbm", false),
			part("claude.gcp", "Claude on Google Cloud", "Claude Models on Google Cloud (Anthropic managed)",
				"csth13wtqbd6", false),
			part("claude.foundry", "Claude in Microsoft Foundry", "", "69zh8pryjjgp", false),
			part("claude.gov", "Claude for Government", "", "0scnb50nvy53", false),
		},
	}
}

// openai is OpenAI's status page, read through incident.io's widget API: Codex's
// components, the APIs Codex calls, and ChatGPT's sign-in, which Codex's sign-in with
// ChatGPT uses. The page lists two components named Login, told apart by their groups.
func openai() Provider {
	part := func(id PartID, name, pageName, pageID, group string, def bool) Part {
		return Part{ID: id, Name: name, PageName: pageName, Group: group, Default: def, PageIDs: []string{pageID}}
	}

	return Provider{
		ID: ProviderOpenAI, Name: "OpenAI", PageURL: "https://status.openai.com/",
		Host: "status.openai.com", Path: "/api/v1/summary", Format: FormatIncidentIO,
		Parts: []Part{
			part("openai.codex-api", "Codex API", "", "01KMP3KP5MGE23B80K1EK4S8PV", "Codex", true),
			part("openai.codex-cli", "Codex CLI", "CLI", "01KMKFAMWKNQ84Z1766MV08ZDE", "Codex", true),
			part("openai.codex-web", "Codex Web", "", "01JVCV8YSWZFRSM1G5CVP253SK", "Codex", false),
			part("openai.codex-vscode", "VS Code extension", "", "01KMP3KP5M8X0EBTVW6KN327EE", "Codex", false),
			part("openai.responses", "Responses", "", "01JP8CD9JR3HR6Y7G4Q75N4DVW", "APIs", true),
			part("openai.chat", "Chat Completions", "", "01JMXBRMFE6N2NNT7DG6XZQ6PW", "APIs", false),
			part("openai.login", "Login", "", "01JMXBNJXG1S2D9V65P1ZZTD94", "ChatGPT", false),
		},
	}
}

// Lookup returns the provider id names, or false when the catalog has none.
func Lookup(id ProviderID) (Provider, bool) {
	for _, p := range Catalog() {
		if p.ID == id {
			return p, true
		}
	}

	return Provider{}, false
}

// PartOf returns the part id names and its provider, or false when the catalog has none.
func PartOf(id PartID) (Provider, Part, bool) {
	for _, p := range Catalog() {
		for _, part := range p.Parts {
			if part.ID == id {
				return p, part, true
			}
		}
	}

	return Provider{}, Part{}, false
}

// Keys returns the preferences the choices are stored under.
func Keys() []string {
	return []string{KeyProvidersOff, KeyPartsOff, KeyPartsOn}
}

// PartKey returns the preference a change to part id is stored under: KeyPartsOff for a
// part that starts ticked, KeyPartsOn for one that starts unticked. A part not in the
// catalog is refused with ErrUnknownPart.
func PartKey(id PartID) (string, error) {
	_, part, ok := PartOf(id)
	if !ok {
		return "", fmt.Errorf("%w %q", ErrUnknownPart, id)
	}

	if part.Default {
		return KeyPartsOff, nil
	}

	return KeyPartsOn, nil
}

// Watch is the user's choice for one provider.
type Watch struct {
	// Provider is the provider.
	Provider ProviderID `json:"provider"`

	// On reports whether its page is checked at all.
	On bool `json:"on"`

	// Parts are the parts the user watches, in catalog order, whether or not the provider
	// is on. Never nil.
	Parts []PartID `json:"parts"`
}

// Watched reports whether the provider's page is checked: it is on, and at least one of
// its parts is watched.
func (w Watch) Watched() bool {
	return w.On && len(w.Parts) > 0
}

// Config is the user's choices: one Watch per provider, in catalog order.
type Config struct {
	Watch []Watch `json:"watch"`
}

// Defaults returns the choices of a fresh database: every provider on, and each one's
// default parts watched.
func Defaults() Config {
	return Decode(nil)
}

// Decode reads the choices from the stored preferences (see Keys). A value that is unset
// or can't be read lists nothing, so its defaults hold, and ids not in the catalog are
// dropped.
func Decode(stored map[string]string) Config {
	providersOff, _ := prefval.List[ProviderID](stored[KeyProvidersOff])
	partsOff, _ := prefval.List[PartID](stored[KeyPartsOff])
	partsOn, _ := prefval.List[PartID](stored[KeyPartsOn])

	out := Config{Watch: []Watch{}}

	for _, p := range Catalog() {
		w := Watch{Provider: p.ID, On: !slices.Contains(providersOff, p.ID), Parts: []PartID{}}

		for _, part := range p.Parts {
			if part.Default && !slices.Contains(partsOff, part.ID) || !part.Default && slices.Contains(partsOn, part.ID) {
				w.Parts = append(w.Parts, part.ID)
			}
		}

		out.Watch = append(out.Watch, w)
	}

	return out
}

// For returns the choice for provider id, or false when the catalog has none.
func (c Config) For(id ProviderID) (Watch, bool) {
	for _, w := range c.Watch {
		if w.Provider == id {
			return w, true
		}
	}

	return Watch{}, false
}

// Off returns the providers turned off, in catalog order. Never nil.
func (c Config) Off() []ProviderID {
	out := []ProviderID{}

	for _, w := range c.Watch {
		if !w.On {
			out = append(out, w.Provider)
		}
	}

	return out
}

// Picked returns every part watched, across the providers and whether or not each is on,
// in catalog order. Never nil.
func (c Config) Picked() []PartID {
	out := []PartID{}

	for _, w := range c.Watch {
		out = append(out, w.Parts...)
	}

	return out
}

// SetProvider returns the choices with provider id turned on or off. A provider not in
// the catalog is refused with ErrUnknownProvider.
func (c Config) SetProvider(id ProviderID, on bool) (Config, error) {
	if _, ok := Lookup(id); !ok {
		return Config{}, fmt.Errorf("%w %q", ErrUnknownProvider, id)
	}

	off := slices.DeleteFunc(c.Off(), func(p ProviderID) bool { return p == id })
	if !on {
		off = append(off, id)
	}

	out, _ := Choose(off, c.Picked())

	return out, nil
}

// SetPart returns the choices with part id ticked or unticked. A part not in the catalog
// is refused with ErrUnknownPart.
func (c Config) SetPart(id PartID, on bool) (Config, error) {
	if _, _, ok := PartOf(id); !ok {
		return Config{}, fmt.Errorf("%w %q", ErrUnknownPart, id)
	}

	picked := slices.DeleteFunc(c.Picked(), func(p PartID) bool { return p == id })
	if on {
		picked = append(picked, id)
	}

	out, _ := Choose(c.Off(), picked)

	return out, nil
}

// Choose builds the choices from the providers turned off and the parts watched, in any
// order, as a backup holds them. Ids not in the catalog are left out and returned, as
// written, in the order given; each kept once.
func Choose(off []ProviderID, picked []PartID) (Config, []string) {
	out := Config{Watch: []Watch{}}

	for _, p := range Catalog() {
		w := Watch{Provider: p.ID, On: !slices.Contains(off, p.ID), Parts: []PartID{}}

		for _, part := range p.Parts {
			if slices.Contains(picked, part.ID) {
				w.Parts = append(w.Parts, part.ID)
			}
		}

		out.Watch = append(out.Watch, w)
	}

	return out, unknownIDs(off, picked)
}

// unknownIDs returns the ids in off and picked that the catalog doesn't know, as written,
// in the order given.
func unknownIDs(off []ProviderID, picked []PartID) []string {
	unknown := []string{}

	for _, id := range off {
		if _, ok := Lookup(id); !ok {
			unknown = append(unknown, string(id))
		}
	}

	for _, id := range picked {
		if _, _, ok := PartOf(id); !ok {
			unknown = append(unknown, string(id))
		}
	}

	return unknown
}

// Encode returns the stored form of the choices, every key of Keys: the providers off, the
// default parts unticked, and the other parts ticked, each in catalog order.
func (c Config) Encode() map[string]string {
	off, partsOff, partsOn := c.Off(), []PartID{}, []PartID{}
	picked := c.Picked()

	for _, p := range Catalog() {
		for _, part := range p.Parts {
			on := slices.Contains(picked, part.ID)

			switch {
			case part.Default && !on:
				partsOff = append(partsOff, part.ID)
			case !part.Default && on:
				partsOn = append(partsOn, part.ID)
			}
		}
	}

	return map[string]string{
		KeyProvidersOff: prefval.EncodeList(off),
		KeyPartsOff:     prefval.EncodeList(partsOff),
		KeyPartsOn:      prefval.EncodeList(partsOn),
	}
}
