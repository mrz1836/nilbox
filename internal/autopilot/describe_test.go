package autopilot_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// library is a fake prompt library: Fix checks, a starter for pull requests, a prompt of
// the user's own for any item, and the replies: an approval's, a request for changes', and
// a comment's for any item.
type library struct {
	err error
}

func (l library) Prompts(context.Context) ([]autopilot.LibraryPrompt, error) {
	return []autopilot.LibraryPrompt{
		{Starter: "fix-checks", Nickname: "Fix checks", Kinds: []item.Kind{item.KindPR}},
		{Nickname: "Triage", Kinds: item.Kinds()},
		{
			Starter: "reply-lgtm", Nickname: "LGTM", Reply: true, ReplyFor: autopilot.ReplyForApproval,
			Kinds: []item.Kind{item.KindPR},
		},
		{
			Starter: "reply-needs-changes", Nickname: "Needs changes", Reply: true,
			ReplyFor: autopilot.ReplyForRequestChanges, Kinds: []item.Kind{item.KindPR},
		},
		{
			Starter: "reply-question", Nickname: "Question", Reply: true, ReplyFor: autopilot.ReplyForComment,
			Kinds: item.Kinds(),
		},
	}, l.err
}

func TestAGitHubReplyStepTakesACommentsReplyAlone(t *testing.T) {
	t.Parallel()

	r := libraryRig(t, library{}, nil)

	reply := func(nickname string) spec.Spec {
		s := searchSpec(catalog.EventAppears, orgSearch)
		s.Steps = []spec.Step{{GitHub: []spec.GitHubAction{{Reply: &spec.ReplyAction{
			Prompt: spec.PromptRef{Nickname: nickname},
		}}}}}

		return s
	}

	for nickname, want := range map[string]string{
		"LGTM":          `"LGTM" is a reply that approves: a GitHub step only comments`,
		"Needs changes": `"Needs changes" is a reply that requests changes: a GitHub step only comments`,
		"Triage":        "is a prompt for an agent",
	} {
		got, err := r.svc.Describe(t.Context(), draft(nickname, reply(nickname)))
		require.NoError(t, err)
		require.Len(t, got.Problems, 1, nickname)
		assert.Equal(t, "steps[0].github[0].reply.prompt", got.Problems[0].Path())
		assert.Contains(t, got.Problems[0].Message, want)

		_, err = r.svc.Save(t.Context(), draft(nickname, reply(nickname)))

		var problems *autopilot.ProblemsError

		require.ErrorAs(t, err, &problems, "Save refuses %s as Describe does", nickname)
	}

	got, err := r.svc.Describe(t.Context(), draft("Question", reply("question")))
	require.NoError(t, err)
	assert.Empty(t, got.Problems, "a comment's reply is what a GitHub step posts")
}

// libraryRig is a rig whose Service checks prompts against lib, and runs the steps
// offers names (nil, the catalog's).
func libraryRig(t *testing.T, lib autopilot.Library, offers *autopilot.Offers) *rig {
	t.Helper()

	r := newRig(t, autopilotprefs.ModeAsk)
	r.svc = autopilot.NewService(autopilot.Config{
		Pool: r.db, Prefs: prefs{cfg: onSettings()}, Accounts: r.accounts, Names: names{}, Library: lib,
		File: autopilot.NewFile(filepath.Join(t.TempDir(), "autopilot.json"), "v0.0.0-test", r.clock), Clock: r.clock,
		Logger: r.logs.Logger(), Offers: offers,
	})

	return r
}

