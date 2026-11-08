package launcher

import (
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/trust"
)

// The flags and summaries the tests expect, spelled out apart from the code that writes
// them.
const (
	flagModel                = "--model"
	flagEffort               = "--effort"
	flagSkipPermissions      = "--dangerously-skip-permissions"
	flagAllowSkipPermissions = "--allow-dangerously-skip-permissions"
	flagPermissionMode       = "--permission-mode"
	flagAddDirPrefix         = "--add-dir="
	flagSandbox              = "--sandbox"
	flagApprovals            = "--ask-for-approval"
	flagBypass               = "--dangerously-bypass-approvals-and-sandbox"
	modePlan                 = "plan"
	claudeConfigEnv          = "CLAUDE_CONFIG_DIR"
	codexConfigEnv           = "CODEX_HOME"

	wantOutsidePlan = "plan mode (outside contributor)"
	wantPlan        = "plan mode"
	wantPlanFull    = "plan mode, full access available after the plan"
)

// codexReadOnly is the access Codex is launched with to plan only.
func codexReadOnly() []string {
	return []string{flagSandbox, "read-only", flagApprovals, "on-request"}
}

// absPath returns an absolute path under a fixed fake root.
func absPath(parts ...string) string {
	return filepath.Join(append([]string{"/", "tmp", "launcher-test"}, parts...)...)
}

// fullProfile returns a discovered-style profile: full access, nothing else set.
func fullProfile() profiles.Profile {
	return profiles.Profile{
		Agent:          agent.Claude,
		Name:           profiles.DefaultName,
		PermissionMode: profiles.PermissionFull,
		AddDirs:        []string{},
		ExtraArgs:      []string{},
		Env:            map[string]string{},
	}
}

// baseRequest returns a trusted pull request launch of Claude with a full-access profile.
func baseRequest() Request {
	return Request{
		Item: Item{
			Kind:    item.KindPR,
			Number:  7,
			URL:     "https://github.com/acme-org/example-repo/pull/7",
			Title:   "Fix the widget",
			Repo:    "acme-org/example-repo",
			Branch:  "fix-widget",
			Trusted: true,
		},
		Agent:      agent.Claude,
		Profile:    fullProfile(),
		Dir:        absPath("projects", "."+brand.Slug, "worktrees", "example-repo", "pr-7"),
		BinaryPath: absPath("bin", "claude"),
	}
}

// codexRequest returns baseRequest for Codex.
func codexRequest() Request {
	req := baseRequest()
	req.Agent, req.Profile.Agent, req.BinaryPath = agent.Codex, agent.Codex, absPath("bin", "codex")

	return req
}

// buildArgs builds a plan for req and returns its arguments.
func buildArgs(t *testing.T, req Request) []string {
	t.Helper()

	plan, err := BuildPlan(req)
	require.NoError(t, err)

	return plan.Args
}

// flagValue returns the value after flag in args, and whether flag is present.
func flagValue(args []string, flag string) (string, bool) {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return "", i >= 0
	}

	return args[i+1], true
}

// lookupAgent returns the agent with id.
func lookupAgent(t *testing.T, id agent.ID) agent.Agent {
	t.Helper()

	a, err := agent.Lookup(id)
	require.NoError(t, err)

	return a
}

func TestPermissionPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		trusted  bool
		override bool
		mode     string
		plan     bool
		want     agent.Access
		summary  string
	}{
		{
			name: "trusted full access", trusted: true, mode: profiles.PermissionFull,
			want: agent.Access{Kind: agent.AccessFull}, summary: summaryFull,
		},
		{
			name: "trusted plan mode with full access keeps bypass reachable", trusted: true,
			mode: profiles.PermissionFull, plan: true,
			want: agent.Access{Kind: agent.AccessPlanAllowFull}, summary: wantPlanFull,
		},
		{
			name: "trusted plan mode with a restricted mode", trusted: true, mode: "acceptEdits", plan: true,
			want: agent.Access{Kind: agent.AccessPlan}, summary: wantPlan,
		},
		{
			name: "trusted plan mode with no permission setting", trusted: true, plan: true,
			want: agent.Access{Kind: agent.AccessPlan}, summary: wantPlan,
		},
		{
			name: "trusted explicit mode", trusted: true, mode: "acceptEdits",
			want: agent.Access{Kind: agent.AccessMode, Mode: "acceptEdits"}, summary: summaryModePrefix + "acceptEdits",
		},
		{
			name: "trusted plan as the permission mode", trusted: true, mode: modePlan,
			want: agent.Access{Kind: agent.AccessMode, Mode: modePlan}, summary: summaryModePrefix + modePlan,
		},
		{
			name: "trusted no setting uses the agent defaults", trusted: true,
			want: agent.Access{}, summary: summaryDefault,
		},
		{
			name: "untrusted full access is downgraded to plan", mode: profiles.PermissionFull,
			want: agent.Access{Kind: agent.AccessPlan}, summary: wantOutsidePlan,
		},
		{
			name: "untrusted plan mode with full access gets no bypass", mode: profiles.PermissionFull, plan: true,
			want: agent.Access{Kind: agent.AccessPlan}, summary: wantOutsidePlan,
		},
		{
			name: "untrusted restricted mode is plan", mode: "dontAsk",
			want: agent.Access{Kind: agent.AccessPlan}, summary: wantOutsidePlan,
		},
		{
			name: "override restores full access", override: true, mode: profiles.PermissionFull,
			want: agent.Access{Kind: agent.AccessFull}, summary: summaryFull,
		},
		{
			name: "override keeps the profile's plan mode", override: true, mode: profiles.PermissionFull, plan: true,
			want: agent.Access{Kind: agent.AccessPlanAllowFull}, summary: wantPlanFull,
		},
		{
			name: "override keeps a restricted mode", override: true, mode: "acceptEdits",
			want: agent.Access{Kind: agent.AccessMode, Mode: "acceptEdits"}, summary: summaryModePrefix + "acceptEdits",
		},
		{
			name: "override keeps the agent defaults", override: true,
			want: agent.Access{}, summary: summaryDefault,
		},
		{
			name: "override on a trusted item changes nothing", trusted: true, override: true, mode: "auto",
			want: agent.Access{Kind: agent.AccessMode, Mode: "auto"}, summary: summaryModePrefix + "auto",
		},
	}

	claude := lookupAgent(t, agent.Claude)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := fullProfile()
			p.PermissionMode = tt.mode
			p.PlanMode = tt.plan

			got, err := resolvePermission(claude, tt.trusted, tt.override, p)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.access)
			assert.Equal(t, tt.summary, got.summary)
		})
	}
}

func TestCodexPermissions(t *testing.T) {
	t.Parallel()

	codex := lookupAgent(t, agent.Codex)

	tests := []struct {
		name    string
		trusted bool
		mode    string
		plan    bool
		want    agent.Access
		summary string
	}{
		{name: "outside contributor", mode: profiles.PermissionFull, want: agent.Access{Kind: agent.AccessPlan}, summary: "read-only (outside contributor)"},
		{name: "plan with full access cannot switch to it", trusted: true, mode: profiles.PermissionFull, plan: true, want: agent.Access{Kind: agent.AccessPlan}, summary: "read-only"},
		{name: "full access", trusted: true, mode: profiles.PermissionFull, want: agent.Access{Kind: agent.AccessFull}, summary: summaryFull},
		{name: "a sandbox", trusted: true, mode: "workspace-write", want: agent.Access{Kind: agent.AccessMode, Mode: "workspace-write"}, summary: summaryModePrefix + "workspace-write"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := fullProfile()
			p.Agent, p.PermissionMode, p.PlanMode = agent.Codex, tt.mode, tt.plan

			got, err := resolvePermission(codex, tt.trusted, false, p)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.access)
			assert.Equal(t, tt.summary, got.summary)
		})
	}

	p := fullProfile()
	p.PermissionMode = "acceptEdits"

	_, err := resolvePermission(codex, true, false, p)
	require.ErrorIs(t, err, ErrInvalidPermissionMode, "Claude's modes are not Codex's")
	require.ErrorContains(t, err, "for Codex")
}

