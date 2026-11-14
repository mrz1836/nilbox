package backup

import (
	"slices"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// Section is one category of the user's setup a backup holds.
type Section string

// The categories, in the order a backup lists and applies them.
const (
	// SectionSettings is the user's settings, but for the repositories and the window's
	// memory.
	SectionSettings Section = "settings"

	// SectionRepositories is the clone folder, the other repository locations, and the
	// pinned and muted repositories and orgs.
	SectionRepositories Section = "repositories"

	// SectionPrompts is the prompt library: the prompts, the review replies, and their
	// defaults.
	SectionPrompts Section = "prompts"

	// SectionLaunchDefaults is each org's and repository's launch defaults.
	SectionLaunchDefaults Section = "launchDefaults"

	// SectionScripts is the user's own scripts, in their order.
	SectionScripts Section = "scripts"
)

// Sections returns every category, in the order a backup lists and applies them.
func Sections() []Section {
	return []Section{SectionSettings, SectionRepositories, SectionPrompts, SectionLaunchDefaults, SectionScripts}
}

// Valid reports whether s is one of Sections.
func (s Section) Valid() bool {
	return slices.Contains(Sections(), s)
}

// Mode is how an import treats what is here already.
type Mode string

// The modes.
const (
	// ModeMerge adds what the file holds and changes what it names, deleting nothing.
	ModeMerge Mode = "combine"

	// ModeReplace makes each category exactly the file's, deleting what the file doesn't
	// hold.
	ModeReplace Mode = "replace"
)

// Valid reports whether m is one of the modes.
func (m Mode) Valid() bool {
	return m == ModeMerge || m == ModeReplace
}

// Change says what importing one item does.
type Change string

// The changes.
const (
	// ChangeAdd adds an item this Mac doesn't have.
	ChangeAdd Change = "add"

	// ChangeUpdate changes an item this Mac has.
	ChangeUpdate Change = "update"

	// ChangeSame leaves an item this Mac has as it is: the file's is the same.
	ChangeSame Change = "same"

	// ChangeRemove deletes an item this Mac has that the file doesn't, under ModeReplace.
	ChangeRemove Change = "remove"
)

// Item is one thing an import adds, changes, keeps, or deletes, as the dialog lists it.
type Item struct {
	// Label names the item, such as a setting, a prompt's nickname, or a repository.
	Label string `json:"label"`

	// Change is what importing does to it.
	Change Change `json:"change"`

	// Detail says more, such as the value it has now; "" for nothing more.
	Detail string `json:"detail"`
}

// Count counts an import's items by what it does to them.
type Count struct {
	Add    int `json:"add"`
	Update int `json:"update"`
	Same   int `json:"same"`
	Remove int `json:"remove"`
}

// With returns the count with one more item whose change is change.
func (c Count) With(change Change) Count {
	switch change {
	case ChangeAdd:
		c.Add++
	case ChangeUpdate:
		c.Update++
	case ChangeSame:
		c.Same++
	case ChangeRemove:
		c.Remove++
	}

	return c
}

// Changes reports whether the count changes anything.
func (c Count) Changes() bool {
	return c.Add+c.Update+c.Remove > 0
}

// Plan is what importing one category would do under one mode: its items, counted, and
// notes on what is left out or kept, and why.
type Plan struct {
	Count Count    `json:"count"`
	Items []Item   `json:"items"`
	Notes []string `json:"notes"`
}

// Add records item, counting it.
func (p *Plan) Add(item Item) {
	p.Count = p.Count.With(item.Change)
	p.Items = append(p.Items, item)
}

// AddNote records a note on what is left out or kept, and why.
func (p *Plan) AddNote(note string) {
	p.Notes = append(p.Notes, note)
}

// normalized returns p with lists that are never nil.
func normalized(p Plan) Plan {
	p.Items, p.Notes = textutil.NonNil(p.Items), textutil.NonNil(p.Notes)

	return p
}
