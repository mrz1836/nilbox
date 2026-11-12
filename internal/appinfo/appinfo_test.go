package appinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/timing"
)

var errLog = errors.New("log write failed")

// stubRecorder returns a fixed result and counts calls.
type stubRecorder struct {
	calls int
	m     timing.Measurement
	err   error
}

func (r *stubRecorder) FirstPaint() (timing.Measurement, bool, error) {
	r.calls++

	if r.calls > 1 {
		return r.m, false, nil
	}

	return r.m, true, r.err
}

func TestInfo(t *testing.T) {
	t.Parallel()

	s := NewService(Config{Version: "v1.2.3-4-gabc1234-dirty", Release: "1.2.3", Build: "42", System: "macOS 15.1 (24B83)"})

	info := s.Info()
	assert.Equal(t, brand.Name, info.Name)
	assert.Equal(t, brand.Tagline, info.Tagline)
	assert.Equal(t, "v1.2.3-4-gabc1234-dirty", info.Version)
	assert.Equal(t, "1.2.3", info.Release)
	assert.Equal(t, "42", info.Build)
	assert.Equal(t, "macOS 15.1 (24B83)", info.System)
	assert.Equal(t, runtime.GOARCH, info.Arch)
}

func TestInfoReportsADataReset(t *testing.T) {
	t.Parallel()

	assert.Nil(t, NewService(Config{Version: "dev"}).Info().DataReset, "no reset by default")

	reset := &DataReset{Path: "~/data/app.db.broken-20260102T030405Z", Reason: "corrupt"}
	info := NewService(Config{Version: "dev", DataReset: reset}).Info()
	assert.Equal(t, reset, info.DataReset)
}

func TestInfoLeavesOutAMissingDataReset(t *testing.T) {
	t.Parallel()

	without, err := json.Marshal(NewService(Config{Version: "dev", System: "macOS"}).Info())
	require.NoError(t, err)
	assert.NotContains(t, string(without), "dataReset")

	with, err := json.Marshal(NewService(Config{
		Version:   "dev",
		System:    "macOS",
		DataReset: &DataReset{Path: "~/data/app.db.broken-20260102T030405Z", Reason: "history"},
	}).Info())
	require.NoError(t, err)
	assert.Contains(t, string(with),
		`"dataReset":{"path":"~/data/app.db.broken-20260102T030405Z","reason":"history"}`)
}

func TestInfoDetectsTheSystem(t *testing.T) {
	t.Parallel()

	info := NewService(Config{Version: "dev"}).Info()

	assert.NotEmpty(t, info.System)

	if runtime.GOOS == "darwin" {
		assert.Regexp(t, `^macOS \d+(\.\d+)* \(\w+\)$`, info.System)
	}
}

func TestBugReportURLFillsInTheEnvironment(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	claude := agentCommand(t, home, "claude")
	s := NewService(Config{
		Version: "abc1234-dirty", Release: "0.0.0", Build: "16", System: "macOS 15.1 (24B83)",
		Finder: testutil.Finder{Paths: map[string]string{"claude": claude}},
		Runner: &testutil.Runner{Stdout: "2.1.319 (Claude Code)\n"}, Home: home,
	})

	u, err := url.Parse(s.BugReportURL(t.Context()))
	require.NoError(t, err)

	assert.Equal(t, brand.Repository+"/issues/new", u.Scheme+"://"+u.Host+u.Path)
	assert.Equal(t, bugReportTemplate, u.Query().Get("template"))
	assert.Equal(t,
		"- Version: 0.0.0 (16)\n- Commit: abc1234-dirty\n- System: macOS 15.1 (24B83), "+runtime.GOARCH+"\n"+
			"- Claude: 2.1.319\n- Codex: not installed\n- GitHub CLI: not installed\n",
		u.Query().Get(environmentField))
}

// fixedState is a StateReader with fixed sections.
type fixedState []StateSection

func (f fixedState) BugReportState(context.Context) []StateSection { return f }

