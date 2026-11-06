package sensitive

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Check statuses.
const (
	// StatusPending is a pull request not checked since its last push: new to the inbox,
	// newly in scope, or pushed since its last check.
	StatusPending = "pending"

	// StatusFailed is a check that could not finish; the next refresh tries again.
	StatusFailed = "failed"

	// StatusFlagged is a pull request that changes sensitive files.
	StatusFlagged = "flagged"

	// StatusPartial is a pull request GitHub listed only some of the changed files of,
	// none of them sensitive.
	StatusPartial = "partial"

	// StatusClear is a pull request whose every changed file was checked, none of them
	// sensitive.
	StatusClear = "clear"
)

// File changes, as GitHub's REST API names them.
const (
	ChangeAdded    = "added"
	ChangeRemoved  = "removed"
	ChangeModified = "modified"
	ChangeRenamed  = "renamed"
	ChangeCopied   = "copied"
	ChangeChanged  = "changed"
)

// File is one changed file of a pull request.
type File struct {
	// Path is the file's path in the repository, after the change.
	Path string

	// PreviousPath is a renamed or copied file's path before the change, or "".
	PreviousPath string

	// Change is one of the Change* values.
	Change string
}

// Result is one stored check of a pull request's changed files.
type Result struct {
	// Head is the head commit the files were listed at.
	Head string

	// Total is how many files GitHub says the pull request changes.
	Total int

	// Files are the files GitHub listed.
	Files []File

	// Complete reports that Files holds every changed file.
	Complete bool

	// Error says why the check failed, or is "".
	Error string

	// CheckedAt is when the check ran.
	CheckedAt time.Time
}

// Finding is one sensitive file. Every field is a plain value.
type Finding struct {
	Path string `json:"path"`

	// PreviousPath is a renamed or copied file's path before the change, or "". A file
	// moved out of a sensitive folder is reported for the path it left.
	PreviousPath string `json:"previousPath"`

	// Change is how the file changed: added, removed, modified, renamed, copied, or
	// changed.
	Change string `json:"change"`

	// Pattern is the pattern the file matched.
	Pattern string `json:"pattern"`

	// URL opens the file's diff in the pull request on GitHub, or is "".
	URL string `json:"url"`
}

// Group is a category's sensitive files. Every field is a plain value.
type Group struct {
	Category    string    `json:"category"`
	Label       string    `json:"label"`
	Short       string    `json:"short"`
	Description string    `json:"description"`
	Level       string    `json:"level"`
	Files       []Finding `json:"files"`
}

// Check is what checking a pull request's changed files found, as the UI shows it. Every
// field is a plain value.
type Check struct {
	// Status is one of the Status* values.
	Status string `json:"status"`

	// Head is the head commit the files were checked at, or "" before any check.
	Head string `json:"head"`

	// Checked is how many files were checked, and Total how many the pull request
	// changes; Complete reports that every one was checked.
	Checked  int  `json:"checked"`
	Total    int  `json:"total"`
	Complete bool `json:"complete"`

	// Groups are the sensitive files by category, in matching order, each sorted by
	// path. It is empty unless Status is StatusFlagged.
	Groups []Group `json:"groups"`

	// CheckedAt is when the files were checked, as RFC 3339, or "".
	CheckedAt string `json:"checkedAt"`

	// Error says why the last check failed, or is "".
	Error string `json:"error"`
}

// Category returns the group of a category's files, if it has any.
func (c Check) Category(id string) (Group, bool) {
	for _, g := range c.Groups {
		if g.Category == id {
			return g, true
		}
	}

	return Group{}, false
}

// maxSummaryFiles caps how many files Summary names.
const maxSummaryFiles = 20

// Summary names the sensitive files for a prompt, in matching order: each path with its
// category's short label, and where a moved file came from, as in
// ".github/workflows/ci.yml (Workflows)" or "docs/old.yml (Workflows, moved from
// .github/workflows/ci.yml)". It names at most 20, then says how many more, and is ""
// when no file is sensitive.
func (c Check) Summary() string {
	var parts []string

	for _, g := range c.Groups {
		for _, f := range g.Files {
			parts = append(parts, f.Path+" ("+describe(g.Short, f)+")")
		}
	}

	if more := len(parts) - maxSummaryFiles; more > 0 {
		parts = append(parts[:maxSummaryFiles], "and "+strconv.Itoa(more)+" more")
	}

	return strings.Join(parts, ", ")
}

// describe labels a sensitive file for Summary: its category, and where it was moved
// from.
func describe(label string, f Finding) string {
	if f.PreviousPath == "" || f.PreviousPath == f.Path {
		return label
	}

	return label + ", moved from " + f.PreviousPath
}

// Matcher matches paths against the enabled categories' patterns and the user's own.
type Matcher struct {
	rules []rule
}

// rule is one compiled pattern of a category.
type rule struct {
	category Category
	raw      string
	pattern  pattern
}

// NewMatcher compiles the patterns of every category cfg enables, the built-in ones in
// matching order and then the user's. A user's pattern that no longer compiles is
// skipped.
func NewMatcher(cfg Config) *Matcher {
	cats := Categories()
	if len(cfg.Patterns) > 0 {
		cats = append(cats, customCategory(cfg.Patterns))
	}

	m := &Matcher{}

	for _, c := range cats {
		if c.ID != CategoryCustom && !c.Locked && !cfg.Enabled(c.ID) {
			continue
		}

		for _, raw := range c.Patterns {
			p, err := compile(raw)
			if err != nil {
				continue
			}

			m.rules = append(m.rules, rule{category: c, raw: raw, pattern: p})
		}
	}

	return m
}