func TestPermissionModesAccepted(t *testing.T) {
	t.Parallel()

	for _, a := range agent.All() {
		modes := make([]string, 0, 2+len(a.PermissionModes))
		modes = append(modes, "", profiles.PermissionFull)

		for _, m := range a.PermissionModes {
			modes = append(modes, m.Value)
		}

		for _, mode := range modes {
			p := fullProfile()
			p.PermissionMode = mode

			_, err := resolvePermission(a, true, false, p)
			require.NoError(t, err, "%s %s", a.ID, mode)
		}

		for _, trusted := range []bool{true, false} {
			p := fullProfile()
			p.PermissionMode = "yolo"

			_, err := resolvePermission(a, trusted, false, p)
			require.ErrorIs(t, err, ErrInvalidPermissionMode)
		}
	}
}

// trustCase is one combination in the trust × override matrix.
type trustCase struct {
	agent    agent.ID
	assoc    string
	cross    bool
	mode     string
	plan     bool
	override bool
}

// name labels the case for t.Run.
func (c trustCase) name() string {
	return string(c.agent) + "/" + c.assoc + "/cross=" + strconv.FormatBool(c.cross) + "/perm=" + c.mode +
		"/plan=" + strconv.FormatBool(c.plan) + "/override=" + strconv.FormatBool(c.override)
}

// expand returns every case in cases combined with every value, applied by set.
func expand[T any](cases []trustCase, values []T, set func(c *trustCase, v T)) []trustCase {
	out := make([]trustCase, 0, len(cases)*len(values))

	for _, c := range cases {
		for _, v := range values {
			next := c
			set(&next, v)
			out = append(out, next)
		}
	}

	return out
}

// expectedAccess states the policy independently of resolvePermission: the access flags
// the agent's arguments should start with.
func expectedAccess(c trustCase) []string {
	trustedAssoc := slices.Contains(
		[]string{trust.AssociationOwner, trust.AssociationMember, trust.AssociationCollaborator}, c.assoc,
	)
	effective := trustedAssoc && !c.cross || c.override
	full := c.mode == profiles.PermissionFull

	if c.agent == agent.Codex {
		return expectedCodexAccess(effective, full, c)
	}

	switch {
	case !effective:
		return []string{flagPermissionMode, modePlan}
	case c.plan && full:
		return []string{flagPermissionMode, modePlan, flagAllowSkipPermissions}
	case c.plan:
		return []string{flagPermissionMode, modePlan}
	case full:
		return []string{flagSkipPermissions}
	case c.mode != "":
		return []string{flagPermissionMode, c.mode}
	default:
		return nil
	}
}

// expectedCodexAccess is expectedAccess for Codex, followed by the "--" that ends its
// flags.
func expectedCodexAccess(effective, full bool, c trustCase) []string {
	switch {
	case !effective, c.plan:
		return append(codexReadOnly(), "--")
	case full:
		return []string{flagBypass, "--"}
	case c.mode != "":
		return []string{flagSandbox, c.mode, "--"}
	default:
		return []string{"--"}
	}
}

func TestTrustOverrideMatrix(t *testing.T) {
	t.Parallel()

	bools := []bool{false, true}

	cases := expand([]trustCase{{}}, []agent.ID{agent.Claude, agent.Codex}, func(c *trustCase, v agent.ID) { c.agent = v })
	cases = expand(cases, []string{
		trust.AssociationOwner, trust.AssociationMember, trust.AssociationCollaborator,
		"CONTRIBUTOR", "FIRST_TIME_CONTRIBUTOR", "NONE",
	}, func(c *trustCase, v string) { c.assoc = v })
	cases = expand(cases, bools, func(c *trustCase, v bool) { c.cross = v })
	cases = expand(cases, []string{profiles.PermissionFull, "", "<mode>"}, func(c *trustCase, v string) { c.mode = v })
	cases = expand(cases, bools, func(c *trustCase, v bool) { c.plan = v })
	cases = expand(cases, bools, func(c *trustCase, v bool) { c.override = v })
	require.Len(t, cases, 2*6*2*3*2*2)

	for _, c := range cases {
		if c.mode == "<mode>" {
			c.mode = map[agent.ID]string{agent.Claude: "acceptEdits", agent.Codex: "workspace-write"}[c.agent]
		}

		t.Run(c.name(), func(t *testing.T) {
			t.Parallel()

			checkTrustCase(t, c)
		})
	}
}

