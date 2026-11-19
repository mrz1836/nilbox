package core

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/notify"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/setup"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// oneCheck checks one need with checks.
func oneCheck(t *testing.T, checks *readyChecks, n autopilot.Need, fresh bool) autopilot.ReadyCheck {
	t.Helper()

	got, err := checks.Check(t.Context(), []autopilot.Need{n}, fresh)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, n, got[0].Need)
	assert.False(t, got[0].CheckedAt.IsZero(), "every check says when it was taken")

	return got[0]
}

// setupGitHub answers Check again's GitHub check as the setup would, counting each ask.
type setupGitHub struct {
	answer setup.GitHub
	err    error
	asked  int
}

func (s *setupGitHub) check(context.Context) (setup.GitHub, error) {
	s.asked++

	return s.answer, s.err
}

// errOffline is a GitHub that couldn't be reached.
var errOffline = errors.New("offline")

// githubNeed is the need of a GitHub sign-in.
func githubNeed() autopilot.Need { return autopilot.Need{Kind: autopilot.NeedGitHub} }

func TestReadinessReadsGitHubFromTheLastRefreshAtNoCost(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	gh := &setupGitHub{answer: setup.GitHub{State: setup.GitHubReady, Login: rigViewer}}
	r.deps.github = gh.check
	checks := newReadyChecks(r.deps)
	prefs := prefsstore.New(r.db, nil)

	unread := oneCheck(t, checks, githubNeed(), false)
	assert.Equal(t, autopilot.ReadyWarning, unread.Level, "before a refresh read the account, it only warns")
	assert.Equal(t, "hasn't been read yet", unread.Found)

	require.NoError(t, prefs.Set(t.Context(), "inbox.viewer", rigViewer))

	read := oneCheck(t, checks, githubNeed(), false)
	assert.Equal(t, autopilot.ReadyOK, read.Level)
	assert.Equal(t, "signed in as @octo-user, from the last refresh", read.Found)
	assert.Zero(t, gh.asked, "the routines' own checks ask GitHub nothing")

	require.NoError(t, prefs.Set(t.Context(), "inbox.signed_out", "1"))

	out := oneCheck(t, checks, githubNeed(), false)
	assert.Equal(t, autopilot.ReadyProblem, out.Level, "a refresh that found no one signed in is a problem")
	assert.Equal(t, "is signed out", out.Found)
	assert.Equal(t, setup.FixSignIn, out.Fix)
	assert.True(t, out.Command)

	broken := newReadyChecks(agentDeps{inbox: inbox.NewStore(testutil.ClosedDB(t), nil)})
	unreadable := broken.github(t.Context(), false)
	assert.Equal(t, autopilot.ReadyWarning, unreadable.Level, "an inbox that can't be read only warns")
	assert.Equal(t, "couldn't be read", unreadable.Found)
}

func TestCheckAgainAsksGitHubAsTheSetupDoes(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	require.NoError(t, prefsstore.New(r.db, nil).Set(t.Context(), "inbox.viewer", rigViewer))

	gh := &setupGitHub{answer: setup.GitHub{State: setup.GitHubReady, Login: rigViewer}}
	r.deps.github = gh.check
	checks := newReadyChecks(r.deps)

	fresh := oneCheck(t, checks, githubNeed(), true)
	assert.Equal(t, autopilot.ReadyOK, fresh.Level)
	assert.Equal(t, "signed in as @octo-user, with repo and read:org", fresh.Found)
	assert.Equal(t, 1, gh.asked, "Check again asks GitHub, for a point")

	gh.err = errOffline

	fellBack := oneCheck(t, checks, githubNeed(), true)
	assert.Equal(t, "signed in as @octo-user, from the last refresh", fellBack.Found,
		"a check that couldn't be made falls back to the last refresh")
}

func TestTheSetupsGitHubAnswerIsWordedForTheRoutines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      setup.GitHub
		level   autopilot.ReadyLevel
		found   string
		command bool
	}{
		{
			name: "missing scopes", in: setup.GitHub{
				State: setup.GitHubMissingScopes, Missing: []string{"repo", "read:org"}, Fix: "gh auth refresh -s repo,read:org",
			},
			level: autopilot.ReadyProblem, found: "is missing repo and read:org", command: true,
		},
		{
			name: "unreachable", in: setup.GitHub{State: setup.GitHubUnreachable, Message: "GitHub couldn't be reached."},
			level: autopilot.ReadyWarning, found: "couldn't be reached",
		},
		{
			name: "no CLI", in: setup.GitHub{State: setup.GitHubNoCLI, Message: "The GitHub CLI isn't installed.", Fix: setup.FixInstall},
			level: autopilot.ReadyProblem, found: "the github cli isn't installed", command: true,
		},
		{
			name: "signed out with no words", in: setup.GitHub{State: setup.GitHubSignedOut},
			level: autopilot.ReadyProblem, found: "isn't signed in",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := githubCheck(tt.in)
			assert.Equal(t, "GitHub", got.Label)
			assert.Equal(t, tt.level, got.Level)
			assert.Equal(t, tt.found, got.Found)
			assert.Equal(t, tt.command, got.Command, "only a fix there is runs as a command")
			assert.Equal(t, tt.in.Fix, got.Fix)
		})
	}
}

