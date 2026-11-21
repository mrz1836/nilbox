package draft_test

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestReadWritesTheSentenceOfADraftThatLoads(t *testing.T) {
	t.Parallel()

	s := &setup{library: library()}

	sp := notifySpec()
	sp.Version = 0

	got, err := draft.Read(t.Context(), sp, "", s, nil)
	require.NoError(t, err)
	assert.Equal(t, []spec.Problem{}, got.Problems)
	assert.Equal(t, []spec.Problem{}, got.Warnings)
	assert.Equal(t, []catalog.StepKind{}, got.DryRunOnly, "Notify me runs in any mode")
	assert.Equal(t, spec.Version, got.Spec.Version, "a draft that names no version is this build's")
	assert.Equal(t, spec.Sentence(spec.Normalize(notifySpec()), nil), got.Sentence)
	assert.Equal(t, 1, s.libraryReads, "its prompts are checked against the library")
	assert.Zero(t, s.scriptReads, "a routine that runs no script reads none")

	named := &setup{library: library(), names: map[string]string{"fix-checks": "My fix"}}

	agent, err := draft.Read(t.Context(), agentSpec(spec.PromptRef{Starter: "fix-checks"}, catalog.WhereTerminal), "",
		named, nil)
	require.NoError(t, err)
	assert.Empty(t, agent.Problems)
	assert.Contains(t, agent.Sentence, "My fix", "the starter is named as the library names it")
}

func TestReadNamesTheSpecsOwnProblemsFirstAndKeepsNoSentence(t *testing.T) {
	t.Parallel()

	bad := notifySpec()
	bad.Watch = spec.Watch{Sources: []spec.Source{{Search: &watch.Search{Kind: item.KindPR}}}}
	bad.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{
		{Field: catalog.FieldTitle, Op: catalog.OpMatchesRegex, Value: "(unclosed"},
	}}
	bad.Steps = []spec.Step{{Snooze: &spec.SnoozeStep{}}, {Agent: &spec.AgentStep{Prompt: spec.PromptRef{Nickname: "Gone"}}}}

	s := &setup{library: library()}

	got, err := draft.Read(t.Context(), bad, autopilotprefs.ModeAuto, s, nil)
	require.NoError(t, err, "problems are the answer, not an error")
	assert.Empty(t, got.Sentence, "the builder keeps the last sentence that read")

	paths := make([]string, 0, len(got.Problems))
	for _, p := range got.Problems {
		paths = append(paths, p.Path())
	}

	assert.Contains(t, paths, "watch.sources[0].search.where", "a search with no scope")
	assert.Contains(t, paths, "conditions.of[0].value", "a regex that doesn't compile")
	assert.NotContains(t, paths, "steps[1].agent.prompt", "the spec's own problems come first")
	assert.Zero(t, s.libraryReads, "nor is the library read for them")

	require.NotEmpty(t, got.Warnings, "its warnings are read all the same")
	assert.Equal(t, "steps[0]", got.Warnings[0].Path(), "a Snooze over a watch that is only searches")
	assert.Equal(t, normalized(bad), got.Spec, "the spec normalized, as far as it reads")
}

// normalized is sp as a draft stores it.
func normalized(sp spec.Spec) spec.Spec {
	_, out, err := draft.Encode(sp)
	if err != nil {
		panic(err)
	}

	return out
}

func TestReadRefusesASpecThatCantBeRead(t *testing.T) {
	t.Parallel()

	large := notifySpec()
	large.Steps[0].Notify.Note = strings.Repeat("a", spec.MaxBytes)

	got, err := draft.Read(t.Context(), large, "", &setup{}, nil)
	require.ErrorIs(t, err, spec.ErrUnreadable)
	assert.True(t, strings.HasPrefix(uierr.Message(err), "The routine can't be read: "), uierr.Message(err))
	assert.Equal(t, draft.Reading{Problems: []spec.Problem{}, Warnings: []spec.Problem{}, DryRunOnly: []catalog.StepKind{}},
		got, "its lists are empty, never nil")

	newer := notifySpec()
	newer.Version = spec.Version + 1

	_, err = draft.Read(t.Context(), newer, "", &setup{}, nil)
	require.ErrorIs(t, err, spec.ErrNewer, "a newer build's spec isn't guessed at")
	assert.True(t, strings.HasPrefix(uierr.Message(err), "The routine can't be read: "), uierr.Message(err))

	nan := agentSpec(spec.PromptRef{ItemDefault: true}, catalog.WhereBackground)
	usd := math.NaN()
	nan.Steps[0].Agent.Fence = &spec.Fence{Budget: &spec.Budget{USD: &usd}}

	_, err = draft.Read(t.Context(), nan, "", &setup{}, nil)
	require.Error(t, err, "a spec that can't be encoded can't be stored")
}

