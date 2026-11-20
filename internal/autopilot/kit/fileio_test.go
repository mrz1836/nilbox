package kit_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
)

// shortRandom is a source of random bytes that runs dry.
type shortRandom struct{}

func (shortRandom) Read([]byte) (int, error) {
	return 0, errors.New("no randomness here") //nolint:err113 // a test's failing reader
}

func TestAProposalIDIsItsMomentAndRandomHex(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.November, 20, 8, 31, 3, 999, time.FixedZone("here", 3*60*60))

	id, err := kit.NewProposalID(at, bytes.NewReader([]byte{0x9f, 0x2c, 0x41, 0x00, 0xab, 0x31, 0xff}))
	require.NoError(t, err)
	assert.Equal(t, "20261120T053103Z-9f2c4100ab31", id, "in UTC, to the second, and 6 bytes as hex")
	assert.True(t, kit.ValidProposalID(id))

	_, err = kit.NewProposalID(at, bytes.NewReader([]byte{1, 2, 3}))
	require.Error(t, err, "too few random bytes")

	_, err = kit.NewProposalID(at, shortRandom{})
	require.Error(t, err)

	for _, id := range []string{
		"", "20261120T053103Z", "20261120T053103Z-9F2C4100AB31", "20261120T053103-9f2c4100ab31",
		"2026112T053103Z-9f2c4100ab31", "20261120T053103Z-9f2c4100ab3", "20261120T053103Z-9f2c4100ab31.json",
		"../20261120T053103Z-9f2c4100ab31", "٢٠٢٦١١٢٠T053103Z-9f2c4100ab31",
	} {
		assert.False(t, kit.ValidProposalID(id), "%q", id)
	}
}

// proposalFile is a proposal file as the tool mode writes one, in-app.
func proposalFile(t *testing.T) kit.ProposalFile {
	t.Helper()

	p, err := kit.DecodeProposal([]byte(proposalJSON))
	require.NoError(t, err)

	return kit.ProposalFile{
		ID: "20261120T053103Z-9f2c4100ab31", At: time.Date(2026, time.November, 20, 5, 31, 3, 0, time.UTC),
		Conversation: "0b6f3a52-1c2d-4e5f-8a9b-0c1d2e3f4a5b", SnapshotAt: checkedAt, Proposal: p,
	}
}

