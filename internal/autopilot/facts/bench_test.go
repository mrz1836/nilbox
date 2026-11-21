package facts_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

// benchRows are n rows as an inbox of n items holds them: mostly pull requests with every
// part read, some whose details aren't read yet, some whose checks and files were read
// only in part, people's pull requests at every review decision, and issues.
func benchRows(n int) []facts.Row {
	decisions := []string{"APPROVED", "CHANGES_REQUESTED", "REVIEW_REQUIRED", "", "review_required"}
	rows := make([]facts.Row, 0, n)

	for i := range n {
		row := prRow()
		row.ID, row.Number = int64(i+1), i+1
		oid := fmt.Sprintf("%040x", i+1)

		row.Details.HeadOID, row.Checks.Head, row.Files.Head = oid, oid, oid

		switch i % 10 {
		case 6:
			row.Details, row.Checks, row.Files = nil, nil, nil
		case 7:
			row.Details.ChecksState, row.Details.ChecksPending = "PENDING", 1
			row.Checks.Complete, row.Files.Complete = false, false
			row.Checks.Checks = append(row.Checks.Checks, facts.CheckRow{Name: "e2e", Workflow: "CI", Outcome: facts.CheckPending})
			row.Files.Files = append(row.Files.Files, sensitive.File{Path: ".github/workflows/ci.yml"})
		case 8:
			row.Kind, row.Title = item.KindIssue, "Crash on start"
			row.Details = &facts.DetailsRow{Labels: []string{"bug", "Needs-Repro"}, Comments: 6}
			row.Checks, row.Files = nil, nil
		case 9:
			row.Author, row.AuthorType, row.Association = "octo-user", "User", "MEMBER"
			row.Title, row.HeadRef = "Fix the login flow", "fix/login"
			row.Details.ReviewDecision = decisions[(i/10)%len(decisions)]
			row.SnoozedUntil = new(now.Add(time.Hour))
		}

		rows = append(rows, row)
	}

	return rows
}

// BenchmarkFactsBuild builds the facts of 500 items, as one engine cycle does for an
// inbox that size.
func BenchmarkFactsBuild(b *testing.B) {
	rows := benchRows(500)
	e := env()

	b.ReportAllocs()

	for b.Loop() {
		for _, r := range rows {
			facts.Build(r, e)
		}
	}
}
