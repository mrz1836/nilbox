package core

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/prompts"
)

// triageNick is the nickname of the user's own prompt the routines run.
const triageNick = "Triage"

// TestRoutinesTravelWithTheirPromptsThroughTheLibrary exports a routine that runs a prompt
// of the user's own, with its text, and imports it into another Mac's app: the prompt
// arrives as a new prompt, the routine arrives off and names it, and a prompt the second
// Mac already holds by nickname keeps its own copy.
func TestRoutinesTravelWithTheirPromptsThroughTheLibrary(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	from := bootstrap(t, testOptions(t, ".claude"))

	mine, err := from.Prompts.Create(ctx, prompts.Input{
		Name: "Triage the checks", Nickname: triageNick, AppliesTo: prompts.AppliesToAny, Body: "Triage {{.URL}}",
	})
	require.NoError(t, err)

	saved, err := from.Autopilot.Save(ctx, autopilot.Draft{Name: "Triage failing checks", Spec: triageSpec(mine.Nickname)})
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "routines.json")
	exported, err := from.Autopilot.Export(ctx, path, []int64{saved.ID}, true)
	require.NoError(t, err)
	assert.Equal(t, autopilot.Exported{Routines: 1, Prompts: 1}, exported)

	to := bootstrap(t, testOptions(t, ".claude"))

	preview, err := to.Autopilot.PreviewImport(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, []autopilot.PromptArrival{{Nickname: triageNick, Arrives: autopilot.ArrivesNew}}, preview.Prompts)
	assert.Empty(t, preview.Missing, "the prompt it names arrives with it")

	imported, err := to.Autopilot.Import(ctx, autopilot.ImportRequest{Path: path, SHA256: preview.SHA256})
	require.NoError(t, err)
	assert.Equal(t, []string{triageNick}, imported.Prompts)
	assert.Equal(t, []string{"Triage failing checks"}, imported.Routines)

	lib := libraryByNickname(t, to)
	require.Contains(t, lib, triageNick)
	assert.Equal(t, "Triage {{.URL}}", lib[triageNick].Body)

	routines, err := to.Autopilot.List(ctx)
	require.NoError(t, err)
	require.Len(t, routines, 1)
	assert.False(t, routines[0].Enabled)
	assert.Equal(t, mine.UUID, lib[triageNick].UUID, "the prompt keeps its UUID on the other Mac")
	assert.Equal(t, spec.PromptRef{UUID: mine.UUID, Nickname: triageNick}, routines[0].Spec.Steps[0].Agent.Prompt,
		"the routine names it by that UUID")

	// A Mac that has its own Triage keeps it, and says the two differ.
	other := bootstrap(t, testOptions(t, ".claude"))
	_, err = other.Prompts.Create(ctx, prompts.Input{
		Name: "My triage", Nickname: triageNick, AppliesTo: prompts.AppliesToAny, Body: "My way {{.URL}}",
	})
	require.NoError(t, err)

	preview, err = other.Autopilot.PreviewImport(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, []autopilot.PromptArrival{{Nickname: triageNick, Arrives: autopilot.ArrivesDiffers}}, preview.Prompts)

	imported, err = other.Autopilot.Import(ctx, autopilot.ImportRequest{Path: path, SHA256: preview.SHA256})
	require.NoError(t, err)
	assert.Empty(t, imported.Prompts)
	myTriage := libraryByNickname(t, other)[triageNick]
	assert.Equal(t, "My way {{.URL}}", myTriage.Body, "the copy on this Mac stays")

	mapped, err := other.Autopilot.List(ctx)
	require.NoError(t, err)
	require.Len(t, mapped, 1)
	assert.Equal(t, spec.PromptRef{UUID: myTriage.UUID, Nickname: triageNick}, mapped[0].Spec.Steps[0].Agent.Prompt,
		"a UUID this Mac doesn't know falls back to the nickname, and the routine names the prompt here by its own")
}

