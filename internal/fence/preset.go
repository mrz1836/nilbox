package fence

// Preset is a tools preset: what a background agent may do, whatever its profile allows.
// The values are the routines' own words (the Autopilot catalog's presets), which core
// converts one for one.
type Preset string

// The tools presets, from the narrowest.
const (
	// ReadOnly reads and plans, and changes nothing. Every untrusted item runs under it.
	ReadOnly Preset = "readOnly"

	// GitHubTasks runs only the gh commands for small GitHub changes on the item: re-run
	// failed jobs, a comment, a label.
	GitHubTasks Preset = "githubTasks"

	// EditTest edits the worktree and runs the repository's checks, and never pushes.
	EditTest Preset = "editTest"

	// EditTestPush is EditTest plus exactly one push, to the pull request's own branch.
	EditTestPush Preset = "editTestPush"

	// Profile is the profile's own permissions, exactly what a click passes, within the
	// fence's hard lines.
	Profile Preset = "profile"
)

// Presets returns every preset, from the narrowest.
func Presets() []Preset {
	return []Preset{ReadOnly, GitHubTasks, EditTest, EditTestPush, Profile}
}

// Valid reports a preset this build knows.
func (p Preset) Valid() bool {
	switch p {
	case ReadOnly, GitHubTasks, EditTest, EditTestPush, Profile:
		return true
	default:
		return false
	}
}

// Label is the preset's name as the builder shows it.
func (p Preset) Label() string {
	switch p {
	case ReadOnly:
		return "Read only"
	case GitHubTasks:
		return "GitHub tasks only"
	case EditTest:
		return "Edit and test, no push"
	case EditTestPush:
		return "Edit, test, and push to the PR's branch"
	case Profile:
		return "The profile's own permissions"
	default:
		return string(p)
	}
}

// RunsCommands reports a preset whose agent runs shell commands.
func (p Preset) RunsCommands() bool {
	return p != ReadOnly && p.Valid()
}

// Edits reports a preset whose agent edits files in its worktree.
func (p Preset) Edits() bool {
	return p == EditTest || p == EditTestPush || p == Profile
}

// MayPush reports a preset under which the agent may push, exactly one way.
func (p Preset) MayPush() bool {
	return p == EditTestPush || p == Profile
}

// Sandboxed reports a preset whose commands run in the agent's own sandbox: the edit
// presets, which run the repository's own code. GitHub tasks runs only gh, which the
// sandbox would cut off from its sign-in, and the profile's own permissions are the
// profile's.
func (p Preset) Sandboxed() bool {
	return p == EditTest || p == EditTestPush
}

// Fixed reports a preset that brings its own permission mode and rules, whatever the
// profile says: every one but the profile's own.
func (p Preset) Fixed() bool {
	return p.Valid() && p != Profile
}