func TestAProposalFileReadsBackAsItself(t *testing.T) {
	t.Parallel()

	f := proposalFile(t)
	f.At = f.At.In(time.FixedZone("here", 3600))

	data, err := kit.EncodeProposalFile(f)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"version":1`, "at this build's version")
	assert.Contains(t, string(data), `"at":"2026-11-20T05:31:03Z"`, "in UTC")

	got, err := kit.DecodeProposalFile(data)
	require.NoError(t, err)
	assert.Equal(t, kit.ProposalFileVersion, got.Version)
	assert.Equal(t, f.ID, got.ID)
	assert.True(t, f.At.Equal(got.At))
	assert.Equal(t, f.Conversation, got.Conversation)
	assert.True(t, f.SnapshotAt.Equal(got.SnapshotAt))
	assert.Equal(t, f.Proposal, got.Proposal)

	outside := proposalFile(t)
	outside.Conversation, outside.Where = "", "example-repo"

	data, err = kit.EncodeProposalFile(outside)
	require.NoError(t, err)

	got, err = kit.DecodeProposalFile(data)
	require.NoError(t, err)
	assert.Equal(t, "example-repo", got.Where, "one from outside says the folder it came from")
}

func TestAProposalFileIsReadStrictly(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		change func(*kit.ProposalFile)
		want   string
	}{
		"an ID":           {func(f *kit.ProposalFile) { f.ID = "../evil" }, `its ID "../evil" isn't a proposal's`},
		"no moment":       {func(f *kit.ProposalFile) { f.At = time.Time{} }, "it says no moment it was written"},
		"a conversation":  {func(f *kit.ProposalFile) { f.Conversation = "../x" }, `its conversation "../x" isn't one`},
		"both places":     {func(f *kit.ProposalFile) { f.Where = "example-repo" }, "it says both the conversation"},
		"a folder's name": {func(f *kit.ProposalFile) { f.Conversation, f.Where = "", "a\nb" }, "isn't one line of plain text"},
		"a proposal":      {func(f *kit.ProposalFile) { f.Proposal.Summary = "" }, "the proposal can't be read: summary:"},
	} {
		f := proposalFile(t)
		c.change(&f)

		_, err := kit.EncodeProposalFile(f)
		require.ErrorIs(t, err, kit.ErrUnreadableProposalFile, name)
		require.ErrorContains(t, err, c.want, "%s: none is written that can't be read", name)
	}

	good, err := kit.EncodeProposalFile(proposalFile(t))
	require.NoError(t, err)

	for data, want := range map[string]string{
		`{"version": 2, "next": true}`: "it is version 2, and this build reads version 1",
		`{"id": "x"}`:                  "it has no version",
		`{"version": 1, "ID": "x"}`:    `a key isn't one the format has, spelled exactly: "ID"`,
		strings.Replace(string(good), `"id":"20261120T053103Z-9f2c4100ab31"`, `"id":"nope"`, 1): `its ID "nope"`,
		strings.Replace(string(good), `"summary":"`, `"summary":"   `, 1)[:20]:                  "unexpected EOF",
		`{"version": 1, "proposal": {"summary": "x", "settings": {"enabled": true}}}`:           `"enabled"`,
	} {
		_, readErr := kit.DecodeProposalFile([]byte(data))
		require.ErrorIs(t, readErr, kit.ErrUnreadableProposalFile, "%.60s", data)
		require.ErrorContains(t, readErr, want, "%.60s", data)
	}

	_, err = kit.DecodeProposalFile(append([]byte(`{"version": 1, "x": "`), make([]byte, kit.MaxProposalFileBytes)...))
	require.ErrorIs(t, err, kit.ErrUnreadableProposalFile)
	require.ErrorContains(t, err, "it is larger than")
}

func TestAFileThatCantBeWrittenWholeIsRefused(t *testing.T) {
	t.Parallel()

	far := time.Date(10_000, time.January, 1, 0, 0, 0, 0, time.UTC)

	f := proposalFile(t)
	f.At = far
	_, err := kit.EncodeProposalFile(f)
	require.ErrorIs(t, err, kit.ErrUnreadableProposalFile, "a moment JSON can't hold")

	f = proposalFile(t)
	f.Proposal.Routines[0].Name = strings.Repeat("a", kit.MaxProposalFileBytes)
	_, err = kit.EncodeProposalFile(f)
	require.ErrorIs(t, err, kit.ErrUnreadableProposalFile)
	require.ErrorContains(t, err, "it would be larger than", "none is written that can't be read")

	_, err = kit.EncodeOutcomes(kit.Outcomes{Items: []kit.Outcome{{
		Proposal: "20261120T053103Z-9f2c4100ab31", Part: kit.PartRules, Kind: kit.OutcomeApplied, At: far,
	}}})
	require.ErrorIs(t, err, kit.ErrUnreadableOutcomes)

	s := checkSnapshot()
	s.WrittenAt = far
	_, err = kit.EncodeSnapshot(s)
	require.Error(t, err)
	require.ErrorContains(t, err, "encode the snapshot")
}