func TestReadinessSaysHowToGetAToolThisMacLacks(t *testing.T) {
	t.Parallel()

	checks := newReadyChecks(agentDeps{finder: testutil.Finder{Paths: map[string]string{"go": "/opt/tools/bin/go"}}})

	tests := []struct {
		tool    string
		fix     string
		command bool
	}{
		{tool: "gh", fix: setup.FixInstall, command: true},
		{tool: "git", fix: "xcode-select --install", command: true},
		{tool: "pnpm", fix: "Install it, then Check again."},
	}

	for _, tt := range tests {
		got := checks.tool(tt.tool)
		assert.Equal(t, autopilot.ReadyProblem, got.Level, tt.tool)
		assert.Equal(t, "isn't on this Mac", got.Found, tt.tool)
		assert.Equal(t, tt.fix, got.Fix, tt.tool)
		assert.Equal(t, tt.command, got.Command, tt.tool)
	}

	found := checks.tool("go")
	assert.Equal(t, autopilot.ReadyOK, found.Level)
	assert.Equal(t, "found", found.Found)
}

func TestReadinessChecksTheTerminalLaunchesOpenIn(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	r.deps.registry = terminal.NewRegistry(&scriptOpener{id: terminal.TerminalApp})
	checks := newReadyChecks(r.deps)
	need := autopilot.Need{Kind: autopilot.NeedTerminal}

	unknown := oneCheck(t, checks, need, false)
	assert.Equal(t, autopilot.ReadyProblem, unknown.Level, "a terminal this build can't open is a problem")
	assert.Equal(t, "iTerm2", unknown.Label)
	assert.Equal(t, "isn't one this Mac can open", unknown.Found)
	assert.Equal(t, "Choose another terminal in Settings → Launching.", unknown.Fix)

	_, err := r.settings.SetTerminal(t.Context(), terminal.TerminalApp)
	require.NoError(t, err)

	got := oneCheck(t, checks, need, false)
	assert.Equal(t, autopilot.ReadyOK, got.Level, "Terminal comes with every Mac")
	assert.Equal(t, "Terminal", got.Label)
	assert.Equal(t, "installed", got.Found)
}

// bannerPoster is a notification center whose banners stand as status.
type bannerPoster notify.BannerStatus

func (bannerPoster) Post(notify.Banner) error { return nil }

func (b bannerPoster) Status() notify.BannerStatus { return notify.BannerStatus(b) }

func (bannerPoster) Authorize() (bool, error) { return false, nil }

func TestReadinessOnlyWarnsOfBannersTurnedOff(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	need := autopilot.Need{Kind: autopilot.NeedBanners}

	off := r.deps
	off.banners = bannerPoster(notify.StatusOff)
	got := oneCheck(t, newReadyChecks(off), need, false)
	assert.Equal(t, autopilot.ReadyWarning, got.Level, "the notices still fill the history without banners")
	assert.Equal(t, "are off", got.Found)
	assert.Equal(t, "System Settings → Notifications", got.Fix)

	on := r.deps
	on.banners = bannerPoster(notify.StatusOn)
	assert.Equal(t, "allowed", oneCheck(t, newReadyChecks(on), need, false).Found)

	for name, deps := range map[string]agentDeps{
		"a build that can't show them": func() agentDeps {
			d := r.deps
			d.banners = bannerPoster(notify.StatusUnavailable)

			return d
		}(),
		"no notification center": r.deps,
	} {
		got := oneCheck(t, newReadyChecks(deps), need, false)
		assert.Equal(t, autopilot.ReadyOK, got.Level, name)
		assert.Equal(t, "not available in this build; the notices go to the history", got.Found, name)
	}
}

func TestReadinessSaysWhatInheritResolvesTo(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	checks := newReadyChecks(r.deps)

	got, err := checks.Defaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, string(agent.Default().ID), got.Agent, "with no default chosen, the app's own")
	assert.NotNil(t, got.Profiles)

	_, err = r.settings.SetDefaultAgent(t.Context(), agent.Codex)
	require.NoError(t, err)
	_, err = r.settings.SetDefaultProfile(t.Context(), agent.Claude, workProfile)
	require.NoError(t, err)

	got, err = checks.Defaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, string(agent.Codex), got.Agent)
	assert.Equal(t, workProfile, got.Profiles[claudeID])
}

