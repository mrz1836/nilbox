package describeit

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// conversationWith keeps a conversation whose one turn, done, made the proposals ps, each
// written as the tool mode writes it, and returns its ID and the proposals' IDs.
func (r *rig) conversationWith(ps ...kit.Proposal) (string, []string) {
	r.t.Helper()

	rec := newRecord(textutil.NewUUID(), AgentChoice{}, Start{}, testNow)
	ids := make([]string, 0, len(ps))

	for i, p := range ps {
		ids = append(ids, r.writeProposal(rec.ID, testNow, byte(i), p))
	}

	rec.Turns = []turnRecord{{N: 1, Text: pinText, State: TurnDone, StartedAt: testNow, EndedAt: testNow, Proposals: ids}}
	require.NoError(r.t, r.svc.saveRecord(rec))

	return rec.ID, ids
}

// fullProposal proposes a new prompt, a new routine that runs it, a change to Nightly, a
// settings change, and a rules change.
func fullProposal() kit.Proposal {
	p := pinProposal()
	p.Prompts = []kit.NewPrompt{{
		Name: "Fix failing checks, tests first", Nickname: "Tests first", Purpose: kit.PurposeAgent,
		AppliesTo: kit.AppliesToPR, Body: "Fix the failing checks on {{.URL}}, starting with the tests.",
	}}
	p.Routines = append(p.Routines, kit.RoutineChange{
		ID: 7, Base: routineUpdated.Format(time.RFC3339Nano), Name: "Nightly", Spec: checksFailSpec(notifyStep(), pinStep()),
	})
	p.Rules = &kit.RulesChange{Trust: []string{"renovate[bot]"}}

	return p
}

func TestACardIsItsProposalCheckedAgainstTheLiveSetup(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, ids := r.conversationWith(fullProposal())

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, id, conv.ID, "the conversation kept is found as the current one")
	require.Len(t, conv.Turns, 1)
	require.Len(t, conv.Turns[0].Cards, 1)

	card := conv.Turns[0].Cards[0]
	assert.Equal(t, CardRef{Source: id, Proposal: ids[0]}, card.Ref)
	assert.Equal(t, testNow, card.At)
	assert.Empty(t, card.Where)
	assert.Equal(t, fullProposal().Summary, card.Summary)
	assert.Equal(t, fullProposal().Routines[1].Base, card.Proposal.Routines[1].Base, "the proposal as the agent wrote it")

	checked := card.Checked
	require.Len(t, checked.Prompts, 1)
	require.Len(t, checked.Routines, 2)
	assert.Equal(t, "Pin failing PRs", checked.Routines[0].Name)
	require.NotNil(t, checked.Routines[1].Before, "a change shows the routine as it is")
	assert.Equal(t, "Nightly", checked.Routines[1].Before.Name)
	assert.False(t, checked.Routines[1].Stale)
	require.NotNil(t, checked.Settings)
	require.NotNil(t, checked.Rules)
	assert.NotEmpty(t, checked.Rules.Risks, "a newly trusted bot is a risk the card shows")
	assert.Empty(t, card.Outcomes)
	assert.False(t, card.Settled)

	reads := r.setup.count()

	_, err = r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, reads, r.setup.count(), "the live snapshot is kept a while")

	changed := testSnapshot()
	changed.Setup.Routines[0].UpdatedAt = routineUpdated.Add(time.Hour)
	changed.Setup.Routines = append(changed.Setup.Routines, kit.Routine{
		ID: 8, Name: "Pin failing PRs", Mode: autopilotprefs.ModeAsk, Spec: checksFailSpec(pinStep()), UpdatedAt: testNow,
	})
	r.setup.change(changed)
	NewWorker(r.svc).Changed()

	conv, err = r.svc.Conversation(t.Context())
	require.NoError(t, err)

	checked = conv.Turns[0].Cards[0].Checked
	assert.True(t, checked.Routines[0].NameTaken, "a card says what applying it does now")
	assert.Equal(t, "Pin failing PRs 2", checked.Routines[0].Name)
	assert.True(t, checked.Routines[1].Stale, "and that a routine changed since the agent read it")
}

