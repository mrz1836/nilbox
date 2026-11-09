package codex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/sessions"
)

func TestLinkAllGivesEachThreadToTheLaunchBeforeIt(t *testing.T) {
	t.Parallel()

	const (
		dir    = "/srv/wt/example-repo/issue-14"
		prompt = "Work on this issue #14 in acme-org/example-repo: Crash on start"
	)

	launched := time.Unix(1794202015, 0)

	launch := func(id string, at time.Time) sessions.Record {
		return sessions.Record{ID: id, Agent: agent.Codex, Dir: dir, LaunchedAt: at, PromptHead: prompt}
	}

	first, second := launch("first", launched), launch("second", launched.Add(time.Minute))
	firstThread := Thread{ID: "t-first", Cwd: dir, Preview: prompt, CreatedAt: launched.Unix() + 3}
	secondThread := Thread{ID: "t-second", Cwd: dir, Preview: prompt, CreatedAt: launched.Add(time.Minute).Unix() + 3}

	t.Run("a launch whose thread never came takes no later launch's", func(t *testing.T) {
		t.Parallel()

		got := linkAll([]Thread{secondThread}, []sessions.Record{first, second}, map[string]bool{})
		assert.Equal(t, map[string]string{"second": "t-second"}, got)
	})

	t.Run("each launch takes its own", func(t *testing.T) {
		t.Parallel()

		got := linkAll([]Thread{secondThread, firstThread}, []sessions.Record{second, first}, map[string]bool{})
		assert.Equal(t, map[string]string{"first": "t-first", "second": "t-second"}, got)
	})

	t.Run("a thread started after a linked launch is that launch's", func(t *testing.T) {
		t.Parallel()

		linked := launch("linked", launched)
		linked.AgentSessionID = "t-linked"

		earlier := launch("earlier", launched.Add(-time.Hour))
		later := Thread{ID: "t-new", Cwd: dir, Preview: prompt, CreatedAt: launched.Unix() + 30}

		got := linkAll([]Thread{later}, []sessions.Record{earlier, linked}, map[string]bool{"t-linked": true})
		assert.Empty(t, got, "the earlier launch does not take a thread its folder's later session started")
	})
}
