package agentrun

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
)

func TestTheReportSchemaInItsStrictFormIsWhatCodexTook(t *testing.T) {
	t.Parallel()

	files, err := agent.HeadlessFiles(agent.Codex, ReportSchema)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, agent.ReportSchemaName, files[0].Name)

	assert.Equal(t, requiredSorted(t, fixture(t, "codex/outcome-schema-strict.json")), requiredSorted(t, files[0].Data),
		"the report's schema as S6 found Codex takes it: closed, every property required, the optional ones nullable")

	report := parseReport(json.RawMessage(`{"outcome":"fixed","summary":"ok","pushed":false,"pushedCommit":null,` +
		`"needsPerson":false,"reason":null}`))
	require.NotNil(t, report, "a strict report's nulls read as none")
	assert.Empty(t, report.PushedCommit)
	assert.Empty(t, report.Reason)
}

// requiredSorted reads a schema with its required list sorted: its order means nothing.
func requiredSorted(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var v map[string]any
	require.NoError(t, json.Unmarshal(data, &v))

	if req, ok := v["required"].([]any); ok {
		slices.SortFunc(req, func(a, b any) int { return strings.Compare(a.(string), b.(string)) })
	}

	return v
}
