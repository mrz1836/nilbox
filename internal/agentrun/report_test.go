package agentrun

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAReportsReviewIsKeptToItsSchema(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	require.NoError(t, json.Unmarshal([]byte(ReportSchema), &schema), "the schema is JSON")

	raw := `{"outcome":"nothing_to_do","summary":"Reviewed.","pushed":false,"needsPerson":false,` +
		`"review":{"verdict":"request_changes","body":"  Fix the test.  ","comments":[` +
		`{"path":"a.go","line":3,"body":"here"},{"path":"","line":1,"body":"no file"},` +
		`{"path":"a.go","line":0,"body":"no line"},{"path":"a.go","line":4,"body":"   "}]}}`

	got := parseReport(json.RawMessage(raw))
	require.NotNil(t, got)
	require.NotNil(t, got.Review)
	assert.Equal(t, "request_changes", got.Review.Verdict)
	assert.Equal(t, "Fix the test.", got.Review.Body)
	assert.Equal(t, []ReviewComment{{Path: "a.go", Line: 3, Body: "here"}}, got.Review.Comments,
		"a comment with no file, line, or text is left out")

	for _, review := range []string{
		`{"verdict":"merge","body":"x"}`, `{"verdict":"approve","body":" "}`,
	} {
		r := parseReport(json.RawMessage(`{"outcome":"fixed","summary":"s","pushed":false,"needsPerson":false,` +
			`"review":` + review + `}`))
		require.NotNil(t, r)
		assert.Nil(t, r.Review, "%s: a verdict it doesn't know, or nothing to post, is no review", review)
	}

	long := strings.Repeat("y", maxReviewBody+10)
	r := parseReport(json.RawMessage(`{"outcome":"fixed","summary":"s","pushed":false,"needsPerson":false,` +
		`"review":{"verdict":"approve","body":"` + long + `"}}`))
	require.NotNil(t, r.Review)
	assert.LessOrEqual(t, len([]rune(r.Review.Body)), maxReviewBody+1, "cut to its cap")
}
