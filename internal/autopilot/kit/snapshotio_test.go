package kit_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

func TestASnapshotIsStampedAndItsNamesKeptAsNames(t *testing.T) {
	t.Parallel()

	routines := []kit.Routine{{ID: 1, Name: "Fix\nchecks\u202e"}}
	prompts := []kit.Prompt{{Nickname: "Tri\tage", Name: "Triage\u200e it", Starter: fixChecksKey}}
	scripts := []kit.Script{{ID: 3, Name: "Lint\nall", Shows: "every clone"}}
	profiles := []kit.Profile{{Agent: "claude", Name: "work\x00"}}
	labels := []string{"bug", "BUG", "needs\nrepro"}
	local := time.FixedZone("here", 3*60*60)

	s := kit.NewSnapshot(kit.Setup{
		Routines: routines, Prompts: prompts, Scripts: scripts, Profiles: profiles,
		Settings: autopilotprefs.Defaults(), Rules: kit.Rules{TrustedBots: []string{dependabotLogin}},
	}, kit.InboxNames{Labels: labels}, kit.Vocabulary{
		Templates: []kit.Template{{Key: "fix-failing-checks-on-my-prs"}},
	}, time.Date(2026, time.November, 20, 8, 0, 0, 0, local))

	assert.Equal(t, kit.SnapshotVersion, s.Version)
	assert.Equal(t, time.Date(2026, time.November, 20, 5, 0, 0, 0, time.UTC), s.WrittenAt, "written at now, in UTC")
	assert.Equal(t, time.UTC, s.WrittenAt.Location())

	assert.Equal(t, "Fix checks", s.Setup.Routines[0].Name)
	assert.Equal(t, "Tri age", s.Setup.Prompts[0].Nickname)
	assert.Equal(t, "Triage it", s.Setup.Prompts[0].Name)
	assert.Equal(t, fixChecksKey, s.Setup.Prompts[0].Starter, "a starter's key is no name")
	assert.Equal(t, "Lint all", s.Setup.Scripts[0].Name)
	assert.Equal(t, "every clone", s.Setup.Scripts[0].Shows)
	assert.Equal(t, "work", s.Setup.Profiles[0].Name)
	assert.Equal(t, "claude", s.Setup.Profiles[0].Agent)
	assert.Equal(t, []string{"bug", "needs repro"}, s.Inbox.Labels)
	assert.Equal(t, []string{dependabotLogin}, s.Setup.Rules.TrustedBots, "the rules are kept as given")

	assert.Equal(t, "Fix\nchecks\u202e", routines[0].Name, "nothing given is changed")
	assert.Equal(t, "Tri\tage", prompts[0].Nickname)
	assert.Equal(t, "Lint\nall", scripts[0].Name)
	assert.Equal(t, "work\x00", profiles[0].Name)

	for name, list := range map[string]int{
		"repositories": len(s.Inbox.Repositories), "owners": len(s.Inbox.Owners), "people": len(s.Inbox.People),
		"bots": len(s.Inbox.Bots), "checks": len(s.Inbox.Checks),
	} {
		assert.Zero(t, list, name)
	}

	assert.NotNil(t, s.Inbox.Repositories, "every list is there")
	assert.NotNil(t, s.Setup.Rules.Pinned)
	assert.NotNil(t, s.Setup.Rules.Muted)
	assert.NotNil(t, s.Setup.Rules.IgnoredWorkflows)
	assert.NotNil(t, s.Vocabulary.Templates[0].Blanks)
	assert.NotNil(t, s.Vocabulary.Starters)
	assert.NotNil(t, s.Vocabulary.Variables)
}

func TestASnapshotReadsBackAsItself(t *testing.T) {
	t.Parallel()

	s := checkSnapshot()

	data, err := kit.EncodeSnapshot(s)
	require.NoError(t, err)

	got, err := kit.DecodeSnapshot(data)
	require.NoError(t, err)

	assert.Equal(t, s.WrittenAt, got.WrittenAt)
	assert.Equal(t, s.Setup.Routines[0].UpdatedAt, got.Setup.Routines[0].UpdatedAt, "to the nanosecond")
	assert.Equal(t, s.Setup.Settings, got.Setup.Settings)
	assert.Equal(t, s.Inbox, got.Inbox)
	assert.Equal(t, s.Vocabulary.Variables, got.Vocabulary.Variables)
	assert.Equal(t, spec.Normalize(s.Setup.Routines[1].Spec), spec.Normalize(got.Setup.Routines[1].Spec))

	again, err := kit.EncodeSnapshot(got)
	require.NoError(t, err)
	assert.Equal(t, string(data), string(again), "it encodes as it did")
	assert.NotContains(t, string(data), `<`, "its text is written as it is")
}