func TestDescribeWritesTheSentenceOfADraftThatLoads(t *testing.T) {
	t.Parallel()

	r := libraryRig(t, library{}, nil)

	got, err := r.svc.Describe(t.Context(), draft("Tell me", notifySpec()))
	require.NoError(t, err)
	assert.Empty(t, got.Problems)
	assert.Equal(t, spec.Sentence(spec.Normalize(withVersion(notifySpec())), nil), got.Sentence)
	assert.NotNil(t, got.Warnings)
	assert.Empty(t, got.DryRunOnly, "Notify me runs in any mode")

	agent, err := r.svc.Describe(t.Context(), draft("Fix", agentSpec()))
	require.NoError(t, err)
	assert.Empty(t, agent.Problems)
	assert.Contains(t, agent.Sentence, "Fix checks", "the starter is named as the library names it")
	assert.Empty(t, agent.DryRunOnly, "an agent in the terminal runs in any mode")

	background, err := r.svc.Describe(t.Context(), draft("Fix", backgroundSpec()))
	require.NoError(t, err)
	assert.Empty(t, background.Problems, "this build runs an agent in the background, under any of the fence's presets")
	assert.Empty(t, background.DryRunOnly, "in any mode")

	assert.Empty(t, r.list(t), "describing saves nothing")

	// A build without the background runner says so plainly, and saves nothing.
	older := libraryRig(t, library{}, terminalOnly())

	unrun, err := older.svc.Describe(t.Context(), draft("Fix", backgroundSpec()))
	require.NoError(t, err)
	require.Len(t, unrun.Problems, 1, "an agent where this build can't run one is a problem it says plainly")
	assert.Equal(t, "steps[0].agent.where", unrun.Problems[0].Path())
	assert.Equal(t, "this version of "+brand.Name+" can't run an agent in the background yet: choose In my terminal",
		unrun.Problems[0].Message)
	assert.Empty(t, unrun.DryRunOnly, "it isn't held in Dry run: it can't be saved")

	_, err = older.svc.Save(t.Context(), draft("Fix", backgroundSpec()))
	require.ErrorIs(t, err, spec.ErrInvalid, "Save refuses it as Describe says")
}

// terminalOnly is the catalog's offers but the background, as a build without its runner.
func terminalOnly() *autopilot.Offers {
	out := &autopilot.Offers{Places: []catalog.Where{catalog.WhereTerminal}}

	for _, st := range catalog.Steps() {
		if st.Offered {
			out.Steps = append(out.Steps, st.Kind)
		}
	}

	for _, a := range catalog.GitHubActions() {
		if a.Offered {
			out.Actions = append(out.Actions, a.Kind)
		}
	}

	return out
}

// withVersion is s at this build's version, as Save stores it.
func withVersion(s spec.Spec) spec.Spec {
	s.Version = spec.Version

	return s
}

func TestDescribeNamesEachProblemWhereItIsAndKeepsNoSentence(t *testing.T) {
	t.Parallel()

	r := libraryRig(t, library{}, nil)

	bad := searchSpec(catalog.EventAppears, watch.Search{Kind: item.KindPR})
	bad.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{
		{Field: catalog.FieldTitle, Op: catalog.OpMatchesRegex, Value: "(unclosed"},
	}}

	got, err := r.svc.Describe(t.Context(), draft("Bad", bad))
	require.NoError(t, err, "problems are the answer, not an error")
	assert.Empty(t, got.Sentence, "the builder keeps the last sentence that read")

	paths := make([]string, 0, len(got.Problems))
	for _, p := range got.Problems {
		paths = append(paths, p.Path())
	}

	assert.Contains(t, paths, "watch.sources[0].search.where", "a search with no scope")
	assert.Contains(t, paths, "conditions.of[0].value", "a regex that doesn't compile")
}

func TestDescribeAndSaveCheckTheAgentStepsPrompt(t *testing.T) {
	t.Parallel()

	r := libraryRig(t, library{}, nil)

	tests := []struct {
		name   string
		prompt spec.PromptRef
		watch  []catalog.ListSource
		want   string
	}{
		{name: "a starter the user deleted", prompt: spec.PromptRef{Starter: "quick-look"}, want: "isn't in your library"},
		{name: "a nickname no prompt has", prompt: spec.PromptRef{Nickname: "Gone"}, want: `"Gone"`},
		{name: "a reply", prompt: spec.PromptRef{Nickname: "LGTM"}, want: "review reply"},
		{
			name: "a pull request's prompt for issues too", prompt: spec.PromptRef{Starter: "fix-checks"},
			watch: []catalog.ListSource{catalog.SourceAssigned}, want: "acts on issues too",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := agentSpec()
			s.Trigger = spec.Trigger{Event: catalog.EventAppears}
			s.Steps[0].Agent.Prompt = tt.prompt

			if tt.watch != nil {
				s.Watch.Sources = []spec.Source{{Lists: tt.watch}}
			}

			got, err := r.svc.Describe(t.Context(), draft(tt.name, s))
			require.NoError(t, err)
			require.Len(t, got.Problems, 1)
			assert.Equal(t, "steps[0].agent.prompt", got.Problems[0].Path())
			assert.Contains(t, got.Problems[0].Message, tt.want)
			assert.Empty(t, got.Sentence)

			_, err = r.svc.Save(t.Context(), draft(tt.name, s))

			var problems *autopilot.ProblemsError

			require.ErrorAs(t, err, &problems, "Save refuses what Describe finds")
		})
	}

	s := agentSpec()
	s.Steps[0].Agent.Prompt = spec.PromptRef{Nickname: "triage"}

	got, err := r.svc.Describe(t.Context(), draft("Any item", s))
	require.NoError(t, err)
	assert.Empty(t, got.Problems, "a nickname matches ignoring case, and a prompt for any item fits")

	issues := agentSpec()
	issues.Trigger = spec.Trigger{Event: catalog.EventAppears}
	issues.Watch.Sources = []spec.Source{{Search: &watch.Search{Kind: item.KindIssue, Where: []string{watch.Me}}}}

	only, err := r.svc.Describe(t.Context(), draft("Issues", issues))
	require.NoError(t, err)
	require.Len(t, only.Problems, 1)
	assert.Equal(t, `"Fix checks" is written for pull requests, and this routine acts only on issues: `+
		"pick a prompt for issues", only.Problems[0].Message, "a routine of issues alone acts on nothing else")
}

