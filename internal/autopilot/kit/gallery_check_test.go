package kit_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/prompts"
)

// gallerySnapshot is the snapshot of a fresh setup, as the app writes one: no routine,
// a library holding a copy of every starter prompt, Settings' defaults, and the whole
// vocabulary this build ships, its gallery, its starters, and its variables.
func gallerySnapshot() kit.Snapshot {
	var vocab kit.Vocabulary

	for _, g := range autopilot.Gallery() {
		blanks := make([]kit.Blank, 0, len(g.Blanks))
		for _, b := range g.Blanks {
			blanks = append(blanks, kit.Blank{
				Kind: string(b.Kind), Label: b.Label, Help: b.Help, Source: b.Source, Condition: b.Condition,
				Required: b.Required,
			})
		}

		vocab.Templates = append(vocab.Templates, kit.Template{
			Key: g.Key, Name: g.Name, Icon: g.Icon, Summary: g.Summary, Suggests: g.Suggests, Blanks: blanks, Spec: g.Spec,
		})
	}

	library := make([]kit.Prompt, 0, len(prompts.Starters()))

	for _, st := range prompts.Starters() {
		purpose := kit.Purpose(st.Purpose)
		if purpose == "" {
			purpose = kit.PurposeAgent
		}

		vocab.Starters = append(vocab.Starters, kit.Starter{
			Key: st.Key, Name: st.Name, Nickname: st.Nickname, AppliesTo: kit.AppliesTo(st.AppliesTo), Purpose: purpose,
			ReplyAction: kit.ReplyAction(st.ReplyAction),
		})
		library = append(library, kit.Prompt{
			Starter: st.Key, Nickname: st.Nickname, Name: st.Name, Purpose: purpose,
			ReplyAction: kit.ReplyAction(st.ReplyAction), AppliesTo: kit.AppliesTo(st.AppliesTo),
		})
	}

	for _, v := range prompts.Variables() {
		vocab.Variables = append(vocab.Variables, kit.Variable{
			Name: v.Name, Group: v.Group, Type: v.Type, Description: v.Description, PROnly: v.PROnly,
		})
	}

	return kit.NewSnapshot(kit.Setup{Prompts: library, Settings: autopilotprefs.Defaults()}, kit.InboxNames{}, vocab,
		time.Date(2026, time.November, 20, 5, 0, 0, 0, time.UTC))
}

// filledTemplate returns t's spec with its blanks filled as quick setup fills them: a
// search's Where with the user's account, and a condition's values with a check's name.
func filledTemplate(t kit.Template) spec.Spec {
	data, err := json.Marshal(t.Spec)
	if err != nil {
		panic(err)
	}

	var sp spec.Spec
	if err = json.Unmarshal(data, &sp); err != nil {
		panic(err)
	}

	for _, b := range t.Blanks {
		switch autopilot.BlankKind(b.Kind) {
		case autopilot.BlankWhere:
			sp.Watch.Sources[b.Source].Search.Where = []string{"@me"}
		case autopilot.BlankChecks:
			sp.Conditions.Of[b.Condition].Values = []string{"CI / flaky"}
		}
	}

	return sp
}

func TestEveryGalleryTemplateChecksClean(t *testing.T) {
	t.Parallel()

	snap := gallerySnapshot()
	require.NotEmpty(t, snap.Vocabulary.Templates)

	for _, tmpl := range snap.Vocabulary.Templates {
		t.Run(tmpl.Key, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(kit.Proposal{
				Summary: "Set up " + tmpl.Name + ".",
				Routines: []kit.RoutineChange{{
					Name: tmpl.Name, Template: tmpl.Key, Mode: tmpl.Suggests, Spec: filledTemplate(tmpl),
				}},
			})
			require.NoError(t, err)

			p, err := kit.DecodeProposal(data)
			require.NoError(t, err, "it reads as the tool mode reads a proposal")

			c := kit.Check(t.Context(), snap, p)
			require.Len(t, c.Routines, 1)

			r := c.Routines[0]
			assert.Empty(t, r.Problems, "a template, its blanks filled, is a routine that checks clean")
			assert.Zero(t, c.Problems())
			assert.NotEmpty(t, r.Sentence)
			assert.Equal(t, tmpl.Name, r.Name)
			assert.False(t, r.NameTaken)
			assert.Empty(t, r.DryRunOnly, "this build runs every step a template takes")
		})
	}
}

// TestNoGalleryTemplateMergesPastTheRules is the guard: a template never offers to merge
// past the rules, which only the user asks for, routine by routine.
func TestNoGalleryTemplateMergesPastTheRules(t *testing.T) {
	t.Parallel()

	templates := autopilot.Gallery()
	require.NotEmpty(t, templates)

	for _, g := range templates {
		data, err := json.Marshal(g.Spec)
		require.NoError(t, err)

		var sp spec.Spec
		require.NoError(t, json.Unmarshal(data, &sp))

		assert.False(t, sp.PastRules(), "%s merges past the rules", g.Key)
		assert.NotContains(t, string(data), "allowBypass", g.Key)
		assert.NotContains(t, string(data), "allowSensitive", g.Key)
	}
}
