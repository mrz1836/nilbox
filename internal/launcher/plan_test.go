package launcher

import (
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/trust"
)

// absPath returns an absolute path under a fixed fake root.
func absPath(parts ...string) string {
	return filepath.Join(append([]string{"/", "tmp", "launcher-test"}, parts...)...)
}

// fullProfile returns a discovered-style profile: full access, nothing else set.
func fullProfile() profiles.Profile {
	return profiles.Profile{
		Name:           profiles.DefaultName,
		PermissionMode: profiles.PermissionFull,
		AddDirs:        []string{},
		ExtraArgs:      []string{},
		Env:            map[string]string{},
	}
}

// baseRequest returns a trusted pull request launch with a full-access profile.
func baseRequest() Request {
	return Request{
		Item: Item{
			Kind:    KindPR,
			Number:  7,
			URL:     "https://github.com/acme-org/example-repo/pull/7",
			Title:   "Fix the widget",
			Repo:    "acme-org/example-repo",
			Branch:  "fix-widget",
			Trusted: true,
		},
		Profile:    fullProfile(),
		Clone:      Clone{Dir: absPath("projects", "example-repo"), OriginIsItemRepo: true},
		ClaudePath: absPath("bin", "claude"),
	}
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

func TestPermissionPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		trusted  bool
		override bool
		mode     string
		plan     bool
		want     []string
		summary  string
	}{
		{
			name: "trusted full access", trusted: true, mode: profiles.PermissionFull,
			want: []string{flagSkipPermissions}, summary: summaryFull,
		},
		{
			name: "trusted plan mode with full access keeps bypass reachable", trusted: true,
			mode: profiles.PermissionFull, plan: true,
			want: []string{flagPermissionMode, modePlan, flagAllowSkipPermissions}, summary: summaryPlanFull,
		},
		{
			name: "trusted plan mode with a restricted mode", trusted: true, mode: "acceptEdits", plan: true,
			want: []string{flagPermissionMode, modePlan}, summary: summaryPlan,
		},
		{
			name: "trusted plan mode with no permission setting", trusted: true, plan: true,
			want: []string{flagPermissionMode, modePlan}, summary: summaryPlan,
		},
		{
			name: "trusted explicit mode", trusted: true, mode: "acceptEdits",
			want: []string{flagPermissionMode, "acceptEdits"}, summary: summaryModePrefix + "acceptEdits",
		},
		{
			name: "trusted plan as the permission mode", trusted: true, mode: modePlan,
			want: []string{flagPermissionMode, modePlan}, summary: summaryModePrefix + modePlan,
		},
		{
			name: "trusted no setting uses the agent defaults", trusted: true,
			want: nil, summary: summaryDefault,
		},
		{
			name: "untrusted full access is downgraded to plan", mode: profiles.PermissionFull,
			want: []string{flagPermissionMode, modePlan}, summary: summaryOutsidePlan,
		},
		{
			name: "untrusted plan mode with full access gets no bypass", mode: profiles.PermissionFull, plan: true,
			want: []string{flagPermissionMode, modePlan}, summary: summaryOutsidePlan,
		},
		{
			name: "untrusted restricted mode is plan", mode: "dontAsk",
			want: []string{flagPermissionMode, modePlan}, summary: summaryOutsidePlan,
		},
		{
			name: "override restores full access", override: true, mode: profiles.PermissionFull,
			want: []string{flagSkipPermissions}, summary: summaryFull,
		},
		{
			name: "override keeps the profile's plan mode", override: true, mode: profiles.PermissionFull, plan: true,
			want: []string{flagPermissionMode, modePlan, flagAllowSkipPermissions}, summary: summaryPlanFull,
		},
		{
			name: "override keeps a restricted mode", override: true, mode: "acceptEdits",
			want: []string{flagPermissionMode, "acceptEdits"}, summary: summaryModePrefix + "acceptEdits",
		},
		{
			name: "override keeps the agent defaults", override: true,
			want: nil, summary: summaryDefault,
		},
		{
			name: "override on a trusted item changes nothing", trusted: true, override: true, mode: "auto",
			want: []string{flagPermissionMode, "auto"}, summary: summaryModePrefix + "auto",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := fullProfile()
			p.PermissionMode = tt.mode
			p.PlanMode = tt.plan

			got, err := resolvePermission(tt.trusted, tt.override, p)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.args)
			assert.Equal(t, tt.summary, got.summary)
		})
	}
}

