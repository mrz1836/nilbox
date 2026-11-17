package autopilot_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// claudeID is Claude Code's agent ID, as a routine names it.
const claudeID = "claude"

// triageNick is the nickname of a prompt of the user's that a routine runs.
const triageNick = "Triage"

// errNoClones fails a read of what suggests a template.
var errNoClones = errors.New("no clones")

// fakeSignals is what this Mac knows that suggests a template.
type fakeSignals struct {
	got   autopilot.TemplateSignals
	err   error
	since []time.Time
}

func (f *fakeSignals) TemplateSignals(_ context.Context, since time.Time) (autopilot.TemplateSignals, error) {
	f.since = append(f.since, since)

	return f.got, f.err
}

// fakeLibrary is a prompt library of the prompts given.
type fakeLibrary struct {
	prompts []autopilot.LibraryPrompt
}

func (f fakeLibrary) Prompts(context.Context) ([]autopilot.LibraryPrompt, error) {
	return f.prompts, nil
}

// starterLibrary holds every starter a template runs but the ones left out, by key.
func starterLibrary(without ...string) fakeLibrary {
	var out []autopilot.LibraryPrompt

	for _, g := range autopilot.Gallery() {
		for _, st := range g.Spec.Steps {
			if st.Agent == nil || st.Agent.Prompt.Starter == "" || slices.Contains(without, st.Agent.Prompt.Starter) {
				continue
			}

			out = append(out, autopilot.LibraryPrompt{
				Starter: st.Agent.Prompt.Starter, Nickname: st.Agent.Prompt.Starter, Kinds: []item.Kind{item.KindPR},
			})
		}
	}

	return fakeLibrary{prompts: out}
}

// easyRig is a rig whose Service reads the given signals, library, items, transfer, and
// profiles, any of them nil.
type easyParts struct {
	signals  autopilot.Signals
	library  autopilot.Library
	items    autopilot.Items
	transfer autopilot.PromptTransfer
	profiles autopilot.Profiles
	cfg      *autopilotprefs.Config
}

func easyRig(t *testing.T, parts easyParts) *rig {
	t.Helper()

	r := newRig(t, autopilotprefs.ModeAsk)
	cfg := onSettings()

	if parts.cfg != nil {
		cfg = *parts.cfg
	}

	c := autopilot.Config{
		Pool: r.db, Prefs: prefs{cfg: cfg}, Accounts: r.accounts, Names: names{}, File: r.file, Clock: r.clock,
		Logger: r.logs.Logger(), App: "v9.9.9-test",
	}

	if parts.signals != nil {
		c.Signals = parts.signals
	}

	if parts.library != nil {
		c.Library = parts.library
	}

	if parts.items != nil {
		c.Items = parts.items
	}

	if parts.transfer != nil {
		c.Transfer = parts.transfer
	}

	if parts.profiles != nil {
		c.Profiles = parts.profiles
	}

	r.svc = autopilot.NewService(c)

	return r
}

