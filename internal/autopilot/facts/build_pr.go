package facts

import (
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/bump"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/trust"
)

// checkFields are the fields read from the head commit's check names.
func checkFields() []catalog.FieldID {
	return []catalog.FieldID{catalog.FieldFailedNames, catalog.FieldCheckNames, catalog.FieldRequiredFailed}
}

// checks fills the fields read from the head commit's check names, once they are read
// at the head the details name.
func (b *builder) checks() {
	d, c := b.row.Details, b.row.Checks

	var missing Value

	switch {
	case d == nil:
		missing = Unknown(whyDetails, NeedDetails)
	case c == nil:
		missing = Unknown("the check names aren't read yet", NeedChecks)
	case !strings.EqualFold(c.Head, d.HeadOID):
		missing = Unknown("the check names were read at another commit", NeedChecks)
	default:
		b.readChecks(*c)

		return
	}

	for _, id := range checkFields() {
		b.set(id, missing)
	}

	b.done = missing
}

// checkSummary is what a head commit's checks add up to.
type checkSummary struct {
	names, failed []string

	// requiredFailed reports a required check that failed, and requiredUnknown a failed
	// one not known to be required or not.
	requiredFailed, requiredUnknown bool

	// running reports a check not finished, and last when the last one finished.
	running bool
	last    time.Time
}

// add counts one check run or status. GitHub lists a check once per event that ran it,
// so one name can come with several outcomes; a name counts as failed when any of its
// runs on the head commit failed, and appears once.
func (s *checkSummary) add(c CheckRow) {
	name := checkName(c)
	s.names = addOnce(s.names, name)

	if c.Outcome == CheckFailed {
		s.failed = addOnce(s.failed, name)
		s.requiredFailed = s.requiredFailed || (c.Required != nil && *c.Required)
		s.requiredUnknown = s.requiredUnknown || c.Required == nil
	}

	// A skipped run may say it completed before it started, so only completion times
	// count, and a skipped run never counts as running.
	s.running = s.running || c.Outcome == CheckPending || (c.CompletedAt.IsZero() && c.Outcome != CheckSkipped)

	if c.CompletedAt.After(s.last) {
		s.last = c.CompletedAt
	}
}

// addOnce appends s to list unless it is "" or already there.
func addOnce(list []string, s string) []string {
	if s == "" || slices.Contains(list, s) {
		return list
	}

	return append(list, s)
}

// checkName is how a condition reads a check's name: "workflow / job" for a workflow's
// run, as GitHub shows it, and the name alone for an app's run or a status.
func checkName(c CheckRow) string {
	name := line(c.Name, maxNameRunes)
	if w := line(c.Workflow, maxNameRunes); w != "" && name != "" {
		return w + " / " + name
	}

	return name
}

// readChecks fills the check fields from the checks read at the head commit.
func (b *builder) readChecks(c ChecksRow) {
	s := &checkSummary{names: []string{}, failed: []string{}}

	for _, check := range c.Checks {
		s.add(check)
	}

	partial := !c.Complete

	b.set(catalog.FieldCheckNames, List(s.names, partial))
	b.set(catalog.FieldFailedNames, List(s.failed, partial))

	switch {
	case s.requiredFailed:
		b.set(catalog.FieldRequiredFailed, Bool(true))
	case s.requiredUnknown || partial:
		b.set(catalog.FieldRequiredFailed, Unknown("whether a failed check is required isn't known", NeedNothing))
	default:
		b.set(catalog.FieldRequiredFailed, Bool(false))
	}

	switch {
	case len(c.Checks) == 0:
		b.done = Unknown("it has no checks", NeedNothing)
	case partial:
		b.done = Unknown("not every check could be read", NeedNothing)
	case s.running:
		b.done = Unknown("a check is still running", NeedChecks)
	default:
		b.done = Since(s.last)
	}
}

// fileFields are the fields read from the changed files.
func fileFields() []catalog.FieldID {
	return []catalog.FieldID{catalog.FieldPaths, catalog.FieldSensitive}
}

// files fills the fields read from the changed files, once they are checked at the head
// the details name.
func (b *builder) files() {
	d, f := b.row.Details, b.row.Files

	var missing Value

	switch {
	case d == nil:
		missing = Unknown(whyDetails, NeedDetails)
	case f == nil:
		missing = Unknown("the changed files aren't checked yet", NeedFiles)
	case f.Error != "":
		missing = Unknown("the last check of its files failed", NeedFiles)
	case !strings.EqualFold(f.Head, d.HeadOID):
		missing = Unknown("the files were checked at another commit", NeedFiles)
	default:
		b.readFiles(*f)

		return
	}

	for _, id := range fileFields() {
		b.set(id, missing)
	}
}