func TestPermissionModesAccepted(t *testing.T) {
	for _, mode := range append(permissionModes(), "", profiles.PermissionFull) {
		t.Run("mode "+mode, func(t *testing.T) {
			p := fullProfile()
			p.PermissionMode = mode

			_, err := resolvePermission(true, false, p)
			require.NoError(t, err)
		})
	}

	for _, trusted := range []bool{true, false} {
		p := fullProfile()
		p.PermissionMode = "yolo"

		_, err := resolvePermission(trusted, false, p)
		require.ErrorIs(t, err, ErrInvalidPermissionMode)
	}
}

// trustCase is one combination in the trust × override matrix.
type trustCase struct {
	assoc    string
	cross    bool
	mode     string
	plan     bool
	override bool
}

// name labels the case for t.Run.
func (c trustCase) name() string {
	return c.assoc + "/cross=" + strconv.FormatBool(c.cross) + "/perm=" + c.mode +
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

// wantPermission is the expected permission outcome of a trust case.
type wantPermission struct {
	effective bool
	mode      string
	skip      bool
	allow     bool
}

// expectPermission states the policy independently of resolvePermission.
func expectPermission(c trustCase) wantPermission {
	trustedAssoc := slices.Contains(
		[]string{trust.AssociationOwner, trust.AssociationMember, trust.AssociationCollaborator}, c.assoc,
	)
	effective := trustedAssoc && !c.cross || c.override
	full := c.mode == profiles.PermissionFull

	w := wantPermission{effective: effective}

	switch {
	case !effective:
		w.mode = modePlan
	case c.plan:
		w.mode, w.allow = modePlan, full
	case full:
		w.skip = true
	default:
		w.mode = c.mode
	}

	return w
}

func TestTrustOverrideMatrix(t *testing.T) {
	bools := []bool{false, true}

	cases := expand([]trustCase{{}}, []string{
		trust.AssociationOwner, trust.AssociationMember, trust.AssociationCollaborator,
		"CONTRIBUTOR", "FIRST_TIME_CONTRIBUTOR", "NONE",
	}, func(c *trustCase, v string) { c.assoc = v })
	cases = expand(cases, bools, func(c *trustCase, v bool) { c.cross = v })
	cases = expand(cases, []string{profiles.PermissionFull, "", "acceptEdits"},
		func(c *trustCase, v string) { c.mode = v })
	cases = expand(cases, bools, func(c *trustCase, v bool) { c.plan = v })
	cases = expand(cases, bools, func(c *trustCase, v bool) { c.override = v })
	require.Len(t, cases, 6*2*3*2*2)

	for _, c := range cases {
		t.Run(c.name(), func(t *testing.T) {
			checkTrustCase(t, c)
		})
	}
}

// checkTrustCase builds one pull request launch and checks every permission flag's
// presence and absence against the policy.
func checkTrustCase(t *testing.T, c trustCase) {
	t.Helper()

	req := baseRequest()
	req.Item.Trusted = trust.Trusted(c.assoc, true, c.cross)
	req.Profile.PermissionMode = c.mode
	req.Profile.PlanMode = c.plan
	req.FullAccessOverride = c.override

	args := buildArgs(t, req)
	want := expectPermission(c)

	gotMode, hasMode := flagValue(args, flagPermissionMode)
	assert.Equal(t, want.mode != "", hasMode, "--permission-mode presence")
	assert.Equal(t, want.mode, gotMode, "--permission-mode value")
	assert.Equal(t, want.skip, slices.Contains(args, flagSkipPermissions), "--dangerously-skip-permissions presence")
	assert.Equal(t, want.allow, slices.Contains(args, flagAllowSkipPermissions),
		"--allow-dangerously-skip-permissions presence")
	assert.False(t, hasMode && slices.Contains(args, flagSkipPermissions),
		"--dangerously-skip-permissions is never combined with --permission-mode")

	if !want.effective {
		assert.Equal(t, []string{flagPermissionMode, modePlan}, args,
			"an outside contributor's item gets plan mode and nothing else")
	}
}

func TestUntrustedLaunchIsPlanMode(t *testing.T) {
	for _, plan := range []bool{false, true} {
		t.Run("profile plan="+strconv.FormatBool(plan), func(t *testing.T) {
			req := baseRequest()
			req.Item.Trusted = trust.Trusted("CONTRIBUTOR", true, true)
			req.Profile.PlanMode = plan

			got, err := BuildPlan(req)
			require.NoError(t, err)
			assert.Equal(t, []string{flagPermissionMode, modePlan}, got.Args)
			assert.NotContains(t, got.Args, flagSkipPermissions)
			assert.NotContains(t, got.Args, flagAllowSkipPermissions)
			assert.Equal(t, summaryOutsidePlan, got.Permission)
		})
	}

	// A bot's issue is untrusted too.
	req := baseRequest()
	req.Item = Item{Kind: KindIssue, Number: 3, Trusted: trust.Trusted("NONE", false, false)}

	assert.Equal(t, []string{flagPermissionMode, modePlan}, buildArgs(t, req))
}

func TestBuildPlanFlagMatrix(t *testing.T) {
	dirA, dirB := absPath("shared"), absPath("with space", "docs")

	tests := []struct {
		name     string
		edit     func(p *profiles.Profile)
		worktree bool
		want     []string
	}{
		{
			name: "discovered profile", edit: func(*profiles.Profile) {},
			want: []string{flagSkipPermissions},
		},
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
		{
			name: "no permission setting", edit: func(p *profiles.Profile) { p.PermissionMode = "" },
			want: nil,
		},
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
		{
			name: "worktree only", edit: func(*profiles.Profile) {}, worktree: true,
			want: []string{flagSkipPermissions, flagWorktree, "#7"},
		},
		{name: "everything, in order", worktree: true, edit: func(p *profiles.Profile) {
			p.Model, p.Effort, p.PlanMode = "opus", "max", true
			p.AddDirs, p.ExtraArgs = []string{dirA}, []string{"--verbose"}
		}, want: []string{
			flagModel, "opus", flagEffort, "max",
			flagPermissionMode, modePlan, flagAllowSkipPermissions,
			flagAddDirPrefix + dirA, "--verbose", flagWorktree, "#7",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := baseRequest()
			tt.edit(&req.Profile)
			req.Worktree = tt.worktree

			assert.Equal(t, tt.want, buildArgs(t, req))
		})
	}

	for _, effort := range effortLevels() {
		t.Run("effort "+effort, func(t *testing.T) {
			req := baseRequest()
			req.Profile.Effort = effort

			got, ok := flagValue(buildArgs(t, req), flagEffort)
			assert.True(t, ok)
			assert.Equal(t, effort, got)
		})
	}
}

func TestBuildPlanFields(t *testing.T) {
	req := baseRequest()
	req.PromptFile = absPath("launch", "pr-7.prompt.md")
	req.Worktree = true

	got, err := BuildPlan(req)
	require.NoError(t, err)
	assert.Equal(t, KindPR, got.Kind)
	assert.Equal(t, 7, got.Number)
	assert.Equal(t, req.Clone.Dir, got.Dir)
	assert.Equal(t, req.ClaudePath, got.Binary)
	assert.Equal(t, req.PromptFile, got.PromptFile)
	assert.Equal(t, "#7", got.Worktree)
	assert.True(t, got.WorktreeOnPRHead)
	assert.Equal(t, summaryFull, got.Permission)
}

func TestBuildPlanRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		edit func(r *Request)
		want error
	}{
		{name: "unknown kind", edit: func(r *Request) { r.Item.Kind = "discussion" }, want: ErrInvalidKind},
		{name: "zero number", edit: func(r *Request) { r.Item.Number = 0 }, want: ErrInvalidNumber},
		{name: "negative number", edit: func(r *Request) { r.Item.Number = -4 }, want: ErrInvalidNumber},
		{name: "relative clone dir", edit: func(r *Request) { r.Clone.Dir = "projects/example-repo" }, want: ErrRelativePath},
		{name: "empty clone dir", edit: func(r *Request) { r.Clone.Dir = "" }, want: ErrRelativePath},
		{name: "relative agent path", edit: func(r *Request) { r.ClaudePath = "claude" }, want: ErrRelativePath},
		{name: "relative prompt file", edit: func(r *Request) { r.PromptFile = "pr-7.prompt.md" }, want: ErrRelativePath},
		{name: "unknown effort", edit: func(r *Request) { r.Profile.Effort = "extreme" }, want: ErrInvalidEffort},
		{name: "model that looks like a flag", edit: func(r *Request) { r.Profile.Model = "--help" }, want: ErrInvalidModel},
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
		{name: "NUL in the clone dir", edit: func(r *Request) { r.Clone.Dir = absPath("a\x00b") }, want: ErrNULByte},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := baseRequest()
			tt.edit(&req)

			got, err := BuildPlan(req)
			require.ErrorIs(t, err, tt.want)
			assert.Equal(t, Plan{}, got)
		})
	}
}

