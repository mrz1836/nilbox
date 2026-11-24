package draft_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
)

// UUIDs the records of a test's library and scripts have.
const (
	fixUUID     = "6f9619ff-8b86-4d01-b42d-00cf4fc964ff"
	triageUUID  = "5d2e8f41-7a63-4c90-9b18-e4f6a2c3d5b7"
	lintUUID    = "3a7f1c52-9d84-4e6b-8f21-6c0d5e9b4a13"
	staleUUID   = "0b1c2d3e-4f50-4a61-8b72-93a4b5c6d7e8"
	deployUUID  = "7c4e2a91-3b5d-4f68-a0c1-9d8e7f6b5a43"
	triageNick  = "Triage"
	questionKey = "reply-question"
)

// uuidLibrary is library with a UUID for each prompt that a test names by one.
func uuidLibrary() []draft.Prompt {
	out := library()
	out[0].UUID, out[1].UUID = fixUUID, triageUUID
	out[4].UUID = staleUUID[:35] + "f" // the question reply's own

	return out
}

// myScripts are the user's scripts: Lint, ID 7, and Deploy, ID 9.
func myScripts() []draft.Script {
	return []draft.Script{{UUID: lintUUID, ID: 7, Name: "Lint"}, {UUID: deployUUID, ID: 9, Name: "Deploy"}}
}

func TestAPromptIsFoundByItsUUIDBeforeItsName(t *testing.T) {
	t.Parallel()

	lib := uuidLibrary()

	tests := []struct {
		name string
		ref  spec.PromptRef
		want int
	}{
		{"by its UUID, whatever it is called now", spec.PromptRef{UUID: triageUUID, Nickname: "Old name"}, 1},
		{"by its UUID, over a nickname another prompt has", spec.PromptRef{UUID: triageUUID, Nickname: "Fix checks"}, 1},
		{"by its UUID, over a starter another prompt is", spec.PromptRef{UUID: fixUUID, Starter: questionKey}, 0},
		{"a UUID no prompt has falls back to the nickname", spec.PromptRef{UUID: staleUUID, Nickname: "triage"}, 1},
		{"a UUID no prompt has falls back to the starter", spec.PromptRef{UUID: staleUUID, Starter: questionKey}, 4},
		{"a nickname alone, ignoring case", spec.PromptRef{Nickname: "FIX CHECKS"}, 0},
		{"nothing it names", spec.PromptRef{UUID: staleUUID, Nickname: "Gone"}, -1},
		{"the item's own default names none", spec.PromptRef{ItemDefault: true}, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, draft.PromptIndex(lib, tt.ref))

			p, ok := draft.FindPrompt(lib, tt.ref)
			assert.Equal(t, tt.want >= 0, ok)

			if ok {
				assert.Equal(t, lib[tt.want], p)
			}
		})
	}
}

func TestAScriptIsFoundByItsUUIDBeforeItsID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		step    spec.ScriptStep
		at      int
		renamed bool
	}{
		{"by its UUID, whatever its ID and name now", spec.ScriptStep{UUID: lintUUID, ID: 3, Name: "Old lint"}, 0, false},
		{"by its UUID, over an ID another script has", spec.ScriptStep{UUID: deployUUID, ID: 7, Name: "Lint"}, 1, false},
		{"by its ID, under its name, with no UUID", spec.ScriptStep{ID: 7, Name: "Lint"}, 0, false},
		{"by its ID, renamed since, with no UUID", spec.ScriptStep{ID: 7, Name: "Format"}, 0, true},
		{"by its ID when no script has its UUID, renamed", spec.ScriptStep{UUID: staleUUID, ID: 9, Name: "Ship"}, 1, true},
		{"gone", spec.ScriptStep{UUID: staleUUID, ID: 4, Name: "Gone"}, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			at, renamed := draft.FindScript(myScripts(), tt.step)
			assert.Equal(t, tt.at, at)
			assert.Equal(t, tt.renamed, renamed)
		})
	}
}

func TestAScriptRenamedUnderItsUUIDIsNoProblem(t *testing.T) {
	t.Parallel()

	sp := lintSpec()
	sp.Steps[1].Script = &spec.ScriptStep{UUID: lintUUID, ID: 3, Name: "Old lint"}

	s := &setup{library: library(), scripts: myScripts()}
	assert.Empty(t, draft.Problems(t.Context(), sp, s, draft.CatalogOffers()), "found by its UUID, it runs")

	sp.Steps[1].Script = &spec.ScriptStep{ID: 7, Name: "Old lint"}
	assert.Equal(t, []spec.Problem{{
		Part: spec.PartSteps, Field: "[1].script.id", Message: draft.RenamedWords("Old lint", "Lint"),
	}}, draft.Problems(t.Context(), sp, s, draft.CatalogOffers()), "found by its ID alone, it waits to be picked again")
}

