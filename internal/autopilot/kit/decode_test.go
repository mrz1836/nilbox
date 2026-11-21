package kit_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// proposalJSON is a proposal holding one of everything: a prompt, a routine that runs
// it, a settings change, and a rules change.
const proposalJSON = `{
  "summary": "  Fix my failing checks with a prompt of their own, and cap the day at $5.  ",
  "prompts": [{"name": "Fix it", "nickname": "Fix it", "purpose": "agent", "appliesTo": "pr",
    "body": "Fix the checks on {{.URL}} <and> & say how."}],
  "routines": [{"name": "Fix my checks", "description": "", "mode": "auto", "template": "fix-failing-checks-on-my-prs",
    "spec": {"version": 1, "watch": {"sources": [{"lists": ["authored"]}]}, "trigger": {"event": "checks_failed"},
      "conditions": {"match": "all", "of": []},
      "steps": [{"agent": {"prompt": {"nickname": "Fix it"}, "fence": {"allow": [], "budget": {"usd": 4.5}}}}],
      "limits": {}, "requires": []}}],
  "settings": {"dailyCap": {"amount": 5, "unit": "usd"}},
  "rules": {"trust": ["renovate[bot]"], "mute": []}
}`

func TestAProposalIsReadAsItEncodesAndReadsBack(t *testing.T) {
	t.Parallel()

	p, err := kit.DecodeProposal([]byte(proposalJSON))
	require.NoError(t, err)

	assert.Equal(t, "Fix my failing checks with a prompt of their own, and cap the day at $5.", p.Summary, "trimmed")
	require.Len(t, p.Prompts, 1)
	assert.Equal(t, "Fix the checks on {{.URL}} <and> & say how.", p.Prompts[0].Body)
	require.Len(t, p.Routines, 1)
	assert.Equal(t, autopilotprefs.ModeAuto, p.Routines[0].Mode)
	assert.Nil(t, p.Routines[0].Spec.Requires, "an empty list encoding leaves out is read as none")
	assert.Nil(t, p.Routines[0].Spec.Steps[0].Agent.Fence.Allow)
	assert.Equal(t, new(4.5), p.Routines[0].Spec.Steps[0].Agent.Fence.Budget.USD)
	assert.Equal(t, &autopilotprefs.DailyCap{Amount: 5, Unit: autopilotprefs.UnitUSD}, p.Settings.DailyCap)
	assert.Equal(t, []string{"renovate[bot]"}, p.Rules.Trust)
	assert.Nil(t, p.Rules.Mute)

	data, err := json.Marshal(p)
	require.NoError(t, err)

	again, err := kit.DecodeProposal(data)
	require.NoError(t, err)
	assert.Equal(t, p, again, "it reads back as itself")
}

func TestCheckRefusesEnabledAndPaused(t *testing.T) {
	t.Parallel()

	for key, want := range map[string]string{
		"enabled": "the proposal can't be read: settings.enabled: Autopilot's switch is the user's alone, so a " +
			"proposal never names it: leave it out",
		"paused": "the proposal can't be read: settings.paused: Pause is the user's alone, so a proposal never " +
			"names it: leave it out",
		"mergePastRules": "the proposal can't be read: settings.mergePastRules: Let routines merge your own pull " +
			"requests past the rules is the user's alone, so a proposal never names it: leave it out",
	} {
		for _, value := range []string{"true", "false"} {
			data := fmt.Sprintf(`{"summary": "Turn it %s.", "settings": {"agentsAtOnce": 1, %q: %s}}`, key, key, value)

			_, err := kit.DecodeProposal([]byte(data))
			require.ErrorIs(t, err, kit.ErrUnreadableProposal, data)
			require.EqualError(t, err, want, data)
		}
	}

	_, err := kit.DecodeProposal([]byte(`{"summary": "Turn it on.", "settings": {"Enabled": true}}`))
	require.ErrorIs(t, err, kit.ErrUnreadableProposal, "a key spelled otherwise is refused all the same")
	require.ErrorContains(t, err, `a key isn't one the format has, spelled exactly: "Enabled"`)

	for _, field := range []string{"enabled", "paused", "mergePastRules", "astRules"} {
		data, err := json.Marshal(kit.SettingsChange{AgentsAtOnce: new(1)})
		require.NoError(t, err)
		assert.NotContains(t, string(data), field, "the settings change has no field for it")
	}
}