func TestWorktreeFlag(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		number   int
		origin   bool
		worktree bool
		want     string
		onHead   bool
	}{
		{
			name: "pr from origin uses the head form", kind: KindPR, number: 7, origin: true, worktree: true,
			want: "#7", onHead: true,
		},
		{name: "pr from another remote", kind: KindPR, number: 7, worktree: true, want: "pr-7"},
		{name: "issue with origin", kind: KindIssue, number: 12, origin: true, worktree: true, want: "issue-12"},
		{name: "issue without origin", kind: KindIssue, number: 12, worktree: true, want: "issue-12"},
		{name: "pr with worktree off", kind: KindPR, number: 7, origin: true},
		{name: "issue with worktree off", kind: KindIssue, number: 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := baseRequest()
			req.Item.Kind, req.Item.Number = tt.kind, tt.number
			req.Clone.OriginIsItemRepo = tt.origin
			req.Worktree = tt.worktree

			first, err := BuildPlan(req)
			require.NoError(t, err)

			again, err := BuildPlan(req)
			require.NoError(t, err)
			assert.Equal(t, first, again, "launching again on the same item reuses the same name")

			assert.Equal(t, tt.want, first.Worktree)
			assert.Equal(t, tt.onHead, first.WorktreeOnPRHead)

			if tt.want == "" {
				assert.NotContains(t, first.Args, flagWorktree)

				return
			}

			require.GreaterOrEqual(t, len(first.Args), 2)
			assert.Equal(t, []string{flagWorktree, tt.want}, first.Args[len(first.Args)-2:],
				"--worktree comes last")
			assert.Equal(t, 1, countOf(first.Args, flagWorktree))
		})
	}
}

