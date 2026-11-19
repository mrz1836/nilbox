package autopilot_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errCarry is the prompt library failing to carry a prompt's text, or this Mac's profiles
// failing to be read.
var errCarry = errors.New("the prompts can't be carried")

// codexID is Codex's agent ID, which the importing Mac lacks, importOrg the org a routine's
// launch rows name, and workProfile a profile of Claude Code's the importing Mac has.
const (
	codexID     = "codex"
	importOrg   = "acme-org"
	workProfile = "work"
)

// brokenTransfer is a prompt library whose every carry of a prompt's text fails.
type brokenTransfer struct{}

func (brokenTransfer) ExportPrompts(context.Context, []string) (json.RawMessage, int, error) {
	return nil, 0, errCarry
}

func (brokenTransfer) PreviewPrompts(context.Context, json.RawMessage) ([]autopilot.PromptArrival, error) {
	return nil, errCarry
}

func (brokenTransfer) ImportPrompts(context.Context, json.RawMessage) ([]string, error) {
	return nil, errCarry
}

// brokenProfiles is this Mac's profiles, which can't be read.
type brokenProfiles struct{}

func (brokenProfiles) Profiles(context.Context) (map[string][]string, error) { return nil, errCarry }

// writeBundle writes the routines in the export format, with prompts when raw is set, and
// returns the file's path.
func writeBundle(t *testing.T, raw json.RawMessage, routines ...autopilot.BundleRoutine) string {
	t.Helper()

	data, err := autopilot.EncodeBundle(autopilot.Bundle{
		Format: autopilot.BundleFormat, ExportedAt: "2026-11-16T09:00:00Z", App: "v9", Routines: routines, Prompts: raw,
	})
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "routines.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	return path
}

// bundled is the routine named name over sp, as a bundle holds it.
func bundled(t *testing.T, name string, sp spec.Spec) autopilot.BundleRoutine {
	t.Helper()

	raw, err := spec.Encode(sp)
	require.NoError(t, err)

	return autopilot.BundleRoutine{Name: name, Mode: "ask", Spec: raw}
}

func TestExportSaysWhyItCouldntWriteTheRoutines(t *testing.T) {
	t.Parallel()

	r := easyRig(t, easyParts{transfer: brokenTransfer{}})
	mine := r.save(t, draft("Triage bugs", traveling()))
	dir := t.TempDir()
	ctx := t.Context()

	_, err := r.svc.Export(ctx, "routines.json", nil, false)
	require.ErrorIs(t, err, autopilot.ErrNoPath)
	assert.Equal(t, "Choose where to save the routines.", uierr.Message(err))

	_, err = r.svc.Export(ctx, filepath.Join(dir, "routines.json"), []int64{mine.ID, 999}, false)
	require.ErrorIs(t, err, autopilot.ErrNotFound, "a routine deleted since the window listed it")

	_, err = r.svc.Export(ctx, filepath.Join(dir, "routines.json"), nil, true)
	require.ErrorIs(t, err, errCarry)
	assert.Equal(t, "The prompts the routines run couldn't be read.", uierr.Message(err))

	_, err = r.svc.Export(ctx, filepath.Join(dir, "gone", "routines.json"), nil, false)
	require.Error(t, err)
	assert.Equal(t, "The routines couldn't be saved there.", uierr.Message(err))

	_, err = autopilot.NewService(autopilot.Config{Pool: testutil.ClosedDB(t)}).
		Export(ctx, filepath.Join(dir, "routines.json"), nil, false)
	require.Error(t, err, "a database that can't be read exports nothing")

	assert.NoFileExists(t, filepath.Join(dir, "routines.json"), "and no export wrote a file")
}

func TestAnExportCarriesEachPromptItsRoutinesNameOnce(t *testing.T) {
	t.Parallel()

	transfer := &fakeTransfer{}
	r := easyRig(t, easyParts{transfer: transfer})
	r.save(t, draft("Triage bugs", traveling()))

	again := traveling()
	again.Steps[0].Agent.Prompt.Nickname = "TRIAGE"
	r.save(t, draft("Triage again", again))

	path := filepath.Join(t.TempDir(), "routines.json")
	got, err := r.svc.Export(t.Context(), path, nil, true)
	require.NoError(t, err)
	assert.Equal(t, autopilot.Exported{Routines: 2, Prompts: 1}, got, "a nickname named twice, ignoring case, is one prompt")
}