func TestAPromptLibraryThatCantBeReadChecksNothing(t *testing.T) {
	t.Parallel()

	r := libraryRig(t, library{err: errLocked}, nil)

	s := agentSpec()
	s.Steps[0].Agent.Prompt = spec.PromptRef{Nickname: "Gone"}

	got, err := r.svc.Describe(t.Context(), draft("Locked", s))
	require.NoError(t, err)
	assert.Empty(t, got.Problems, "a library that can't be read refuses nothing")
	assert.Contains(t, r.logs.String(), "could not read the prompt library")
}

func TestDescribeWarnsOfWhatMayDoLessThanMeant(t *testing.T) {
	t.Parallel()

	snooze := searchSpec(catalog.EventChecksPassed, orgSearch)
	snooze.Steps = []spec.Step{{Snooze: &spec.SnoozeStep{}}}

	r := libraryRig(t, library{}, nil)

	got, err := r.svc.Describe(t.Context(), draft("Snooze", snooze))
	require.NoError(t, err)
	require.Len(t, got.Warnings, 1)
	assert.Equal(t, "steps[0]", got.Warnings[0].Path())
	assert.Contains(t, got.Warnings[0].Message, "inbox lists")
	assert.NotEmpty(t, got.Sentence, "a warning keeps nothing from being saved")

	terminal := agentSpec()

	// A build that ran no agent would hold the routine in Dry run.
	held := libraryRig(t, library{}, &autopilot.Offers{Steps: []catalog.StepKind{catalog.StepPin}}).svc

	dry, err := held.Describe(t.Context(), autopilot.Draft{Name: "Held", Mode: autopilotprefs.ModeAuto, Spec: terminal})
	require.NoError(t, err)
	assert.Empty(t, dry.Warnings, "a routine held in Dry run starts nothing on its own")

	open, err := r.svc.Describe(t.Context(), autopilot.Draft{Name: "Open", Mode: autopilotprefs.ModeAuto, Spec: terminal})
	require.NoError(t, err)
	require.Len(t, open.Warnings, 1)
	assert.Equal(t, "steps[0].agent.where", open.Warnings[0].Path())

	offered := libraryRig(t, library{}, &autopilot.Offers{
		Steps: []catalog.StepKind{catalog.StepAgent}, Places: []catalog.Where{catalog.WhereBackground},
	})

	fenced := backgroundSpec()
	fenced.Steps[0].Agent.Fence = &spec.Fence{Tools: catalog.PresetProfile}

	own, err := offered.svc.Describe(t.Context(), autopilot.Draft{Name: "Own", Mode: autopilotprefs.ModeAuto, Spec: fenced})
	require.NoError(t, err)
	require.Len(t, own.Warnings, 1)
	assert.Equal(t, "steps[0].agent.fence.tools", own.Warnings[0].Path())

	asks, err := offered.svc.Describe(t.Context(), autopilot.Draft{Name: "Asks", Mode: autopilotprefs.ModeAsk, Spec: fenced})
	require.NoError(t, err)
	assert.Empty(t, asks.Warnings, "Ask first waits for the user")
}

