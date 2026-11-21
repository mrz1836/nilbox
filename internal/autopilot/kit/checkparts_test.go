package kit_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/heldruns"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/reporules"
)

// agentPrompt is a new agent's prompt for pull requests, nicknamed nick, whose text is
// body.
func agentPrompt(nick, body string) kit.NewPrompt {
	return kit.NewPrompt{Name: "A prompt", Nickname: nick, Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToPR, Body: body}
}

// promptProblemsOf checks one new prompt against the fixtures' snapshot and returns its
// problems.
func promptProblemsOf(t *testing.T, p kit.NewPrompt) []string {
	t.Helper()

	c := kit.Check(t.Context(), checkSnapshot(), kit.Proposal{Summary: "A prompt.", Prompts: []kit.NewPrompt{p}})
	require.Len(t, c.Prompts, 1)
	assert.Equal(t, p, c.Prompts[0].Prompt, "it carries the prompt as proposed")

	return c.Prompts[0].Problems
}

func TestANewPromptIsCheckedAsTheLibraryChecksOne(t *testing.T) {
	t.Parallel()

	assert.Empty(t, promptProblemsOf(t, agentPrompt("Fix flaky", "Fix the flaky check on {{.URL}}, {{$.Title}}.")))

	reply := kit.NewPrompt{
		Name: "Ping", Nickname: "Ping", Purpose: kit.PurposeReply, ReplyAction: kit.ReplyComment,
		AppliesTo: kit.AppliesToIssue, Body: "Any news, {{if .IsPR}}author{{end}}?",
	}
	assert.Empty(t, promptProblemsOf(t, reply), "a comment's reply may be for issues")

	for name, c := range map[string]struct {
		change func(*kit.NewPrompt)
		want   string
	}{
		"no title":             {func(p *kit.NewPrompt) { p.Name = " " }, "Give the prompt a title."},
		"a long title":         {func(p *kit.NewPrompt) { p.Name = strings.Repeat("a", 81) }, "Keep the title to 80 characters."},
		"a title of two lines": {func(p *kit.NewPrompt) { p.Name = "a\nb" }, "Keep the title to one line of plain text."},
		"no nickname":          {func(p *kit.NewPrompt) { p.Nickname = "" }, "Give the prompt a nickname for the launch menu."},
		"a long nickname": {
			func(p *kit.NewPrompt) { p.Nickname = strings.Repeat("a", 21) },
			"Keep the nickname to 20 characters so it fits the launch button.",
		},
		"a hidden nickname": {
			func(p *kit.NewPrompt) { p.Nickname = "Fix\u202eit" }, "Keep the nickname to one line of plain text.",
		},
		"the library's nickname": {
			func(p *kit.NewPrompt) { p.Nickname = "triage" },
			`Another prompt is already called "triage": give this one a nickname of its own.`,
		},
		"no purpose": {func(p *kit.NewPrompt) { p.Purpose = "" }, `Say what the prompt is for: "agent" or "reply".`},
		"an agent's review": {
			func(p *kit.NewPrompt) { p.ReplyAction = kit.ReplyComment },
			"Only a reply names the review it is for: leave replyAction out of an agent's prompt.",
		},
		"a reply of no review": {
			func(p *kit.NewPrompt) { p.Purpose = kit.PurposeReply },
			`Choose the review the reply is for: "approve", "request_changes", or "comment".`,
		},
		"an approval for issues": {
			func(p *kit.NewPrompt) {
				p.Purpose, p.ReplyAction, p.AppliesTo = kit.PurposeReply, kit.ReplyApprove, kit.AppliesToAny
			},
			`A reply that approves or requests changes is a review of a pull request: say "pr".`,
		},
		"no kind": {func(p *kit.NewPrompt) { p.AppliesTo = "both" }, `Choose what the prompt is for: "pr", "issue", or "any".`},
		"no text": {func(p *kit.NewPrompt) { p.Body = " \n" }, "Write what the agent should do."},
		"a reply's empty text": {
			func(p *kit.NewPrompt) { p.Purpose, p.ReplyAction, p.Body = kit.PurposeReply, kit.ReplyComment, "" },
			"Write the reply's text.",
		},
		"a NUL":     {func(p *kit.NewPrompt) { p.Body = "a\x00b" }, "The text holds characters that can't be sent to the agent."},
		"not UTF-8": {func(p *kit.NewPrompt) { p.Body = "a\xffb" }, "The text holds characters that can't be sent to the agent."},
		"too many bytes": {
			func(p *kit.NewPrompt) { p.Body = strings.Repeat("a", kit.MaxPromptBody+1) }, "Keep the text to 32 KB.",
		},
		"too many characters": {
			func(p *kit.NewPrompt) { p.Body = strings.Repeat("a", 20_001) }, "Keep the text to 20,000 characters.",
		},
	} {
		p := agentPrompt("Fix flaky", "Fix it.")
		c.change(&p)
		assert.Equal(t, []string{c.want}, promptProblemsOf(t, p), name)
	}
}

