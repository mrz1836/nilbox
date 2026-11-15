package repoguard_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/update"
)

const (
	// projectEnv holds this repository's own CI settings, which a sync leaves alone.
	projectEnv = ".github/env/90-project.env"

	// fortressWorkflow and fortressSetup are GoFortress's main workflow and its setup,
	// both synced from go-broadcast.
	fortressWorkflow = ".github/workflows/fortress.yml"
	fortressSetup    = ".github/workflows/fortress-setup-config.yml"
)

// releaseGate matches GoFortress's release job gating on the setup's release-enabled
// output, as go-broadcast's ENABLE_RELEASE switch writes it.
var releaseGate = regexp.MustCompile(`needs\.setup\.outputs\.release-enabled\s*(!=\s*'false'|==\s*'true')`)

// releaseOutput matches the setup declaring its release-enabled output.
var releaseOutput = regexp.MustCompile(`(?m)^\s+release-enabled:`)

// envSetting returns the value an env file's last assignment gives key, without quotes,
// and whether it assigns one; comments are skipped.
func envSetting(text, key string) (string, bool) {
	var (
		value string
		found bool
	)

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, key+"="); ok {
			value, found = strings.Trim(strings.TrimSpace(after), `"'`), true
		}
	}

	return value, found
}

// codeOnly returns text without its comment lines, so a comment can't pass for code.
func codeOnly(text string) string {
	var lines []string

	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n")
}

// jobBlock returns the lines of a workflow's job named job: from the line after its key,
// two spaces in under jobs:, up to the next key at that depth.
func jobBlock(text, job string) string {
	var (
		block        []string
		inJobs, inIt bool
	)

	for _, line := range strings.Split(text, "\n") {
		switch {
		case line == "jobs:":
			inJobs = true

			continue
		case !inJobs:
			continue
		case strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") &&
			strings.HasSuffix(strings.TrimSpace(line), ":") && !strings.HasPrefix(strings.TrimSpace(line), "#"):
			inIt = strings.TrimSpace(line) == job+":"

			continue
		}

		if inIt {
			block = append(block, line)
		}
	}

	return strings.Join(block, "\n")
}

// gofortressHonorsEnableRelease returns what GoFortress's synced files lack to read
// ENABLE_RELEASE: the release job's gate on the setup's release-enabled output, and the
// setup reading ENABLE_RELEASE and declaring that output.
func gofortressHonorsEnableRelease(fortress, setup string) []string {
	var missing []string

	if !releaseGate.MatchString(codeOnly(jobBlock(fortress, "release"))) {
		missing = append(missing, fortressWorkflow+": the release job doesn't gate on needs.setup.outputs.release-enabled")
	}

	if !strings.Contains(codeOnly(setup), "ENABLE_RELEASE") {
		missing = append(missing, fortressSetup+": nothing reads ENABLE_RELEASE")
	}

	if !releaseOutput.MatchString(codeOnly(setup)) {
		missing = append(missing, fortressSetup+": no release-enabled output")
	}

	return missing
}

// TestGoFortressReadsEnableRelease keeps this repository from turning GoFortress's
// publishing off before GoFortress can read the switch. With ENABLE_RELEASE=false in
// 90-project.env but go-broadcast's switch not yet synced, GoFortress would still publish
// each v* tag's release, before desktop-release.yml attaches its files; the releases are
// immutable, so that version could never get them. It fails until the sync lands, which is
// what holds the pull request that turns publishing off.
func TestGoFortressReadsEnableRelease(t *testing.T) {
	t.Parallel()

	env, ok := readText(t, projectEnv)
	require.True(t, ok, "%s is missing", projectEnv)

	if value, set := envSetting(env, "ENABLE_RELEASE"); !set || value != "false" {
		return // GoFortress publishes, as it always has
	}

	fortress, ok := readText(t, fortressWorkflow)
	require.True(t, ok, "%s is missing", fortressWorkflow)

	setup, ok := readText(t, fortressSetup)
	require.True(t, ok, "%s is missing", fortressSetup)

	assert.Empty(t, gofortressHonorsEnableRelease(fortress, setup),
		"%s sets ENABLE_RELEASE=false, but GoFortress doesn't read it yet: sync go-broadcast's ENABLE_RELEASE switch "+
			"into fortress.yml and fortress-setup-config.yml first. Until then GoFortress publishes each v* tag's "+
			"release itself, before desktop-release.yml attaches its files, and an immutable release can't get them later.",
		projectEnv)
}

