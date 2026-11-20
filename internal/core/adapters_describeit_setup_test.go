package core

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/scripts"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// snapshotAt is when the snapshot tests' snapshots are written.
func snapshotAt() time.Time { return time.Date(2026, time.November, 20, 5, 31, 3, 0, time.UTC) }

// describeSetupOf is Describe it's setup over c's own services and stores, its clock at
// snapshotAt.
func describeSetupOf(c *Core) describeSetup {
	return describeSetup{
		routines: c.Autopilot, prompts: prompts.NewStore(c.DB, nil), profiles: c.ProfileStore, settings: c.Settings,
		inbox: inbox.NewStore(c.DB, nil), clock: testutil.NewClock(snapshotAt()),
	}
}

// storedSnapshot is c's snapshot as the tool mode reads it: written by the adapter,
// encoded as the app stores it, and read back strictly.
func storedSnapshot(t *testing.T, c *Core) kit.Snapshot {
	t.Helper()

	snap, err := describeSetupOf(c).Snapshot(t.Context())
	require.NoError(t, err)

	data, err := kit.EncodeSnapshot(snap)
	require.NoError(t, err)

	stored, err := kit.DecodeSnapshot(data)
	require.NoError(t, err, "the tool mode reads the snapshot the adapter writes")

	return stored
}

// usedSetup fills c as a user would: a routine with a description, a prompt and a script
// of their own, Autopilot's settings and the rules changed, and the inbox's items. It
// returns the routine as stored.
func usedSetup(t *testing.T, c *Core) autopilot.Routine {
	t.Helper()

	ctx := t.Context()

	saved, err := c.Autopilot.Save(ctx, autopilot.Draft{
		Name: "Nightly notice", Description: "Tells me when CI breaks", Spec: boundSpec(),
	})
	require.NoError(t, err)

	_, err = c.Prompts.Create(ctx, prompts.Input{
		Name: "Security pass", Nickname: "Security", AppliesTo: prompts.AppliesToAny, Body: "A private body {{.URL}}",
	})
	require.NoError(t, err)

	_, err = c.Scripts.Save(ctx, scripts.UserScript{
		Name: "Lint all", Command: "magex lint --private-flag", Icon: scripts.IconPlay, Scope: scripts.ScopeAll,
	})
	require.NoError(t, err)

	_, err = c.Settings.SetAutopilotAgentsAtOnce(ctx, 2)
	require.NoError(t, err)
	_, err = c.Settings.SetRepoRule(ctx, "acme-org/api", reporules.RulePinned)
	require.NoError(t, err)
	_, err = c.Settings.SetRepoRule(ctx, "acme-org/legacy", reporules.RuleMuted)
	require.NoError(t, err)
	_, err = c.Settings.SetIgnoredWorkflows(ctx, []string{"Deploy preview"})
	require.NoError(t, err)

	attachedItems(t, c)

	r, err := c.Autopilot.Get(ctx, saved.ID)
	require.NoError(t, err)

	return r
}