// checkTrustCase builds one pull request launch and checks its access flags against the
// policy.
func checkTrustCase(t *testing.T, c trustCase) {
	t.Helper()

	req := baseRequest()
	if c.agent == agent.Codex {
		req = codexRequest()
	}

	req.Item.Trusted = trust.NewPolicy(nil).Trusted(trust.Author{Association: c.assoc}, true, c.cross)
	req.Profile.PermissionMode = c.mode
	req.Profile.PlanMode = c.plan
	req.FullAccessOverride = c.override

	args := buildArgs(t, req)
	assert.Equal(t, expectedAccess(c), args, "an access setting is the only flag here")
	assert.False(t, slices.Contains(args, flagPermissionMode) && slices.Contains(args, flagSkipPermissions),
		"full access is never combined with a mode")
	assert.False(t, slices.Contains(args, flagSandbox) && slices.Contains(args, flagBypass),
		"full access is never combined with a sandbox")
}

func TestUntrustedLaunchOnlyPlans(t *testing.T) {
	t.Parallel()

	for _, plan := range []bool{false, true} {
		t.Run("profile plan="+strconv.FormatBool(plan), func(t *testing.T) {
			t.Parallel()

			req := baseRequest()
			req.Item.Trusted = trust.NewPolicy(nil).Trusted(trust.Author{Association: "CONTRIBUTOR"}, true, true)
			req.Profile.PlanMode = plan

			got, err := BuildPlan(req)
			require.NoError(t, err)
			assert.Equal(t, []string{flagPermissionMode, modePlan}, got.Args)
			assert.Equal(t, wantOutsidePlan, got.Permission)

			codex := codexRequest()
			codex.Item.Trusted, codex.Profile.PlanMode = req.Item.Trusted, plan

			got, err = BuildPlan(codex)
			require.NoError(t, err)
			assert.Equal(t, append(codexReadOnly(), "--"), got.Args)
			assert.Equal(t, "read-only (outside contributor)", got.Permission)
		})
	}

	// A bot's issue is untrusted too.
	req := baseRequest()
	req.Item = Item{Kind: item.KindIssue, Number: 3, Trusted: trust.NewPolicy(nil).Trusted(trust.Author{Association: "NONE"}, false, false)}

	assert.Equal(t, []string{flagPermissionMode, modePlan}, buildArgs(t, req))
}