func TestEncodeIsTheSpecAsSaveStoresIt(t *testing.T) {
	t.Parallel()

	sp := notifySpec()
	sp.Version = 0
	sp.Steps[0].Notify.Note = "  Have a look  "

	data, normal, err := draft.Encode(sp)
	require.NoError(t, err)
	assert.Equal(t, spec.Version, normal.Version, "at this build's version")
	assert.Equal(t, "Have a look", normal.Steps[0].Notify.Note, "normalized")

	want, err := spec.Encode(normal)
	require.NoError(t, err)
	assert.Equal(t, want, data, "compact, in the format's order")

	loaded, problems, err := draft.Load(sp)
	require.NoError(t, err)
	assert.Empty(t, problems)
	assert.Equal(t, normal, loaded, "a draft that loads reads back as it encodes")
}

func TestLoadReturnsTheSpecsOwnProblems(t *testing.T) {
	t.Parallel()

	bad := notifySpec()
	bad.Trigger = spec.Trigger{Event: "no_such_event"}

	loaded, problems, err := draft.Load(bad)
	require.NoError(t, err)
	require.NotEmpty(t, problems)
	assert.Equal(t, spec.PartTrigger, problems[0].Part)
	assert.Equal(t, normalized(bad), loaded, "normalized, as it was read")
}

func TestProblemsAreThoseSaveRefusesBeyondTheSpecsOwn(t *testing.T) {
	t.Parallel()

	sp := notifySpec()
	sp.Steps = []spec.Step{
		{Script: &spec.ScriptStep{ID: 7, Name: "Lint"}},
		{Agent: &spec.AgentStep{
			Agent: "codex", Prompt: spec.PromptRef{Nickname: "Gone"}, Where: catalog.WhereBackground,
			Fence: &spec.Fence{Tools: catalog.PresetGitHubTasks},
		}},
	}

	s := &setup{library: library()}
	problems := draft.Problems(t.Context(), sp, s, terminalOnly())

	paths := make([]string, 0, len(problems))
	for _, p := range problems {
		paths = append(paths, p.Path())
	}

	assert.Equal(t, []string{"steps[1].agent.where", "steps[1].agent.fence.tools", "steps[1].agent.prompt", "steps[0].script.id"},
		paths, "the place, then the fence, the prompts, and the scripts")
	assert.Equal(t, "this version of "+brand.Name+" can't run an agent in the background yet: choose In my terminal",
		problems[0].Message, "an agent where this build can't run one is a problem it says plainly")
	assert.Equal(t, 1, s.scriptReads)

	none := draft.Problems(t.Context(), notifySpec(), &setup{library: library()}, nil)
	assert.Equal(t, []spec.Problem{}, none, "never nil")

	unread := &setup{noLibrary: true, noScripts: true}
	assert.Equal(t, problems[:2], draft.Problems(t.Context(), sp, unread, terminalOnly()),
		"a library and scripts that can't be read find none of theirs")
	assert.Equal(t, 1, unread.libraryReads)
	assert.Equal(t, 1, unread.scriptReads)
}