func TestDescribeItsSnapshotHoldsTheSetupTheInboxsNamesAndTheVocabulary(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude", ".claude_work", ".codex"))
	routine := usedSetup(t, c)
	snap := storedSnapshot(t, c)
	ctx := t.Context()

	assert.Equal(t, kit.SnapshotVersion, snap.Version)
	assert.Equal(t, snapshotAt(), snap.WrittenAt)

	require.Len(t, snap.Setup.Routines, 1)
	got := snap.Setup.Routines[0]
	assert.Equal(t, kit.Routine{
		ID: routine.ID, Name: "Nightly notice", Description: "Tells me when CI breaks", Enabled: false, Mode: routine.Mode,
		Sentence: routine.Sentence, Spec: routine.Spec, UpdatedAt: routine.UpdatedAt,
	}, got)
	assert.NotEmpty(t, got.Sentence)

	library, err := prompts.NewStore(c.DB, nil).All(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Setup.Prompts, len(library), "the whole library, by its names")
	assert.Contains(t, snap.Setup.Prompts, kit.Prompt{
		Nickname: "Security", Name: "Security pass", Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToAny,
	})
	assert.Contains(t, snap.Setup.Prompts, kit.Prompt{
		Starter: prompts.StarterReplyLGTM, Nickname: "LGTM", Name: "Approve with thanks", Purpose: kit.PurposeReply,
		ReplyAction: kit.ReplyApprove, AppliesTo: kit.AppliesToPR,
	})

	mine, err := c.Autopilot.MyScripts(ctx)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, []kit.Script{{ID: mine[0].ID, Name: "Lint all", Shows: mine[0].Shows}}, snap.Setup.Scripts)

	assert.Equal(t, []kit.Profile{
		{Agent: claudeID, Name: defaultProfile},
		{Agent: claudeID, Name: workProfile},
		{Agent: string(agent.Codex), Name: defaultProfile},
	}, snap.Setup.Profiles, "every agent's, as an agent step or a fallback may name any")

	s, err := c.Settings.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, s.Autopilot, snap.Setup.Settings)
	assert.Equal(t, 2, snap.Setup.Settings.AgentsAtOnce)
	assert.Equal(t, kit.Rules{
		TrustedBots: s.TrustedBots, Pinned: []string{"acme-org/api"}, Muted: []string{"acme-org/legacy"},
		IgnoredWorkflows: []string{"Deploy preview"},
	}, snap.Setup.Rules)

	assert.Equal(t, []string{boundOwner + "/" + boundRepo}, snap.Inbox.Repositories)
	assert.Equal(t, []string{boundOwner}, snap.Inbox.Owners)
	assert.Equal(t, []string{rigViewer}, snap.Inbox.People)
	assert.Equal(t, []string{"dependabot"}, snap.Inbox.Bots)
	assert.ElementsMatch(t, []string{"dependencies", "needs review"}, snap.Inbox.Labels, "each name one line of plain text")
	assert.ElementsMatch(t, []string{"CI / test", "CI / lint", "CodeQL"}, snap.Inbox.Checks)

	assert.Len(t, snap.Vocabulary.Templates, len(autopilot.Gallery()))
	assert.Len(t, snap.Vocabulary.Starters, len(prompts.Starters()))
	assert.Len(t, snap.Vocabulary.Variables, len(prompts.Variables()))

	data, err := kit.EncodeSnapshot(snap)
	require.NoError(t, err)

	for _, never := range []string{"A private body", "magex lint", "Rewrite the secret parser", "https://github.com"} {
		assert.NotContains(t, string(data), never, "no prompt's text, script's command, or item's title or address")
	}
}

func TestDescribeItsVocabularyIsTheBuildsOwn(t *testing.T) {
	t.Parallel()

	v := describeVocabulary()

	for i, g := range autopilot.Gallery() {
		tmpl := v.Templates[i]
		assert.Equal(t, g.Key, tmpl.Key)
		assert.Equal(t, g.Name, tmpl.Name)
		assert.Equal(t, g.Suggests, tmpl.Suggests)
		assert.Equal(t, g.Spec, tmpl.Spec)
		require.Len(t, tmpl.Blanks, len(g.Blanks), g.Key)

		for j, b := range g.Blanks {
			assert.Equal(t, kit.Blank{
				Kind: string(b.Kind), Label: b.Label, Help: b.Help, Source: b.Source, Condition: b.Condition,
				Required: b.Required,
			}, tmpl.Blanks[j], g.Key)
		}
	}

	for i, st := range prompts.Starters() {
		assert.Equal(t, st.Key, v.Starters[i].Key)
		assert.Equal(t, st.Nickname, v.Starters[i].Nickname)
		assert.NotEmpty(t, v.Starters[i].Purpose, "a starter's \"\" purpose is an agent's, as the library reads it")
	}

	for i, pv := range prompts.Variables() {
		assert.Equal(t, kit.Variable{
			Name: pv.Name, Group: pv.Group, Type: pv.Type, Description: pv.Description, PROnly: pv.PROnly,
		}, v.Variables[i])
	}
}