// countOf counts how often v appears in s.
func countOf(s []string, v string) int {
	n := 0

	for _, e := range s {
		if e == v {
			n++
		}
	}

	return n
}

func TestEnvHandling(t *testing.T) {
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
			wantEnv: map[string]string{}, wantUnset: []string{envConfigDir},
		},
		{
			name:    "default profile drops a config dir from its env",
			env:     map[string]string{envConfigDir: absPath("elsewhere"), "EDITOR": "vi"},
			wantEnv: map[string]string{"EDITOR": "vi"}, wantUnset: []string{envConfigDir},
		},
		{
			name: "named profile sets the config dir", configDir: workDir, env: nil,
			wantEnv: map[string]string{envConfigDir: workDir},
		},
		{
			name: "the config dir wins over the profile env", configDir: workDir,
			env:     map[string]string{envConfigDir: absPath("elsewhere"), "_PRIVATE_1": "a b 'c'"},
			wantEnv: map[string]string{envConfigDir: workDir, "_PRIVATE_1": "a b 'c'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			req := baseRequest()
			req.Profile.Env = map[string]string{key: "value"}

			_, err := BuildPlan(req)
			require.ErrorIs(t, err, ErrInvalidEnvKey)
		})
	}
}

func TestPromptIsFirstArgument(t *testing.T) {
	req := baseRequest()
	req.Profile.AddDirs = []string{absPath("shared"), absPath("docs")}
	req.Profile.ExtraArgs = []string{"--verbose"}
	req.Worktree = true

	plan, err := BuildPlan(req)
	require.NoError(t, err)

	argv := plan.Argv("Review pull request #7")
	require.Len(t, argv, len(plan.Args)+2)
	assert.Equal(t, req.ClaudePath, argv[0])
	assert.Equal(t, "Review pull request #7", argv[1])
	assert.Equal(t, plan.Args, argv[2:])
	assert.NotContains(t, argv, "--add-dir", "add-dir always uses the equals form")
}

func TestExtraArgsCannotOverridePermissions(t *testing.T) {
	forms := map[string][]string{}

	for _, flag := range permissionFlags() {
		forms[flag+" alone"] = []string{flag}
		forms[flag+" with a separate value"] = []string{flag, "bypassPermissions"}
		forms[flag+" with an equals value"] = []string{flag + "=bypassPermissions"}
		forms[flag+" after a harmless arg"] = []string{"--verbose", flag + "=true"}
	}

	for name, extra := range forms {
		for _, trusted := range []bool{true, false} {
			t.Run(name+"/trusted="+strconv.FormatBool(trusted), func(t *testing.T) {
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