// Match returns the category and the pattern of the first rule a path matches.
func (m *Matcher) Match(p string) (Category, string, bool) {
	return m.match(splitPath(p))
}

// Evaluate turns a stored result into a Check for a pull request whose head is now head
// ("" when unknown) and whose page is prURL. A nil result, or one for another head, is
// pending; a failed one stays failed whatever its head, so the failure shows until a
// check succeeds.
func (m *Matcher) Evaluate(r *Result, head, prURL string) Check {
	if r == nil {
		return Check{Status: StatusPending, Groups: []Group{}}
	}

	c := Check{
		Head: r.Head, Checked: len(r.Files), Total: max(r.Total, len(r.Files)), Complete: r.Complete,
		Groups: []Group{}, Error: r.Error,
	}

	if !r.CheckedAt.IsZero() {
		c.CheckedAt = r.CheckedAt.UTC().Format(time.RFC3339)
	}

	switch {
	case r.Error != "":
		c.Status = StatusFailed
	case head != "" && !strings.EqualFold(head, r.Head):
		c.Status = StatusPending
	default:
		c.Groups = m.groups(r.Files, prURL)
		c.Status = status(c.Groups, r.Complete)
	}

	return c
}

// match is Match for a path already split.
func (m *Matcher) match(names []string) (Category, string, bool) {
	if len(names) == 0 {
		return Category{}, "", false
	}

	for _, r := range m.rules {
		if r.pattern.match(names) {
			return r.category, r.raw, true
		}
	}

	return Category{}, "", false
}

// status names a finished check's outcome.
func status(groups []Group, complete bool) string {
	switch {
	case len(groups) > 0:
		return StatusFlagged
	case !complete:
		return StatusPartial
	default:
		return StatusClear
	}
}

// groups sorts the sensitive files into their categories, in matching order.
func (m *Matcher) groups(files []File, prURL string) []Group {
	byCategory := make(map[string]*Group)

	var order []string

	for _, f := range files {
		cat, raw, ok := m.matchFile(f)
		if !ok {
			continue
		}

		g, seen := byCategory[cat.ID]
		if !seen {
			g = &Group{
				Category: cat.ID, Label: cat.Label, Short: cat.Short, Description: cat.Description, Level: cat.Level,
			}
			byCategory[cat.ID] = g

			order = append(order, cat.ID)
		}

		g.Files = append(g.Files, Finding{
			Path: f.Path, PreviousPath: f.PreviousPath, Change: f.Change, Pattern: raw, URL: DiffURL(prURL, f.Path),
		})
	}

	out := make([]Group, 0, len(order))

	for _, r := range m.rules {
		g, ok := byCategory[r.category.ID]
		if !ok {
			continue
		}

		slices.SortFunc(g.Files, func(a, b Finding) int { return strings.Compare(a.Path, b.Path) })
		out = append(out, *g)

		delete(byCategory, r.category.ID)
	}

	return out
}

// matchFile matches a changed file by its path or, for a renamed or copied file, by the
// path it came from, whichever matches the earlier rule. A file renamed out of a
// sensitive folder is as sensitive as one renamed into it: moving a workflow away
// disables it.
func (m *Matcher) matchFile(f File) (Category, string, bool) {
	cat, raw, ok := m.match(splitPath(f.Path))

	if f.PreviousPath == "" {
		return cat, raw, ok
	}

	prevCat, prevRaw, prevOK := m.match(splitPath(f.PreviousPath))

	switch {
	case !prevOK:
		return cat, raw, ok
	case !ok || m.rank(prevCat.ID) < m.rank(cat.ID):
		return prevCat, prevRaw, true
	default:
		return cat, raw, ok
	}
}

// rank is a category's place in matching order.
func (m *Matcher) rank(id string) int {
	return slices.IndexFunc(m.rules, func(r rule) bool { return r.category.ID == id })
}

// DiffURL returns the address of a file's diff in a pull request on GitHub: the files
// page, at the anchor GitHub names after the SHA-256 of the file's path. It is "" for a
// page that is not on GitHub.
func DiffURL(prURL, filePath string) string {
	if !strings.HasPrefix(prURL, "https://github.com/") || filePath == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(filePath))

	return strings.TrimRight(prURL, "/") + "/files#diff-" + hex.EncodeToString(sum[:])
}

// PathMatch says whether a path is sensitive under a configuration, and why. Every field
// is a plain value.
type PathMatch struct {
	// Sensitive reports that the path matched a pattern.
	Sensitive bool `json:"sensitive"`

	// Category, Label, and Level describe the category it matched, and Pattern the
	// pattern; all are "" when it matched none.
	Category string `json:"category"`
	Label    string `json:"label"`
	Level    string `json:"level"`
	Pattern  string `json:"pattern"`
}

// Lookup reports whether a path is sensitive under cfg, for trying patterns out.
func Lookup(cfg Config, filePath string) PathMatch {
	cat, raw, ok := NewMatcher(cfg).Match(strings.TrimSpace(filePath))
	if !ok {
		return PathMatch{}
	}

	return PathMatch{Sensitive: true, Category: cat.ID, Label: cat.Label, Level: cat.Level, Pattern: raw}
}