func TestAPartDealtWithShowsAsItWasDealtWith(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, ids := r.conversationWith(fullProposal())
	ref := CardRef{Source: id, Proposal: ids[0]}

	for _, s := range []SettleRequest{
		{Part: kit.PartPrompt, Kind: kit.OutcomeAdded, Words: "Added “Tests first” to your prompts"},
		{Part: kit.PartRoutine, Kind: kit.OutcomeAdded, Words: "Added “Pin failing PRs”, switched off", RoutineID: 8},
		{Part: kit.PartRoutine, Index: 1, Kind: kit.OutcomeSaved, Words: "Saved the changes to “Nightly”", RoutineID: 7},
		{Part: kit.PartSettings, Kind: kit.OutcomeApplied, Words: "Applied the settings change"},
		{Part: kit.PartRules, Kind: kit.OutcomeApplied, Words: "Applied the rules change"},
	} {
		s.Ref = ref
		require.NoError(t, r.svc.Settle(t.Context(), s))
	}

	// The setup as applying every part left it.
	applied := testSnapshot()
	applied.Setup.Routines[0].Spec, applied.Setup.Routines[0].UpdatedAt = checksFailSpec(notifyStep(), pinStep()), testNow
	applied.Setup.Routines = append(applied.Setup.Routines, kit.Routine{
		ID: 8, Name: "Pin failing PRs", Mode: autopilotprefs.ModeAsk, Spec: checksFailSpec(pinStep()), UpdatedAt: testNow,
	})
	applied.Setup.Prompts = append(applied.Setup.Prompts, kit.Prompt{
		Nickname: "Tests first", Name: "Fix failing checks, tests first", Purpose: kit.PurposeAgent,
		AppliesTo: kit.AppliesToPR,
	})
	applied.Setup.Settings.AgentsAtOnce = 1
	applied.Setup.Rules.TrustedBots = []string{"dependabot", "renovate"}
	r.setup.change(applied)
	NewWorker(r.svc).Changed()

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)

	card := conv.Turns[0].Cards[0]
	require.True(t, card.Settled)

	checked := card.Checked
	assert.Empty(t, checked.Prompts[0].Problems, "an added prompt never finds its own nickname taken")
	assert.Equal(t, "Pin failing PRs", checked.Routines[0].Name, "an added routine goes by its name, never its own name taken")
	assert.False(t, checked.Routines[0].NameTaken)
	assert.False(t, checked.Routines[1].Stale, "a saved change never reads as its routine changed since")
	assert.Equal(t, []kit.SettingChange{{Key: kit.SettingAgentsAtOnce, Label: "Agents at once", After: "1"}},
		checked.Settings.Changes, "applied settings say what they set, never that they change nothing")
	assert.Empty(t, checked.Settings.Problems)
	assert.Empty(t, checked.Settings.Risks)
	assert.Equal(t, []kit.RuleChange{{Kind: kit.RuleTrust, Target: "renovate", Words: "Trust renovate[bot]"}},
		checked.Rules.Changes, "and applied rules too")
	assert.Empty(t, checked.Rules.Problems)
	assert.Empty(t, checked.Rules.Risks, "a part dealt with has nothing left to warn of")

	renamed := applied
	renamed.Setup.Routines = append([]kit.Routine{}, applied.Setup.Routines...)
	renamed.Setup.Routines[1].Name = "Pinned PRs"
	r.setup.change(renamed)
	NewWorker(r.svc).Changed()

	conv, err = r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Pinned PRs", conv.Turns[0].Cards[0].Checked.Routines[0].Name,
		"a routine a part added goes by its name now")
}

