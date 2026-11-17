package agentrun

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
)

func TestTheReportSchemaInItsStrictFormIsWhatCodexTakes(t *testing.T) {
	t.Parallel()

	files, err := agent.HeadlessFiles(agent.Codex, ReportSchema)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, agent.ReportSchemaName, files[0].Name)

	var got, took map[string]any
	require.NoError(t, json.Unmarshal(files[0].Data, &got))
	require.NoError(t, json.Unmarshal(fixture(t, "codex/outcome-schema-strict.json"), &took))

	props, _ := got["properties"].(map[string]any)
	for name, want := range took["properties"].(map[string]any) {
		assert.Equal(t, want, props[name], "%s is as S6 found Codex takes it", name)
	}

	assertStrict(t, "report", got)

	review, _ := props["review"].(map[string]any)
	assert.Equal(t, []any{"object", "null"}, review["type"], "the review is optional, so nullable")

	report := parseReport(json.RawMessage(`{"outcome":"fixed","summary":"ok","pushed":false,"pushedCommit":null,` +
		`"needsPerson":false,"reason":null,"review":null}`))
	require.NotNil(t, report, "a strict report's nulls read as none")
	assert.Empty(t, report.PushedCommit)
	assert.Empty(t, report.Reason)
}

// assertStrict checks every object of a schema is closed with every property required, as
// Codex's strict mode needs, its arrays' items included.
func assertStrict(t *testing.T, at string, node map[string]any) {
	t.Helper()

	if props, ok := node["properties"].(map[string]any); ok {
		assert.Equal(t, false, node["additionalProperties"], "%s is closed", at)

		listed, _ := node["required"].([]any)

		required := make([]string, 0, len(listed))
		for _, r := range listed {
			required = append(required, r.(string))
		}

		assert.ElementsMatch(t, slices.Collect(maps.Keys(props)), required, "%s requires every property", at)

		for name, p := range props {
			assertStrict(t, at+"."+name, p.(map[string]any))
		}
	}

	if items, ok := node["items"].(map[string]any); ok {
		assertStrict(t, at+"[]", items)
	}
}