func TestADecodedSnapshotHasEveryList(t *testing.T) {
	t.Parallel()

	got, err := kit.DecodeSnapshot([]byte(`{"version": 2, "writtenAt": "2026-11-20T05:31:03Z",
		"setup": {"routines": null, "prompts": null, "scripts": null, "profiles": null,
			"settings": {"enabled": false, "paused": false, "defaultMode": "ask", "agentsAtOnce": 2, "runsPerDay": 20,
				"actionsPerDay": 50, "dailyCap": {"amount": 0, "unit": "usd"}, "keepAwake": false, "holdOnOutage": true,
				"catchUpHours": 24, "historyDays": 90, "logDays": 30},
			"rules": {"trustedBots": null, "pinned": null, "muted": null, "ignoredWorkflows": null}},
		"inbox": {"repositories": null, "owners": null, "people": null, "bots": null, "labels": null, "checks": null},
		"vocabulary": {"templates": [{"key": "a", "name": "A", "icon": "", "summary": "", "suggests": "ask",
			"blanks": null, "spec": {"version": 1, "watch": {"sources": null}, "trigger": {"event": "appears"},
				"conditions": {"match": "all", "of": null}, "steps": null, "limits": {}}}],
			"starters": null, "variables": null}}`))
	require.NoError(t, err)

	assert.NotNil(t, got.Setup.Routines)
	assert.NotNil(t, got.Setup.Prompts)
	assert.NotNil(t, got.Setup.Scripts)
	assert.NotNil(t, got.Setup.Profiles)
	assert.NotNil(t, got.Setup.Rules.TrustedBots)
	assert.NotNil(t, got.Setup.Rules.Pinned)
	assert.NotNil(t, got.Setup.Rules.Muted)
	assert.NotNil(t, got.Setup.Rules.IgnoredWorkflows)
	assert.NotNil(t, got.Inbox.Repositories)
	assert.NotNil(t, got.Inbox.Owners)
	assert.NotNil(t, got.Inbox.People)
	assert.NotNil(t, got.Inbox.Bots)
	assert.NotNil(t, got.Inbox.Labels)
	assert.NotNil(t, got.Inbox.Checks)
	assert.NotNil(t, got.Vocabulary.Templates[0].Blanks)
	assert.NotNil(t, got.Vocabulary.Starters)
	assert.NotNil(t, got.Vocabulary.Variables)
}

func TestASnapshotOfAnotherVersionIsWrittenAgainAsTheAppStarts(t *testing.T) {
	t.Parallel()

	_, err := kit.DecodeSnapshot([]byte(`{"version": 3, "somethingNew": true}`))
	require.ErrorIs(t, err, kit.ErrSnapshotVersion, "a newer one says so, not that its keys are unknown")
	require.EqualError(t, err, "the snapshot is of another version: it is version 3, and this build reads version 2; "+
		"the app writes it again as it starts")

	_, err = kit.DecodeSnapshot([]byte(`{"version": "1"}`))
	require.ErrorIs(t, err, kit.ErrSnapshotVersion, "a version is a number")
}

func TestASnapshotIsReadStrictly(t *testing.T) {
	t.Parallel()

	for data, want := range map[string]string{
		`{"setup": {}}`:                 "it has no version",
		`{"version": null}`:             "it has no version",
		`[1]`:                           "cannot unmarshal",
		`{"version": 2, "Setup": {}}`:   `a key isn't one the format has, spelled exactly: "Setup"`,
		`{"version": 2, "version": 2}`:  `a key is given twice: "version"`,
		`{"version": 2} {}`:             "more after the document",
		`{"version": 2, "inbox": "no"}`: "cannot unmarshal",
		`{"version": 2, "setup": {"routines": [{"spec": {"watch": {"sources": [{"search": {"where": ` +
			strings.Repeat("[", 20) + strings.Repeat("]", 20) + `}}]}}}]}}`: "it nests too deep",
	} {
		_, err := kit.DecodeSnapshot([]byte(data))
		require.ErrorIs(t, err, kit.ErrUnreadableSnapshot, data)
		require.ErrorContains(t, err, want, data)
	}

	_, err := kit.DecodeSnapshot([]byte(`{"version": 2, "x": "` + strings.Repeat("a", kit.MaxSnapshotBytes) + `"}`))
	require.ErrorIs(t, err, kit.ErrUnreadableSnapshot)
	require.ErrorContains(t, err, "it is larger than")
}

func TestASnapshotTooLargeToReadIsNeverWritten(t *testing.T) {
	t.Parallel()

	s := checkSnapshot()
	s.Setup.Routines[0].Description = strings.Repeat("a", kit.MaxSnapshotBytes)

	_, err := kit.EncodeSnapshot(s)
	require.Error(t, err)
	require.ErrorContains(t, err, "encode the snapshot: it is larger than")
}