func TestTestSaysWhatTheHistoryHoldsOfEachMatch(t *testing.T) {
	t.Parallel()

	q := query(t, orgSearch)
	listed := failingPR(8, 80)
	listed.InAuthored = true
	items := &fakeItems{
		searches: map[string]autopilot.SearchResult{q.Text: {IDs: []int64{7, 8, 9}, Count: 340, Truncated: true}},
		rows:     map[int64]facts.Row{7: failingPR(7, 70), 8: listed, 9: failingPR(9, 90)},
		owners:   map[string]bool{"acme-org": true},
	}
	r := itemsRig(t, items)

	s := searchSpec(catalog.EventChecksFailed, orgSearch)
	s.Trigger.WaitForChecks = new(false)
	s.Steps = []spec.Step{{Snooze: &spec.SnoozeStep{}}}

	searchesAlone, err := r.svc.Test(t.Context(), draft("Searches alone", s))
	require.NoError(t, err)
	assert.Empty(t, searchesAlone.Warnings, "Describe warns of triage over searches alone, so Test says it once")

	s.Watch.Sources = append(s.Watch.Sources, spec.Source{Lists: []catalog.ListSource{catalog.SourceAssigned}})
	saved := r.save(t, draft("Snooze failing", s))
	d := draft("Snooze failing", s)
	d.ID = saved.ID

	first, err := r.svc.Test(t.Context(), d)
	require.NoError(t, err)
	require.Len(t, first.Items, 3)

	for _, it := range first.Items {
		assert.Empty(t, it.Record, "nothing is recorded yet, so each match is one it would run on")
	}

	require.Len(t, first.Searches[0].Warnings, 1)
	assert.Contains(t, first.Searches[0].Warnings[0], "340 items", "a search past a page says only the newest are checked")
	require.Len(t, first.Warnings, 1)
	assert.Contains(t, first.Warnings[0].Message, "2 of the 3 items", "most of its items aren't in the inbox lists")

	byID := map[int64]autopilot.TestItem{}
	for _, it := range first.Items {
		byID[it.ID] = it
	}

	assert.True(t, byID[8].Listed)
	assert.False(t, byID[7].Listed)

	now := startedAt()

	// The ledger's matches name stored items.
	testutil.Exec(t, r.db, `INSERT INTO accounts (id, login, created_at, updated_at) VALUES (1, ?, ?, ?)`, viewer, now, now)
	testutil.Exec(t, r.db, `INSERT INTO repos (id, owner, name, created_at, updated_at)
		VALUES (1, 'acme-org', 'example-repo', ?, ?)`, now, now)

	for _, id := range []int64{7, 8, 9} {
		testutil.Exec(t, r.db, `INSERT INTO items (id, account_id, repo_id, kind, number, item_created_at, item_updated_at,
			fetched_at) VALUES (?, 1, 1, 'pr', ?, ?, ?, ?)`, id, id*10, now, now, now)
	}

	for _, rec := range []struct {
		item          int64
		state, reason string
		dry           bool
	}{{7, "done", "ran", false}, {8, "closed", "merged", false}, {9, "done", "would_run", true}} {
		testutil.Exec(t, r.db, `INSERT INTO autopilot_matches (routine_id, item_id, repo_owner, repo_name, kind, number,
			fire_key, dry_run, state, reason, created_at, updated_at, decided_at) VALUES (?, ?, 'acme-org', 'example-repo',
			'pr', 1, ?, ?, ?, ?, ?, ?, ?)`, saved.ID, rec.item, byID[rec.item].FireKey, rec.dry, rec.state, rec.reason,
			now, now, now)
	}

	again, err := r.svc.Test(t.Context(), d)
	require.NoError(t, err)

	for _, it := range again.Items {
		switch it.ID {
		case 7:
			assert.Equal(t, "It already ran on this.", it.Record)
		case 8:
			assert.Equal(t, "Closed: it merged.", it.Record, "a closed record says why in Activity's words")
		case 9:
			assert.Contains(t, it.Record, "Dry run", "a Dry run's record is said apart, since it uses nothing up")
		}
	}

	fresh, err := r.svc.Test(t.Context(), draft("New", s))
	require.NoError(t, err)

	for _, it := range fresh.Items {
		assert.Empty(t, it.Record, "a new routine has no history")
	}
}