// readFiles fills the paths a pull request changes, a renamed file's earlier path too,
// and the sensitive categories they touch.
func (b *builder) readFiles(f FilesRow) {
	matcher := sensitive.NewMatcher(b.env.Sensitive)
	paths := make([]string, 0, len(f.Files))
	categories := []string{}

	for _, file := range f.Files {
		for _, p := range []string{line(file.Path, maxPathRunes), line(file.PreviousPath, maxPathRunes)} {
			paths = addOnce(paths, p)

			if cat, _, ok := matcher.Match(p); ok {
				categories = addOnce(categories, cat.ID)
			}
		}
	}

	b.set(catalog.FieldPaths, List(paths, !f.Complete))
	b.set(catalog.FieldSensitive, List(categories, !f.Complete))
}

// depFields are the fields read from a dependency update's title and branch, beside the
// tool.
func depFields() []catalog.FieldID {
	return []catalog.FieldID{
		catalog.FieldEcosystem, catalog.FieldPackage, catalog.FieldFrom, catalog.FieldTo, catalog.FieldUpdateType,
		catalog.FieldGrouped, catalog.FieldSecurity,
	}
}

// dependency fills the dependency update's fields. They apply only to a pull request
// Dependabot or Renovate opened; on one of theirs whose title doesn't read as an update,
// they aren't known.
func (b *builder) dependency() {
	tool, toolValue := b.tool()
	b.set(catalog.FieldDepTool, toolValue)

	var missing Value

	switch {
	case !toolValue.Known():
		missing = toolValue
	case tool == bump.ToolNone:
		missing = NotApplicable("Dependabot or Renovate didn't open it")
	default:
		b.readUpdate(tool)

		return
	}

	for _, id := range depFields() {
		b.set(id, missing)
	}
}

// tool returns the bot that opened the pull request, by its type and login.
func (b *builder) tool() (bump.Tool, Value) {
	r := b.row
	if r.AuthorType == "" {
		return bump.ToolNone, Unknown(whyAuthorType, NeedNothing)
	}

	login := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(r.Author), "[bot]"))

	if r.AuthorType == trust.TypeBot {
		for _, tool := range []bump.Tool{bump.ToolDependabot, bump.ToolRenovate} {
			if login == string(tool) {
				return tool, Text(string(tool))
			}
		}
	}

	return bump.ToolNone, Text(catalog.ToolNone)
}

// readUpdate fills the update's fields from its title, and its ecosystem from its head
// branch when the branch is the same bot's.
func (b *builder) readUpdate(tool bump.Tool) {
	if br, ok := bump.ParseBranch(b.row.HeadRef); ok && br.Tool == tool && br.Ecosystem != bump.EcosystemUnknown {
		b.set(catalog.FieldEcosystem, Text(string(br.Ecosystem)))
	} else {
		b.set(catalog.FieldEcosystem, Unknown("the head branch doesn't name an ecosystem", NeedNothing))
	}

	u, ok := bump.Parse(b.row.Title)
	if !ok {
		for _, id := range depFields()[1:] {
			b.set(id, Unknown(whyNotUpdate, NeedNothing))
		}

		return
	}

	b.set(catalog.FieldPackage, textOrUnknown(u.Name, "a grouped update names no single package"))
	b.set(catalog.FieldFrom, textOrUnknown(u.From, "the title doesn't say the version it updates from"))
	b.set(catalog.FieldTo, textOrUnknown(u.To, "the title doesn't say the version it updates to"))
	b.set(catalog.FieldUpdateType, textOrUnknown(string(u.Level), "the title doesn't say how big the update is"))
	b.set(catalog.FieldGrouped, Bool(u.Name == "" || u.Count > 1 || u.Group != ""))
	b.set(catalog.FieldSecurity, Bool(u.Security))
}

// textOrUnknown returns s as one line, or unknown with why when it is "".
func textOrUnknown(s, why string) Value {
	if s = line(s, maxNameRunes); s == "" {
		return Unknown(why, NeedNothing)
	}

	return Text(s)
}