func TestPreviewAndImportSayWhatTheyCouldntRead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	raw := json.RawMessage(`["Triage"]`)
	path := writeBundle(t, raw, bundled(t, "Triage bugs", traveling()))

	missing := filepath.Join(t.TempDir(), "missing.json")
	garbage := filepath.Join(t.TempDir(), "garbage.json")
	require.NoError(t, os.WriteFile(garbage, []byte("not a bundle"), 0o600))

	r := easyRig(t, easyParts{})

	_, err := r.svc.PreviewImport(ctx, missing)
	assert.Equal(t, "The file couldn't be read.", uierr.Message(err))

	_, err = r.svc.Import(ctx, autopilot.ImportRequest{Path: missing})
	assert.Equal(t, "The file couldn't be read.", uierr.Message(err))

	_, err = r.svc.PreviewImport(ctx, garbage)
	require.Error(t, err)
	assert.NotEmpty(t, uierr.Message(err), "a file that isn't routines says why, in a sentence")

	carried := easyRig(t, easyParts{transfer: brokenTransfer{}})

	_, err = carried.svc.PreviewImport(ctx, path)
	require.ErrorIs(t, err, errCarry)
	assert.Equal(t, "The prompts in the file couldn't be read.", uierr.Message(err))

	imported, err := carried.svc.Import(ctx, autopilot.ImportRequest{Path: path})
	require.ErrorIs(t, err, errCarry)
	assert.Equal(t, "The prompts in the file couldn't be added.", uierr.Message(err))
	assert.Equal(t, autopilot.Imported{Routines: []string{}, Prompts: []string{}}, imported)
	assert.Empty(t, carried.list(t), "no routine arrives without its prompts")

	profiled := easyRig(t, easyParts{profiles: brokenProfiles{}})

	_, err = profiled.svc.PreviewImport(ctx, path)
	require.ErrorIs(t, err, errCarry)
	assert.Equal(t, "The agents' profiles on this Mac couldn't be read.", uierr.Message(err))

	_, err = profiled.svc.Import(ctx, autopilot.ImportRequest{Path: path})
	require.ErrorIs(t, err, errCarry)
	assert.Empty(t, profiled.list(t))

	closed := autopilot.NewService(autopilot.Config{Pool: testutil.ClosedDB(t)})

	_, err = closed.PreviewImport(ctx, path)
	require.Error(t, err)

	_, err = closed.Import(ctx, autopilot.ImportRequest{Path: path})
	assert.Equal(t, "The routines couldn't be imported.", uierr.Message(err))
}

