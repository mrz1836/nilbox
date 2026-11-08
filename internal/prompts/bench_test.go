package prompts

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
)

// benchStarter returns the review starter, the prompt a pull request launches with
// unless the user picks another.
func benchStarter(b *testing.B) Starter {
	b.Helper()

	st, ok := starterFor(StarterReviewPR)
	require.True(b, ok)

	return st
}

// benchData returns a pull request from an outside contributor that changes sensitive
// files, launched in a worktree at its head, so a render fills in the head branch and
// adds both notices after the prompt.
func benchData() Data {
	data := Sample(item.KindPR).WithSession(true, true)
	data.OutsideContributor = true
	data.SensitiveFiles = ".github/workflows/ci.yml (Workflows), Makefile (Build tooling)"

	return data
}

// BenchmarkParse checks and parses the review starter, as every launch and preview does
// with the chosen prompt's text: Go's own parse, the prompt rules, and the sample fills
// for each session a pull request can start in.
func BenchmarkParse(b *testing.B) {
	st := benchStarter(b)

	b.ReportAllocs()

	for b.Loop() {
		if tmpl, diags := Parse(st.Body, st.AppliesTo); tmpl == nil {
			b.Fatal(diags)
		}
	}
}

// BenchmarkRender fills in the parsed review starter for one pull request, as every
// launch and preview does once the prompt is parsed: sanitizing the data, executing the
// template, tidying the text, and adding the notices.
func BenchmarkRender(b *testing.B) {
	st := benchStarter(b)

	tmpl, diags := Parse(st.Body, st.AppliesTo)
	require.NotNil(b, tmpl, "%+v", diags)

	data := benchData()

	out, err := tmpl.Render(data)
	require.NoError(b, err)
	require.Contains(b, out, "outside contributor")

	b.ReportAllocs()

	for b.Loop() {
		if _, err = tmpl.Render(data); err != nil {
			b.Fatal(err)
		}
	}
}