func TestBuildPlanFlagMatrix(t *testing.T) {
	t.Parallel()

	dirA, dirB := absPath("shared"), absPath("with space", "docs")

	tests := []struct {
		name string
		edit func(p *profiles.Profile)
		want []string
	}{
		{name: "discovered profile", edit: func(*profiles.Profile) {}, want: []string{flagSkipPermissions}},
		{
			name: "model only", edit: func(p *profiles.Profile) { p.Model = "opus" },
			want: []string{flagModel, "opus", flagSkipPermissions},
		},
		{
			name: "effort only", edit: func(p *profiles.Profile) { p.Effort = "xhigh" },
			want: []string{flagEffort, "xhigh", flagSkipPermissions},
		},
		{
			name: "model and effort", edit: func(p *profiles.Profile) { p.Model, p.Effort = "claude-sonnet-5-5", "low" },
			want: []string{flagModel, "claude-sonnet-5-5", flagEffort, "low", flagSkipPermissions},
		},
		{name: "no permission setting", edit: func(p *profiles.Profile) { p.PermissionMode = "" }, want: nil},
		{
			name: "explicit permission mode", edit: func(p *profiles.Profile) { p.PermissionMode = "bypassPermissions" },
			want: []string{flagPermissionMode, "bypassPermissions"},
		},
		{
			name: "plan mode", edit: func(p *profiles.Profile) { p.PlanMode = true },
			want: []string{flagPermissionMode, modePlan, flagAllowSkipPermissions},
		},
		{
			name: "add dirs use the equals form", edit: func(p *profiles.Profile) { p.AddDirs = []string{dirA, dirB} },
			want: []string{flagSkipPermissions, flagAddDirPrefix + dirA, flagAddDirPrefix + dirB},
		},
		{
			name: "extra args stay separate elements",
			edit: func(p *profiles.Profile) { p.ExtraArgs = []string{"--verbose", "--append-system-prompt", "be brief"} },
			want: []string{flagSkipPermissions, "--verbose", "--append-system-prompt", "be brief"},
		},
		{name: "everything, in order", edit: func(p *profiles.Profile) {
			p.Model, p.Effort, p.PlanMode = "opus", "max", true
			p.AddDirs, p.ExtraArgs = []string{dirA}, []string{"--verbose"}
		}, want: []string{
			flagModel, "opus", flagEffort, "max",
			flagPermissionMode, modePlan, flagAllowSkipPermissions,
			flagAddDirPrefix + dirA, "--verbose",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := baseRequest()
			tt.edit(&req.Profile)

			assert.Equal(t, tt.want, buildArgs(t, req))
			assert.NotContains(t, buildArgs(t, req), "--worktree", "the app makes the worktree; no agent's flag is used")
		})
	}

	for _, effort := range agent.EffortLevels(agent.Claude) {
		t.Run("effort "+effort, func(t *testing.T) {
			t.Parallel()

			req := baseRequest()
			req.Profile.Effort = effort

			got, ok := flagValue(buildArgs(t, req), flagEffort)
			assert.True(t, ok)
			assert.Equal(t, effort, got)
		})
	}
}

func TestBuildPlanForCodex(t *testing.T) {
	t.Parallel()

	req := codexRequest()
	req.Profile.Model, req.Profile.Effort = "gpt-6-sol", "ultra"
	req.Profile.ConfigDir = absPath("home", ".codex_work")
	req.Profile.AddDirs = []string{absPath("shared")}
	req.Profile.ExtraArgs = []string{"--search"}
	req.SessionName = "Review · example-repo#7"
	req.PromptFile = absPath("launch", "pr-7.prompt.md")

	got, err := BuildPlan(req)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"--model", "gpt-6-sol", "--config", `model_reasoning_effort="ultra"`, flagBypass,
		flagAddDirPrefix + absPath("shared"), "--search", "--",
	}, got.Args, "Codex names no session, and ends its flags with --")
	assert.True(t, got.PromptLast)
	assert.Equal(t, agent.Codex, got.Agent)
	assert.Equal(t, map[string]string{codexConfigEnv: absPath("home", ".codex_work")}, got.Env)
	assert.Empty(t, got.Unset)

	argv := got.Argv("Review pull request #7")
	assert.Equal(t, req.BinaryPath, argv[0])
	assert.Equal(t, "Review pull request #7", argv[len(argv)-1], "the prompt comes last, after --")
	assert.Equal(t, "--", argv[len(argv)-2])

	req.Profile.ConfigDir = ""

	got, err = BuildPlan(req)
	require.NoError(t, err)
	assert.Equal(t, []string{codexConfigEnv}, got.Unset, "the default profile runs without CODEX_HOME")

	req.Profile.ExtraArgs = []string{"-c", "sandbox_mode=danger-full-access"}
	_, err = BuildPlan(req)
	require.ErrorIs(t, err, ErrConflictingExtraArg, "a config override cannot widen access either")
}