func TestANewPromptsNicknameIsItsOwn(t *testing.T) {
	t.Parallel()

	c := kit.Check(t.Context(), checkSnapshot(), kit.Proposal{Summary: "x", Prompts: []kit.NewPrompt{
		agentPrompt("Fix flaky", "a"), agentPrompt("FIX FLAKY", "b"), agentPrompt("Other", "c"),
	}})

	assert.Empty(t, c.Prompts[0].Problems)
	assert.Equal(t, []string{`Another prompt is already called "FIX FLAKY": give this one a nickname of its own.`},
		c.Prompts[1].Problems, "another new prompt has it, ignoring case")
	assert.Empty(t, c.Prompts[2].Problems)
	assert.Equal(t, 2, c.Prompts[2].Index)
}

func TestANewPromptsTextNamesOnlyTheVariablesThereAre(t *testing.T) {
	t.Parallel()

	for body, want := range map[string][]string{
		"{{.URL}} {{if .IsPR}}{{.Branch}}{{else}}none{{end}} {{printf \"%s\" .Title}} {{(.Repo)}}": {},
		"{{with .Branch}}{{.}} {{.Anything}} {{$.Title}}{{end}}":                                   {},
		"{{.Body}}":               {".Body isn't a variable a prompt can use: the prompts topic lists the ones it can."},
		"{{.title}}":              {".title isn't a variable a prompt can use: did you mean .Title?"},
		"{{.Title.Len}}":          {".Title has no parts to pick: write .Title on its own."},
		"{{$.Nope}}":              {".Nope isn't a variable a prompt can use: the prompts topic lists the ones it can."},
		"{{range .Repo}}x{{end}}": {"{{range}} isn't available in prompts: there are no lists to go through."},
		`{{define "x"}}y{{end}}{{template "x"}}`: {
			"{{define}} and {{block}} aren't available in prompts.", "{{template}} isn't available in prompts.",
		},
		"{{.Title": {"Its text isn't a template the prompt library can read: prompt:1: unclosed action."},
	} {
		got := promptProblemsOf(t, agentPrompt("Fix flaky", body))
		assert.Equal(t, want, got, body)
	}

	snap := checkSnapshot()
	snap.Vocabulary.Variables = nil
	c := kit.Check(t.Context(), snap, kit.Proposal{Summary: "x", Prompts: []kit.NewPrompt{agentPrompt("Fix flaky", "{{.Body}}")}})
	assert.Empty(t, c.Prompts[0].Problems, "with no variables to read, the library checks them as it adds it")
}

func TestThePromptLibrarysCapsAreKitsToo(t *testing.T) {
	t.Parallel()

	_, diags := prompts.Parse(strings.Repeat("a", 20_000), prompts.AppliesToAny)
	assert.Empty(t, diags, "the library takes a text of 20,000 characters")

	_, diags = prompts.Parse(strings.Repeat("a", 20_001), prompts.AppliesToAny)
	require.Len(t, diags, 1, "and refuses one longer, as kit does")
	assert.Equal(t, prompts.SeverityError, diags[0].Severity)

	variables := prompts.Variables()
	for _, v := range variables {
		body := fmt.Sprintf("{{.%s}} and {{$.%s}}", v.Name, v.Name)
		_, diags := prompts.Parse(body, prompts.AppliesToAny)

		for _, d := range diags {
			assert.NotEqual(t, prompts.SeverityError, d.Severity, "%s: a variable the library lists is one it takes", v.Name)
		}
	}
}

