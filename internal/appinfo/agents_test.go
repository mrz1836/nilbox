package appinfo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var errExit = errors.New("exit status 1")

// agentCommand writes a stand-in for an agent's command under home and returns its path.
func agentCommand(t *testing.T, home, name string) string {
	t.Helper()

	dir := filepath.Join(home, ".local", "bin")
	require.NoError(t, os.MkdirAll(dir, 0o700))

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700)) //nolint:gosec // a command must be executable

	return path
}

// versionScript answers each command's --version with out[its base name], or fails with
// fail[its base name], counting the runs.
type versionScript struct {
	mu   sync.Mutex
	out  map[string]string
	fail map[string]string
	runs map[string]int
}

func (v *versionScript) runner() *testutil.Runner {
	return &testutil.Runner{Script: func(_ context.Context, argv []string) ([]byte, []byte, error) {
		v.mu.Lock()
		defer v.mu.Unlock()

		name := filepath.Base(argv[0])
		v.runs[name]++

		if stderr, ok := v.fail[name]; ok {
			return nil, []byte(stderr), errExit
		}

		return []byte(v.out[name]), nil, nil
	}}
}

func (v *versionScript) count(name string) int {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.runs[name]
}

func TestAgentVersionsReadsEachAgentOnceForEachBuild(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	claude, codex := agentCommand(t, home, "claude"), agentCommand(t, home, "codex")
	script := &versionScript{
		out:  map[string]string{"claude": "2.1.319 (Claude Code)\n", "codex": "codex-cli 0.163.0\n"},
		runs: map[string]int{},
	}
	run := script.runner()
	s := NewService(Config{
		Finder: testutil.Finder{Paths: map[string]string{"claude": claude, "codex": codex}}, Runner: run, Home: home,
	})

	assert.Equal(t, []AgentVersion{
		{Agent: agent.Claude, Name: "Claude", Installed: true, Path: "~/.local/bin/claude", Version: "2.1.319"},
		{Agent: agent.Codex, Name: "Codex", Installed: true, Path: "~/.local/bin/codex", Version: "0.163.0"},
	}, s.AgentVersions(t.Context()))
	assert.Contains(t, run.Calls(), []string{codex, "--version"})

	s.AgentVersions(t.Context())
	s.BugReportURL(t.Context())
	assert.Equal(t, 1, script.count("claude"), "the same build is read once")

	// The agent updated itself: its command changed, so its version is read again.
	require.NoError(t, os.WriteFile(claude, []byte("#!/bin/sh\n# 2.1.320\n"), 0o700)) //nolint:gosec // a command must be executable
	script.mu.Lock()
	script.out["claude"] = "2.1.320 (Claude Code)\n"
	script.mu.Unlock()

	assert.Equal(t, "2.1.320", s.AgentVersions(t.Context())[0].Version)
	assert.Equal(t, 2, script.count("claude"))
	assert.Equal(t, 1, script.count("codex"), "an unchanged agent is not read again")
}

func TestAgentVersionsFollowsALinkToTheBuild(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	versions := filepath.Join(home, ".local", "share", "claude", "versions")
	require.NoError(t, os.MkdirAll(versions, 0o700))

	for _, v := range []string{"2.1.319", "2.1.320"} {
		require.NoError(t, os.WriteFile(filepath.Join(versions, v), []byte("#!/bin/sh\n"), 0o700)) //nolint:gosec // a command must be executable
	}

	link := filepath.Join(home, "claude")
	require.NoError(t, os.Symlink(filepath.Join(versions, "2.1.319"), link))

	script := &versionScript{out: map[string]string{"claude": "2.1.319 (Claude Code)"}, runs: map[string]int{}}
	s := NewService(Config{Finder: testutil.Finder{Paths: map[string]string{"claude": link}}, Runner: script.runner()})

	assert.Equal(t, "2.1.319", s.AgentVersions(t.Context())[0].Version)

	// An update points the link at the new build, which has the same size and may have
	// the same time, but another path.
	require.NoError(t, os.Remove(link))
	require.NoError(t, os.Symlink(filepath.Join(versions, "2.1.320"), link))
	script.mu.Lock()
	script.out["claude"] = "2.1.320 (Claude Code)"
	script.mu.Unlock()

	assert.Equal(t, "2.1.320", s.AgentVersions(t.Context())[0].Version)
}

func TestAgentVersionsSaysWhyAVersionIsMissing(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	claude, codex := agentCommand(t, home, "claude"), agentCommand(t, home, "codex")
	script := &versionScript{
		out:  map[string]string{"codex": "no version here\n"},
		fail: map[string]string{"claude": "\nError: unknown option '--version'\n"},
		runs: map[string]int{},
	}
	s := NewService(Config{
		Finder: testutil.Finder{Paths: map[string]string{"claude": claude, "codex": codex}}, Runner: script.runner(),
	})

	got := s.AgentVersions(t.Context())
	assert.Equal(t, "claude --version failed: Error: unknown option '--version'", got[0].Error)
	assert.Empty(t, got[0].Version)
	assert.Equal(t, "codex --version printed no version", got[1].Error)

	s.AgentVersions(t.Context())
	assert.Equal(t, 1, script.count("claude"), "a build that fails, fails again, so it is not run again")

	report := environment(Info{}, got)
	assert.Contains(t, report, "- Claude: unknown\n")
	assert.NotContains(t, report, "--version", "a report never carries the error")
}

func TestAgentVersionsWithoutTheirCommands(t *testing.T) {
	t.Parallel()

	got := NewService(Config{}).AgentVersions(t.Context())
	require.Len(t, got, len(agent.All()))

	for _, a := range got {
		assert.False(t, a.Installed)
		assert.Empty(t, a.Path)
	}

	got = NewService(Config{
		Finder: testutil.Finder{Paths: map[string]string{"claude": "/gone/claude"}},
		Runner: &testutil.Runner{},
	}).AgentVersions(t.Context())
	assert.True(t, got[0].Installed)
	assert.True(t, strings.HasPrefix(got[0].Error, "couldn't read claude: "), got[0].Error)
}

func TestAgentVersionsGivesUpOnAnAgentThatDoesNotAnswer(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	codex := agentCommand(t, home, "codex")

	var mu sync.Mutex

	answer := false
	run := &testutil.Runner{Script: func(ctx context.Context, _ []string) ([]byte, []byte, error) {
		mu.Lock()
		defer mu.Unlock()

		if answer {
			return []byte("codex-cli 0.163.0"), nil, nil
		}

		<-ctx.Done()

		return nil, nil, ctx.Err()
	}}
	s := NewService(Config{Finder: testutil.Finder{Paths: map[string]string{"codex": codex}}, Runner: run})

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	got := s.AgentVersions(ctx)[1]
	assert.Empty(t, got.Version)
	assert.NotEmpty(t, got.Error)

	mu.Lock()
	answer = true
	mu.Unlock()

	assert.Equal(t, "0.163.0", s.AgentVersions(t.Context())[1].Version, "a run cut short is tried again")
}
