package scripts

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Scope is which clones one of the user's own scripts shows in.
type Scope string

// The scopes.
const (
	// ScopeAll shows the script in every clone.
	ScopeAll Scope = "all"

	// ScopeGo shows it in a clone with a go.mod at its root.
	ScopeGo Scope = "go"

	// ScopeRepos shows it in the clones of the repositories it names.
	ScopeRepos Scope = "repos"
)

// Valid reports whether s is one of the scopes.
func (s Scope) Valid() bool {
	return s == ScopeAll || s == ScopeGo || s == ScopeRepos
}

// Icon is what one of the user's scripts shows beside its name: t3.json's icon names.
type Icon string

// The icons.
const (
	IconPlay      Icon = "play"
	IconTest      Icon = "test"
	IconLint      Icon = "lint"
	IconBuild     Icon = "build"
	IconConfigure Icon = "configure"
)

// Valid reports whether i is one of the icons.
func (i Icon) Valid() bool {
	return slices.Contains([]Icon{IconPlay, IconTest, IconLint, IconBuild, IconConfigure}, i)
}

// RunsIn is where one of the user's scripts runs: where Settings → Integrations sends
// runs, or a place of its own.
type RunsIn string

// The places a script of the user's can name.
const (
	// RunsInSetting runs it where the setting sends runs.
	RunsInSetting RunsIn = ""

	// RunsInApp runs it in the app.
	RunsInApp RunsIn = "app"

	// RunsInTerminal runs it in the user's terminal, for a command that asks questions.
	RunsInTerminal RunsIn = "terminal"
)

// Valid reports whether r is one of the places.
func (r RunsIn) Valid() bool {
	return r == RunsInSetting || r == RunsInApp || r == RunsInTerminal
}

// Direction is the way Move moves a script in the order.
type Direction string

// The directions.
const (
	MoveUp   Direction = "up"
	MoveDown Direction = "down"
)

// Limits on what a script of the user's holds.
const (
	maxNameRunes    = 100
	maxCommandBytes = 8 << 10
)

// ErrInvalidScript is returned by Save for a script whose fields aren't one.
var ErrInvalidScript = errors.New("invalid script")

// UserScript is one of the user's own scripts, saved in the app: a command they run from
// Repo scripts, in every clone its scope includes, needing no trust (D13).
type UserScript struct {
	// ID names it; Save creates one whose ID is 0.
	ID int64 `json:"id"`

	// Name is what the page calls it, and Command the shell command it runs in the
	// clone, as the user would type it in a terminal.
	Name    string `json:"name"`
	Command string `json:"command"`

	// Icon is what shows beside its name.
	Icon Icon `json:"icon"`

	// Scope is which clones it shows in, and Repos, for ScopeRepos, the repositories
	// whose clones those are, owner/name; empty for any other scope.
	Scope Scope    `json:"scope"`
	Repos []string `json:"repos"`

	// RunsIn is where it runs, unless a run asks for the other place.
	RunsIn RunsIn `json:"runsIn"`

	// Confirm asks before each run. The window asks: it is the user's own setting, which
	// the service leaves to them.
	Confirm bool `json:"confirm"`
}

// includes reports whether u shows in a clone of the repository name, the zero Name for
// one whose origin isn't on GitHub, with a go.mod at its root when goModule.
func (u UserScript) includes(name repo.Name, goModule bool) bool {
	switch u.Scope {
	case ScopeAll:
		return true
	case ScopeGo:
		return goModule
	case ScopeRepos:
		return name.Valid() && slices.ContainsFunc(u.Repos, func(r string) bool { return strings.EqualFold(r, name.String()) })
	}

	return false
}

// checked returns u as Save stores it, its name and command trimmed and its repositories
// read, or says in words which field isn't one.
func checked(u UserScript) (UserScript, error) {
	u.Name, u.Command = strings.TrimSpace(u.Name), strings.TrimSpace(u.Command)

	if err := cmp.Or(checkText(u), checkChoices(u)); err != nil {
		return UserScript{}, err
	}

	repos, err := scopeRepos(u.Scope, u.Repos)
	if err != nil {
		return UserScript{}, err
	}

	u.Repos = repos

	return u, nil
}

// checkText says why u's name or command, trimmed, isn't one: missing, too long, or
// holding a character that hides what it says.
func checkText(u UserScript) error {
	switch {
	case u.Name == "":
		return invalidScript("Name the script.", "no name")
	case utf8.RuneCountInString(u.Name) > maxNameRunes:
		return invalidScript(fmt.Sprintf("Keep the name to %d characters.", maxNameRunes), "a long name")
	case hidden(u.Name):
		return invalidScript("The name holds a character that can't be shown, such as a line break.",
			"a hidden character in the name")
	case u.Command == "":
		return invalidScript("Write the command it runs.", "no command")
	case len(u.Command) > maxCommandBytes:
		return invalidScript(fmt.Sprintf("Keep the command under %d KB.", maxCommandBytes>>10), "a long command")
	case hidden(u.Command):
		return invalidScript("The command holds a character that hides what it runs, such as a line break or a "+
			"text direction mark. Write it on one line.", "a hidden character in the command")
	}

	return nil
}