func TestReadinessFailsOnlyWhenTheSettingsCantBeRead(t *testing.T) {
	t.Parallel()

	db := testutil.ClosedDB(t)
	checks := newReadyChecks(agentDeps{settings: settings.NewService(db, nil, t.TempDir())})

	_, err := checks.Defaults(t.Context())
	require.Error(t, err)

	_, err = checks.Check(t.Context(), []autopilot.Need{githubNeed()}, false)
	require.Error(t, err)
}

func TestReadinessNamesWhatThisVersionDoesntKnow(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	checks := newReadyChecks(r.deps)

	agentCheck := oneCheck(t, checks, autopilot.Need{Kind: autopilot.NeedAgent, Agent: "gemini"}, false)
	assert.Equal(t, autopilot.ReadyProblem, agentCheck.Level)
	assert.Equal(t, "gemini", agentCheck.Label)
	assert.Equal(t, "isn't an agent this version knows", agentCheck.Found)

	profile := oneCheck(t, checks, autopilot.Need{Kind: autopilot.NeedProfile, Agent: "gemini", Profile: workProfile}, false)
	assert.Equal(t, autopilot.ReadyProblem, profile.Level)
	assert.Equal(t, "Profile work", profile.Label)
	assert.Equal(t, "is for an agent this version doesn't know", profile.Found)

	place := oneCheck(t, checks, autopilot.Need{Kind: autopilot.NeedPlace}, false)
	assert.Equal(t, autopilot.ReadyOK, place.Level, "where a step runs is the engine's to check, never this Mac's")
}

func TestASignInCheckBeforeTheShellIsReadRunsWithThisProcesssEnvironment(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	r.deps.environ = func() ([]string, bool) { return nil, false }
	checks := newReadyChecks(r.deps)

	got := oneCheck(t, checks, needOf(needWork), true)
	assert.Equal(t, autopilot.ReadyOK, got.Level)

	r.rec.mu.Lock()
	env := r.rec.envs[len(r.rec.envs)-1]
	r.rec.mu.Unlock()

	assert.Equal(t, os.Getenv("PATH"), shellenv.Lookup(env, "PATH"), "the shell not read yet, the app's own PATH stands in")
}

func TestASignInWithAKeyLetInStillAsksTheAgentWhenTheShellSetsNone(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	_, err := r.settings.SetClaudeAPIKey(t.Context(), true)
	require.NoError(t, err)

	checks := newReadyChecks(r.deps)

	runs := r.rec.runs()
	got := oneCheck(t, checks, needOf(needWork), true)
	assert.Equal(t, autopilot.ReadyOK, got.Level)
	assert.Equal(t, "signed in · max", got.Found, "no key in the shell, so the profile's own sign-in is read")
	assert.Equal(t, runs+1, r.rec.runs(), "by asking the agent")
}

func TestAutopilotReadsWhereAgentsMayRunOnOutsideContributorsItems(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	defaults := launchdefaults.NewService(launchdefaults.Config{
		Store: r.defaults, Profiles: r.deps.profiles, Prompts: r.prompts,
	})

	_, err := defaults.Save(t.Context(), launchdefaults.Input{Owner: boundOwner, AutopilotOutside: launchdefaults.OutsideOn})
	require.NoError(t, err)
	_, err = defaults.Save(t.Context(), launchdefaults.Input{
		Owner: boundOwner, Name: "private-repo", AutopilotOutside: launchdefaults.OutsideOff,
	})
	require.NoError(t, err)

	idx, err := outsideRules{defaults: r.defaults}.Outside(t.Context())
	require.NoError(t, err)

	allowed, from := idx.Allowed("acme-org/example-repo")
	assert.True(t, allowed, "the org lets them")
	assert.Equal(t, "org", from)

	allowed, from = idx.Allowed("acme-org/private-repo")
	assert.False(t, allowed, "a repository's own choice wins over its org's")
	assert.Equal(t, "repo", from)

	allowed, from = idx.Allowed("other-org/example-repo")
	assert.False(t, allowed, "with no choice, never")
	assert.Empty(t, from)

	allowed, from = idx.Allowed("not a repository")
	assert.False(t, allowed)
	assert.Empty(t, from)

	_, err = outsideRules{defaults: launchdefaults.NewStore(testutil.ClosedDB(t), nil)}.Outside(t.Context())
	require.Error(t, err, "launch defaults that can't be read let nothing through")
}

func TestTheRoutinesPromptUsesSayWhatTheyCouldNotRead(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	uses := routinePrompts{
		follower: autopilot.NewPromptFollower(autopilot.NewService(autopilot.Config{Pool: testutil.ClosedDB(t)})),
		logger:   slog.New(slog.NewTextHandler(&logs, nil)),
	}

	_, err := uses.RoutineUses(t.Context())
	require.Error(t, err, "routines that can't be read are the library's error, not no uses")

	uses.Renamed(t.Context(), "Triage", "Triage checks")
	assert.Contains(t, logs.String(), "the routines could not follow a prompt renamed",
		"a rename the routines couldn't follow is logged")
}
