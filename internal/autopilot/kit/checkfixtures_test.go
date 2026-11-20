package kit_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// The -update flag rewrites the golden files. Every golden test of the package shares one
// flag, so it is declared here only when no test file of the package declared it as a
// package variable first: init runs after every one of them.
func init() { //nolint:gochecknoinits // registers -update after every package variable, so one declared elsewhere wins
	if flag.Lookup("update") == nil {
		flag.Bool("update", false, "rewrite the golden files from the current output")
	}
}

// checkUpdating reports whether the tests run with -update.
func checkUpdating() bool {
	f := flag.Lookup("update")

	return f != nil && f.Value.String() == "true"
}

// checkGolden compares got with the golden file at path, under testdata, rewriting it with
// -update.
func checkGolden(t *testing.T, path, got string) {
	t.Helper()

	file := filepath.Join("testdata", path)

	if checkUpdating() {
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o750))
		require.NoError(t, os.WriteFile(file, []byte(got), 0o600))
	}

	want, err := os.ReadFile(file) //nolint:gosec // a golden file under testdata
	require.NoError(t, err, "run the test with -update to write %s", file)
	assert.Equal(t, string(want), got, "%s differs: run the test with -update if the change is meant", file)
}

// The names the fixtures use most: the starter that fixes checks, and Dependabot's login.
const (
	fixChecksKey    = "fix-checks"
	dependabotLogin = "dependabot"
)

// checkedAt is when the fixtures' routines last changed: to the nanosecond, as the
// database stamps one.
var checkedAt = time.Date(2026, time.November, 20, 5, 31, 3, 123456789, time.UTC) //nolint:gochecknoglobals // a fixed test moment

// checkSnapshot is a setup to check proposals against: two routines, Nightly on in Ask
// first and Fix checks off; a library of starter copies, a reply for each review, and
// Triage, a prompt of the user's own; the script Lint; Settings' defaults; and rules
// trusting Dependabot and GitHub Actions, with acme-org/api pinned.
func checkSnapshot() kit.Snapshot {
	return kit.NewSnapshot(kit.Setup{
		Routines: []kit.Routine{
			{
				ID: 7, Name: "Nightly", Enabled: true, Mode: autopilotprefs.ModeAsk, Spec: checksFailSpec(notifyStep()),
				UpdatedAt: checkedAt,
			},
			{
				ID: 9, Name: "Fix checks", Mode: autopilotprefs.ModeAsk,
				Spec:      checksFailSpec(agentStep(spec.PromptRef{Starter: fixChecksKey}, catalog.WhereBackground, "")),
				UpdatedAt: checkedAt,
			},
		},
		Prompts: []kit.Prompt{
			{
				Starter: fixChecksKey, Nickname: "Fix checks", Name: "Fix a pull request's failing checks",
				Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToPR,
			},
			{
				Starter: "review-pr", Nickname: "Review", Name: "Review a pull request", Purpose: kit.PurposeAgent,
				AppliesTo: kit.AppliesToPR,
			},
			{Nickname: "Triage", Name: "Triage an item", Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToAny},
			{
				Starter: "reply-lgtm", Nickname: "LGTM", Name: "Approve with thanks", Purpose: kit.PurposeReply,
				ReplyAction: kit.ReplyApprove, AppliesTo: kit.AppliesToPR,
			},
			{
				Starter: "reply-question", Nickname: "Question", Name: "Ask a question", Purpose: kit.PurposeReply,
				ReplyAction: kit.ReplyComment, AppliesTo: kit.AppliesToAny,
			},
		},
		Scripts:  []kit.Script{{ID: 3, Name: "Lint", Shows: "every clone"}},
		Profiles: []kit.Profile{{Agent: "claude", Name: "work"}},
		Settings: autopilotprefs.Defaults(),
		Rules: kit.Rules{
			TrustedBots: []string{dependabotLogin, "github-actions"}, Pinned: []string{"acme-org/api"},
		},
	}, kit.InboxNames{
		Repositories: []string{"acme-org/api", "acme-org/web"}, Labels: []string{"bug", "needs-repro"},
		Checks: []string{"CI / test"},
	}, kit.Vocabulary{
		Templates: []kit.Template{{
			Key: "fix-failing-checks-on-my-prs", Name: "Fix failing checks on my PRs", Suggests: autopilotprefs.ModeAsk,
			Spec: checksFailSpec(agentStep(spec.PromptRef{Starter: fixChecksKey}, catalog.WhereBackground, "")),
		}},
		Starters: []kit.Starter{
			{
				Key: fixChecksKey, Name: "Fix a pull request's failing checks", Nickname: "Fix checks",
				AppliesTo: kit.AppliesToPR, Purpose: kit.PurposeAgent,
			},
			{
				Key: "rerun-failed-jobs", Name: "Re-run a pull request's failed jobs", Nickname: "Re-run failed jobs",
				AppliesTo: kit.AppliesToPR, Purpose: kit.PurposeAgent,
			},
		},
		Variables: []kit.Variable{
			{Name: "Title", Group: "Item", Type: "text"},
			{Name: "Number", Group: "Item", Type: "number"},
			{Name: "URL", Group: "Item", Type: "text"},
			{Name: "Repo", Group: "Repository", Type: "text"},
			{Name: "IsPR", Group: "Item", Type: "yes/no"},
			{Name: "Branch", Group: "Pull request", Type: "text", PROnly: true},
		},
	}, checkedAt.Add(time.Hour))
}

// checksFailSpec is a routine that runs steps when the checks fail on the user's own pull
// requests.
func checksFailSpec(steps ...spec.Step) spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{{Lists: []catalog.ListSource{catalog.SourceAuthored}}}},
		Trigger: spec.Trigger{Event: catalog.EventChecksFailed},
		Steps:   steps,
	}
}

// notifyStep is Notify me.
func notifyStep() spec.Step {
	return spec.Step{Notify: &spec.NotifyStep{}}
}

// agentStep is an agent step that runs prompt where says, with tools, "" for the default.
func agentStep(prompt spec.PromptRef, where catalog.Where, tools catalog.Preset) spec.Step {
	a := &spec.AgentStep{Prompt: prompt, Where: where}
	if tools != "" {
		a.Fence = &spec.Fence{Tools: tools}
	}

	return spec.Step{Agent: a}
}

// newRoutine is a proposal's new routine named name that runs sp.
func newRoutine(name string, sp spec.Spec) kit.RoutineChange {
	return kit.RoutineChange{Name: name, Spec: sp}
}

// changeOf is a proposal's change to the snapshot's routine id, named name, read at
// checkedAt, to run sp.
func changeOf(id int64, name string, sp spec.Spec) kit.RoutineChange {
	return kit.RoutineChange{ID: id, Base: checkedAt.Format(time.RFC3339Nano), Name: name, Spec: sp}
}

// someNumber returns a pointer to n.
func someNumber[T any](n T) *T {
	return &n
}