// settingsChecked checks a settings change against the fixtures' snapshot, whose settings
// are the defaults.
func settingsChecked(t *testing.T, c kit.SettingsChange) kit.CheckedSettings {
	t.Helper()

	got := kit.Check(t.Context(), checkSnapshot(), kit.Proposal{Summary: "Settings.", Settings: &c})
	require.NotNil(t, got.Settings)

	return *got.Settings
}

func TestASettingsChangeSaysEachSettingItChangesInTheWindowsWords(t *testing.T) {
	t.Parallel()

	auto := autopilotprefs.ModeAuto
	s := settingsChecked(t, kit.SettingsChange{
		DefaultMode: &auto, AgentsAtOnce: new(1), RunsPerDay: new(100), ActionsPerDay: new(50),
		DailyCap:  &autopilotprefs.DailyCap{Amount: 2_000_000, Unit: autopilotprefs.UnitTokens},
		KeepAwake: new(true), HoldOnOutage: new(false), CatchUpHours: new(168),
		HistoryDays: new(14),
	})

	assert.Empty(t, s.Problems)
	assert.Equal(t, []kit.SettingChange{
		{Key: "defaultMode", Label: "New routines start in", Before: "Ask first", After: "Autopilot"},
		{Key: "agentsAtOnce", Label: "Agents at once", Before: "2", After: "1"},
		{Key: "runsPerDay", Label: "Agent runs a day", Before: "20", After: "100"},
		{Key: "dailyCap", Label: "Daily cap", Before: "Off", After: "2,000,000 tokens"},
		{Key: "keepAwake", Label: "Keep the Mac awake while agents work", Before: "Off", After: "On"},
		{Key: "holdOnOutage", Label: "Hold agent starts while their provider reports an outage", Before: "On", After: "Off"},
		{Key: "catchUpHours", Label: "Ask before catching up after", Before: "24 hours", After: "1 week"},
		{Key: "historyDays", Label: "History kept", Before: "90 days", After: "14 days"},
		{Key: "logDays", Label: "Run logs kept", Before: "30 days", After: "14 days"},
	}, s.Changes, "a value set to what it is isn't a change, and a shorter history shortens the logs with it")

	want := autopilotprefs.Config{
		DefaultMode: auto, AgentsAtOnce: 1, RunsPerDay: 100, ActionsPerDay: 50,
		DailyCap:  autopilotprefs.DailyCap{Amount: 2_000_000, Unit: autopilotprefs.UnitTokens},
		KeepAwake: true, CatchUpHours: 168, HistoryDays: 14, LogDays: 14,
	}
	assert.Equal(t, want, s.After, "the whole of the settings once applied")

	both := settingsChecked(t, kit.SettingsChange{HistoryDays: new(30), LogDays: new(7)})
	assert.Equal(t, 7, both.After.LogDays, "logs it names come after the history")
	assert.Empty(t, both.Problems)

	dollars := settingsChecked(t, kit.SettingsChange{DailyCap: &autopilotprefs.DailyCap{Amount: 25, Unit: autopilotprefs.UnitUSD}})
	assert.Equal(t, "$25", dollars.Changes[0].After)
}