// filledTemplate returns t's spec with its blanks filled as quick setup fills them: your
// account and an org for a where, a check's name for a list of check names.
func filledTemplate(t autopilot.Template) spec.Spec {
	sp := t.Spec

	for _, b := range t.Blanks {
		switch b.Kind {
		case autopilot.BlankWhere:
			sp.Watch.Sources[b.Source].Search.Where = []string{"@me", "org:acme-org"}
		case autopilot.BlankChecks:
			sp.Conditions.Of[b.Condition].Values = []string{"e2e / browser"}
		}
	}

	return sp
}

// TestEveryShippedTemplateSavesCleanly saves every template of the gallery, its blanks
// filled, through Describe and Save over the real prompt library: one this build runs saves
// with no problem in any mode, and one whose steps this build can't run yet is held back by
// those alone, in plain words, never by a prompt, a kind, or anything else, so it simply
// saves once its steps land.
func TestEveryShippedTemplateSavesCleanly(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	c := bootstrap(t, testOptions(t, ".claude"))

	templates, err := c.Autopilot.Templates(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, templates)

	for _, tmpl := range templates {
		d := autopilot.Draft{Name: tmpl.Name, Spec: filledTemplate(tmpl), TemplateKey: tmpl.Key}

		described, err := c.Autopilot.Describe(ctx, d)
		require.NoError(t, err, tmpl.Key)

		for _, p := range described.Problems {
			assert.NotEmpty(t, tmpl.Needs, "%s: %s at %s, though it needs nothing", tmpl.Key, p.Message, p.Field)
			assert.True(t, strings.HasSuffix(p.Field, ".agent.where"),
				"%s: only where its agent runs may hold it back: %s at %s", tmpl.Key, p.Message, p.Field)
		}

		if len(tmpl.Needs) > 0 {
			// A step a later part runs holds it in Dry run until it lands, and says so; it
			// still saves, and describes with nothing else wrong.
			if len(described.Problems) == 0 {
				assert.NotEmpty(t, described.DryRunOnly, "%s needs %v", tmpl.Key, tmpl.Needs)

				saved, saveErr := c.Autopilot.Save(ctx, d)
				require.NoError(t, saveErr, "%s saves, in Dry run until its steps land", tmpl.Key)
				assert.Equal(t, autopilotprefs.ModeDryRun, saved.Mode, tmpl.Key)
			}

			continue
		}

		assert.Empty(t, described.DryRunOnly, "%s runs here in any mode", tmpl.Key)
		assert.NotEmpty(t, described.Sentence, tmpl.Key)

		for _, mode := range []autopilotprefs.Mode{autopilotprefs.ModeAsk, autopilotprefs.ModeAuto} {
			saved, saveErr := c.Autopilot.Save(ctx, autopilot.Draft{
				Name: tmpl.Name + " " + string(mode), Spec: d.Spec, TemplateKey: tmpl.Key, Mode: mode,
			})
			require.NoError(t, saveErr, "%s saves in %s", tmpl.Key, mode)
			assert.Equal(t, mode, saved.Mode, tmpl.Key)
			assert.Equal(t, tmpl.Key, saved.TemplateKey)
		}
	}
}

// TestTheGalleryReadsTheLibraryAndTheStoredItems checks the gallery as core builds it: the
// templates' starters, which every new library holds, and no suggestion from a Mac that
// knows nothing yet.
func TestTheGalleryReadsTheLibraryAndTheStoredItems(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))

	got, err := c.Autopilot.Templates(t.Context())
	require.NoError(t, err)
	require.Len(t, got, len(autopilot.Gallery()))

	for _, tmpl := range got {
		assert.Empty(t, tmpl.Suggested, "%s: nothing stored suggests it", tmpl.Key)

		for _, p := range tmpl.Prompts {
			assert.True(t, p.InLibrary, "%s: a new library holds %s", tmpl.Key, p.Starter)
		}
	}

	flagship := got[0]
	assert.Equal(t, "fix-dependabot-security", flagship.Key)
	assert.Equal(t, []autopilot.TemplatePrompt{{Starter: prompts.StarterFixDependencySecurity, Name: "Fix dep security", InLibrary: true}},
		flagship.Prompts, "the starter named by its copy's nickname")
}