func TestAProposalIsReadStrictlyAndWithinItsCaps(t *testing.T) {
	t.Parallel()

	routine := func(sp string) string {
		return `{"summary": "One routine.", "routines": [{"name": "R", "spec": ` + sp + `}]}`
	}
	many := func(key, item string, n int) string {
		return `{"summary": "Many.", "` + key + `": [` + strings.TrimSuffix(strings.Repeat(item+",", n), ",") + `]}`
	}
	prompt := `{"name": "P", "nickname": "P", "purpose": "agent", "appliesTo": "pr", "body": "Do it."}`

	for data, want := range map[string]string{
		`{"summary": "Nothing."}`: "it proposes nothing: add a prompt, a routine, a settings change, or a rules change",
		`{"summary": "", "settings": {"agentsAtOnce": 1}}`: "summary: say what it does in a sentence or two, as the " +
			"user reads it on the card",
		`{"summary": "Two\nlines.", "settings": {"agentsAtOnce": 1}}`:        "summary: keep it to one line of plain text",
		`{"summary": "Hidden\u202e mark.", "settings": {"agentsAtOnce": 1}}`: "summary: keep it to one line of plain text",
		`{"summary": "` + strings.Repeat("a", kit.MaxSummaryRunes+1) + `", "settings": {"agentsAtOnce": 1}}`: "summary: " +
			"keep it to 400 characters",
		`{"summary": "No settings.", "settings": {}}`: "settings: name at least one setting it changes, or leave " +
			"settings out",
		`{"summary": "No rules.", "rules": {"trust": [], "pin": []}}`: "rules: name at least one change, or leave rules out",
		`{"summary": "Too many.", "rules": {"mute": [` + strings.TrimSuffix(strings.Repeat(`"a",`, kit.MaxRuleChanges+1),
			",") + `]}}`: "rules.mute: at most 100 in one proposal",
		many("routines", `{"name": "R", "spec": {}}`, kit.MaxRoutines+1): "routines: at most 10 in one proposal; " +
			"split the rest into proposals of their own",
		many("prompts", prompt, kit.MaxPrompts+1): "prompts: at most 10 in one proposal; split the rest into " +
			"proposals of their own",
		`{"summary": "Long.", "prompts": [{"name": "P", "nickname": "P", "purpose": "agent", "appliesTo": "pr", "body": "` +
			strings.Repeat("a", kit.MaxPromptBody+1) + `"}]}`: "prompts[0].body: keep a prompt's text to 32768 bytes",
		routine(`{"version": 1, "requires": ["` + strings.Repeat("a", spec.MaxBytes) + `"]}`): "routines[0].spec: a " +
			"routine is at most 65536 bytes once encoded",
		routine(`{"Version": 1}`): `a key isn't one the format has, spelled exactly: "Version"`,
		routine(`{"version": 1, "trigger": {"event": "appears", "event": "manual"}}`): `a key is given twice: "event"`,
		routine(`{"version": "1"}`):                            "cannot unmarshal string",
		`{"summary": "x", "settings": {"agentsAtOnce": 1}} {}`: "more after the document",
		`{"summary": "x", "routines": [{"spec": {"watch": {"sources": [{"search": {"where": ` +
			strings.Repeat("[", 20) + strings.Repeat("]", 20) + `}}]}}}]}`: "it nests too deep",
		`not json`: "invalid character",
	} {
		_, err := kit.DecodeProposal([]byte(data))
		require.ErrorIs(t, err, kit.ErrUnreadableProposal, "%.80s", data)
		require.ErrorContains(t, err, want, "%.80s", data)
		assert.True(t, strings.HasPrefix(err.Error(), "the proposal can't be read: "), "%.80s", data)
	}

	_, err := kit.DecodeProposal([]byte(`{"summary": "` + strings.Repeat("a", kit.MaxProposalBytes) + `"}`))
	require.ErrorIs(t, err, kit.ErrUnreadableProposal)
	require.ErrorContains(t, err, "larger than 4194304 bytes")
}

func TestAProposalHoldsASpecAsDeepAsASpecMayNest(t *testing.T) {
	t.Parallel()

	// nested is a spec whose search's where holds arrays nested depth deep: one past what
	// a spec may nest is too deep for a spec alone, and inside a proposal too.
	nested := func(depth int) string {
		return `{"version": 1, "watch": {"sources": [{"search": {"where": ` + strings.Repeat("[", depth) +
			strings.Repeat("]", depth) + `}}]}}`
	}

	for depth, tooDeep := range map[int]bool{11: false, 12: true} {
		_, specErr := spec.Decode([]byte(nested(depth)))
		_, err := kit.DecodeProposal([]byte(`{"summary": "Deep.", "routines": [{"name": "R", "spec": ` + nested(depth) + `}]}`))

		require.Error(t, specErr)
		require.Error(t, err)
		assert.Equal(t, tooDeep, strings.Contains(specErr.Error(), "it nests too deep"), "a spec %d deep alone", depth)
		assert.Equal(t, tooDeep, strings.Contains(err.Error(), "it nests too deep"), "a spec %d deep in a proposal", depth)
	}
}

func FuzzDecodeProposal(f *testing.F) {
	for _, seed := range []string{
		proposalJSON,
		`{"summary": "Turn it on.", "settings": {"enabled": true}}`,
		`{"summary": "x", "settings": {"historyDays": 7, "logDays": 7}}`,
		`{"summary": "x", "rules": {"trust": ["app/renovate"], "unignoreWorkflows": ["CI / flaky"]}}`,
		`{"summary": "x", "prompts": [{"name": "", "nickname": "", "purpose": "", "appliesTo": "", "body": ""}]}`,
		`{"summary": "x", "routines": [{"id": 7, "base": "2026-11-20T05:31:03Z", "name": "N", "spec": {"steps": [{"github": []}]}}]}`,
		`{"summary": "x", "routines": [null], "prompts": [null]}`,
		`{"summary": "\u2028", "rules": {}}`,
		`{"summary": "x", "routines": [{"name": "R", "spec": {"steps": [{"agent": {"prompt": {}, "fence": {"budget": {"usd": 1e-7}}}}]}}]}`,
		`[]`, `null`, `{}`, `{"summary": 1}`, `{"summary": "x"}{}`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := kit.DecodeProposal(data)
		if err != nil {
			require.ErrorIs(t, err, kit.ErrUnreadableProposal)

			return
		}

		again, err := json.Marshal(p)
		require.NoError(t, err)

		if len(again) > kit.MaxProposalBytes {
			return // its cap is the size rule's, not a difference
		}

		read, err := kit.DecodeProposal(again)
		require.NoError(t, err, "a proposal it accepts reads again: %s", again)
		require.Equal(t, p, read, "and reads back as itself")
		require.NotEmpty(t, read.Summary)
		require.LessOrEqual(t, len(read.Routines), kit.MaxRoutines)
		require.LessOrEqual(t, len(read.Prompts), kit.MaxPrompts)
	})
}