func TestGoFortressHonorsEnableRelease(t *testing.T) {
	t.Parallel()

	// The shapes before go-broadcast's switch and after it (its pull request #265), cut
	// down to the lines that matter.
	fortressBefore := strings.Join([]string{
		"jobs:",
		"  setup:",
		"    uses: ./.github/workflows/fortress-setup-config.yml",
		"  release:",
		"    needs: [setup, test-suite]",
		"    # Skip only on ENABLE_RELEASE=false: needs.setup.outputs.release-enabled != 'false'",
		"    if: |",
		"      always() &&",
		"      startsWith(github.ref, 'refs/tags/v') &&",
		"      needs.setup.outputs.is-fork-pr != 'true'",
		"    uses: ./.github/workflows/fortress-release.yml",
		"  completion-report:",
		"    if: needs.setup.outputs.release-enabled != 'false'",
	}, "\n")
	fortressAfter := strings.Replace(fortressBefore, "      startsWith(github.ref, 'refs/tags/v') &&",
		"      startsWith(github.ref, 'refs/tags/v') &&\n      needs.setup.outputs.release-enabled != 'false' &&", 1)

	setupBefore := strings.Join([]string{
		"on:",
		"  workflow_call:",
		"    outputs:",
		"      release-runner:",
		"        value: ${{ jobs.setup-config.outputs.release-runner }}",
		"      # release-enabled: one day",
	}, "\n")
	setupAfter := setupBefore + "\n" + strings.Join([]string{
		"      release-enabled:",
		"        value: ${{ jobs.setup-config.outputs.release-enabled }}",
		"jobs:",
		"  setup-config:",
		"    steps:",
		"      - run: |",
		`          RELEASE_ENABLED="${ENABLE_RELEASE:-true}"`,
		`          echo "release-enabled=$RELEASE_ENABLED" >> $GITHUB_OUTPUT`,
	}, "\n")

	assert.Len(t, gofortressHonorsEnableRelease(fortressBefore, setupBefore), 3,
		"a comment, or another job's gate, is no gate")
	assert.Empty(t, gofortressHonorsEnableRelease(fortressAfter, setupAfter), "the synced switch passes")
	assert.Len(t, gofortressHonorsEnableRelease(fortressAfter, setupBefore), 2)
	assert.Len(t, gofortressHonorsEnableRelease(fortressBefore, setupAfter), 1)
}

func TestEnvSetting(t *testing.T) {
	t.Parallel()

	env := "# ENABLE_RELEASE=true\nOTHER=1\nENABLE_RELEASE=true\n  ENABLE_RELEASE=\"false\"\n"

	value, ok := envSetting(env, "ENABLE_RELEASE")
	assert.True(t, ok)
	assert.Equal(t, "false", value, "the last assignment wins, without its quotes")

	_, ok = envSetting("# ENABLE_RELEASE=false\n", "ENABLE_RELEASE")
	assert.False(t, ok, "a comment assigns nothing")
}

// tagPattern matches a release workflow line that sets the pattern a tag is checked
// against.
var tagPattern = regexp.MustCompile(`(?m)^\s*tag_pattern='([^']+)'\s*$`)

// TestReleaseTagPatternIsTheAppsVersionRule holds the pattern the release workflow checks
// a tag with, before anything is built or any secret is read, to the app's own rule for a
// release version (update.ValidVersion), with the tag's v. Bash reads the pattern as a
// POSIX extended regular expression and Go as RE2; it uses only anchors, groups,
// alternation, bracket expressions, *, and +, which both read alike for whether a whole
// string matches.
func TestReleaseTagPatternIsTheAppsVersionRule(t *testing.T) {
	t.Parallel()

	text, ok := readText(t, releaseWorkflow)
	require.True(t, ok, "%s is missing", releaseWorkflow)

	matches := tagPattern.FindAllStringSubmatch(text, -1)
	patterns := make([]string, 0, len(matches))

	for _, m := range matches {
		patterns = append(patterns, m[1])
	}

	require.Len(t, patterns, 3, "the check, build, and publish jobs each check the tag")

	for _, p := range patterns[1:] {
		assert.Equal(t, patterns[0], p, "every job checks the tag alike")
	}

	pattern := regexp.MustCompile(patterns[0])

	for _, version := range []string{
		"0.4.0", "0.4.0-rc.1", "1.10.3-beta.2", "10.20.30", "1.2.3-0", "1.2.3-0a", "1.2.3-alpha-1",
		"1.2.3-x.7.z.92", "1.2.3--", "01.2.3", "1.02.3", "1.2.03", "1.2.3-01", "1.2.3-rc.01", "1.2.3-..",
		"1.2.3-", "1.2.3-rc..1", "1.2.3-rc_1", "1.2.3+build", "1.2.3-rc.1+build", "1.2", "1.2.3.4",
		"x.y.z", "", "1.2.3 ", " 1.2.3", "1.2.3\n", "1.2.3-rc.1\nv9.9.9",
	} {
		assert.Equal(t, update.ValidVersion(version), pattern.MatchString("v"+version), "v%q", version)
		assert.False(t, pattern.MatchString(version), "%q has no v", version)
	}
}