func TestBuildPlanFields(t *testing.T) {
	t.Parallel()

	req := baseRequest()
	req.PromptFile = absPath("launch", "pr-7.prompt.md")

	got, err := BuildPlan(req)
	require.NoError(t, err)
	assert.Equal(t, item.KindPR, got.Kind)
	assert.Equal(t, 7, got.Number)
	assert.Equal(t, agent.Claude, got.Agent)
	assert.Equal(t, req.Dir, got.Dir)
	assert.Equal(t, req.BinaryPath, got.Binary)
	assert.Equal(t, req.PromptFile, got.PromptFile)
	assert.False(t, got.PromptLast)
	assert.Equal(t, summaryFull, got.Permission)
}

func TestBuildPlanRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		edit func(r *Request)
		want error
	}{
		{name: "unknown kind", edit: func(r *Request) { r.Item.Kind = "discussion" }, want: item.ErrInvalidKind},
		{name: "zero number", edit: func(r *Request) { r.Item.Number = 0 }, want: ErrInvalidNumber},
		{name: "negative number", edit: func(r *Request) { r.Item.Number = -4 }, want: ErrInvalidNumber},
		{name: "unknown agent", edit: func(r *Request) { r.Agent = "example-agent" }, want: agent.ErrUnknownAgent},
		{name: "relative dir", edit: func(r *Request) { r.Dir = "projects/example-repo" }, want: ErrRelativePath},
		{name: "empty dir", edit: func(r *Request) { r.Dir = "" }, want: ErrRelativePath},
		{name: "relative agent path", edit: func(r *Request) { r.BinaryPath = "claude" }, want: ErrRelativePath},
		{name: "relative prompt file", edit: func(r *Request) { r.PromptFile = "pr-7.prompt.md" }, want: ErrRelativePath},
		{name: "unknown effort", edit: func(r *Request) { r.Profile.Effort = "extreme" }, want: agent.ErrInvalidEffort},
		{name: "another agent's effort", edit: func(r *Request) { r.Profile.Effort = "ultra" }, want: agent.ErrInvalidEffort},
		{name: "model that looks like a flag", edit: func(r *Request) { r.Profile.Model = "--help" }, want: agent.ErrInvalidModel},
		{
			name: "unknown permission mode", edit: func(r *Request) { r.Profile.PermissionMode = "yolo" },
			want: ErrInvalidPermissionMode,
		},
		{name: "empty add-dir", edit: func(r *Request) { r.Profile.AddDirs = []string{""} }, want: ErrEmptyArg},
		{
			name: "relative add-dir", edit: func(r *Request) { r.Profile.AddDirs = []string{"../shared"} },
			want: ErrRelativePath,
		},
		{
			name: "relative config dir", edit: func(r *Request) { r.Profile.ConfigDir = ".claude_work" },
			want: ErrRelativePath,
		},
		{
			name: "NUL in an extra arg", edit: func(r *Request) { r.Profile.ExtraArgs = []string{"a\x00b"} },
			want: ErrNULByte,
		},
		{
			name: "NUL in an env value", edit: func(r *Request) { r.Profile.Env = map[string]string{"A": "x\x00"} },
			want: ErrNULByte,
		},
		{name: "NUL in the dir", edit: func(r *Request) { r.Dir = absPath("a\x00b") }, want: ErrNULByte},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := baseRequest()
			tt.edit(&req)

			got, err := BuildPlan(req)
			require.ErrorIs(t, err, tt.want)
			assert.Equal(t, Plan{}, got)
		})
	}
}