func TestASettingsChangeSaysWhatItSetsWhateverTheSettingsAreNow(t *testing.T) {
	t.Parallel()

	auto := autopilotprefs.ModeAuto
	every := kit.SettingsChange{
		DefaultMode: &auto, AgentsAtOnce: new(2), RunsPerDay: new(100), ActionsPerDay: new(50),
		DailyCap:  &autopilotprefs.DailyCap{Amount: 5, Unit: autopilotprefs.UnitUSD},
		KeepAwake: new(true), HoldOnOutage: new(true), CatchUpHours: new(168),
		HistoryDays: new(14), LogDays: new(7),
	}

	assert.Equal(t, []kit.SettingChange{
		{Key: "defaultMode", Label: "New routines start in", After: "Autopilot"},
		{Key: "agentsAtOnce", Label: "Agents at once", After: "2"},
		{Key: "runsPerDay", Label: "Agent runs a day", After: "100"},
		{Key: "actionsPerDay", Label: "GitHub actions a day", After: "50"},
		{Key: "dailyCap", Label: "Daily cap", After: "$5"},
		{Key: "keepAwake", Label: "Keep the Mac awake while agents work", After: "On"},
		{Key: "holdOnOutage", Label: "Hold agent starts while their provider reports an outage", After: "On"},
		{Key: "catchUpHours", Label: "Ask before catching up after", After: "1 week"},
		{Key: "historyDays", Label: "History kept", After: "14 days"},
		{Key: "logDays", Label: "Run logs kept", After: "7 days"},
	}, kit.SettingsSet(every), "every setting it names, a value set to what it is now included, in the window's order")

	assert.Equal(t, []kit.SettingChange{{Key: "dailyCap", Label: "Daily cap", After: "$5"}},
		kit.SettingsSet(kit.SettingsChange{DailyCap: every.DailyCap}), "and only those it names")
	assert.Equal(t, []kit.SettingChange{}, kit.SettingsSet(kit.SettingsChange{}))
}

func TestARulesChangeSaysWhatItSetsWhateverTheRulesAreNow(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []kit.RuleChange{
		{Kind: "trust", Target: renovateLogin, Words: "Trust renovate[bot]"},
		{Kind: "trust", Target: dependabotLogin, Words: "Trust dependabot[bot]"},
		{Kind: "untrust", Target: "github-actions", Words: "Stop trusting github-actions[bot]"},
		{Kind: "pin", Target: "acme-org/tools", Words: "Pin acme-org/tools"},
		{Kind: "mute", Target: "acme-org/legacy", Words: "Mute acme-org/legacy"},
		{Kind: "clear", Target: "acme-labs", Words: "Unpin or unmute acme-labs"},
		{Kind: "ignoreWorkflows", Target: "Nightly build", Words: "Ignore Nightly build"},
		{Kind: "unignoreWorkflows", Target: "Deploy preview", Words: "Stop ignoring Deploy preview"},
	}, kit.RulesSet(kit.RulesChange{
		Trust: []string{" Renovate[bot] ", dependabotLogin, "renovate", "not a bot!"}, Untrust: []string{"github-actions"},
		Pin: []string{"acme-org/tools"}, Mute: []string{"acme-org/legacy", "ACME-ORG/TOOLS", "not/a/repo/at/all"},
		Clear: []string{"acme-labs"}, IgnoreWorkflows: []string{" Nightly build ", "Nightly build"},
		UnignoreWorkflows: []string{"Deploy preview"},
	}), "a rule already so included, in the check's order, each once, and a name that isn't one left out")

	assert.Equal(t, []kit.RuleChange{}, kit.RulesSet(kit.RulesChange{}))
}

func TestASettingsChangeIsCheckedAsSettingsChecksIt(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		change kit.SettingsChange
		want   []string
	}{
		"nothing changes": {
			kit.SettingsChange{AgentsAtOnce: new(2)}, []string{"It changes nothing: each setting it names is already so."},
		},
		"out of range": {
			kit.SettingsChange{AgentsAtOnce: new(4)}, []string{"Agents at once is out of range: 4; choose 1 to 3."},
		},
		"a mode": {
			kit.SettingsChange{DefaultMode: someNumber(autopilotprefs.Mode("always"))},
			[]string{`Say "dryRun", "ask", or "auto" for defaultMode.`},
		},
		"a unit": {
			kit.SettingsChange{DailyCap: &autopilotprefs.DailyCap{Amount: 5}},
			[]string{`Say "usd" or "tokens" for dailyCap's unit.`},
		},
		"logs past the history": {
			kit.SettingsChange{LogDays: new(91)}, []string{"Days of run logs is out of range: 91; choose 1 to 90."},
		},
		"logs longer than the history": {
			kit.SettingsChange{HistoryDays: new(30), LogDays: new(60)},
			[]string{"The run logs can't be kept longer than the history: 60 days of logs, 30 of history."},
		},
	} {
		assert.Equal(t, c.want, settingsChecked(t, c.change).Problems, name)
	}
}

