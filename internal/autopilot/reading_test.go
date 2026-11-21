package autopilot_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	drafts "github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// The draft leaf is the one validator: the builder's Describe, Save's checks, and a check
// outside the app's own process all read a draft through it. A routine checked outside
// the app is read over a Setup of plain slices, so this holds draft.Read over one to read
// every draft the tests here read, and every template, exactly as Describe does.

func TestReadingMatchesDescribe(t *testing.T) {
	t.Parallel()

	for _, c := range append(describedDrafts(), scriptDrafts()...) {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			readsAlike(t, c)
		})
	}

	for _, g := range autopilot.Gallery() {
		t.Run(g.Key, func(t *testing.T) {
			t.Parallel()

			clean := 0

			for _, c := range templateDrafts(g) {
				if got := readsAlike(t, c); got.Sentence != "" {
					clean++
				}
			}

			assert.Positive(t, clean, "filled in, against a library holding its starters, it reads clean")
		})
	}
}

// readsAlike holds Describe, over c's library and scripts, and draft.Read, over a Setup of
// slices holding the same, to one reading of c's draft, and returns it.
func readsAlike(t *testing.T, c described) autopilot.Described {
	t.Helper()

	svc := autopilot.NewService(autopilot.Config{
		Names: names{}, Library: c.library, MyScripts: c.scripts, Offers: c.offers, Logger: (&testutil.Logs{}).Logger(),
	})

	want, wantErr := svc.Describe(t.Context(), c.draft)
	read, err := drafts.Read(t.Context(), c.draft.Spec, c.draft.Mode, setupOf(t, c), c.offers)

	if wantErr != nil {
		require.EqualError(t, err, wantErr.Error(), c.name)
	} else {
		require.NoError(t, err, c.name)
	}

	got := autopilot.Described{
		Sentence: read.Sentence, Problems: read.Problems, Warnings: read.Warnings, DryRunOnly: read.DryRunOnly,
	}
	assert.Equal(t, want, got, "%s, in mode %q", c.name, c.draft.Mode)

	return got
}

// sliceSetup is a draft leaf's Setup of plain slices, as a check outside the app reads
// one: the library, the user's scripts, each with whether it was read, and the starter
// prompts' names.
type sliceSetup struct {
	library   []autopilot.LibraryPrompt
	libraryOK bool
	scripts   []drafts.Script
	scriptsOK bool
	names     map[string]string
	pastRules bool
}

func (s sliceSetup) Library(context.Context) ([]autopilot.LibraryPrompt, bool) {
	return s.library, s.libraryOK
}

func (s sliceSetup) Scripts(context.Context) ([]drafts.Script, bool) {
	return s.scripts, s.scriptsOK
}

func (s sliceSetup) StarterNames(context.Context) spec.PromptNames {
	return func(p spec.PromptRef) string { return s.names[p.Starter] }
}

func (s sliceSetup) PastRules(context.Context) bool {
	return s.pastRules
}

// setupOf returns a Setup of slices holding what c's library, scripts, and the tests'
// starter names hold, read once, as a snapshot of the app's setup would.
func setupOf(t *testing.T, c described) sliceSetup {
	t.Helper()

	var out sliceSetup

	if c.library != nil {
		library, err := c.library.Prompts(t.Context())
		out.library, out.libraryOK = library, err == nil
	}

	if c.scripts != nil {
		mine, err := c.scripts.MyScripts(t.Context())
		out.scriptsOK = err == nil

		for _, m := range mine {
			out.scripts = append(out.scripts, drafts.Script{ID: m.ID, Name: m.Name})
		}
	}

	names, err := names{}.StarterNames(t.Context())
	require.NoError(t, err)

	out.names = names

	return out
}

// myScripts is the user's own Repo scripts, as a read returns them, or err.
type myScripts struct {
	list []autopilot.MyScript
	err  error
}

func (m myScripts) MyScripts(context.Context) ([]autopilot.MyScript, error) { return m.list, m.err }

// scriptDrafts are a routine that runs the script picked as Lint, read against the user's
// scripts as they may be: the script under its name, among others, renamed, gone, another
// under its name, unreadable, or none to read at all.
func scriptDrafts() []described {
	lint := func(name string) autopilot.MyScript {
		return autopilot.MyScript{ID: 7, Name: name, Command: "make lint"}
	}

	scripts := map[string]autopilot.MyScripts{
		"the script it picked":        myScripts{list: []autopilot.MyScript{lint("Lint")}},
		"a script among the others":   myScripts{list: []autopilot.MyScript{{ID: 3, Name: "Test"}, lint("Lint")}},
		"a script renamed":            myScripts{list: []autopilot.MyScript{lint("Lint all")}},
		"a script gone":               myScripts{},
		"another under the same name": myScripts{list: []autopilot.MyScript{{ID: 3, Name: "Lint"}}},
		"scripts that can't be read":  myScripts{err: errLocked},
		"no scripts to read":          nil,
	}

	out := make([]described, 0, len(scripts))

	for name, mine := range scripts {
		out = append(out, described{name: name, library: library{}, scripts: mine, draft: draft("Lint", lintSpec())})
	}

	return out
}

