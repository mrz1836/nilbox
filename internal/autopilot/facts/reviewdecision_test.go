package facts_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/item"
)

// TestBuildReadsTheReviewDecision pins how a pull request's review decision, as GitHub
// names it, reads as the catalog spells it: each of GitHub's three words in any case, none
// for no decision, and any other word lower-cased as it came, a space kept, for the
// condition to compare.
func TestBuildReadsTheReviewDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		decision string
		want     string
	}{
		{decision: "APPROVED", want: "approved"},
		{decision: "CHANGES_REQUESTED", want: "changesRequested"},
		{decision: "REVIEW_REQUIRED", want: "reviewRequired"},
		{decision: "", want: catalog.ReviewNone},
		{decision: "approved", want: "approved"},
		{decision: "changes_requested", want: "changesRequested"},
		{decision: "Review_Required", want: "reviewRequired"},
		{decision: "DISMISSED", want: "dismissed"},
		{decision: "SomethingNew", want: "somethingnew"},
		{decision: "none", want: "none"},
		{decision: "changesRequested", want: "changesrequested"},
		{decision: " APPROVED", want: " approved"},
	}

	for _, tt := range tests {
		t.Run(tt.decision, func(t *testing.T) {
			t.Parallel()

			row := prRow()
			row.Details.ReviewDecision = tt.decision

			assert.Equal(t, facts.Text(tt.want), facts.Build(row, env()).Value(catalog.FieldReviewDecision))
		})
	}
}

// TestBuildReviewDecisionUntilReadAndOnAnIssue pins the review decision of a pull request
// whose details aren't read yet, unknown until they are, and of an issue, which it doesn't
// apply to.
func TestBuildReviewDecisionUntilReadAndOnAnIssue(t *testing.T) {
	t.Parallel()

	row := prRow()
	row.Details = nil
	assert.Equal(t, facts.Unknown("its details aren't read yet", facts.NeedDetails),
		facts.Build(row, env()).Value(catalog.FieldReviewDecision))

	row = prRow()
	row.Kind = item.KindIssue
	assert.Equal(t, facts.NotApplicable("it's an issue"), facts.Build(row, env()).Value(catalog.FieldReviewDecision))
}