// rulesChecked checks a rules change against the fixtures' snapshot, which trusts
// Dependabot and GitHub Actions and pins acme-org/api.
func rulesChecked(t *testing.T, c kit.RulesChange) kit.CheckedRules {
	t.Helper()

	got := kit.Check(t.Context(), checkSnapshot(), kit.Proposal{Summary: "Rules.", Rules: &c})
	require.NotNil(t, got.Rules)

	return *got.Rules
}

func TestARulesChangeGoesThroughTheRulesOwnPackages(t *testing.T) {
	t.Parallel()

	r := rulesChecked(t, kit.RulesChange{
		Trust: []string{" Renovate[bot] ", "app/my-bot", dependabotLogin}, Untrust: []string{"github-actions", "nobody"},
		Pin: []string{"acme-org/web/"}, Mute: []string{"acme-org/legacy", "Acme-Org/API"}, Clear: []string{"acme-labs"},
		IgnoreWorkflows: []string{" CI / flaky ", "CI / flaky"}, UnignoreWorkflows: []string{"Never ignored"},
	})

	assert.Empty(t, r.Problems)
	assert.Equal(t, []kit.RuleChange{
		{Kind: "trust", Target: renovateLogin, Words: "Trust renovate[bot]"},
		{Kind: "trust", Target: "my-bot", Words: "Trust my-bot[bot]"},
		{Kind: "untrust", Target: "github-actions", Words: "Stop trusting github-actions[bot]"},
		{Kind: "pin", Target: "acme-org/web", Words: "Pin acme-org/web"},
		{Kind: "mute", Target: "acme-org/legacy", Words: "Mute acme-org/legacy"},
		{Kind: "mute", Target: "Acme-Org/API", Words: "Mute Acme-Org/API"},
		{Kind: "ignoreWorkflows", Target: "CI / flaky", Words: "Ignore CI / flaky"},
	}, r.Changes, "one line each, in the proposal's order, a rule already so none")

	assert.Equal(t, kit.Rules{
		TrustedBots: []string{dependabotLogin, "my-bot", renovateLogin}, Pinned: []string{"acme-org/web"},
		Muted: []string{"Acme-Org/API", "acme-org/legacy"}, IgnoredWorkflows: []string{"CI / flaky"},
	}, r.After)

	stored, err := heldruns.Encode(r.After.IgnoredWorkflows)
	require.NoError(t, err)
	assert.Equal(t, `["CI / flaky"]`, stored, "as heldruns stores them")

	off := rulesChecked(t, kit.RulesChange{Clear: []string{"acme-org/api"}, UnignoreWorkflows: []string{"x"}})
	assert.Equal(t, []kit.RuleChange{{Kind: "clear", Target: "acme-org/api", Words: "Unpin acme-org/api"}}, off.Changes)

	snap := checkSnapshot()
	snap.Setup.Rules.Muted, snap.Setup.Rules.IgnoredWorkflows = []string{"acme-org"}, []string{"CI / flaky"}
	got := kit.Check(t.Context(), snap, kit.Proposal{Summary: "x", Rules: &kit.RulesChange{
		Clear: []string{"acme-org"}, UnignoreWorkflows: []string{"CI / flaky"},
	}})
	assert.Equal(t, []kit.RuleChange{
		{Kind: "clear", Target: "acme-org", Words: "Unmute acme-org"},
		{Kind: "unignoreWorkflows", Target: "CI / flaky", Words: "Stop ignoring CI / flaky"},
	}, got.Rules.Changes)
	assert.Equal(t, []string{}, got.Rules.After.Muted)
	assert.Equal(t, []string{}, got.Rules.After.IgnoredWorkflows)
}

