package kit_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/trust"
)

// The -update flag rewrites the goldens of kit's words. Another test file of the package
// may declare the flag too, so it is declared here only when no file before this one has.
var _ = wordsDeclareUpdate()

// wordsDeclareUpdate declares the -update flag unless it is declared already.
func wordsDeclareUpdate() bool {
	if flag.Lookup("update") == nil {
		flag.Bool("update", false, "rewrite the golden files from the current output")
	}

	return true
}

// wordsUpdating reports whether the tests run with -update.
func wordsUpdating() bool {
	f := flag.Lookup("update")

	return f != nil && f.Value.String() == "true"
}

// wordsGolden compares got with the golden file at path under testdata, or rewrites it
// with -update. A golden is the text exactly as a tool returns it.
func wordsGolden(t *testing.T, path, got string) {
	t.Helper()

	path = filepath.Join("testdata", path)

	if wordsUpdating() {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644)) //nolint:gosec // committed test data, world-readable
		require.NoError(t, os.Chmod(path, 0o644))                  //nolint:gosec // committed test data, world-readable
	}

	want, err := os.ReadFile(path) //nolint:gosec // a fixed golden's name under testdata
	require.NoError(t, err, "rewrite the goldens with go test ./internal/autopilot/kit -update")
	require.Equal(t, string(want), got, "rewrite the goldens with go test ./internal/autopilot/kit -update, "+
		"and review the diff")
}

// routineStarters are the starter prompts a routine can run, as the app hands kit them:
// every starter but those for repositories, which launch on no pull request or issue.
func routineStarters() []prompts.Starter {
	return slices.DeleteFunc(prompts.Starters(), func(s prompts.Starter) bool {
		return s.AppliesTo == prompts.AppliesToRepo
	})
}

// wordsVocabulary is the vocabulary this build ships, converted as the app converts it:
// the gallery's templates, the starter prompts a routine can run, and the variables a
// prompt can name.
func wordsVocabulary() kit.Vocabulary {
	v := kit.Vocabulary{Templates: []kit.Template{}, Starters: []kit.Starter{}, Variables: []kit.Variable{}}

	for _, g := range autopilot.Gallery() {
		t := kit.Template{
			Key: g.Key, Name: g.Name, Icon: g.Icon, Summary: g.Summary, Suggests: g.Suggests, Spec: g.Spec,
			Blanks: []kit.Blank{},
		}

		for _, b := range g.Blanks {
			t.Blanks = append(t.Blanks, kit.Blank{
				Kind: string(b.Kind), Label: b.Label, Help: b.Help, Source: b.Source, Condition: b.Condition,
				Required: b.Required,
			})
		}

		v.Templates = append(v.Templates, t)
	}

	for _, s := range routineStarters() {
		purpose := kit.Purpose(s.Purpose)
		if purpose == "" {
			purpose = kit.PurposeAgent
		}

		v.Starters = append(v.Starters, kit.Starter{
			Key: s.Key, Name: s.Name, Nickname: s.Nickname, AppliesTo: kit.AppliesTo(s.AppliesTo), Purpose: purpose,
			ReplyAction: kit.ReplyAction(s.ReplyAction),
		})
	}

	for _, x := range prompts.Variables() {
		v.Variables = append(v.Variables, kit.Variable{
			Name: x.Name, Group: x.Group, Type: x.Type, Description: x.Description, PROnly: x.PROnly,
		})
	}

	return v
}

// wordsTemplate returns the vocabulary's template key names.
func wordsTemplate(t *testing.T, v kit.Vocabulary, key string) kit.Template {
	t.Helper()

	for _, tmpl := range v.Templates {
		if tmpl.Key == key {
			return tmpl
		}
	}

	require.Failf(t, "no template", "the gallery has no %q", key)

	return kit.Template{}
}

// wordsAt is a moment in the fixtures, in UTC, with nanoseconds, as the store keeps one.
func wordsAt(day, hour int) time.Time {
	return time.Date(2026, time.November, day, hour, 4, 5, 123456789, time.UTC)
}

// wordsRoutine is a routine of the fixtures, made from a template.
func wordsRoutine(t *testing.T, v kit.Vocabulary, id int64, key, name string, on bool) kit.Routine {
	t.Helper()

	tmpl := wordsTemplate(t, v, key)

	return kit.Routine{
		ID: id, Name: name, Enabled: on, Mode: tmpl.Suggests, Spec: tmpl.Spec,
		Description: tmpl.Summary, Sentence: spec.Sentence(tmpl.Spec, nil), UpdatedAt: wordsAt(19, int(id%24)),
	}
}