// lintSpec is a routine that tells the user of an item that appears, then runs the script
// picked as Lint, ID 7.
func lintSpec() spec.Spec {
	s := notifySpec()
	s.Trigger = spec.Trigger{Event: catalog.EventAppears}
	s.Steps = append(s.Steps, spec.Step{Script: &spec.ScriptStep{ID: 7, Name: "Lint"}})

	return s
}

// templateDrafts are g's spec as it ships and with its blanks filled, in no mode and in
// each, read against the tests' library and one holding every starter the gallery runs,
// in this build and in one that runs agents in the terminal alone.
func templateDrafts(g autopilot.GalleryTemplate) []described {
	modes := append([]autopilotprefs.Mode{""}, autopilotprefs.Modes()...)
	libraries := map[string]autopilot.Library{"the tests' library": library{}, "its starters": galleryLibrary()}
	builds := map[string]*autopilot.Offers{"this build": nil, "a terminal-only build": terminalOnly()}
	shapes := map[string]spec.Spec{"as it ships": g.Spec, "filled in": filledSpec(g)}

	out := make([]described, 0, len(shapes)*len(libraries)*len(builds)*len(modes))

	for shape, sp := range shapes {
		for libName, lib := range libraries {
			for build, offers := range builds {
				for _, mode := range modes {
					out = append(out, described{
						name: g.Key + " " + shape + ", against " + libName + ", in " + build, library: lib, offers: offers,
						draft: autopilot.Draft{Name: g.Name, Mode: mode, Spec: sp, TemplateKey: g.Key},
					})
				}
			}
		}
	}

	return out
}

// filledSpec returns g's spec with every blank filled as quick setup would: a where with
// your account and an org, and a list of check names.
func filledSpec(g autopilot.GalleryTemplate) spec.Spec {
	data, err := json.Marshal(g.Spec)
	if err != nil {
		panic(err)
	}

	var sp spec.Spec
	if err = json.Unmarshal(data, &sp); err != nil {
		panic(err)
	}

	for _, b := range g.Blanks {
		switch b.Kind {
		case autopilot.BlankWhere:
			sp.Watch.Sources[b.Source].Search.Where = []string{"@me", "org:acme-org"}
		case autopilot.BlankChecks:
			sp.Conditions.Of[b.Condition].Values = []string{"e2e / browser"}
		}
	}

	return spec.Normalize(sp)
}

// galleryLibrary is a library holding every starter the gallery's templates run, each for
// any item: a comment's reply where a GitHub step comments it, else an agent's prompt.
func galleryLibrary() fakeLibrary {
	var all []autopilot.LibraryPrompt

	for _, g := range autopilot.Gallery() {
		for _, st := range g.Spec.Steps {
			all = append(all, starterPrompts(st)...)
		}
	}

	slices.SortStableFunc(all, func(a, b autopilot.LibraryPrompt) int { return strings.Compare(a.Starter, b.Starter) })

	return fakeLibrary{prompts: slices.CompactFunc(all, func(a, b autopilot.LibraryPrompt) bool {
		return a.Starter == b.Starter
	})}
}

// starterPrompts are the library's copies of the starters st runs, each for any item: a
// comment's reply for a GitHub step's reply, else an agent's prompt.
func starterPrompts(st spec.Step) []autopilot.LibraryPrompt {
	var out []autopilot.LibraryPrompt

	if st.Agent != nil && st.Agent.Prompt.Starter != "" {
		out = append(out, autopilot.LibraryPrompt{
			Starter: st.Agent.Prompt.Starter, Nickname: st.Agent.Prompt.Starter, Kinds: item.Kinds(),
		})
	}

	for _, a := range st.GitHub {
		if a.Reply != nil && a.Reply.Prompt.Starter != "" {
			out = append(out, autopilot.LibraryPrompt{
				Starter: a.Reply.Prompt.Starter, Nickname: a.Reply.Prompt.Starter, Reply: true,
				ReplyFor: autopilot.ReplyForComment, Kinds: item.Kinds(),
			})
		}
	}

	return out
}
