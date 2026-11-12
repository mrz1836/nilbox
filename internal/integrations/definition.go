package integrations

import (
	"maps"
	"slices"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/shellenv"
)

// Feature is an upstream change a tool gained in some release, which the app uses once
// the copy it finds has it.
type Feature string

// The features the app looks for.
const (
	// FeaturePushFails is magex failing version:bump when the push it was asked for
	// didn't happen.
	FeaturePushFails Feature = "pushFails"

	// FeatureExpect is magex's expect=, which refuses to tag any version but the one
	// named.
	FeatureExpect Feature = "expect"

	// FeatureBumpJSON is magex's json=true, which reports version:bump's outcome as one
	// JSON line.
	FeatureBumpJSON Feature = "bumpJson"

	// FeatureFastForwardPull is magex's ff-only, which updates the branch by fast-forward
	// only and refuses to tag otherwise.
	FeatureFastForwardPull Feature = "fastForwardPull"

	// FeatureVerifiedTools is go-pre-commit installing its own tools from checksum-verified
	// releases, with an opt-out.
	FeatureVerifiedTools Feature = "verifiedTools"

	// FeatureInstallExitCode is go-pre-commit's install exiting non-zero when it skips a
	// hook.
	FeatureInstallExitCode Feature = "installExitCode"

	// FeatureStatusJSON is go-pre-commit's status --json.
	FeatureStatusJSON Feature = "statusJson"

	// FeatureRunJSON is go-pre-commit's run --json.
	FeatureRunJSON Feature = "runJson"

	// FeatureSelfUpdate is a tool's own updater, which the app runs to update a copy that
	// can update itself: it downloads the latest release, checks it against the release's
	// SHA-256 checksums, and swaps it in for the copy it runs as, with a rename. Older
	// updaters put the new copy elsewhere, or ran go install.
	FeatureSelfUpdate Feature = "selfUpdate"
)

// Definition describes one tool the app can find, install, and drive. Adding a tool is
// one more Definition and, if it gets a page, one more page.
type Definition struct {
	// ID is the tool's integration, which keys its preferences.
	ID integrationprefs.ID

	// Name is what the settings call it, Binary its program's name, and Repo where its
	// releases live.
	Name   string
	Binary string
	Repo   repo.Name

	// Purpose is the settings card's one line on what it does for the user.
	Purpose string

	// VersionArgs make it print its version, which ParseVersion reads.
	VersionArgs  []string
	ParseVersion func([]byte) (Version, error)

	// Asset names a release's archive for an OS and architecture, and Checksums the
	// release's checksums file. Both take the version without a leading v.
	Asset     func(version, goos, goarch string) string
	Checksums func(version string) string

	// QuietEnv is set on every run of it: no update banners, no color.
	QuietEnv map[string]string

	// UpdateArgs make it update itself, in place, from its latest release, once the copy
	// has FeatureSelfUpdate.
	UpdateArgs []string

	// MinVersion is the oldest version the app drives.
	MinVersion string

	// Features maps each upstream change to the first release that has it.
	Features map[Feature]string
}

// Has reports whether a copy at version v has feature f: the feature is released and v is
// that release or newer. A development build has none, since its version can't say.
func (d Definition) Has(f Feature, v Version) bool {
	first := d.Features[f]

	return first != "" && v.AtLeast(first)
}

// FeaturesOf lists, sorted, every feature a copy at version v has (Has); the list is never
// nil.
func (d Definition) FeaturesOf(v Version) []Feature {
	out := []Feature{}

	for _, f := range slices.Sorted(maps.Keys(d.Features)) {
		if d.Has(f, v) {
			out = append(out, f)
		}
	}

	return out
}

// RunEnv is the environment a run of the tool gets (§5.5). In the app it is base, the login
// shell's, with the tool's quiet settings and git's terminal prompts off, since no one can
// answer one there, each in place of base's own. In the terminal it is only the quiet
// settings the script exports, colors kept: the terminal's own shell sets the rest up.
func (d Definition) RunEnv(base []string, runsIn integrationprefs.RunsIn) []string {
	if runsIn == integrationprefs.RunsInTerminal {
		out := []string{}

		for _, k := range slices.Sorted(maps.Keys(d.QuietEnv)) {
			if k != colorVar {
				out = append(out, k+"="+d.QuietEnv[k])
			}
		}

		return out
	}

	set := map[string]string{"GIT_TERMINAL_PROMPT": "0"}
	maps.Copy(set, d.QuietEnv)

	keys := slices.Sorted(maps.Keys(set))
	out := shellenv.Without(base, keys...)

	for _, k := range keys {
		out = append(out, k+"="+set[k])
	}

	return out
}

// colorVar is the variable that turns a tool's colors off: a run in the terminal keeps
// them, since it has a terminal to show them in.
const colorVar = "NO_COLOR"

// Definitions returns every tool the app knows, in the order the settings list them.
func Definitions() []Definition {
	return []Definition{MageX(), GoPreCommit()}
}

// quietEnv is the environment every tool runs with, plus the tool's own variables.
func quietEnv(own map[string]string) map[string]string {
	env := map[string]string{colorVar: "1"}

	maps.Copy(env, own)

	return env
}

// releaseFiles returns a GoReleaser project's archive and checksums names.
func releaseFiles(project string) (func(version, goos, goarch string) string, func(version string) string) {
	asset := func(version, goos, goarch string) string {
		return project + "_" + version + "_" + goos + "_" + goarch + ".tar.gz"
	}

	checksums := func(version string) string {
		return project + "_" + version + "_checksums.txt"
	}

	return asset, checksums
}