func TestTheGalleryPutsWhatThisMacSuggestsFirst(t *testing.T) {
	t.Parallel()

	signals := &fakeSignals{got: autopilot.TemplateSignals{Fortress: 2, Bots: 4}}
	r := easyRig(t, easyParts{signals: signals, library: starterLibrary("resolve-conflicts")})

	got, err := r.svc.Templates(t.Context())
	require.NoError(t, err)
	require.Len(t, got, len(autopilot.Gallery()))

	require.Len(t, signals.since, 1)
	assert.WithinDuration(t, r.clock.Now().Add(-7*24*time.Hour), signals.since[0], 5*time.Second, "the signals look back a week")

	suggested := []string{}

	for _, tmpl := range got {
		if tmpl.Suggested != "" {
			suggested = append(suggested, tmpl.Key)
		}
	}

	assert.Equal(t, []string{"fix-dependabot-security", "snooze-passing-bot-prs", "tell-me-about-security-failures"},
		suggested)
	assert.Equal(t, []string{got[0].Key, got[1].Key, got[2].Key}, suggested, "the suggested ones come first")
	assert.Equal(t, "2 of your clones run go-fortress’s security checks.", got[0].Suggested)
	assert.Equal(t, "Bots opened 4 pull requests in your Review requested and Assigned lists this week.", got[1].Suggested)

	byKey := map[string]autopilot.Template{}
	for _, tmpl := range got {
		byKey[tmpl.Key] = tmpl
		assert.NotNil(t, tmpl.Needs, tmpl.Key)
		assert.NotNil(t, tmpl.Blanks, tmpl.Key)
		assert.NotNil(t, tmpl.Prompts, tmpl.Key)
	}

	flagship := byKey["fix-dependabot-security"]
	assert.Equal(t, autopilotprefs.ModeAsk, flagship.Suggests)
	assert.Contains(t, flagship.Sentence, "run \"Fix dependency security\" in the background")
	assert.Empty(t, flagship.Needs, "this build runs each of its steps, Wait for the checks included")
	assert.Empty(t, flagship.NeedsWords)

	heal := byKey["heal-dependabot-then-merge"]
	assert.Equal(t, []string{"“Merge it”"}, heal.Needs, "it waits for Merge it alone")
	assert.NotEmpty(t, heal.NeedsWords, "and says so in words")
	assert.Equal(t, []autopilot.Blank{{
		Kind: autopilot.BlankWhere, Label: "Where", Help: "Your account, and the orgs, users, or repositories to watch too.",
	}}, flagship.Blanks, "the flagship asks one thing")

	snooze := byKey["snooze-passing-bot-prs"]
	assert.Empty(t, snooze.Needs, "a template of local steps runs here")
	assert.Empty(t, snooze.Blanks, "and asks nothing")

	resolve := byKey["resolve-merge-conflicts"]
	assert.Equal(t, []autopilot.TemplatePrompt{{Starter: "resolve-conflicts", Name: "Resolve conflicts"}}, resolve.Prompts,
		"a starter the library lacks is offered back")
	assert.True(t, byKey["address-requested-changes"].Prompts[0].InLibrary)

	flaky := byKey["re-run-flaky-checks-once"]
	assert.Empty(t, flaky.Sentence, "a template whose blank it needs is empty reads as no sentence yet")
}

func TestTheGallerySuggestsNothingWithoutWhatThisMacKnows(t *testing.T) {
	t.Parallel()

	for name, parts := range map[string]easyParts{
		"no signals":        {},
		"signals that fail": {signals: &fakeSignals{err: errNoClones}},
		"nothing known":     {signals: &fakeSignals{}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := easyRig(t, parts).svc.Templates(t.Context())
			require.NoError(t, err)

			for i, tmpl := range got {
				assert.Empty(t, tmpl.Suggested, tmpl.Key)
				assert.Equal(t, autopilot.Gallery()[i].Key, tmpl.Key, "the gallery's own order")
			}
		})
	}
}

func TestARoutineFromATemplateStartsInTheDefaultMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []autopilotprefs.Mode{autopilotprefs.ModeDryRun, autopilotprefs.ModeAsk, autopilotprefs.ModeAuto} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			r := newRig(t, mode)
			snooze, ok := autopilot.TemplateByKey("snooze-passing-bot-prs")
			require.True(t, ok)
			require.Equal(t, autopilotprefs.ModeAuto, snooze.Suggests)

			got := r.save(t, autopilot.Draft{Name: snooze.Name, Spec: snooze.Spec, TemplateKey: snooze.Key})
			assert.Equal(t, mode, got.Mode, "the mode Settings says, never the template's suggestion")
			assert.Equal(t, snooze.Key, got.TemplateKey)
			assert.False(t, got.Enabled, "it starts off")
		})
	}
}

func TestDraftReadinessSaysWhatAnUnsavedRoutineNeeds(t *testing.T) {
	t.Parallel()

	checker := &fakeChecker{levels: map[string]autopilot.ReadyLevel{"agent:claude": autopilot.ReadyProblem}}
	r := newRig(t, autopilotprefs.ModeAsk)
	r.svc = autopilot.NewService(autopilot.Config{
		Pool: r.db, Prefs: prefs{cfg: onSettings()}, Accounts: r.accounts, Clock: r.clock, Readiness: checker,
	})

	got, err := r.svc.DraftReadiness(t.Context(), autopilot.Draft{Name: "Draft", Spec: agentSpec()})
	require.NoError(t, err)
	assert.False(t, got.Ready)
	require.Len(t, got.Problems, 1)
	assert.Equal(t, "agent:claude", got.Problems[0].ID)
	assert.False(t, checker.fresh, "a draft's checks never ask GitHub or run the slow ones afresh")

	none, err := newRig(t, autopilotprefs.ModeAsk).svc.DraftReadiness(t.Context(), autopilot.Draft{Spec: agentSpec()})
	require.NoError(t, err)
	assert.True(t, none.Ready, "with nothing to check, a draft is ready")
	assert.NotNil(t, none.Problems)
}