func TestStampNamesEachRecordByItsUUIDAndItsNameNow(t *testing.T) {
	t.Parallel()

	sp := agentSpec(spec.PromptRef{Nickname: "triage"}, catalog.WhereBackground)
	sp.Steps = append(sp.Steps,
		replyStep(spec.PromptRef{Starter: questionKey}),
		spec.Step{Script: &spec.ScriptStep{UUID: lintUUID, ID: 3, Name: "Old lint"}},
		spec.Step{Script: &spec.ScriptStep{ID: 9, Name: "Ship"}},
		agentStep(spec.PromptRef{ItemDefault: true}),
		agentStep(spec.PromptRef{Nickname: "Gone"}),
	)

	lib := uuidLibrary()
	require.True(t, draft.Stamp(lib, myScripts(), &sp))

	assert.Equal(t, spec.PromptRef{UUID: triageUUID, Nickname: triageNick}, sp.Steps[0].Agent.Prompt,
		"a nickname takes the prompt's UUID and its nickname as the library spells it")
	assert.Equal(t, spec.PromptRef{UUID: lib[4].UUID, Starter: questionKey}, sp.Steps[1].GitHub[0].Reply.Prompt,
		"a starter's key stays beside the UUID of its copy")
	assert.Equal(t, spec.ScriptStep{UUID: lintUUID, ID: 7, Name: "Lint"}, *sp.Steps[2].Script,
		"a script found by its UUID takes its ID and name now")
	assert.Equal(t, spec.ScriptStep{ID: 9, Name: "Ship"}, *sp.Steps[3].Script,
		"one that waits to be picked again is left as it is")
	assert.Equal(t, spec.PromptRef{ItemDefault: true}, sp.Steps[4].Agent.Prompt, "the item's own default names none")
	assert.Equal(t, spec.PromptRef{Nickname: "Gone"}, sp.Steps[5].Agent.Prompt, "nothing resolves, nothing changes")

	assert.False(t, draft.Stamp(lib, myScripts(), &sp), "stamped, it changes no more")
}

func TestStampFollowsARenameAndAResetByUUID(t *testing.T) {
	t.Parallel()

	sp := agentSpec(spec.PromptRef{UUID: triageUUID, Nickname: triageNick}, catalog.WhereBackground)
	lib := uuidLibrary()

	lib[1].Nickname = "sort-it"
	require.True(t, draft.Stamp(lib, nil, &sp))
	assert.Equal(t, spec.PromptRef{UUID: triageUUID, Nickname: "sort-it"}, sp.Steps[0].Agent.Prompt, "renamed")

	// Another prompt took the old nickname: the UUID still names the one it was.
	lib = append(lib, draft.Prompt{UUID: staleUUID, Nickname: triageNick, Kinds: item.Kinds()})
	assert.False(t, draft.Stamp(lib, nil, &sp))
	assert.Equal(t, "sort-it", sp.Steps[0].Agent.Prompt.Nickname)

	// A starter ref whose UUID now names a prompt that is no copy of the starter names it by
	// its nickname.
	starter := agentSpec(spec.PromptRef{UUID: triageUUID, Starter: lib[0].Starter}, catalog.WhereBackground)
	require.True(t, draft.Stamp(lib, nil, &starter))
	assert.Equal(t, spec.PromptRef{UUID: triageUUID, Nickname: "sort-it"}, starter.Steps[0].Agent.Prompt)
}

func TestStampedKeepsTheSpecItWasHandedAndItsSize(t *testing.T) {
	t.Parallel()

	sp := agentSpec(spec.PromptRef{Nickname: triageNick}, catalog.WhereBackground)
	before := spec.Clone(sp)

	stamped, data, ok := draft.Stamped(uuidLibrary(), nil, sp)
	require.True(t, ok)
	assert.Equal(t, before, sp, "the spec handed in is left as it was")
	assert.Equal(t, triageUUID, stamped.Steps[0].Agent.Prompt.UUID)
	assert.Contains(t, string(data), `"prompt":{"uuid":"`+triageUUID+`","nickname":"Triage"}`)
	assert.Equal(t, spec.Version, stamped.Version)

	_, none, ok := draft.Stamped(uuidLibrary(), nil, stamped)
	assert.False(t, ok, "nothing more to stamp")
	assert.Nil(t, none)

	// A spec that stamping would grow past the size a spec may be keeps its text.
	big := fullSpec(t, 20)

	data, err := spec.Encode(spec.Normalize(big))
	require.NoError(t, err)
	require.Less(t, len(data), spec.MaxBytes, "it fits before")

	_, err = spec.Load(data)
	require.NoError(t, err, "and loads")

	kept, none, ok := draft.Stamped(uuidLibrary(), nil, big)
	assert.False(t, ok, "it wouldn't after")
	assert.Nil(t, none)
	assert.Empty(t, kept.Steps[0].Agent.Prompt.UUID)
}

// fullSpec is a routine that runs Triage, by its nickname alone, with conditions long
// enough to leave room bytes, and no more, below the size a spec may be.
func fullSpec(t *testing.T, room int) spec.Spec {
	t.Helper()

	sp := agentSpec(spec.PromptRef{Nickname: triageNick}, catalog.WhereBackground)
	sp.Conditions.Match = spec.MatchAll

	size := func() int {
		data, err := spec.Encode(spec.Normalize(sp))
		require.NoError(t, err)

		return len(data)
	}
	add := func(i, n int) {
		value := fmt.Sprintf("%05d-%s", i, strings.Repeat("a", n))

		last := len(sp.Conditions.Of) - 1
		if last < 0 || len(sp.Conditions.Of[last].Values) == spec.MaxValues {
			sp.Conditions.Of = append(sp.Conditions.Of, spec.Clause{Field: "checks.failedNames", Op: "anyContains"})
			last++
		}

		sp.Conditions.Of[last].Values = append(sp.Conditions.Of[last].Values, value)
	}

	i := 0
	for ; size()+400 < spec.MaxBytes; i++ {
		add(i, 180)
	}

	// Each value takes its text, its quotes, and a comma; a new condition takes more, which
	// the last few values never start.
	for need := spec.MaxBytes - room - size(); need > 0; need = spec.MaxBytes - room - size() {
		add(i, min(max(need-9, 1), 180))
		i++
	}

	require.Equal(t, spec.MaxBytes-room, size(), "the spec leaves just the room asked for")

	return sp
}