func TestAPartDiscardedGoesByTheNameProposed(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, ids := r.conversationWith(pinProposal())

	require.NoError(t, r.svc.Settle(t.Context(), SettleRequest{
		Ref: CardRef{Source: id, Proposal: ids[0]}, Part: kit.PartRoutine, Kind: kit.OutcomeBuilder,
		Words: "Opened “Pin failing PRs” in the builder",
	}))

	// The builder saved it under the name proposed, which a check now finds taken.
	saved := testSnapshot()
	saved.Setup.Routines = append(saved.Setup.Routines, kit.Routine{
		ID: 8, Name: "Pin failing PRs", Mode: autopilotprefs.ModeAsk, Spec: checksFailSpec(pinStep()), UpdatedAt: testNow,
	})
	r.setup.change(saved)
	NewWorker(r.svc).Changed()

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)

	routine := conv.Turns[0].Cards[0].Checked.Routines[0]
	assert.Equal(t, "Pin failing PRs", routine.Name, "never the free name a save would give it now")
	assert.False(t, routine.NameTaken)
}

func TestACardsListsAreNeverNull(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	p := pinProposal()
	p.Routines[0].Spec.Steps = nil
	r.conversationWith(p)

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)

	card := conv.Turns[0].Cards[0]
	assert.NotNil(t, card.Proposal.Routines[0].Spec.Steps, "a list the agent left out is [] for the window")
	assert.NotNil(t, card.Proposal.Routines[0].Spec.Conditions.Of)
	assert.NotNil(t, card.Checked.Routines[0].Problems)
	assert.NotEmpty(t, card.Checked.Routines[0].Problems, "a routine with no steps can't be saved")
}

func TestACardIsReadOnlyAsTheToolModeWritesIt(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, ids := r.conversationWith(pinProposal())
	dir := r.path("proposals", id)
	good := filepath.Join(dir, ids[0]+".json")

	at := testNow.Add(time.Minute)
	other := textutil.NewUUID()

	write := func(n byte, data []byte) string {
		pid := proposalID(t, at, n)
		require.NoError(t, os.WriteFile(filepath.Join(dir, pid+".json"), data, 0o600))

		return pid
	}

	link := proposalID(t, at, 1)
	require.NoError(t, os.Symlink(good, filepath.Join(dir, link+".json")))

	pipe := proposalID(t, at, 2)
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, pipe+".json"), 0o600))

	misnamed := write(3, proposalFile(t, id, proposalID(t, at, 9), at, pinProposal()))
	elsewhere := write(4, proposalFile(t, other, proposalID(t, at, 5), at, pinProposal()))
	broken := write(6, []byte(`{"version":1,"id":`))
	large := write(7, []byte(strings.Repeat(" ", kit.MaxProposalFileBytes+1)))

	_, err := r.svc.readProposal(id, ids[0])
	require.NoError(t, err)

	for name, pid := range map[string]string{
		"a link": link, "a pipe": pipe, "one whose ID isn't its name": misnamed, "another conversation's": elsewhere,
		"one that isn't JSON": broken, "one too large": large,
	} {
		_, err = r.svc.readProposal(id, pid)
		require.Error(t, err, "%s is refused", name)
	}

	_, err = r.svc.readProposal(id, ids[0]+"x")
	require.Error(t, err, "a name the tool mode doesn't make is never read")

	_, err = r.svc.readProposal("../"+id, ids[0])
	require.Error(t, err, "nor a folder that isn't a conversation's")

	_, err = r.svc.readProposal(id, link)
	require.ErrorIs(t, err, paths.ErrNotRegular)

	outside := r.writeProposal(Outside, at, 8, pinProposal())
	require.NoError(t, os.Rename(r.path("proposals", Outside, outside+".json"), filepath.Join(dir, outside+".json")))

	_, err = r.svc.readProposal(id, outside)
	require.ErrorIs(t, err, errMisplaced, "an outside proposal in a conversation's folder isn't its")

	inside := r.writeProposal(id, at, 10, pinProposal())
	require.NoError(t, os.MkdirAll(r.path("proposals", Outside), 0o700))
	require.NoError(t, os.Rename(filepath.Join(dir, inside+".json"), r.path("proposals", Outside, inside+".json")))

	_, err = r.svc.readProposal(Outside, inside)
	require.ErrorIs(t, err, errMisplaced, "nor a conversation's in the outside folder")

	rec, err := r.svc.readRecord(id)
	require.NoError(t, err)

	rec.Turns[0].Proposals = append(rec.Turns[0].Proposals, link, misnamed, broken)
	require.NoError(t, r.svc.saveRecord(rec))

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	require.Len(t, conv.Turns[0].Cards, 1, "a card that can't be read isn't shown")
	assert.Equal(t, ids[0], conv.Turns[0].Cards[0].Ref.Proposal)
}