func TestBugReportURLFoldsTheAppStateUnderTheVersions(t *testing.T) {
	t.Parallel()

	s := NewService(Config{
		Version: "abc1234", Release: "0.1.0", Build: "16", System: "macOS 15.1 (24B83)",
		State: fixedState{
			{Title: "Settings", Lines: []string{"Theme: dark", "Terminal: iTerm2"}},
			{Title: "Integrations", Lines: []string{"magex: on (automatic) · 1.30.0 from Homebrew"}},
		},
	})

	u, err := url.Parse(s.BugReportURL(t.Context()))
	require.NoError(t, err)

	assert.Equal(t,
		"- Version: 0.1.0 (16)\n- Commit: abc1234\n- System: macOS 15.1 (24B83), "+runtime.GOARCH+"\n"+
			"- Claude: not installed\n- Codex: not installed\n- GitHub CLI: not installed\n"+
			"\n<details><summary>Settings and integrations</summary>\n"+
			"\n**Settings**\n\n- Theme: dark\n- Terminal: iTerm2\n"+
			"\n**Integrations**\n\n- magex: on (automatic) · 1.30.0 from Homebrew\n"+
			"\n</details>\n",
		u.Query().Get(environmentField))
}

// TestBugReportFormHasTheEnvironmentField guards the link against a renamed issue form or
// field: GitHub ignores a prefill for a field the form does not have.
func TestBugReportFormHasTheEnvironmentField(t *testing.T) {
	t.Parallel()

	form, err := os.ReadFile(filepath.Join("..", "..", ".github", "ISSUE_TEMPLATE", bugReportTemplate))
	require.NoError(t, err)

	assert.Contains(t, string(form), "id: "+environmentField+"\n")
}

func TestReportFirstPaintEmitsOnce(t *testing.T) {
	t.Parallel()

	rec := &stubRecorder{m: timing.Measurement{FirstPaintMS: 321, Source: timing.SourceOS}}
	events := &testutil.Events{}

	s := NewService(Config{Recorder: rec, Events: events})

	require.NoError(t, s.ReportFirstPaint())
	require.NoError(t, s.ReportFirstPaint())

	assert.Equal(t, 2, rec.calls)
	assert.Equal(t, []event.Event{event.FirstPaint{Measurement: rec.m}}, events.All())
}

func TestReportFirstPaintReturnsLogErrorAfterTheEvent(t *testing.T) {
	t.Parallel()

	rec := &stubRecorder{m: timing.Measurement{FirstPaintMS: 12, Source: timing.SourceRuntime}, err: errLog}
	events := &testutil.Events{}

	s := NewService(Config{Recorder: rec, Events: events})

	require.ErrorIs(t, s.ReportFirstPaint(), errLog)
	assert.Equal(t, 1, events.Count(), "the measurement is valid even when the timing log fails")
}

func TestReportFirstPaintWithoutRecorderOrSink(t *testing.T) {
	t.Parallel()

	require.NoError(t, NewService(Config{}).ReportFirstPaint())

	rec := &stubRecorder{}
	require.NoError(t, NewService(Config{Recorder: rec}).ReportFirstPaint())
	assert.Equal(t, 1, rec.calls)
}

func TestReportFirstPaintWithTimingRecorder(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rec := timing.NewRecorder(timing.Config{
		Logger: slog.New(slog.NewTextHandler(&buf, nil)),
		Start:  start,
		Source: timing.SourceOS,
		Now:    func() time.Time { return start.Add(250 * time.Millisecond) },
	})

	events := &testutil.Events{}

	s := NewService(Config{Recorder: rec, Events: events})

	require.NoError(t, s.ReportFirstPaint())
	require.Equal(t, 1, events.Count())

	paint, ok := events.All()[0].(event.FirstPaint)
	require.True(t, ok)
	assert.Equal(t, "startup.first_paint_ms=250 source=os", paint.Measurement.String())
	assert.Contains(t, buf.String(), "startup.first_paint_ms=250 source=os")
}
