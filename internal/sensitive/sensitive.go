// Package sensitive decides which of a pull request's changed files deserve a closer look
// before anyone reviews, merges, or opens an agent on it: files that run on CI with the
// repository's token and secrets, files a coding agent loads or runs, files that run on
// the reviewer's machine, files that change who must review, and files that bring in
// third-party code.
//
// Only pull requests from outside the repository are checked: every pull request from a
// fork, from someone who is not the repository's owner, an org member, or a collaborator,
// and from a bot not on the trusted list. Collaborators' pull requests are checked too
// unless the user narrows the scope. The repository's own people, the trusted bots, and
// the signed-in user are never checked.
//
// A check reads each changed file's path, and a renamed file's previous path, never its
// content. A path is sensitive when it matches a pattern of an enabled category: the
// built-in categories, in the order Categories returns them, then the user's own
// patterns. A file is reported under the first category that matches it.
//
// Patterns follow .gitignore, matched without regard to case, since a case-insensitive
// file system, as macOS uses by default, opens .Claude/Settings.json for
// .claude/settings.json:
//
//   - A pattern without a slash matches a file or folder of that name at any depth:
//     CLAUDE.md, *.sh.
//   - A pattern with a slash at its start or in its middle is matched from the
//     repository's root: .github/workflows, /Makefile.
//   - A pattern that ends with a slash matches only a folder: .claude/.
//   - A pattern that matches a folder matches everything inside it.
//   - * matches any run of characters within one name, ? one character, [abc] one of a
//     set, and ** any number of folders: deploy/**/*.tf.
//
// The settings are kept in the prefs table under the files.* keys, which the settings
// service writes and the inbox and the launcher read. A missing or unreadable value falls
// back to its default.
package sensitive

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/trust"
)

// Category IDs. The built-in ones are listed by Categories; CategoryCustom holds the
// user's own patterns.
const (
	// CategoryAgent is what coding agents load or run: settings, hooks, MCP servers, and
	// instructions. It is always on, because launches are held back by it.
	CategoryAgent = "agent"

	// CategoryWorkflows is what runs on CI with the repository's token and secrets.
	CategoryWorkflows = "workflows"

	// CategoryTooling is what runs on the reviewer's machine: build scripts, git hooks,
	// editor and container settings.
	CategoryTooling = "tooling"

	// CategoryReview is what decides who must review, and what merges or updates on its
	// own.
	CategoryReview = "review"

	// CategoryDependencies is what brings in third-party code or changes where it comes
	// from.
	CategoryDependencies = "dependencies"

	// CategoryCustom holds the user's own patterns.
	CategoryCustom = "custom"
)

// Level says how closely a category's files deserve a look.
type Level string

// Levels, from the most pressing.
const (
	// LevelDanger is for files that run, or steer an agent, without anyone choosing to
	// run them.
	LevelDanger Level = "danger"

	// LevelWarning is for files that change what the project trusts or depends on.
	LevelWarning Level = "warning"

	// LevelNone is the level of a path that matched no category.
	LevelNone Level = ""
)

// Scope says whose pull requests are checked.
type Scope string

// Scopes, from the narrowest.
const (
	// ScopeOutside checks every pull request from a fork, from outside contributors, and
	// from bots not on the trusted list.
	ScopeOutside Scope = "outside"

	// ScopeCollaborators also checks collaborators' pull requests: people with access to
	// the repository who are not its owner or members of its org.
	ScopeCollaborators Scope = "collaborators"
)

// DefaultScope is the scope of a fresh database.
const DefaultScope = ScopeCollaborators

// Prefs keys for the settings, which the settings package writes.
const (
	// PrefScope is ScopeOutside or ScopeCollaborators.
	PrefScope = "files.scope"

	// PrefOff lists the built-in categories turned off, as a JSON array of IDs.
	PrefOff = "files.off"

	// PrefPatterns lists the user's patterns, as a JSON array of strings.
	PrefPatterns = "files.patterns"
)

// Bounds on the user's patterns.
const (
	// MaxPatterns caps how many patterns the user can add.
	MaxPatterns = 100

	// MaxPatternLength caps one pattern, in bytes.
	MaxPatternLength = 200

	// maxDoubleStars caps the ** segments in one pattern, which each multiply the work
	// of a match.
	maxDoubleStars = 4
)

var (
	// ErrInvalidScope is returned for a scope that is not one of the choices.
	ErrInvalidScope = errors.New("unknown scope")

	// ErrUnknownCategory is returned for a category ID that is not built in.
	ErrUnknownCategory = errors.New("unknown category")

	// ErrLockedCategory is returned when turning off a category that is always on.
	ErrLockedCategory = errors.New("this category is always on")

	// ErrInvalidPattern is returned for a pattern that cannot be used.
	ErrInvalidPattern = errors.New("invalid pattern")

	// ErrTooManyPatterns is returned for more than MaxPatterns patterns.
	ErrTooManyPatterns = errors.New("too many patterns")

	// ErrNotChecked is returned for an item whose files are not checked: an issue, or a
	// pull request from the repository's own people, a trusted bot, or the signed-in user.
	ErrNotChecked = errors.New("this item's files are not checked")
)