func TestDescribeItsSnapshotFailsWholeWhenAPartCantBeRead(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		table, words string
	}{
		{"autopilot_routines", "read the routines"},
		{"user_scripts", "read the scripts"},
		{"agent_profiles", "read the profiles"},
		{"item_checks", "read the inbox's names"},
	} {
		t.Run(tt.table, func(t *testing.T) {
			t.Parallel()

			c := bootstrap(t, testOptions(t, ".claude"))
			testutil.Exec(t, c.DB, "DROP TABLE "+tt.table)

			_, err := describeSetupOf(c).Snapshot(t.Context())
			require.ErrorContains(t, err, tt.words, "no snapshot is a setup with a part missing")
		})
	}
}

// filledKitTemplate returns a copy of the snapshot's template tmpl's spec with its blanks
// filled as quick setup fills them, as filledTemplate fills the gallery's: the user's
// account and an org for a where, a check's name for a list of check names.
func filledKitTemplate(t *testing.T, tmpl kit.Template) spec.Spec {
	t.Helper()

	// A copy: the snapshot, and so its specs, are every subtest's.
	data, err := json.Marshal(tmpl.Spec)
	require.NoError(t, err)

	var sp spec.Spec
	require.NoError(t, json.Unmarshal(data, &sp))

	for _, b := range tmpl.Blanks {
		switch autopilot.BlankKind(b.Kind) {
		case autopilot.BlankWhere:
			sp.Watch.Sources[b.Source].Search.Where = []string{"@me", "org:acme-org"}
		case autopilot.BlankChecks:
			sp.Conditions.Of[b.Condition].Values = []string{"e2e / browser"}
		}
	}

	return sp
}

// TestEveryTemplateChecksCleanInTheKit holds the gallery to the snapshot the app writes:
// each template, its blanks filled as quick setup fills them, proposed as a new routine
// and read as the tool mode reads a proposal, checks clean through kit.Check over the
// adapter's own snapshot of a fresh setup, as the tool mode reads it from its file.
func TestEveryTemplateChecksCleanInTheKit(t *testing.T) {
	t.Parallel()

	snap := storedSnapshot(t, bootstrap(t, testOptions(t, ".claude")))
	require.NotEmpty(t, snap.Vocabulary.Templates)

	for _, tmpl := range snap.Vocabulary.Templates {
		t.Run(tmpl.Key, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(kit.Proposal{
				Summary: "Set up " + tmpl.Name + ".",
				Routines: []kit.RoutineChange{{
					Name: tmpl.Name, Template: tmpl.Key, Mode: tmpl.Suggests, Spec: filledKitTemplate(t, tmpl),
				}},
			})
			require.NoError(t, err)

			p, err := kit.DecodeProposal(data)
			require.NoError(t, err, "it reads as the tool mode reads a proposal")

			checked := kit.Check(t.Context(), snap, p)
			require.Len(t, checked.Routines, 1)

			r := checked.Routines[0]
			assert.Empty(t, r.Problems, "a template, its blanks filled, is a routine that checks clean")
			assert.Zero(t, checked.Problems())
			assert.NotEmpty(t, r.Sentence)
			assert.Equal(t, tmpl.Name, r.Name)
			assert.False(t, r.NameTaken)
		})
	}
}

// promptSetNames are the prompts package's named string types whose values kit's proposal
// spells for a new prompt, by the key the proposal's schema gives each.
func promptSetNames() map[string]string {
	return map[string]string{"Purpose": "purpose", "ReplyAction": "replyAction", "AppliesTo": "appliesTo"}
}

// promptSetValues are the values of the constants the prompts package declares of those
// types, by their names: a constant added there fails TestKitsPromptSetsAreThePromptLibrarys
// until kit's closed set, and this list, have it too.
func promptSetValues() map[string]string {
	return map[string]string{
		"PurposeAgent": string(prompts.PurposeAgent), "PurposeReply": string(prompts.PurposeReply),
		"ReplyNone": string(prompts.ReplyNone), "ReplyApprove": string(prompts.ReplyApprove),
		"ReplyRequestChanges": string(prompts.ReplyRequestChanges), "ReplyComment": string(prompts.ReplyComment),
		"AppliesToPR": string(prompts.AppliesToPR), "AppliesToIssue": string(prompts.AppliesToIssue),
		"AppliesToAny": string(prompts.AppliesToAny),
	}
}