func TestOutcomesReadBackAsThemselves(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.November, 20, 6, 0, 0, 0, time.UTC)
	o := kit.Outcomes{Items: []kit.Outcome{
		{
			Proposal: "20261120T053103Z-9f2c4100ab31", Part: kit.PartRoutine, Kind: kit.OutcomeAdded,
			Words: "Added “Fix checks”, switched off", RoutineID: 12, At: at,
		},
		{
			Proposal: "20261120T053103Z-9f2c4100ab31", Part: kit.PartSettings, Kind: kit.OutcomeDiscarded, At: at,
			Words: "Discarded the settings change",
		},
	}}

	data, err := kit.EncodeOutcomes(o)
	require.NoError(t, err)

	got, err := kit.DecodeOutcomes(data)
	require.NoError(t, err)
	assert.Equal(t, kit.OutcomesVersion, got.Version)
	assert.Equal(t, o.Items, got.Items)

	data, err = kit.EncodeOutcomes(kit.Outcomes{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"version": 1, "items": []}`, string(data), "no outcome is an empty list")

	got, err = kit.DecodeOutcomes([]byte(`{"version": 1, "items": null}`))
	require.NoError(t, err)
	assert.NotNil(t, got.Items)
}

func TestOutcomesAreReadStrictly(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.November, 20, 6, 0, 0, 0, time.UTC)
	good := kit.Outcome{Proposal: "20261120T053103Z-9f2c4100ab31", Part: kit.PartPrompt, Kind: kit.OutcomeAdded, At: at}

	for name, c := range map[string]struct {
		change func(*kit.Outcome)
		want   string
	}{
		"a proposal": {func(o *kit.Outcome) { o.Proposal = "x" }, `items[0]: "x" isn't a proposal's ID`},
		"a part":     {func(o *kit.Outcome) { o.Part = "everything" }, `"everything" isn't a part of a proposal`},
		"a kind":     {func(o *kit.Outcome) { o.Kind = "merged" }, `"merged" isn't something done with a part`},
		"an index":   {func(o *kit.Outcome) { o.Index = -1 }, "it counts below zero"},
		"a routine":  {func(o *kit.Outcome) { o.RoutineID = -1 }, "it counts below zero"},
		"two lines":  {func(o *kit.Outcome) { o.Words = "a\nb" }, "its words aren't one line of plain text within 300"},
		"long words": {func(o *kit.Outcome) { o.Words = strings.Repeat("a", 301) }, "its words aren't one line"},
		"no moment":  {func(o *kit.Outcome) { o.At = time.Time{} }, "it says no moment it was done"},
	} {
		o := good
		c.change(&o)

		_, err := kit.EncodeOutcomes(kit.Outcomes{Items: []kit.Outcome{o}})
		require.ErrorIs(t, err, kit.ErrUnreadableOutcomes, name)
		require.ErrorContains(t, err, c.want, name)
	}

	for data, want := range map[string]string{
		`{"version": 2}`:                     "it is version 2",
		`{"version": 1, "Items": []}`:        `"Items"`,
		`{"version": 1, "items": [{"x":1}]}`: `"x"`,
		`{"version": 1, "items": [{"proposal": "x", "part": "prompt", "index": 0, "kind": "added", "words": "",
			"at": "2026-11-20T06:00:00Z"}]}`: `isn't a proposal's ID`,
	} {
		_, err := kit.DecodeOutcomes([]byte(data))
		require.ErrorIs(t, err, kit.ErrUnreadableOutcomes, data)
		require.ErrorContains(t, err, want, data)
	}

	_, err := kit.DecodeOutcomes(append([]byte(`{"version": 1, "x": "`), make([]byte, kit.MaxOutcomesBytes)...))
	require.ErrorIs(t, err, kit.ErrUnreadableOutcomes)

	many := make([]kit.Outcome, 0, 20_000)

	for range 20_000 {
		o := good
		o.Words = strings.Repeat("a", 250)
		many = append(many, o)
	}

	_, err = kit.EncodeOutcomes(kit.Outcomes{Items: many})
	require.ErrorIs(t, err, kit.ErrUnreadableOutcomes, "none is written larger than can be read")
}

func TestOutcomeWordsAreOneSentenceForTheNextTurn(t *testing.T) {
	t.Parallel()

	assert.Empty(t, kit.OutcomeWords(nil), "nothing done, nothing said")
	assert.Empty(t, kit.OutcomeWords([]kit.Outcome{{Words: " "}}))

	assert.Equal(t, "Added “Fix checks”, switched off; discarded the settings change; LGTM added to your prompts.",
		kit.OutcomeWords([]kit.Outcome{
			{Words: "Added “Fix checks”, switched off"},
			{Words: ""},
			{Words: "Discarded the settings change."},
			{Words: "LGTM added to your prompts"},
		}), "each outcome's words, joined into one sentence")

	assert.Equal(t, "Saved “Nightly”; opened “Fix it” in the builder.", kit.OutcomeWords([]kit.Outcome{
		{Words: "saved “Nightly”"}, {Words: "Opened “Fix it”\nin the builder"},
	}), "each one line")
}