// checkChoices says which of u's scope, place, and icon isn't one there is.
func checkChoices(u UserScript) error {
	switch {
	case !u.Scope.Valid():
		return invalidScript("Choose the repositories it shows in.", "the scope "+string(u.Scope))
	case !u.RunsIn.Valid():
		return invalidScript("Choose where it runs.", "the place "+string(u.RunsIn))
	case !u.Icon.Valid():
		return invalidScript("Choose its icon.", "the icon "+string(u.Icon))
	}

	return nil
}

// scopeRepos returns the repositories a script of scope shows in: for ScopeRepos, given
// read as owner/name, each once, in any case, sorted; for any other scope, none.
func scopeRepos(scope Scope, given []string) ([]string, error) {
	out := []string{}
	if scope != ScopeRepos {
		return out, nil
	}

	seen := map[string]bool{}

	for _, r := range given {
		name, err := repo.Parse(strings.TrimSpace(r))
		if err != nil {
			return nil, invalidScript(fmt.Sprintf("%q isn't a repository: write it as owner/name.", r), "a repository")
		}

		if !seen[name.Key()] {
			seen[name.Key()] = true

			out = append(out, name.String())
		}
	}

	if len(out) == 0 {
		return nil, invalidScript("Choose a repository for it to show in.", "no repository")
	}

	slices.SortFunc(out, byFold)

	return out, nil
}

// byFold orders words as the user reads them: in any case, then by case.
func byFold(a, b string) int {
	return cmp.Or(cmp.Compare(strings.ToLower(a), strings.ToLower(b)), cmp.Compare(a, b))
}

// invalidScript refuses a script, saying msg; what names the field for the log.
func invalidScript(msg, what string) error {
	return uierr.New(msg, fmt.Errorf("%w: %s", ErrInvalidScript, what))
}

// copyName is the name Duplicate gives a copy of the script called name: "<name> copy",
// with name cut short when the two would be over the limit.
func copyName(name string) string {
	const suffix = " copy"

	runes := []rune(name)
	if keep := maxNameRunes - utf8.RuneCountInString(suffix); len(runes) > keep {
		runes = runes[:keep]
	}

	return strings.TrimSpace(string(runes)) + suffix
}

// goModule reports whether the clone at dir has a go.mod at its root, which makes it one
// of the Go repositories ScopeGo means.
func goModule(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "go.mod"))

	return err == nil && info.Mode().IsRegular()
}

// yoursRef names the user's script id within any clone, for a run.
func yoursRef(id int64) string {
	return ref(SourceYours, strconv.FormatInt(id, 10))
}

// yoursScript works out u as the page lists it and a run runs it: [$SHELL, -c, command] in
// the clone, run exactly as written, needing no trust; greyed when its name or command
// holds a character that hides what it runs, and pointed to Release when it would bump a
// version.
func yoursScript(u UserScript) script {
	mine := u
	mine.Repos = append([]string{}, u.Repos...)
	sc := script{
		entry: Entry{
			Ref: yoursRef(u.ID), Source: SourceYours, Name: u.Name, Command: u.Command, Icon: string(u.Icon),
			Runnable: true, Release: bumps([]string{u.Command}, nil), Inputs: []Input{}, Mine: &mine,
		},
		name: strconv.FormatInt(u.ID, 10), place: integrationprefs.RunsIn(u.RunsIn),
		parts: []part{{kind: partShell, command: u.Command, raw: true}}, inputs: map[string]Input{},
	}

	if anyHidden(u.Name, u.Command) {
		sc.entry.Runnable, sc.entry.Why = false, hiddenWhy
	}

	return sc
}

// envPrefix starts the names of the variables a script of the user's runs with: the
// product's slug, in capitals.
func envPrefix() string {
	return strings.ToUpper(brand.Slug)
}

// Variables names the variables each of the user's scripts runs with: the clone's
// repository (owner/name, or "" when its origin isn't on GitHub), its folder, and its
// repository's default branch (or "" when the app can't tell), so a command never has to
// quote a path.
func Variables() []string {
	p := envPrefix()

	return []string{p + "_REPO", p + "_REPO_DIR", p + "_DEFAULT_BRANCH"}
}

// withVars returns env with each of vars, KEY=value, set in place of any value it had.
func withVars(env, vars []string) []string {
	keys := make([]string, 0, len(vars))
	for _, kv := range vars {
		key, _, _ := strings.Cut(kv, "=")
		keys = append(keys, key)
	}

	out := make([]string, 0, len(env)+len(vars))

	for _, kv := range env {
		if key, _, _ := strings.Cut(kv, "="); !slices.Contains(keys, key) {
			out = append(out, kv)
		}
	}

	return append(out, vars...)
}
