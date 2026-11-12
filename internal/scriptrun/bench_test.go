package scriptrun

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// BenchmarkRunsFullHistory lists a full history, KeepRuns runs, each with warnings and
// unrecognized lines, as the Scripts tab does when it first opens on Runs.
func BenchmarkRunsFullHistory(b *testing.B) {
	e := NewEngine(Config{DB: testutil.DB(b), LogDir: filepath.Join(b.TempDir(), "script-runs")})
	svc := NewService(e)
	ctx := b.Context()
	start := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)

	for i := range KeepRuns {
		at := start.Add(time.Duration(i) * time.Minute)

		id, err := e.store.insert(ctx, started{
			spec: Spec{
				Kind: KindChecks, Title: "Checks · all files", Repo: testRepo(), Dir: "/work/example-repo",
				Argv: []string{"/usr/local/bin/go-pre-commit", "run", "--all-files"},
			},
			command: "go-pre-commit run --all-files", runsIn: RunsInApp, at: at,
			result: Result{Tool: "go-pre-commit", Warnings: []string{}, Unrecognized: []string{}},
		})
		require.NoError(b, err)

		require.NoError(b, e.store.finish(ctx, finished{
			id: id, status: StatusSucceeded, exitCode: new(i % 2), summary: "All 5 checks passed", logBytes: 4096,
			at: at.Add(38 * time.Second),
			result: Result{
				Tool: "go-pre-commit", Warnings: []string{fmt.Sprintf("warning %d", i)},
				Unrecognized: []string{"a line", "another"}, UnrecognizedCount: 2,
			},
		}))
	}

	b.ReportAllocs()

	for b.Loop() {
		runs, err := svc.Runs(ctx, Filter{})
		if err != nil {
			b.Fatal(err)
		}

		if len(runs) != KeepRuns {
			b.Fatalf("listed %d runs, want %d", len(runs), KeepRuns)
		}
	}
}