// TestKitsPromptSetsAreThePromptLibrarys holds kit's closed sets of a new prompt's purpose,
// review, and kinds, as its proposal's schema enumerates them, to the values of the
// constants the prompts package declares of each type, read from its source: a value one
// has and the other lacks would check clean as a proposal and be refused as it is applied,
// or be read from the library as no value kit knows. Kit leaves an agent prompt's review
// out, where the library writes "".
func TestKitsPromptSetsAreThePromptLibrarys(t *testing.T) {
	t.Parallel()

	declared := promptsConstants(t)
	enums := newPromptEnums(t)
	values := promptSetValues()

	for typ, key := range promptSetNames() {
		names := declared[typ]
		require.NotEmpty(t, names, "the guard reads prompts.%s's constants", typ)

		library := make([]string, 0, len(names))

		for _, name := range names {
			value, known := values[name]
			require.True(t, known, "prompts declares %s: add it to kit's closed set of %s, and to promptSetValues", name, key)

			if value != "" {
				library = append(library, value)
			}
		}

		assert.ElementsMatch(t, library, enums[key], "kit's %s and prompts.%s hold the same values", key, typ)
	}

	assert.Equal(t, kit.Prompt{Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToIssue}, kitPrompt(prompts.Prompt{
		Purpose: prompts.PurposeAgent, ReplyAction: prompts.ReplyComment, AppliesTo: prompts.AppliesToIssue,
	}), "an agent prompt writes no review")
	assert.Equal(t, kit.Prompt{Purpose: kit.PurposeReply, ReplyAction: kit.ReplyRequestChanges, AppliesTo: kit.AppliesToPR},
		kitPrompt(prompts.Prompt{
			Purpose: prompts.PurposeReply, ReplyAction: prompts.ReplyRequestChanges, AppliesTo: prompts.AppliesToPR,
		}))
}

// newPromptEnums returns the values kit's proposal schema allows for a new prompt's
// purpose, replyAction, and appliesTo, by key.
func newPromptEnums(t *testing.T) map[string][]string {
	t.Helper()

	var schema struct {
		Defs struct {
			NewPrompt struct {
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"NewPrompt"`
		} `json:"$defs"`
	}

	require.NoError(t, json.Unmarshal(kit.Schema(), &schema))

	out := map[string][]string{}

	for key, p := range schema.Defs.NewPrompt.Properties {
		if len(p.Enum) > 0 {
			out[key] = p.Enum
		}
	}

	require.Len(t, out, len(promptSetNames()), "the schema enumerates each of a new prompt's closed sets")

	return out
}

// promptsConstants returns the names of the constants the prompts package's source
// declares, by the name of each one's type, "" for an untyped one.
func promptsConstants(t *testing.T) map[string][]string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("..", "prompts", "*.go"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	out := map[string][]string{}

	for _, path := range files {
		if !strings.HasSuffix(path, "_test.go") {
			constantsIn(t, path, out)
		}
	}

	return out
}

// constantsIn adds the constants the Go file at path declares to out, by their type.
func constantsIn(t *testing.T, path string, out map[string][]string) {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	require.NoError(t, err)

	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.CONST {
			constantsOf(gen, out)
		}
	}
}

// constantsOf adds a const declaration's constants to out, by their type: a spec's own, a
// conversion's (AppliesTo(item.KindPR)), or for a spec with neither, the one before it's.
func constantsOf(gen *ast.GenDecl, out map[string][]string) {
	typ := ""

	for _, s := range gen.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			continue
		}

		typ = constType(vs, typ)
		for _, name := range vs.Names {
			out[typ] = append(out[typ], name.Name)
		}
	}
}

// constType is the type the const spec vs declares, before for one that repeats the spec
// before it, "" for an untyped one.
func constType(vs *ast.ValueSpec, before string) string {
	switch {
	case vs.Type != nil:
		id, _ := vs.Type.(*ast.Ident)
		if id == nil {
			return ""
		}

		return id.Name
	case len(vs.Values) == 0:
		return before
	}

	call, _ := vs.Values[0].(*ast.CallExpr)
	if call == nil {
		return ""
	}

	if id, isIdent := call.Fun.(*ast.Ident); isIdent {
		return id.Name
	}

	return ""
}