// fakeChecker answers every need ok but those it names.
type fakeChecker struct {
	levels map[string]autopilot.ReadyLevel
	fresh  bool
}

func (f *fakeChecker) Check(_ context.Context, needs []autopilot.Need, fresh bool) ([]autopilot.ReadyCheck, error) {
	f.fresh = f.fresh || fresh

	out := make([]autopilot.ReadyCheck, 0, len(needs))

	for _, n := range needs {
		level := autopilot.ReadyOK
		if l, ok := f.levels[n.ID()]; ok {
			level = l
		}

		out = append(out, autopilot.ReadyCheck{Need: n, Label: n.ID(), Level: level, Found: "checked"})
	}

	return out, nil
}

func (f *fakeChecker) Defaults(context.Context) (autopilot.LaunchDefaults, error) {
	return autopilot.LaunchDefaults{Agent: claudeID, Profiles: map[string]string{}}, nil
}

func TestFromItemFillsTheBuilderFromAStoredItem(t *testing.T) {
	t.Parallel()

	row := failingPR(5, 45)
	row.Checks = &facts.ChecksRow{
		Head: row.Details.HeadOID, Complete: true, Checks: []facts.CheckRow{
			{Name: "Analyze (go)", Workflow: "CodeQL", Outcome: facts.CheckFailed, CompletedAt: startedAt().Add(-time.Hour)},
		},
	}
	items := &fakeItems{rows: map[int64]facts.Row{5: row}}
	r := easyRig(t, easyParts{items: items})
	r.save(t, draft("Dependabot’s pull requests in acme-org", notifySpec()))

	got, err := r.svc.FromItem(t.Context(), 5)
	require.NoError(t, err)

	assert.Equal(t, "Dependabot’s pull requests in acme-org 2", got.Draft.Name, "a name no routine has")
	assert.Equal(t, autopilotprefs.ModeAsk, got.Draft.Mode, "the mode new routines start in")
	assert.Zero(t, got.Draft.ID)
	assert.Equal(t, int64(5), got.Item.ID)
	assert.Equal(t, "acme-org/example-repo", got.Item.Repo)

	search := got.Draft.Spec.Watch.Sources[0].Search
	require.NotNil(t, search)
	assert.Equal(t, watch.Search{Kind: item.KindPR, Authors: []string{"app/dependabot"}, Where: []string{"org:acme-org"}}, *search)
	assert.Equal(t, catalog.EventChecksFailed, got.Draft.Spec.Trigger.Event)
	assert.Equal(t, []spec.Clause{{Field: catalog.FieldFailedNames, Op: catalog.OpAnyContains, Values: []string{"CodeQL / Analyze (go)"}}},
		got.Draft.Spec.Conditions.Of, "the actual failed checks' names")
	assert.Equal(t, []spec.Clause{{Field: catalog.FieldUpdateType, Op: catalog.OpOneOf, Values: []string{"patch"}}},
		got.Suggestions)
	assert.NotEmpty(t, got.Trace, "what the draft decides about the item now")

	_, err = r.svc.FromItem(t.Context(), 99)
	require.ErrorIs(t, err, autopilot.ErrNoItems, "an item no longer stored")

	_, err = newRig(t, autopilotprefs.ModeAsk).svc.FromItem(t.Context(), 5)
	require.ErrorIs(t, err, autopilot.ErrNoItems, "a Service with no inbox")
}