// wordsSnapshot is a small snapshot of a user's setup and inbox: three routines, one of
// them broken, a few prompts, scripts, and profiles, the settings, the rules, and a few
// of each of the inbox's names.
func wordsSnapshot(t *testing.T) kit.Snapshot {
	t.Helper()

	v := wordsVocabulary()

	broken := wordsRoutine(t, v, 21, "ask-dependabot-to-rebase", `Rebase "conflicting" updates`, false)
	broken.Sentence, broken.Mode = "", autopilotprefs.ModeDryRun
	broken.Broken = "steps[0]: the routine isn't valid"

	settings := autopilotprefs.Defaults()
	settings.Enabled = true
	settings.DailyCap = autopilotprefs.DailyCap{Amount: 5, Unit: autopilotprefs.UnitUSD}

	return kit.Snapshot{
		Version: kit.SnapshotVersion, WrittenAt: wordsAt(20, 5),
		Setup: kit.Setup{
			Routines: []kit.Routine{
				wordsRoutine(t, v, 12, "fix-failing-checks-on-my-prs", "Fix failing checks on my PRs", true),
				wordsRoutine(t, v, 15, "snooze-passing-bot-prs", "Snooze passing bot PRs", true),
				broken,
			},
			Prompts: []kit.Prompt{
				{Starter: "fix-checks", Nickname: "Fix checks", Name: "Fix a pull request's failing checks", Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToPR},
				{Starter: "reply-question", Nickname: "Question", Name: "Ask a question", Purpose: kit.PurposeReply, ReplyAction: kit.ReplyComment, AppliesTo: kit.AppliesToPR},
				{Nickname: "Triage", Name: "Triage a new issue", Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToIssue},
			},
			Scripts: []kit.Script{{ID: 3, Name: "Lint", Shows: "every clone"}, {ID: 7, Name: "Go tests", Shows: "Go repositories"}},
			Profiles: []kit.Profile{
				{Agent: string(agent.Claude), Name: "default"}, {Agent: string(agent.Claude), Name: "night-shift"},
			},
			Settings: settings,
			Rules: kit.Rules{
				TrustedBots: trust.DefaultBots(), Pinned: []string{"acme-org/api"}, Muted: []string{"acme-org/legacy"},
				IgnoredWorkflows: []string{"Deploy preview"},
			},
		},
		Inbox: kit.InboxNames{
			Repositories: []string{"acme-org/api", "acme-org/web", "octo-user/dotfiles"},
			Owners:       []string{"acme-org", "octo-user"},
			People:       []string{"octo-user", "octo-other"},
			Bots:         []string{dependabotLogin, renovateLogin},
			Labels:       []string{"bug", "dependencies", "help wanted"},
			Checks:       []string{"CI / test", "CodeQL", "Security Scans / govulncheck"},
		},
		Vocabulary: v,
	}
}

// wordsBigSnapshot is a large setup, as a heavy user's might be: 60 routines with their
// definitions and descriptions, 60 prompts, 20 scripts, 6 profiles, long rules, and 200 of
// each of the inbox's names.
func wordsBigSnapshot(t *testing.T) kit.Snapshot {
	t.Helper()

	s := wordsSnapshot(t)
	v := s.Vocabulary
	s.Setup.Routines, s.Setup.Prompts, s.Setup.Scripts = nil, nil, nil

	for i := range 60 {
		tmpl := v.Templates[i%len(v.Templates)]
		r := wordsRoutine(t, v, int64(100+i), tmpl.Key, fmt.Sprintf("%s for acme-org team %02d", tmpl.Name, i), i%3 != 0)
		r.Description = strings.Repeat("Keeps the team's pull requests moving. ", 4)
		s.Setup.Routines = append(s.Setup.Routines, r)
	}

	for i := range 60 {
		s.Setup.Prompts = append(s.Setup.Prompts, kit.Prompt{
			Nickname: fmt.Sprintf("Prompt %02d", i), Name: fmt.Sprintf("A prompt the team wrote for case %02d", i),
			Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToAny,
		})
	}

	for i := range 20 {
		s.Setup.Scripts = append(s.Setup.Scripts, kit.Script{
			ID: int64(i + 1), Name: fmt.Sprintf("Script %02d", i), Shows: "acme-org/api and acme-org/web",
		})
	}

	for i := range 40 {
		s.Setup.Rules.Muted = append(s.Setup.Rules.Muted, fmt.Sprintf("acme-org/archived-service-%02d", i))
	}

	s.Inbox = wordsInbox(200, func(list string, i int) string { return fmt.Sprintf("acme-%s-%03d", list, i) })

	return s
}

// wordsInbox is an inbox with n of each name, as name writes the i-th of a list.
func wordsInbox(n int, name func(list string, i int) string) kit.InboxNames {
	list := func(kind string) []string {
		out := make([]string, 0, n)
		for i := range n {
			out = append(out, name(kind, i))
		}

		return out
	}

	return kit.InboxNames{
		Repositories: list("repository"), Owners: list("owner"), People: list("person"), Bots: list("bot"),
		Labels: list("label"), Checks: list("check"),
	}
}

// wordsStructs returns every struct type reachable from root, through pointers and lists,
// root first, each once.
func wordsStructs(root reflect.Type) []reflect.Type {
	out := []reflect.Type{root}

	for i := 0; i < len(out); i++ {
		for field := range out[i].Fields() {
			if inner, _ := wordsElem(field.Type); inner.Kind() == reflect.Struct && !slices.Contains(out, inner) {
				out = append(out, inner)
			}
		}
	}

	return out
}

// wordsElem returns the type a field of type t holds, through pointers and lists, and
// whether it holds a list.
func wordsElem(t reflect.Type) (reflect.Type, bool) {
	list := false

	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		list = list || t.Kind() == reflect.Slice
		t = t.Elem()
	}

	return t, list
}
