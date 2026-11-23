package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// promptUUID and scriptUUID are UUIDs a spec names a prompt and a script by.
const (
	promptUUID = "6f9619ff-8b86-4d01-b42d-00cf4fc964ff"
	scriptUUID = "5d2e8f41-7a63-4c90-9b18-e4f6a2c3d5b7"
)

// withScript is a spec that comments a reply, then runs one of the user's scripts.
func withScript(t *testing.T) spec.Spec {
	t.Helper()

	s := loadTemplate(t, "ask-for-a-repro-on-quiet-bugs")
	s.Steps = append(s.Steps, spec.Step{Script: &spec.ScriptStep{ID: 4, Name: "Fix all"}})

	return s
}

func TestAVersionOneSpecLoadsAsVersionTwo(t *testing.T) {
	t.Parallel()

	data := templateJSON(t, "ask-for-a-repro-on-quiet-bugs")
	require.Contains(t, string(data), `"version": 1`)

	s, err := spec.Load(data)
	require.NoError(t, err)
	assert.Equal(t, 2, s.Version)
	assert.Equal(t, 2, spec.Normalize(spec.Spec{Version: 1}).Version, "Normalize brings it forward too")

	again, err := spec.Encode(s)
	require.NoError(t, err)

	back, err := spec.Load(again)
	require.NoError(t, err)
	assert.Equal(t, s, back, "it stores as version 2 and loads back the same")
}

func TestAVersionAfterTwoIsANewerBuilds(t *testing.T) {
	t.Parallel()

	data := strings.Replace(string(templateJSON(t, "ask-for-a-repro-on-quiet-bugs")), `"version": 1`, `"version": 3`, 1)

	_, err := spec.Load([]byte(data))
	require.ErrorIs(t, err, spec.ErrNewer)
}

func TestUUIDsLoadNormalizedAndRoundTrip(t *testing.T) {
	t.Parallel()

	s := withScript(t)
	s.Steps[0].GitHub[0].Reply.Prompt.UUID = "  " + strings.ToUpper(promptUUID) + " "
	s.Steps[1].Script.UUID = strings.ToUpper(scriptUUID)

	data, err := spec.Encode(spec.Normalize(s))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"prompt":{"uuid":"`+promptUUID+`","starter":"reply-needs-repro"}`)
	assert.Contains(t, string(data), `"script":{"uuid":"`+scriptUUID+`","id":4,"name":"Fix all"}`)

	loaded, err := spec.Load(data)
	require.NoError(t, err)
	assert.Equal(t, promptUUID, loaded.Steps[0].GitHub[0].Reply.Prompt.UUID)
	assert.Equal(t, scriptUUID, loaded.Steps[1].Script.UUID)
}

func TestValidateHoldsUUIDsToTheirForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		edit  func(*spec.Spec)
		field string
	}{
		{
			name:  "a prompt's UUID that isn't one",
			edit:  func(s *spec.Spec) { s.Steps[0].GitHub[0].Reply.Prompt.UUID = "not-a-uuid" },
			field: "[0].github[0].reply.prompt.uuid",
		},
		{
			name: "the Nil UUID",
			edit: func(s *spec.Spec) {
				s.Steps[0].GitHub[0].Reply.Prompt.UUID = "00000000-0000-0000-0000-000000000000"
			},
			field: "[0].github[0].reply.prompt.uuid",
		},
		{
			name: "a UUID with no fallback",
			edit: func(s *spec.Spec) {
				s.Steps[0].GitHub[0].Reply.Prompt = spec.PromptRef{UUID: promptUUID}
			},
			field: "[0].github[0].reply.prompt",
		},
		{
			name: "a UUID beside both fallbacks",
			edit: func(s *spec.Spec) {
				s.Steps[0].GitHub[0].Reply.Prompt = spec.PromptRef{UUID: promptUUID, Starter: "reply-needs-repro", Nickname: "Repro"}
			},
			field: "[0].github[0].reply.prompt",
		},
		{
			name: "a UUID on the item's own default",
			edit: func(s *spec.Spec) {
				s.Steps = []spec.Step{{Agent: &spec.AgentStep{Prompt: spec.PromptRef{UUID: promptUUID, ItemDefault: true}}}}
			},
			field: "[0].agent.prompt.uuid",
		},
		{
			name:  "a script's UUID that isn't one",
			edit:  func(s *spec.Spec) { s.Steps[1].Script.UUID = "6f9619ff8b864d01b42d00cf4fc964ff" },
			field: "[1].script.uuid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := withScript(t)
			tt.edit(&s)

			var v *spec.ValidationError

			require.ErrorAs(t, spec.Validate(s), &v, "refused")

			fields := make([]string, 0, len(v.Problems))
			for _, p := range v.Problems {
				fields = append(fields, p.Field)
			}

			assert.Contains(t, fields, tt.field)
		})
	}

	s := withScript(t)
	s.Steps[0].GitHub[0].Reply.Prompt.UUID, s.Steps[1].Script.UUID = promptUUID, scriptUUID
	require.NoError(t, spec.Validate(s), "a canonical UUID beside its fallback")
}

func TestCloneAndWithoutUUIDsLeaveTheSpecAsItWas(t *testing.T) {
	t.Parallel()

	s := withScript(t)
	s.Steps[0].GitHub[0].Reply.Prompt.UUID, s.Steps[1].Script.UUID = promptUUID, scriptUUID

	bare := spec.WithoutUUIDs(s)
	assert.Empty(t, bare.Steps[0].GitHub[0].Reply.Prompt.UUID)
	assert.Empty(t, bare.Steps[1].Script.UUID)
	assert.Equal(t, "reply-needs-repro", bare.Steps[0].GitHub[0].Reply.Prompt.Starter, "the fallback stays")
	assert.Equal(t, promptUUID, s.Steps[0].GitHub[0].Reply.Prompt.UUID, "the spec it copied keeps its UUIDs")
	assert.Equal(t, scriptUUID, s.Steps[1].Script.UUID)

	c := spec.Clone(s)
	for _, ref := range spec.PromptRefs(&c) {
		ref.Nickname = "changed"
	}

	for _, sc := range spec.ScriptSteps(&c) {
		sc.Name = "changed"
	}

	assert.Empty(t, s.Steps[0].GitHub[0].Reply.Prompt.Nickname)
	assert.Equal(t, "Fix all", s.Steps[1].Script.Name)
	assert.Len(t, spec.PromptRefs(&c), 1)
	assert.Len(t, spec.ScriptSteps(&c), 1)
}