func TestAnImportThatCantStoreARoutineStoresNone(t *testing.T) {
	t.Parallel()

	r := easyRig(t, easyParts{})
	path := writeBundle(t, nil, bundled(t, "One", notifySpec()), bundled(t, "Two", notifySpec()))

	testutil.Exec(t, r.db, `CREATE TRIGGER no_two BEFORE INSERT ON autopilot_routines WHEN NEW.name = 'Two'
		BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)

	got, err := r.svc.Import(t.Context(), autopilot.ImportRequest{Path: path})
	require.Error(t, err)
	assert.Equal(t, "The routines couldn't be imported.", uierr.Message(err))
	assert.Equal(t, []string{}, got.Routines)
	assert.Empty(t, r.list(t), "one transaction: the first is taken back with the second")
}

func TestARoutineWhoseDefinitionIsntJSONNeitherArrivesNorIsPreviewed(t *testing.T) {
	t.Parallel()

	r := easyRig(t, easyParts{})
	path := writeBundle(t, nil, autopilot.BundleRoutine{Name: "Garbled", Mode: "ask", Spec: json.RawMessage(`"not a spec"`)},
		bundled(t, "One", notifySpec()))

	preview, err := r.svc.PreviewImport(t.Context(), path)
	require.NoError(t, err)
	require.Len(t, preview.Routines, 1)
	assert.Equal(t, "One", preview.Routines[0].Name)

	got, err := r.svc.Import(t.Context(), autopilot.ImportRequest{Path: path, SHA256: preview.SHA256})
	require.NoError(t, err)
	assert.Equal(t, []string{"One"}, got.Routines)
}

// launching is a routine whose agent inherits its agent with a profile no agent here has,
// runs a starter this Mac's library lacks, overrides its launch for three places and falls
// back on two profiles, then replies twice with replies the library lacks.
func launching() spec.Spec {
	s := notifySpec()
	s.Steps = []spec.Step{
		{Agent: &spec.AgentStep{
			Prompt: spec.PromptRef{Starter: "fix-checks"}, Where: catalog.WhereTerminal, Profile: "shared",
			LaunchFor: []spec.LaunchRow{
				{Target: importOrg, Agent: claudeID, Profile: "night"},
				{Target: importOrg + "/api", Agent: codexID, Profile: workProfile},
				{Target: importOrg + "/web", Profile: "shared"},
				{Target: importOrg + "/docs", Agent: claudeID},
			},
			Fallbacks: []spec.ProfileRef{{Agent: claudeID, Profile: "night"}, {Agent: codexID, Profile: workProfile}},
		}},
		{GitHub: []spec.GitHubAction{
			{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "Thanks"}}},
			{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "Cheers"}}},
		}},
	}

	return s
}

// fallingBack is a routine whose agent falls back on a profile of an agent this Mac lacks
// alone, running the same starter.
func fallingBack() spec.Spec {
	s := notifySpec()
	s.Steps = []spec.Step{{Agent: &spec.AgentStep{
		Agent: claudeID, Prompt: spec.PromptRef{Starter: "fix-checks"}, Where: catalog.WhereTerminal,
		Fallbacks: []spec.ProfileRef{{Agent: codexID, Profile: workProfile}},
	}}}

	return s
}

func TestAnImportMapsEveryLaunchSettingAndReplyItsRoutinesName(t *testing.T) {
	t.Parallel()

	r := easyRig(t, easyParts{
		profiles: fakeProfiles{claudeID: {workProfile, "default"}},
		library: fakeLibrary{prompts: []autopilot.LibraryPrompt{
			{Nickname: "My fix", Kinds: []item.Kind{item.KindPR}},
			{Nickname: "Ack", Reply: true, ReplyFor: autopilot.ReplyForComment, Kinds: []item.Kind{item.KindPR}},
		}},
	})
	path := writeBundle(t, nil, bundled(t, "Fix everything", launching()), bundled(t, "Fall back", fallingBack()))

	preview, err := r.svc.PreviewImport(t.Context(), path)
	require.NoError(t, err)
	assert.Equal(t, []autopilot.MissingRef{
		{
			Kind: autopilot.RefPrompt, Name: "Fix checks", Starter: "fix-checks",
			Routines: []string{"Fix everything", "Fall back"}, Choices: []string{"My fix"},
		},
		{
			Kind: autopilot.RefProfile, Agent: catalog.Inherit, Name: "shared", Routines: []string{"Fix everything"},
			Choices: []string{"default", workProfile},
		},
		{
			Kind: autopilot.RefProfile, Agent: claudeID, Name: "night", Routines: []string{"Fix everything"},
			Choices: []string{"default", workProfile},
		},
		{
			Kind: autopilot.RefAgent, Agent: codexID, Name: codexID, Routines: []string{"Fix everything", "Fall back"},
			Choices: []string{}, Inherits: true,
		},
		{
			Kind: autopilot.RefPrompt, Name: "Thanks", Reply: true, Routines: []string{"Fix everything"},
			Choices: []string{"Ack"},
		},
		{
			Kind: autopilot.RefPrompt, Name: "Cheers", Reply: true, Routines: []string{"Fix everything"},
			Choices: []string{"Ack"},
		},
	}, preview.Missing, "each once, with every routine that names it, in the order they are named")

	_, err = r.svc.Import(t.Context(), autopilot.ImportRequest{Path: path, SHA256: preview.SHA256, Map: []autopilot.RefChoice{
		{Kind: autopilot.RefPrompt, Name: "Fix checks", Starter: "fix-checks", To: "My fix"},
		{Kind: autopilot.RefProfile, Agent: claudeID, Name: "night", To: " " + workProfile + " "},
		{Kind: autopilot.RefPrompt, Name: "Thanks", To: "Ack"},
	}})
	require.NoError(t, err)

	got := map[string]spec.Spec{}

	for _, rt := range r.list(t) {
		sp, decodeErr := spec.Decode([]byte(r.storedSpec(t, rt.ID)))
		require.NoError(t, decodeErr)

		got[rt.Name] = sp
	}

	a := got["Fix everything"].Steps[0].Agent
	require.NotNil(t, a)
	assert.Equal(t, spec.PromptRef{Nickname: "My fix"}, a.Prompt, "the starter mapped to the prompt chosen")
	assert.Empty(t, a.Profile, "a profile no agent has, left unmapped, inherits")
	assert.Equal(t, []spec.LaunchRow{
		{Target: importOrg, Agent: claudeID, Profile: workProfile},
		{Target: importOrg + "/api"},
		{Target: importOrg + "/web"},
		{Target: importOrg + "/docs", Agent: claudeID},
	}, a.LaunchFor, "a row's profile mapped as chosen, and an agent this Mac lacks inherits with its profile")
	assert.Equal(t, []spec.ProfileRef{{Agent: claudeID, Profile: workProfile}}, a.Fallbacks,
		"a fallback on an agent this Mac lacks is left out")

	replies := got["Fix everything"].Steps[1].GitHub
	require.Len(t, replies, 2)
	assert.Equal(t, spec.PromptRef{Nickname: "Ack"}, replies[0].Reply.Prompt, "a reply mapped as chosen")
	assert.Equal(t, spec.PromptRef{Nickname: "Cheers"}, replies[1].Reply.Prompt, "a reply can't inherit, so it stays")

	assert.Nil(t, got["Fall back"].Steps[0].Agent.Fallbacks, "no fallback left")
}
