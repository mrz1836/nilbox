package bump

import (
	"regexp"
	"slices"
	"strings"
)

// Tool is the bot that opened a dependency update.
type Tool string

const (
	// ToolNone is a branch neither bot would name.
	ToolNone Tool = ""

	// ToolDependabot is GitHub's Dependabot.
	ToolDependabot Tool = "dependabot"

	// ToolRenovate is Mend's Renovate.
	ToolRenovate Tool = "renovate"
)

// Ecosystem is a package ecosystem, named as Dependabot's configuration names it (its
// package-ecosystem values), so a condition reads the way the repository's
// dependabot.yml does.
type Ecosystem string

// The ecosystems a branch can name. EcosystemUnknown is a branch that names none.
const (
	EcosystemUnknown       Ecosystem = ""
	EcosystemGo            Ecosystem = "gomod"
	EcosystemNPM           Ecosystem = "npm"
	EcosystemActions       Ecosystem = "github-actions"
	EcosystemDocker        Ecosystem = "docker"
	EcosystemDockerCompose Ecosystem = "docker-compose"
	EcosystemPip           Ecosystem = "pip"
	EcosystemUV            Ecosystem = "uv"
	EcosystemBundler       Ecosystem = "bundler"
	EcosystemCargo         Ecosystem = "cargo"
	EcosystemComposer      Ecosystem = "composer"
	EcosystemMaven         Ecosystem = "maven"
	EcosystemGradle        Ecosystem = "gradle"
	EcosystemNuGet         Ecosystem = "nuget"
	EcosystemTerraform     Ecosystem = "terraform"
	EcosystemSubmodules    Ecosystem = "gitsubmodule"
	EcosystemMix           Ecosystem = "mix"
	EcosystemElm           Ecosystem = "elm"
	EcosystemPub           Ecosystem = "pub"
	EcosystemSwift         Ecosystem = "swift"
	EcosystemDevcontainers Ecosystem = "devcontainers"
	EcosystemBun           Ecosystem = "bun"
	EcosystemHelm          Ecosystem = "helm"
)

// Branch is what a dependency update's head branch says.
type Branch struct {
	// Tool is the bot whose branch it is.
	Tool Tool `json:"tool"`

	// Ecosystem is the ecosystem the update is in, EcosystemUnknown when the branch
	// doesn't say, as Renovate's don't. A Dependabot branch naming an ecosystem this
	// package doesn't know gives Dependabot's own word for it, unchanged.
	Ecosystem Ecosystem `json:"ecosystem"`
}

// EcosystemInfo names one ecosystem.
type EcosystemInfo struct {
	ID    Ecosystem `json:"id"`
	Label string    `json:"label"`
}

// branchManager is how Dependabot names an ecosystem in a branch, which is its own
// package manager's name rather than the configuration's word.
type branchManager struct {
	word      string
	ecosystem Ecosystem
	label     string
}