// Category is one kind of sensitive file. Every field is a plain value.
type Category struct {
	// ID is what the settings store, such as "workflows".
	ID string `json:"id"`

	// Label names the category in full, and Short on a card.
	Label string `json:"label"`
	Short string `json:"short"`

	// Description says why its files deserve a look.
	Description string `json:"description"`

	// Level is LevelDanger or LevelWarning.
	Level Level `json:"level"`

	// Locked reports a category that cannot be turned off.
	Locked bool `json:"locked"`

	// Patterns are the category's patterns, in the syntax the package describes.
	Patterns []string `json:"patterns"`
}

// Categories returns the built-in categories, in the order a file is matched against
// them.
func Categories() []Category {
	return []Category{
		{
			ID: CategoryAgent, Label: "Agent configuration", Short: "Agent config", Level: LevelDanger, Locked: true,
			Description: "Settings, hooks, MCP servers, and instructions that coding agents load. Hooks run " +
				"commands as soon as a session starts, even in plan mode.",
			Patterns: []string{
				".claude/", ".mcp.json", "CLAUDE.md", "CLAUDE.local.md", "AGENTS.md", "AGENTS.override.md",
				"GEMINI.md", ".gemini/", ".codex/", ".cursor/", ".cursorrules", ".windsurfrules",
				".github/copilot-instructions.md", ".github/instructions/", ".github/prompts/",
			},
		},
		{
			ID: CategoryWorkflows, Label: "Workflows and CI", Short: "Workflows", Level: LevelDanger,
			Description: "Run on CI with the repository's token and secrets once merged, and some run on the " +
				"pull request itself.",
			Patterns: []string{
				".github/workflows/", ".github/actions/", "action.yml", "action.yaml", ".github/env/",
				".gitlab-ci.yml", ".circleci/", ".buildkite/", "Jenkinsfile", "azure-pipelines.yml", ".travis.yml",
				".goreleaser.yml", ".goreleaser.yaml",
			},
		},
		{
			ID: CategoryTooling, Label: "Build and dev tooling", Short: "Build tooling", Level: LevelDanger,
			Description: "Runs on your machine when you build, test, install, or commit, or open the project " +
				"in an editor or container.",
			Patterns: []string{
				"Makefile", "GNUmakefile", "*.mk", "magefile.go", "magefiles/", "Taskfile.yml", "Taskfile.yaml",
				"justfile", ".justfile", "scripts/", "*.sh", "*.bash", "*.zsh", "*.ps1", ".pre-commit-config.yaml",
				".husky/", ".githooks/", "lefthook.yml", ".lefthook.yml", ".envrc", "mise.toml", ".mise.toml",
				".vscode/", ".idea/", ".devcontainer/", ".devcontainer.json", "Dockerfile", "*.dockerfile",
				"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml", "package.json",
				"setup.py", "build.rs",
			},
		},
		{
			ID: CategoryReview, Label: "Review and merge rules", Short: "Review rules", Level: LevelWarning,
			Description: "Decide who must review a change, and what updates or merges on its own.",
			Patterns: []string{
				"CODEOWNERS", ".github/dependabot.yml", ".github/dependabot.yaml", "renovate.json", "renovate.json5",
				".renovaterc", ".renovaterc.json", ".mergify.yml", ".mergify/", ".github/mergify.yml",
				".github/settings.yml",
			},
		},
		{
			ID: CategoryDependencies, Label: "Dependencies", Short: "Dependencies", Level: LevelWarning,
			Description: "Bring in third-party code, or change where it comes from.",
			Patterns: []string{
				"go.mod", "go.sum", "go.work", "go.work.sum", "vendor/", ".gitmodules", "package-lock.json",
				"npm-shrinkwrap.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "yarn.lock", "bun.lock", "bun.lockb",
				".npmrc", ".yarnrc", ".yarnrc.yml", "requirements*.txt", "pyproject.toml", "poetry.lock", "uv.lock",
				"Pipfile", "Pipfile.lock", "Cargo.toml", "Cargo.lock", "Gemfile", "Gemfile.lock", "*.gemspec",
				"composer.json", "composer.lock",
			},
		},
	}
}

// customCategory describes the user's own patterns.
func customCategory(patterns []string) Category {
	return Category{
		ID: CategoryCustom, Label: "Your patterns", Short: "Your patterns", Level: LevelDanger,
		Description: "Patterns you added in Settings.", Patterns: patterns,
	}
}