func TestWhyDidntItRunSaysWhatHoldsTheRoutineAndTheItem(t *testing.T) {
	t.Parallel()

	person := failingPR(6, 46)
	person.Author, person.AuthorType = "octo-other", "User"
	items := &fakeItems{rows: map[int64]facts.Row{5: failingPR(5, 45), 6: person}}
	cfg := onSettings()
	cfg.Paused = true
	r := easyRig(t, easyParts{items: items, cfg: &cfg})

	flagship, ok := autopilot.TemplateByKey("tell-me-about-security-failures")
	require.True(t, ok)

	sp := flagship.Spec
	sp.Watch.Sources[0].Search.Where = []string{"org:acme-org"}
	saved := r.save(t, autopilot.Draft{Name: "Tell me", Spec: sp, Mode: autopilotprefs.ModeDryRun})

	got, err := r.svc.Explain(t.Context(), saved.ID, 6)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"Autopilot is paused, so nothing new starts until you resume it.",
		"It's switched off, so it runs on nothing.",
		"It's in Dry run: it records what it would do, and does nothing else.",
	}, got.Routine)
	assert.False(t, got.InWatch)
	assert.Equal(t, []string{"Not in its search: its author is octo-other, not Dependabot."}, got.Watch)
	assert.NotNil(t, got.Records)

	now := startedAt()
	testutil.Exec(t, r.db, `INSERT INTO accounts (id, login, created_at, updated_at) VALUES (1, ?, ?, ?)`, viewer, now, now)
	testutil.Exec(t, r.db, `INSERT INTO repos (id, owner, name, created_at, updated_at)
		VALUES (1, 'acme-org', 'example-repo', ?, ?)`, now, now)
	testutil.Exec(t, r.db, `INSERT INTO items (id, account_id, repo_id, kind, number, item_created_at, item_updated_at,
		fetched_at) VALUES (5, 1, 1, 'pr', 45, ?, ?, ?)`, now, now, now)
	testutil.Exec(t, r.db, `INSERT INTO autopilot_matches (routine_id, item_id, repo_owner, repo_name, kind, number,
		fire_key, state, reason, created_at, updated_at, decided_at, closed_at)
		VALUES (?, 5, 'acme-org', 'example-repo', 'pr', 45, 'checks_failed:commit:old', 'done', 'ran', ?, ?, ?, ?)`,
		saved.ID, startedAt(), startedAt(), startedAt(), startedAt())

	got, err = r.svc.Explain(t.Context(), saved.ID, 5)
	require.NoError(t, err)
	assert.Equal(t, []string{"Its search should find it, but the last refresh didn't bring it: it shows once a refresh " +
		"reads the search again."}, got.Watch, "an item that fits the search but isn't in its rows yet")
	require.Len(t, got.Records, 1, "the ledger's full view: every record of the routine on the item")
	assert.Equal(t, autopilot.StateDone, got.Records[0].State)
}