func TestSettleRecordsWhatTheUserDidAndTheFirstOutcomeWins(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, ids := r.conversationWith(pinProposal())
	ref := CardRef{Source: id, Proposal: ids[0]}

	require.NoError(t, r.svc.Settle(t.Context(), SettleRequest{
		Ref: ref, Part: kit.PartRoutine, Kind: kit.OutcomeAdded, Words: "Added “Pin failing PRs”,\nswitched off",
		RoutineID: 12,
	}))
	assert.Equal(t, []event.DescribeItUpdated{{ConversationID: id, Turn: 1}}, r.sink.updates(),
		"the window hears of the card's turn")

	require.NoError(t, r.svc.Settle(t.Context(), SettleRequest{Ref: ref, Part: kit.PartRoutine, Kind: kit.OutcomeDiscarded}))
	assert.Len(t, r.sink.updates(), 1, "a part settled already keeps its first outcome, quietly")

	require.NoError(t, r.svc.Settle(t.Context(), SettleRequest{
		Ref: ref, Part: kit.PartSettings, Kind: kit.OutcomeApplied, RoutineID: 5,
	}))

	o, err := r.svc.readOutcomes(id)
	require.NoError(t, err)
	assert.Equal(t, []kit.Outcome{
		{
			Proposal: ids[0], Part: kit.PartRoutine, Kind: kit.OutcomeAdded, Words: "Added “Pin failing PRs”, switched off",
			RoutineID: 12, At: testNow,
		},
		{Proposal: ids[0], Part: kit.PartSettings, Kind: kit.OutcomeApplied, Words: "Applied the settings change", At: testNow},
	}, o.Items, "the words are one line, and a routine's ID is a routine part's alone")

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)

	card := conv.Turns[0].Cards[0]
	assert.True(t, card.Settled)
	assert.Len(t, card.Outcomes, 2)
}

func TestSettleRefusesWhatIsntAPartOfACard(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, ids := r.conversationWith(pinProposal())
	ref := CardRef{Source: id, Proposal: ids[0]}

	for name, req := range map[string]SettleRequest{
		"another folder":    {Ref: CardRef{Source: "../x", Proposal: ids[0]}, Part: kit.PartRoutine, Kind: kit.OutcomeAdded},
		"no such proposal":  {Ref: CardRef{Source: id, Proposal: proposalID(t, testNow, 20)}, Part: kit.PartRoutine},
		"a part it lacks":   {Ref: ref, Part: kit.PartPrompt, Kind: kit.OutcomeAdded},
		"an index past it":  {Ref: ref, Part: kit.PartRoutine, Index: 3, Kind: kit.OutcomeAdded},
		"the rules it lack": {Ref: ref, Part: kit.PartRules, Kind: kit.OutcomeApplied},
		"applied, routine":  {Ref: ref, Part: kit.PartRoutine, Kind: kit.OutcomeApplied},
		"saved, settings":   {Ref: ref, Part: kit.PartSettings, Kind: kit.OutcomeSaved},
		"no kind at all":    {Ref: ref, Part: kit.PartSettings, Kind: "maybe"},
		"no part at all":    {Ref: ref, Part: "everything", Kind: kit.OutcomeDiscarded},
	} {
		err := r.svc.Settle(t.Context(), req)
		require.Error(t, err, name)
		assert.NotEmpty(t, uierr.Message(err), name)
	}

	o, err := r.svc.readOutcomes(id)
	require.NoError(t, err)
	assert.Empty(t, o.Items, "nothing refused is recorded")
	assert.Empty(t, r.sink.updates())
}