// branchManagers are the package managers Dependabot names in its branches, with the
// ecosystem each one is in.
func branchManagers() []branchManager {
	return []branchManager{
		{word: "go_modules", ecosystem: EcosystemGo, label: "Go modules"},
		{word: "npm_and_yarn", ecosystem: EcosystemNPM, label: "npm, Yarn, and pnpm"},
		{word: "github_actions", ecosystem: EcosystemActions, label: "GitHub Actions"},
		{word: "docker", ecosystem: EcosystemDocker, label: "Docker"},
		{word: "docker_compose", ecosystem: EcosystemDockerCompose, label: "Docker Compose"},
		{word: "pip", ecosystem: EcosystemPip, label: "pip"},
		{word: "uv", ecosystem: EcosystemUV, label: "uv"},
		{word: "bundler", ecosystem: EcosystemBundler, label: "Bundler"},
		{word: "cargo", ecosystem: EcosystemCargo, label: "Cargo"},
		{word: "composer", ecosystem: EcosystemComposer, label: "Composer"},
		{word: "maven", ecosystem: EcosystemMaven, label: "Maven"},
		{word: "gradle", ecosystem: EcosystemGradle, label: "Gradle"},
		{word: "nuget", ecosystem: EcosystemNuGet, label: "NuGet"},
		{word: "terraform", ecosystem: EcosystemTerraform, label: "Terraform"},
		{word: "submodules", ecosystem: EcosystemSubmodules, label: "Git submodules"},
		{word: "hex", ecosystem: EcosystemMix, label: "Hex (Mix)"},
		{word: "elm", ecosystem: EcosystemElm, label: "Elm"},
		{word: "pub", ecosystem: EcosystemPub, label: "Pub"},
		{word: "swift", ecosystem: EcosystemSwift, label: "Swift"},
		{word: "devcontainers", ecosystem: EcosystemDevcontainers, label: "Dev containers"},
		{word: "bun", ecosystem: EcosystemBun, label: "Bun"},
		{word: "helm", ecosystem: EcosystemHelm, label: "Helm"},
	}
}

// Ecosystems returns the ecosystems a branch names, the most common first, each with
// the words the builder shows for it.
func Ecosystems() []EcosystemInfo {
	managers := branchManagers()
	out := make([]EcosystemInfo, 0, len(managers))

	for _, m := range managers {
		out = append(out, EcosystemInfo{ID: m.ecosystem, Label: m.label})
	}

	return out
}

// managerWord matches a package manager's name as a branch spells it.
var managerWord = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// ParseBranch reads a pull request's head branch as Dependabot or Renovate names it, and
// reports false for a branch neither would write.
//
// Dependabot's branches are "dependabot/<manager>/<where and what>", such as
// "dependabot/go_modules/golang.org/x/net-0.31.0" or a group's
// "dependabot/npm_and_yarn/frontend/npm-deps-1a2b3c4d5e", and a repository may choose "-"
// or "_" in place of each "/". The manager is the package manager's name, which this
// reads as the ecosystem it is in. Renovate's branches are "renovate/<topic>", which
// name no ecosystem.
//
// It reads only the branch's name, so a person's branch named like a bot's reads as the
// bot's: who opened the pull request is the caller's to check.
func ParseBranch(head string) (Branch, bool) {
	head = strings.TrimSpace(head)

	if rest, ok := strings.CutPrefix(head, "renovate/"); ok && rest != "" {
		return Branch{Tool: ToolRenovate}, true
	}

	rest, ok := strings.CutPrefix(head, "dependabot")
	if !ok || rest == "" {
		return Branch{}, false
	}

	sep, rest := rest[:1], rest[1:]
	if !slices.Contains([]string{"/", "-", "_"}, sep) {
		return Branch{}, false
	}

	if eco, ok := knownManager(rest, sep); ok {
		return Branch{Tool: ToolDependabot, Ecosystem: eco}, true
	}

	return unknownManager(rest, sep)
}

// knownManager reads the manager at the start of rest, followed by sep and more, as
// one of the managers Dependabot names; the longest that fits wins, so "docker_compose"
// is never read as "docker".
func knownManager(rest, sep string) (Ecosystem, bool) {
	best := branchManager{}

	for _, m := range branchManagers() {
		after, ok := strings.CutPrefix(rest, m.word+sep)
		if ok && after != "" && len(m.word) > len(best.word) {
			best = m
		}
	}

	return best.ecosystem, best.word != ""
}

// unknownManager reads a manager this package doesn't know, which only a "/" separator
// sets apart without doubt: its name is given as it is.
func unknownManager(rest, sep string) (Branch, bool) {
	if sep != "/" {
		return Branch{}, false
	}

	word, after, found := strings.Cut(rest, "/")
	if !found || after == "" || !managerWord.MatchString(word) {
		return Branch{}, false
	}

	return Branch{Tool: ToolDependabot, Ecosystem: Ecosystem(word)}, true
}