func TestGraduatingIsASuggestionAfterFiveCleanApprovals(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	saved := r.save(t, draft("Fix it", agentSpec()))
	_, err := r.svc.SetEnabled(t.Context(), saved.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	clean := `[{"step":{"agent":{"prompt":{"starter":"fix-checks"}}},"status":"done"}]`
	at := startedAt()
	approve := func(n int, state, reason, steps string, approved bool) {
		for range n {
			at = at.Add(time.Minute)
			testutil.Exec(t, r.db, `INSERT INTO autopilot_matches (routine_id, fire_key, state, reason, approved, steps,
				created_at, updated_at, decided_at, closed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				saved.ID, fmt.Sprintf("manual:%d", at.Unix()), state, reason, approved, steps, at, at, at, at)
		}
	}
	graduates := func() bool {
		t.Helper()

		for _, rt := range r.list(t) {
			if rt.ID == saved.ID {
				return rt.Figures.Graduate
			}
		}

		t.Fatal("the routine isn't listed")

		return false
	}

	approve(4, "done", "ran", clean, true)
	assert.False(t, graduates(), "four clean approvals aren't enough")

	approve(1, "done", "ran", clean, true)
	assert.True(t, graduates(), "five in a row offer to go on Autopilot")
	assert.Equal(t, autopilotprefs.ModeAsk, r.list(t)[0].Mode, "and never change the mode")

	approve(1, "dismissed", "you_skipped", clean, false)
	assert.False(t, graduates(), "a run the user skipped breaks the streak")

	approve(5, "done", "ran", clean, true)
	assert.True(t, graduates())

	approve(1, "done", "ran", `[{"step":{"agent":{}},"status":"done","outcome":"notPushed"}]`, true)
	assert.False(t, graduates(), "one that didn't finish cleanly breaks it")

	approve(5, "done", "ran", clean, true)
	require.True(t, graduates())

	r.clock.Advance(time.Hour)

	declined, err := r.svc.DeclineGraduate(t.Context(), saved.ID)
	require.NoError(t, err)
	assert.Equal(t, autopilotprefs.ModeAsk, declined.Mode, "Not now changes no mode")
	assert.False(t, graduates(), "Not now waits for five more")

	at = r.clock.Now()

	approve(5, "done", "ran", clean, true)
	assert.True(t, graduates(), "five more after Not now offer again")

	_, err = r.svc.SetMode(t.Context(), saved.ID, autopilotprefs.ModeAuto, autopilot.CurrentRun)
	require.NoError(t, err)
	assert.False(t, graduates(), "a routine on Autopilot already offers nothing")

	_, err = r.svc.DeclineGraduate(t.Context(), 9999)
	require.ErrorIs(t, err, autopilot.ErrNotFound)
}

func TestGraduatingNeedsAStepThatAsksFirst(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	saved := r.save(t, draft("Tell me", notifySpec()))
	_, err := r.svc.SetEnabled(t.Context(), saved.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	for i := range 6 {
		at := startedAt().Add(time.Duration(i) * time.Minute)
		testutil.Exec(t, r.db, `INSERT INTO autopilot_matches (routine_id, fire_key, state, reason, approved, steps,
			created_at, updated_at, decided_at, closed_at) VALUES (?, ?, 'done', 'ran', 1, '[]', ?, ?, ?, ?)`,
			saved.ID, fmt.Sprintf("k%d", i), at, at, at, at)
	}

	assert.False(t, r.list(t)[0].Figures.Graduate, "Ask first and Autopilot run a routine of local steps alike")
}

// fakeTransfer carries prompts as a list of nicknames, the library holding have.
type fakeTransfer struct {
	have  []string
	added [][]string
}

func (f *fakeTransfer) ExportPrompts(_ context.Context, nicknames []string) (json.RawMessage, int, error) {
	data, err := json.Marshal(nicknames)

	return data, len(nicknames), err
}

func (f *fakeTransfer) PreviewPrompts(_ context.Context, raw json.RawMessage) ([]autopilot.PromptArrival, error) {
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, err
	}

	out := []autopilot.PromptArrival{}

	for _, n := range names {
		arrives := autopilot.ArrivesNew
		if slices.Contains(f.have, n) {
			arrives = autopilot.ArrivesDiffers
		}

		out = append(out, autopilot.PromptArrival{Nickname: n, Arrives: arrives})
	}

	return out, nil
}

func (f *fakeTransfer) ImportPrompts(_ context.Context, raw json.RawMessage) ([]string, error) {
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, err
	}

	var added []string

	for _, n := range names {
		if !slices.Contains(f.have, n) {
			added = append(added, n)
		}
	}

	f.added = append(f.added, added)

	return added, nil
}

// fakeProfiles is this Mac's agents and their profiles.
type fakeProfiles map[string][]string

func (f fakeProfiles) Profiles(context.Context) (map[string][]string, error) {
	return f, nil
}

// traveling is a routine that names a prompt by nickname, a profile, an agent, and a
// fallback, as one exported from another Mac does.
func traveling() spec.Spec {
	s := notifySpec()
	s.Steps = []spec.Step{{Agent: &spec.AgentStep{
		Agent: "codex", Prompt: spec.PromptRef{Nickname: triageNick}, Where: catalog.WhereTerminal, Profile: "work",
		Fallbacks: []spec.ProfileRef{{Agent: claudeID, Profile: "spare"}},
	}}}

	return s
}

func TestExportAndImportCarryRoutinesWithTheirPromptsAndMapWhatIsMissing(t *testing.T) {
	t.Parallel()

	from := easyRig(t, easyParts{transfer: &fakeTransfer{}})
	mine := from.save(t, draft("Triage bugs", traveling()))
	from.save(t, draft("Second", notifySpec()))

	path := filepath.Join(t.TempDir(), "routines.json")
	exported, err := from.svc.Export(t.Context(), path, []int64{mine.ID}, true)
	require.NoError(t, err)
	assert.Equal(t, autopilot.Exported{Routines: 1, Prompts: 1}, exported)

	data, err := os.ReadFile(path) //nolint:gosec // the test's own file
	require.NoError(t, err)

	b, err := autopilot.DecodeBundle(data)
	require.NoError(t, err)
	assert.Equal(t, "v9.9.9-test", b.App)
	require.Len(t, b.Routines, 1)
	assert.JSONEq(t, `["Triage"]`, string(b.Prompts), "the text of the prompts it names by nickname")

	// This Mac has no Codex, a Claude without the spare profile, and no prompt named
	// Triage, though the file brings one; it already has a routine of the name.
	transfer := &fakeTransfer{}
	to := easyRig(t, easyParts{
		transfer: transfer, profiles: fakeProfiles{claudeID: {"default", "personal"}},
		library: fakeLibrary{prompts: []autopilot.LibraryPrompt{{Nickname: "Fix checks", Kinds: []item.Kind{item.KindPR}}}},
	})
	to.save(t, draft("Triage bugs", notifySpec()))

	preview, err := to.svc.PreviewImport(t.Context(), path)
	require.NoError(t, err)
	assert.NotEmpty(t, preview.SHA256)
	require.Len(t, preview.Routines, 1)
	assert.Equal(t, "Triage bugs 2", preview.Routines[0].ArrivesAs)
	assert.NotEmpty(t, preview.Routines[0].Sentence)
	assert.Equal(t, []autopilot.PromptArrival{{Nickname: triageNick, Arrives: autopilot.ArrivesNew}}, preview.Prompts)
	assert.Equal(t, []autopilot.MissingRef{
		{
			Kind: autopilot.RefAgent, Agent: "codex", Name: "codex", Routines: []string{"Triage bugs"}, Choices: []string{},
			Inherits: true,
		},
		{
			Kind: autopilot.RefProfile, Agent: claudeID, Name: "spare", Routines: []string{"Triage bugs"},
			Choices: []string{"default", "personal"},
		},
	}, preview.Missing, "the prompt arriving with it is no longer missing; Codex's profile goes with Codex")

	imported, err := to.svc.Import(t.Context(), autopilot.ImportRequest{
		Path: path, SHA256: preview.SHA256,
		Map: []autopilot.RefChoice{{Kind: autopilot.RefProfile, Agent: claudeID, Name: "spare", To: "personal"}},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Triage bugs 2"}, imported.Routines)
	assert.Equal(t, []string{triageNick}, imported.Prompts)

	var arrived autopilot.Routine

	for _, rt := range to.list(t) {
		if rt.Name == "Triage bugs 2" {
			arrived = rt
		}
	}

	require.NotZero(t, arrived.ID)
	assert.False(t, arrived.Enabled, "an imported routine arrives off")
	assert.Equal(t, viewer, arrived.Account)

	a := arrived.Spec.Steps[0].Agent
	require.NotNil(t, a)
	assert.Equal(t, catalog.Inherit, a.Agent, "an agent this Mac lacks inherits")
	assert.Empty(t, a.Profile, "and its profile with it")
	assert.Equal(t, []spec.ProfileRef{{Agent: claudeID, Profile: "personal"}}, a.Fallbacks, "a profile mapped as chosen")
	assert.Equal(t, spec.PromptRef{Nickname: triageNick}, a.Prompt, "the prompt that arrived with it")
}

func TestImportMapsAPromptItLacksToTheItemsOwnDefault(t *testing.T) {
	t.Parallel()

	from := easyRig(t, easyParts{})
	mine := from.save(t, draft("Triage bugs", traveling()))
	path := filepath.Join(t.TempDir(), "routines.json")
	_, err := from.svc.Export(t.Context(), path, nil, false)
	require.NoError(t, err)

	to := easyRig(t, easyParts{
		profiles: fakeProfiles{"codex": {"work"}, claudeID: {"spare"}},
		library:  fakeLibrary{prompts: []autopilot.LibraryPrompt{{Nickname: "Look", Kinds: []item.Kind{item.KindPR}}}},
	})

	preview, err := to.svc.PreviewImport(t.Context(), path)
	require.NoError(t, err)
	require.Len(t, preview.Missing, 1)
	assert.Equal(t, autopilot.MissingRef{
		Kind: autopilot.RefPrompt, Name: triageNick, Routines: []string{"Triage bugs"}, Choices: []string{"Look"},
	}, preview.Missing[0])

	_, err = to.svc.Import(t.Context(), autopilot.ImportRequest{Path: path, SHA256: preview.SHA256})
	require.NoError(t, err)

	got := to.list(t)
	require.Len(t, got, 1)
	assert.Equal(t, mine.Name, got[0].Name)
	assert.Equal(t, spec.PromptRef{ItemDefault: true}, got[0].Spec.Steps[0].Agent.Prompt, "left unmapped, it inherits")
	assert.Equal(t, "codex", got[0].Spec.Steps[0].Agent.Agent, "what this Mac has stays as it was")

	again := filepath.Join(t.TempDir(), "again.json")
	_, err = from.svc.Export(t.Context(), again, nil, false)
	require.NoError(t, err)

	mapped := easyRig(t, easyParts{
		library: fakeLibrary{prompts: []autopilot.LibraryPrompt{{Nickname: "Look", Kinds: []item.Kind{item.KindPR}}}},
	})
	_, err = mapped.svc.Import(t.Context(), autopilot.ImportRequest{
		Path: again, Map: []autopilot.RefChoice{{Kind: autopilot.RefPrompt, Name: triageNick, To: "Look"}},
	})
	require.NoError(t, err)
	assert.Equal(t, spec.PromptRef{Nickname: "Look"}, mapped.list(t)[0].Spec.Steps[0].Agent.Prompt)
}

func TestImportRefusesAFileThatChangedSinceItsPreview(t *testing.T) {
	t.Parallel()

	from := easyRig(t, easyParts{})
	from.save(t, draft("One", notifySpec()))
	path := filepath.Join(t.TempDir(), "routines.json")
	_, err := from.svc.Export(t.Context(), path, nil, false)
	require.NoError(t, err)

	to := easyRig(t, easyParts{})
	preview, err := to.svc.PreviewImport(t.Context(), path)
	require.NoError(t, err)

	from.save(t, draft("Two", notifySpec()))
	_, err = from.svc.Export(t.Context(), path, nil, false)
	require.NoError(t, err)

	_, err = to.svc.Import(t.Context(), autopilot.ImportRequest{Path: path, SHA256: preview.SHA256})
	require.ErrorIs(t, err, autopilot.ErrChanged)
	assert.Empty(t, to.list(t), "nothing is imported")

	_, err = to.svc.PreviewImport(t.Context(), "relative.json")
	require.ErrorIs(t, err, autopilot.ErrNoPath)

	_, err = to.svc.Export(t.Context(), filepath.Join(t.TempDir(), "none.json"), nil, false)
	require.ErrorIs(t, err, autopilot.ErrNothingToExport, "a Mac with no routine has nothing to export")
}

func TestAnImportedRoutineFromANewerBuildArrivesBrokenAndUnrewritten(t *testing.T) {
	t.Parallel()

	newer := `{"format":"` + autopilot.BundleFormat + `","exportedAt":"2026-11-16T09:00:00Z","app":"v9",` +
		`"routines":[{"name":"Later","mode":"auto","enabled":true,"spec":{"version":99,"steps":[{"teleport":{}}]}}]}`
	path := filepath.Join(t.TempDir(), "routines.json")
	require.NoError(t, os.WriteFile(path, []byte(newer), 0o600))

	r := easyRig(t, easyParts{profiles: fakeProfiles{}})

	preview, err := r.svc.PreviewImport(t.Context(), path)
	require.NoError(t, err)
	require.Len(t, preview.Routines, 1)
	assert.Empty(t, preview.Routines[0].Sentence)
	assert.Contains(t, preview.Routines[0].Broken, "newer")

	_, err = r.svc.Import(t.Context(), autopilot.ImportRequest{Path: path, SHA256: preview.SHA256})
	require.NoError(t, err)

	got := r.list(t)
	require.Len(t, got, 1)
	assert.False(t, got[0].Enabled, "it arrives off, as every imported routine does")
	assert.JSONEq(t, `{"version":99,"steps":[{"teleport":{}}]}`, r.storedSpec(t, got[0].ID), "never rewritten")
}
