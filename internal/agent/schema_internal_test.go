package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// schemaChecker checks a JSON value against the draft-07 keywords the recorded Claude Code
// settings schema uses: type, properties, additionalProperties, required, items, enum,
// const, minLength, pattern, minimum, maximum, exclusiveMinimum, uniqueItems, $ref, and
// anyOf. Every other keyword is a description, which it skips.
type schemaChecker struct {
	defs map[string]any
}

// loadSchema reads the recorded schema.
func loadSchema(t *testing.T) (map[string]any, schemaChecker) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "claude-settings.schema.json"))
	require.NoError(t, err)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(data, &schema))

	defs, _ := schema["$defs"].(map[string]any)

	return schema, schemaChecker{defs: defs}
}

func TestTheSchemaCheckerCatchesWhatClaudeCodeWouldRefuse(t *testing.T) {
	t.Parallel()

	schema, checker := loadSchema(t)

	for name, raw := range map[string]string{
		"a misspelled sandbox key": `{"sandbox":{"enabeld":true}}`,
		"a string for a switch":    `{"sandbox":{"enabled":"yes"}}`,
		"an unknown top key":       `{"sandboxx":{}}`,
		"a rule with a paren":      `{"permissions":{"allow":["Bash(a))"]}}`,
		"a hook with no command":   `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command"}]}]}}`,
		"a repeated rule":          `{"permissions":{"deny":["Edit","Edit"]}}`,
		"an unknown onFailure":     `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x","onFailure":"ignore"}]}]}}`,
		"a zero timeout":           `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x","timeout":0}]}]}}`,
	} {
		var v any
		require.NoError(t, json.Unmarshal([]byte(raw), &v))
		require.NotEmpty(t, checker.check(schema, v, "settings"), name)
	}
}