// Config is what the user chose. Every field is a plain value.
type Config struct {
	// Scope is ScopeOutside or ScopeCollaborators.
	Scope Scope `json:"scope"`

	// Off lists the built-in categories turned off, sorted. A locked category is never
	// in it.
	Off []string `json:"off"`

	// Patterns are the user's own patterns, in the order they were added.
	Patterns []string `json:"patterns"`
}

// DefaultConfig is the configuration of a fresh database: collaborators checked too,
// every category on, and no patterns of the user's.
func DefaultConfig() Config {
	return Config{Scope: DefaultScope, Off: []string{}, Patterns: []string{}}
}

// Enabled reports whether a built-in category is on.
func (c Config) Enabled(id string) bool {
	return !slices.Contains(c.Off, id)
}

// DecodeConfig reads the stored settings; each one missing or unreadable falls back to
// its default. A stored category that is unknown or locked, or a pattern that is no longer
// valid, is dropped.
func DecodeConfig(prefs map[string]string) Config {
	cfg := DefaultConfig()

	if v := Scope(prefs[PrefScope]); v.Valid() {
		cfg.Scope = v
	}

	cfg.Off = decodeOff(prefs[PrefOff])
	cfg.Patterns = decodePatterns(prefs[PrefPatterns])

	return cfg
}

// decodeOff reads PrefOff: the built-in categories turned off, sorted, without unknown
// or locked ones or repeats.
func decodeOff(raw string) []string {
	var stored []string

	_ = json.Unmarshal([]byte(raw), &stored) // unreadable is none off

	off := []string{}

	for _, id := range stored {
		if c, ok := builtIn(id); ok && !c.Locked && !slices.Contains(off, id) {
			off = append(off, id)
		}
	}

	slices.Sort(off)

	return off
}

// decodePatterns reads PrefPatterns: the user's patterns that still compile, at most
// MaxPatterns of them.
func decodePatterns(raw string) []string {
	var stored []string

	_ = json.Unmarshal([]byte(raw), &stored) // unreadable is no patterns

	patterns := []string{}

	for _, p := range stored {
		if _, err := compile(p); err == nil && len(patterns) < MaxPatterns {
			patterns = append(patterns, p)
		}
	}

	return patterns
}

// Valid reports whether the scope is one of the choices.
func (s Scope) Valid() bool {
	return s == ScopeOutside || s == ScopeCollaborators
}

// SetCategory returns off with a built-in category turned on or off, sorted, for PrefOff.
// A locked category cannot be turned off.
func SetCategory(off []string, id string, on bool) ([]string, error) {
	c, ok := builtIn(id)
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownCategory, id)
	}

	out := slices.DeleteFunc(slices.Clone(off), func(v string) bool { return v == id })

	if !on {
		if c.Locked {
			return nil, fmt.Errorf("%w: %s", ErrLockedCategory, c.Label)
		}

		out = append(out, id)
	}

	slices.Sort(out)

	return out, nil
}

// NormalizePatterns cleans the user's patterns as entered: each is trimmed, empty ones
// and repeats (ignoring case) are dropped, and the order is kept. A pattern that cannot
// be used is an ErrInvalidPattern naming it, and more than MaxPatterns is
// ErrTooManyPatterns.
func NormalizePatterns(patterns []string) ([]string, error) {
	out := make([]string, 0, len(patterns))
	seen := make(map[string]bool, len(patterns))

	for _, raw := range patterns {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}

		if _, err := compile(p); err != nil {
			return nil, err
		}

		if key := strings.ToLower(p); !seen[key] {
			seen[key] = true

			out = append(out, p)
		}
	}

	if len(out) > MaxPatterns {
		return nil, fmt.Errorf("%w: at most %d", ErrTooManyPatterns, MaxPatterns)
	}

	return out, nil
}

// EncodeList writes a list for PrefOff or PrefPatterns.
func EncodeList(list []string) string {
	if list == nil {
		list = []string{}
	}

	b, _ := json.Marshal(list) //nolint:errchkjson // a []string always encodes

	return string(b)
}

// Checked reports whether a pull request's files are checked under scope: every pull
// request whose trust class is trust.ClassOutside, and a collaborator's under
// ScopeCollaborators, but never one the signed-in user opened (mine).
func Checked(scope Scope, class trust.Class, association string, mine bool) bool {
	if mine {
		return false
	}

	if class == trust.ClassOutside {
		return true
	}

	return scope == ScopeCollaborators && class == trust.ClassMember &&
		strings.EqualFold(strings.TrimSpace(association), trust.AssociationCollaborator)
}

// builtIn returns the built-in category with id.
func builtIn(id string) (Category, bool) {
	for _, c := range Categories() {
		if c.ID == id {
			return c, true
		}
	}

	return Category{}, false
}