func TestEnvHandling(t *testing.T) {
	t.Parallel()

	workDir := absPath("home", ".claude_work")

	tests := []struct {
		name      string
		configDir string
		env       map[string]string
		wantEnv   map[string]string
		wantUnset []string
	}{
		{
			name: "default profile unsets the config dir", env: map[string]string{},
			wantEnv: map[string]string{}, wantUnset: []string{claudeConfigEnv},
		},
		{
			name:    "default profile drops a config dir from its env",
			env:     map[string]string{claudeConfigEnv: absPath("elsewhere"), "EDITOR": "vi"},
			wantEnv: map[string]string{"EDITOR": "vi"}, wantUnset: []string{claudeConfigEnv},
		},
		{
			name: "named profile sets the config dir", configDir: workDir, env: nil,
			wantEnv: map[string]string{claudeConfigEnv: workDir},
		},
		{
			name: "the config dir wins over the profile env", configDir: workDir,
			env:     map[string]string{claudeConfigEnv: absPath("elsewhere"), "_PRIVATE_1": "a b 'c'"},
			wantEnv: map[string]string{claudeConfigEnv: workDir, "_PRIVATE_1": "a b 'c'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := baseRequest()
			req.Profile.ConfigDir = tt.configDir
			req.Profile.Env = tt.env

			got, err := BuildPlan(req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantEnv, got.Env)
			assert.Equal(t, tt.wantUnset, got.Unset)
		})
	}

	for _, key := range []string{"", "1BAD", "BAD-KEY", "A B", "A=B", "ÄRGER"} {
		t.Run("rejects key "+strconv.Quote(key), func(t *testing.T) {
			t.Parallel()

			req := baseRequest()
			req.Profile.Env = map[string]string{key: "value"}

			_, err := BuildPlan(req)
			require.ErrorIs(t, err, ErrInvalidEnvKey)
		})
	}
}

func TestPromptIsFirstArgumentForClaude(t *testing.T) {
	t.Parallel()

	req := baseRequest()
	req.Profile.AddDirs = []string{absPath("shared"), absPath("docs")}
	req.Profile.ExtraArgs = []string{"--verbose"}

	plan, err := BuildPlan(req)
	require.NoError(t, err)

	argv := plan.Argv("Review pull request #7")
	require.Len(t, argv, len(plan.Args)+2)
	assert.Equal(t, req.BinaryPath, argv[0])
	assert.Equal(t, "Review pull request #7", argv[1])
	assert.Equal(t, plan.Args, argv[2:])
	assert.NotContains(t, argv, "--add-dir", "add-dir always uses the equals form")
}

func TestExtraArgsCannotOverridePermissions(t *testing.T) {
	t.Parallel()

	forms := map[string][]string{}

	for _, flag := range []string{flagPermissionMode, flagSkipPermissions, flagAllowSkipPermissions} {
		forms[flag+" alone"] = []string{flag}
		forms[flag+" with a separate value"] = []string{flag, "bypassPermissions"}
		forms[flag+" with an equals value"] = []string{flag + "=bypassPermissions"}
		forms[flag+" after a harmless arg"] = []string{"--verbose", flag + "=true"}
	}

	for name, extra := range forms {
		for _, trusted := range []bool{true, false} {
			t.Run(name+"/trusted="+strconv.FormatBool(trusted), func(t *testing.T) {
				t.Parallel()

				req := baseRequest()
				req.Item.Trusted = trusted
				req.FullAccessOverride = !trusted
				req.Profile.ExtraArgs = extra

				got, err := BuildPlan(req)
				require.ErrorIs(t, err, ErrConflictingExtraArg)
				assert.Equal(t, Plan{}, got, "a rejected launch has no plan to run")
			})
		}
	}

	t.Run("harmless extra args are kept as separate elements", func(t *testing.T) {
		t.Parallel()

		req := baseRequest()
		req.Item.Trusted = false
		req.Profile.ExtraArgs = []string{
			"--verbose",
			"--append-system-prompt", "never use --dangerously-skip-permissions",
			"--permission-modes-note",
		}

		got := buildArgs(t, req)
		assert.Equal(t, append([]string{flagPermissionMode, modePlan}, req.Profile.ExtraArgs...), got)
	})
}

func TestSessionName(t *testing.T) {
	t.Parallel()

	req := baseRequest()
	req.SessionName = "Review · example-repo#7"

	args := buildArgs(t, req)
	assert.Equal(t, []string{"--dangerously-skip-permissions", "--name=Review · example-repo#7"}, args,
		"the session name comes last, as one argument")

	req.SessionName = ""
	assert.NotContains(t, buildArgs(t, req), "--name=")

	req.SessionName = "a\x00b"
	_, err := BuildPlan(req)
	require.ErrorIs(t, err, ErrNULByte)
}