func TestARulesChangeRefusesWhatTheRulesOwnPackagesRefuse(t *testing.T) {
	t.Parallel()

	tooMany := make([]string, 0, 99)
	for i := range 99 {
		tooMany = append(tooMany, fmt.Sprintf("bot-%d", i))
	}

	for name, c := range map[string]struct {
		change kit.RulesChange
		want   []string
	}{
		"nothing changes": {
			kit.RulesChange{Trust: []string{dependabotLogin}, Pin: []string{"acme-org/api"}},
			[]string{"It changes nothing: each rule it names is already so."},
		},
		"not a bot": {
			kit.RulesChange{Trust: []string{"not a bot"}, Untrust: []string{" "}},
			[]string{
				`rules.trust[0]: "not a bot" isn't a bot's login GitHub could give: write its login, as renovate or renovate[bot].`,
				"rules.untrust[0] is empty: name a bot, or leave it out.",
			},
		},
		"a bot both trusted and not": {
			kit.RulesChange{Trust: []string{renovateLogin}, Untrust: []string{"Renovate[bot]"}},
			[]string{"rules: renovate is in both trust and untrust: keep it in one."},
		},
		"too many bots": {
			kit.RulesChange{Trust: tooMany},
			[]string{"rules.trust: Too many trusted bots: at most 100."},
		},
		"not a target": {
			kit.RulesChange{Pin: []string{"acme org"}},
			[]string{`rules.pin[0]: "acme org" isn't an org or a repository: write one as acme-org or acme-org/example-repo.`},
		},
		"a target named twice": {
			kit.RulesChange{Pin: []string{"acme-org/web"}, Mute: []string{"ACME-ORG/web"}},
			[]string{"rules.mute[0]: ACME-ORG/web is named more than once: name it once."},
		},
		"a workflow both ignored and not": {
			kit.RulesChange{IgnoreWorkflows: []string{"CI"}, UnignoreWorkflows: []string{" CI "}},
			[]string{"rules: CI is in both ignoreWorkflows and unignoreWorkflows: keep it in one."},
		},
		"workflows that aren't": {
			kit.RulesChange{IgnoreWorkflows: []string{"", strings.Repeat("a", 201), "a\tb", "a\u202eb"}},
			[]string{
				"rules.ignoreWorkflows[0] is empty: name a workflow, or leave it out.",
				"rules.ignoreWorkflows[1]: An ignored workflow's name is too long: keep each to 200 characters.",
				`rules.ignoreWorkflows[2]: An ignored workflow's name holds a control character: "a\tb".`,
				"rules.ignoreWorkflows[3]: keep a workflow's name to one line of plain text, as GitHub shows it.",
			},
		},
	} {
		assert.Equal(t, c.want, rulesChecked(t, c.change).Problems, name)
	}

	many := make([]string, 0, heldruns.MaxIgnored+1)
	for i := range heldruns.MaxIgnored + 1 {
		many = append(many, fmt.Sprintf("workflow %d", i))
	}

	snap := checkSnapshot()
	snap.Setup.Rules.IgnoredWorkflows = many[:heldruns.MaxIgnored]
	got := kit.Check(t.Context(), snap, kit.Proposal{Summary: "x", Rules: &kit.RulesChange{IgnoreWorkflows: many[heldruns.MaxIgnored:]}})
	assert.Equal(t, []string{"rules.ignoreWorkflows: Too many ignored workflows: 101; keep it to 100."}, got.Rules.Problems)

	muted := make([]string, 0, reporules.MaxTargets)
	for i := range reporules.MaxTargets {
		muted = append(muted, fmt.Sprintf("acme-org/repo-%d", i))
	}

	snap = checkSnapshot()
	snap.Setup.Rules.Muted = muted
	got = kit.Check(t.Context(), snap, kit.Proposal{Summary: "x", Rules: &kit.RulesChange{Mute: []string{"acme-org/one-more"}}})
	assert.Equal(t, []string{"rules.mute[0]: Too many repositories and orgs: keep each list to 500."}, got.Rules.Problems)
	assert.Equal(t, muted, got.Rules.After.Muted, "a change it refuses leaves the list as it was")

	snap = checkSnapshot()
	snap.Setup.Rules.TrustedBots = []string{dependabotLogin, "not a bot"}
	got = kit.Check(t.Context(), snap, kit.Proposal{Summary: "x", Rules: &kit.RulesChange{Trust: []string{renovateLogin}}})
	assert.Empty(t, got.Rules.Problems, "a trusted list read from before holds the bots it names")
	assert.Equal(t, []string{dependabotLogin, renovateLogin}, got.Rules.After.TrustedBots)
}