func TestAnOutcomesWordsAreItsOwnWhenTheWindowGivesNone(t *testing.T) {
	t.Parallel()

	p := fullProposal()

	for _, tt := range []struct {
		part  kit.Part
		index int
		kind  kit.OutcomeKind
		given string
		want  string
	}{
		{kit.PartRoutine, 0, kit.OutcomeAdded, "", "Added the routine “Pin failing PRs”"},
		{kit.PartRoutine, 1, kit.OutcomeSaved, "", "Saved the change to the routine “Nightly”"},
		{kit.PartRoutine, 0, kit.OutcomeBuilder, "", "Opened the routine “Pin failing PRs” in the builder to finish it there"},
		{kit.PartPrompt, 0, kit.OutcomeAdded, "  ", "Added the prompt “Tests first”"},
		{kit.PartSettings, 0, kit.OutcomeDiscarded, "", "Discarded the settings change"},
		{kit.PartRules, 0, kit.OutcomeApplied, "", "Applied the rules change"},
		{kit.PartRules, 0, kit.OutcomeApplied, "Trusted renovate", "Trusted renovate"},
		{kit.PartRules, 0, kit.OutcomeApplied, "Trusted \U000E0041renovate", "Applied the rules change"},
		{kit.PartRules, 0, kit.OutcomeApplied, strings.Repeat("a", kit.MaxOutcomeRunes+10), strings.Repeat("a", kit.MaxOutcomeRunes-1) + "…"},
	} {
		assert.Equal(t, tt.want, outcomeWords(tt.given, p, tt.part, tt.index, tt.kind), "%s %d %s %q", tt.part, tt.index,
			tt.kind, tt.given)
	}
}

func TestDiscardProposalDiscardsWhatIsLeftOfACard(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	at := testNow.Add(-time.Hour)
	id := r.writeProposal(Outside, at, 0, pinProposal())
	ref := CardRef{Source: Outside, Proposal: id}

	require.NoError(t, r.svc.Settle(t.Context(), SettleRequest{Ref: ref, Part: kit.PartRoutine, Kind: kit.OutcomeAdded}))
	assert.Equal(t, 1, r.sink.proposalsEvents(), "a card from outside tells the outside cards' listeners")

	require.NoError(t, r.svc.DiscardProposal(t.Context(), ref))
	assert.Equal(t, 2, r.sink.proposalsEvents())

	o, err := r.svc.readOutcomes(Outside)
	require.NoError(t, err)
	require.Len(t, o.Items, 2)
	assert.Equal(t, kit.OutcomeAdded, o.Items[0].Kind, "what the user did first stays")
	assert.Equal(t, kit.Outcome{
		Proposal: id, Part: kit.PartSettings, Kind: kit.OutcomeDiscarded, Words: "Discarded the settings change", At: testNow,
	}, o.Items[1])

	require.NoError(t, r.svc.DiscardProposal(t.Context(), ref))
	assert.Equal(t, 2, r.sink.proposalsEvents(), "a card with nothing left to discard changes nothing")

	err = r.svc.DiscardProposal(t.Context(), CardRef{Source: Outside, Proposal: proposalID(t, at, 3)})
	require.Error(t, err)
	assert.Equal(t, "That card isn't there any more.", uierr.Message(err))
}

func TestOutcomesThatCantBeReadAreWrittenOver(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, ids := r.conversationWith(pinProposal())
	require.NoError(t, os.WriteFile(r.path("proposals", id, "outcomes.json"), []byte("not json"), 0o600))

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Empty(t, conv.Turns[0].Cards[0].Outcomes, "outcomes that can't be read are none")

	require.NoError(t, r.svc.Settle(t.Context(), SettleRequest{
		Ref: CardRef{Source: id, Proposal: ids[0]}, Part: kit.PartRoutine, Kind: kit.OutcomeDiscarded,
	}))

	o, err := r.svc.readOutcomes(id)
	require.NoError(t, err)
	assert.Len(t, o.Items, 1)
}