func TestReadWarnsOfWhatMayDoLessThanMeant(t *testing.T) {
	t.Parallel()

	snooze := notifySpec()
	snooze.Trigger = spec.Trigger{Event: catalog.EventChecksPassed}
	snooze.Watch = spec.Watch{Sources: []spec.Source{{Search: &watch.Search{
		Kind: item.KindPR, Authors: []string{"app/dependabot"}, Where: []string{"org:acme-org"},
	}}}}
	snooze.Steps = []spec.Step{{Snooze: &spec.SnoozeStep{}}}

	got, err := draft.Read(t.Context(), snooze, "", &setup{}, nil)
	require.NoError(t, err)
	require.Len(t, got.Warnings, 1)
	assert.Equal(t, "steps[0]", got.Warnings[0].Path())
	assert.Contains(t, got.Warnings[0].Message, "aren't in your inbox")
	assert.NotEmpty(t, got.Sentence, "a warning keeps nothing from being saved")

	terminal := agentSpec(spec.PromptRef{Starter: "fix-checks"}, catalog.WhereTerminal)
	s := &setup{library: library()}

	held, err := draft.Read(t.Context(), terminal, autopilotprefs.ModeAuto, s,
		&draft.Offers{Steps: []catalog.StepKind{catalog.StepPin}})
	require.NoError(t, err)
	assert.Equal(t, []catalog.StepKind{catalog.StepAgent}, held.DryRunOnly, "a build that runs no agent holds it")
	assert.Empty(t, held.Warnings, "a routine held in Dry run starts nothing on its own")

	open, err := draft.Read(t.Context(), terminal, autopilotprefs.ModeAuto, s, nil)
	require.NoError(t, err)
	require.Len(t, open.Warnings, 1)
	assert.Equal(t, "steps[0].agent.where", open.Warnings[0].Path())
	assert.Equal(t, "On Autopilot, a terminal run opens a terminal tab while you work: Ask first suits it better.",
		open.Warnings[0].Message)

	fenced := agentSpec(spec.PromptRef{Starter: "fix-checks"}, catalog.WhereBackground)
	fenced.Steps[0].Agent.Fence = &spec.Fence{Tools: catalog.PresetProfile}

	own, err := draft.Read(t.Context(), fenced, autopilotprefs.ModeAuto, s, nil)
	require.NoError(t, err)
	require.Len(t, own.Warnings, 1)
	assert.Equal(t, "steps[0].agent.fence.tools", own.Warnings[0].Path())

	asks, err := draft.Read(t.Context(), fenced, autopilotprefs.ModeAsk, s, nil)
	require.NoError(t, err)
	assert.Empty(t, asks.Warnings, "Ask first waits for the user")

	codex := agentSpec(spec.PromptRef{Starter: "fix-checks"}, catalog.WhereBackground)
	codex.Steps[0].Agent.Agent = "codex"

	budget, err := draft.Read(t.Context(), codex, autopilotprefs.ModeAsk, s, nil)
	require.NoError(t, err)
	require.Len(t, budget.Warnings, 1)
	assert.Equal(t, "steps[0].agent.fence.budget", budget.Warnings[0].Path(), "a budget its agent can't keep")
}

func TestReadWarnsOfAMergePastTheRulesWhileTheSwitchIsOff(t *testing.T) {
	t.Parallel()

	sync := notifySpec()
	sync.Trigger = spec.Trigger{Event: catalog.EventChecksPassed}
	sync.Steps = []spec.Step{
		{Notify: &spec.NotifyStep{}},
		{Merge: &spec.MergeStep{How: catalog.MergeWhenMergeable, Method: catalog.MergeSquash, AllowBypass: true}},
	}

	off := &setup{}

	got, err := draft.Read(t.Context(), sync, autopilotprefs.ModeAuto, off, nil)
	require.NoError(t, err)
	require.Empty(t, got.Problems)
	require.Len(t, got.Warnings, 1)
	assert.Equal(t, "steps[1].merge.allowBypass", got.Warnings[0].Path())
	assert.Equal(t, draft.PastRulesOff, got.Warnings[0].Message)
	assert.NotEmpty(t, got.Sentence, "the warning keeps nothing from being saved")

	on := &setup{pastRules: true}

	got, err = draft.Read(t.Context(), sync, autopilotprefs.ModeAuto, on, nil)
	require.NoError(t, err)
	assert.Empty(t, got.Warnings, "the switch is on")

	plain := notifySpec()
	asks := &setup{}

	got, err = draft.Read(t.Context(), plain, autopilotprefs.ModeAuto, asks, nil)
	require.NoError(t, err)
	assert.Empty(t, got.Warnings)
	assert.Zero(t, asks.pastRulesReads, "the switch is read only for a merge past the rules")
}

func TestOnlySearchesAreAWatchOfSearchesAlone(t *testing.T) {
	t.Parallel()

	search := spec.Source{Search: &watch.Search{Kind: item.KindPR, Where: []string{watch.Me}}}
	lists := spec.Source{Lists: []catalog.ListSource{catalog.SourceAuthored}}

	assert.False(t, draft.OnlySearches(spec.Watch{}), "a watch of nothing")
	assert.True(t, draft.OnlySearches(spec.Watch{Sources: []spec.Source{search, search}}))
	assert.False(t, draft.OnlySearches(spec.Watch{Sources: []spec.Source{search, lists}}), "the inbox's lists too")

	assert.Equal(t, []catalog.StepKind{catalog.StepPin, catalog.StepSnooze, catalog.StepDismiss, catalog.StepRestore},
		draft.TriageSteps())
}
