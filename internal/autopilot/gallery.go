package autopilot

import (
	"context"
	"embed"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// The template gallery: the routines the app ships ready to use, each a spec with a few
// blanks (most often only where to look), which quick setup asks for and Customize opens in
// the full builder. A routine made from one is an ordinary routine, the user's to edit, and
// starts in the mode Settings says new routines start in, never the template's suggestion,
// which shows only as a hint.
//
// A template whose steps this build can't run yet (a GitHub action, an agent in the
// background, Wait for the checks, Merge it, Post the agent's review) is listed all the
// same, with what it needs in plain words (Template.Needs), worked out from the build's
// Offers on every read: once the part that runs the step lands and the catalog offers it,
// the template simply works, with no change here.

// galleryFiles are the templates' specs, one JSON file each, named by its key.
//
//go:embed gallery/*.json
var galleryFiles embed.FS

// signalsSince is how far back the stored items' signals look: this week.
const signalsSince = 7 * 24 * time.Hour

// BlankKind is a kind of blank a template leaves for quick setup.
type BlankKind string

// The kinds of blank.
const (
	// BlankWhere is a search's Where: your account, orgs, users, and repositories.
	BlankWhere BlankKind = "where"

	// BlankChecks is a condition's check names.
	BlankChecks BlankKind = "checks"
)

// Blank is one thing quick setup asks for: a part of the template's spec the user fills
// in, with its words.
type Blank struct {
	Kind BlankKind `json:"kind"`

	// Label and Help say what it asks.
	Label string `json:"label"`
	Help  string `json:"help"`

	// Source is the watch's source whose search's Where it fills (BlankWhere).
	Source int `json:"source"`

	// Condition is the condition whose values it fills (BlankChecks).
	Condition int `json:"condition"`

	// Required reports a blank the template can't run without: the spec is invalid until
	// it is filled.
	Required bool `json:"required"`
}

// SignalKind is something this Mac already knows that suggests a template.
type SignalKind string

// The signals.
const (
	// SignalFortress is clones whose CI runs go-fortress's security checks.
	SignalFortress SignalKind = "fortress"

	// SignalDependabot is pull requests Dependabot opened in the inbox this week.
	SignalDependabot SignalKind = "dependabot"

	// SignalBots is pull requests bots opened in the Review requested and Assigned lists
	// this week.
	SignalBots SignalKind = "bots"
)

// GalleryTemplate is a template as the app ships it.
type GalleryTemplate struct {
	// Key names it, and Name is the routine's name it suggests.
	Key  string
	Name string

	// Icon is its emoji, and Summary one line on what it does.
	Icon    string
	Summary string

	// Suggests is the mode it suggests, shown as a hint beside the mode a routine made
	// from it starts in.
	Suggests autopilotprefs.Mode

	// Blanks are what quick setup asks for.
	Blanks []Blank

	// Signals are what suggests it.
	Signals []SignalKind

	// Spec is its definition, as its file writes it.
	Spec spec.Spec
}

// whereBlank asks where a template's search at source looks.
func whereBlank(source int) Blank {
	return Blank{
		Kind: BlankWhere, Source: source, Label: "Where",
		Help: "Your account, and the orgs, users, or repositories to watch too.",
	}
}

// galleryInfo is every template's words, in the gallery's order; its spec is its file.
func galleryInfo() []GalleryTemplate {
	return []GalleryTemplate{
		{
			Key: "fix-dependabot-security", Name: "Fix Dependabot security failures", Icon: "🛡️",
			Summary: "When a Dependabot update fails a security scan, an agent fixes the cause in the " +
				"background and pushes, then the checks run again. Its fence keeps it out of package.json and " +
				"the other tooling, workflow, and agent files, so a fix that needs one pushes nothing and says " +
				"what a person must change.",
			Suggests: autopilotprefs.ModeAsk, Blanks: []Blank{whereBlank(0)},
			Signals: []SignalKind{SignalFortress, SignalDependabot},
		},
		{
			Key: "heal-dependabot-then-merge", Name: "Heal Dependabot PRs, then merge them", Icon: "🩹",
			Summary: "When a patch or minor Dependabot update fails its checks, an agent adapts the code, and " +
				"once the checks pass it merges inside GitHub's own rules. Its fence keeps it out of package.json " +
				"and the other tooling, workflow, and agent files, so a fix that needs one pushes nothing.",
			Suggests: autopilotprefs.ModeAsk, Blanks: []Blank{whereBlank(0)}, Signals: []SignalKind{SignalDependabot},
		},
		{
			Key: "review-members", Name: "Review members' PRs", Icon: "👥",
			Summary: "When a member opens a small pull request with no sensitive file, an agent reviews it read " +
				"only and posts its verdict, never an approval unless you allow it.",
			Suggests: autopilotprefs.ModeAsk,
			Blanks: []Blank{{
				Kind: BlankWhere, Source: 0, Label: "Where",
				Help: "The repositories whose pull requests to review: your account, or orgs and repositories.",
			}},
		},
		{
			Key: "snooze-passing-bot-prs", Name: "Snooze passing bot PRs", Icon: "⏾",
			Summary:  "A trusted bot's patch or minor update whose checks pass leaves your lists for three days.",
			Suggests: autopilotprefs.ModeAuto, Signals: []SignalKind{SignalBots},
		},
		{
			Key: "tell-me-about-security-failures", Name: "Tell me about security failures", Icon: "🔔",
			Summary:  "A notification when a Dependabot update fails a security scan.",
			Suggests: autopilotprefs.ModeAuto, Blanks: []Blank{whereBlank(0)},
			Signals: []SignalKind{SignalFortress, SignalDependabot},
		},
		{
			Key: "address-requested-changes", Name: "Address requested changes", Icon: "💬",
			Summary: "When a reviewer asks for changes on your pull request, an agent works through them in " +
				"your terminal.",
			Suggests: autopilotprefs.ModeAsk,
		},
		{
			Key: "resolve-merge-conflicts", Name: "Resolve merge conflicts on my PRs", Icon: "🔀",
			Summary: "When your pull request conflicts with its base, an agent merges the base in and resolves it. " +
				"Its fence keeps it out of package.json and the other tooling, workflow, and agent files, so a " +
				"conflict in one waits for you.",
			Suggests: autopilotprefs.ModeAsk,
		},
		{
			Key: "re-run-flaky-checks-once", Name: "Re-run flaky checks once", Icon: "🔁",
			Summary: "When a check you know is flaky fails, an agent re-runs its failed jobs once, and changes " +
				"nothing else.",
			Suggests: autopilotprefs.ModeAuto,
			Blanks: []Blank{
				{
					Kind: BlankChecks, Condition: 0, Label: "Flaky checks", Required: true,
					Help: "The checks that fail now and then for no fault of the code, as GitHub names them.",
				},
				whereBlank(1),
			},
		},
		{
			Key: "ask-dependabot-to-rebase", Name: "Ask Dependabot to rebase what conflicts", Icon: "↻",
			Summary: "When a Dependabot update has merge conflicts, comment @dependabot rebase on it, once a " +
				"commit. No agent, nothing spent.",
			Suggests: autopilotprefs.ModeAuto, Blanks: []Blank{whereBlank(0)}, Signals: []SignalKind{SignalDependabot},
		},
		{
			Key: "ask-for-a-repro-on-quiet-bugs", Name: "Ask for a repro on quiet bugs", Icon: "🏷️",
			Summary: "When a bug assigned to you goes quiet for two weeks with no repro, comment your Needs repro " +
				"reply and label it needs-repro. No agent, nothing spent.",
			Suggests: autopilotprefs.ModeAsk,
		},
		{
			Key: "update-my-prs-when-behind", Name: "Update my PRs when they fall behind", Icon: "⬆️",
			Summary: "When your pull request falls behind its base branch, update its branch, as the review " +
				"sheet's Update branch does.",
			Suggests: autopilotprefs.ModeAsk,
		},
		{
			Key: "fix-failing-checks-on-my-prs", Name: "Fix failing checks on my PRs", Icon: "🔧",
			Summary: "When the checks on your pull request fail, an agent finds the cause and pushes a fix in " +
				"the background.",
			Suggests: autopilotprefs.ModeAsk,
		},
	}
}

// Gallery returns every template the app ships, in the gallery's order, each with its
// spec read from its file. It panics on a file that won't load, which a test holds every
// one to, as it holds every file to a template and every template to a file.
func Gallery() []GalleryTemplate {
	info := galleryInfo()

	for i, t := range info {
		data, err := galleryFiles.ReadFile("gallery/" + t.Key + ".json")
		if err != nil {
			panic(fmt.Sprintf("the %q template has no file: %v", t.Key, err))
		}

		sp, err := spec.Decode(data)
		if err != nil {
			panic(fmt.Sprintf("the %q template's file won't decode: %v", t.Key, err))
		}

		info[i].Spec = spec.Normalize(sp)
		info[i].Blanks = nonNil(info[i].Blanks)
		info[i].Signals = nonNil(info[i].Signals)
	}

	return info
}

// TemplateByKey returns the template key names.
func TemplateByKey(key string) (GalleryTemplate, bool) {
	for _, t := range Gallery() {
		if t.Key == key {
			return t, true
		}
	}

	return GalleryTemplate{}, false
}

// Template is a template as the gallery shows it on this Mac.
type Template struct {
	Key      string              `json:"key"`
	Name     string              `json:"name"`
	Icon     string              `json:"icon"`
	Summary  string              `json:"summary"`
	Suggests autopilotprefs.Mode `json:"suggests"`
	Blanks   []Blank             `json:"blanks"`

	// Spec is its definition, its blanks as shipped, and Sentence the sentence it reads as
	// so, "" while a required blank is empty.
	Spec     spec.Spec `json:"spec"`
	Sentence string    `json:"sentence"`

	// Needs names, in plain words, each part of it this build can't run yet ("agents in
	// the background", "“Wait for the checks”"), and NeedsWords says it as a sentence; both
	// empty when it runs here.
	Needs      []string `json:"needs"`
	NeedsWords string   `json:"needsWords"`

	// Prompts are the starter prompts its steps run, each with whether the library holds
	// it: one the user deleted can be added again (the prompts' AddStarter).
	Prompts []TemplatePrompt `json:"prompts"`

	// Suggested says why it fits what this Mac already knows, "" when nothing does.
	Suggested string `json:"suggested"`
}

// TemplatePrompt is a starter prompt a template runs.
type TemplatePrompt struct {
	// Starter is the starter's key, and Name its nickname in the library, or its key's
	// words when the library lacks it.
	Starter string `json:"starter"`
	Name    string `json:"name"`

	// InLibrary reports a library that holds a copy of it.
	InLibrary bool `json:"inLibrary"`
}

// TemplateSignals are what this Mac already knows that suggests a template, read from
// what is stored and what is on disk, never from GitHub.
type TemplateSignals struct {
	// Fortress counts the clones whose CI is go-fortress's.
	Fortress int

	// Dependabot counts the pull requests Dependabot opened in the inbox since the time
	// asked, and Bots those bots opened in the Review requested and Assigned lists.
	Dependabot int
	Bots       int
}

// Signals reads what suggests a template. Core adapts the first-run setup's look at the
// clones and the stored items.
type Signals interface {
	TemplateSignals(ctx context.Context, since time.Time) (TemplateSignals, error)
}

// Templates returns the gallery as this Mac sees it: each template with its sentence,
// what this build can't run of it yet, the starter prompts it runs and whether the library
// holds them, and why it is suggested; the suggested ones first, each list in the
// gallery's order. It reads the prompt library, the stored items, and the clones' marker
// files, and asks GitHub nothing.
func (s *Service) Templates(ctx context.Context) ([]Template, error) {
	names := s.view(ctx).names
	library := s.libraryPrompts(ctx)
	signals := s.templateSignals(ctx)

	all := Gallery()
	out := make([]Template, 0, len(all))

	for _, g := range all {
		t := Template{
			Key: g.Key, Name: g.Name, Icon: g.Icon, Summary: g.Summary, Suggests: g.Suggests,
			Blanks: slices.Clone(g.Blanks), Spec: g.Spec, Prompts: []TemplatePrompt{},
		}

		if spec.Validate(g.Spec) == nil {
			t.Sentence = spec.Sentence(g.Spec, names)
		}

		t.Needs = s.offers.needs(g.Spec)
		t.NeedsWords = needsWords(t.Needs)
		t.Prompts = templatePrompts(g.Spec, library, names)
		t.Suggested = suggestedWhy(g.Signals, signals)
		out = append(out, t)
	}

	slices.SortStableFunc(out, func(a, b Template) int {
		return countIf(a.Suggested == "") - countIf(b.Suggested == "")
	})

	return out, nil
}

// libraryPrompts reads the prompt library, nil without one or when it can't be read
// (logged).
func (s *Service) libraryPrompts(ctx context.Context) []LibraryPrompt {
	if s.library == nil {
		return nil
	}

	library, err := s.library.Prompts(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "could not read the prompt library for the templates", "error", err)

		return nil
	}

	return library
}

// templateSignals reads what suggests a template, none without Signals or when they can't
// be read (logged).
func (s *Service) templateSignals(ctx context.Context) TemplateSignals {
	if s.signals == nil {
		return TemplateSignals{}
	}

	got, err := s.signals.TemplateSignals(ctx, s.clock.Now().Add(-signalsSince))
	if err != nil {
		s.logger.WarnContext(ctx, "could not read what suggests a template", "error", err)

		return TemplateSignals{}
	}

	return got
}

// needs names each part of s this build can't run yet: an agent's place, then each kind of
// step and GitHub action it doesn't offer, in the catalog's order.
func (o *Offers) needs(s spec.Spec) []string {
	out := []string{}

	for _, words := range append(o.placeNeeds(s), o.stepNeeds(s)...) {
		if !slices.Contains(out, words) {
			out = append(out, words)
		}
	}

	return out
}

// placeNeeds names each place s runs an agent that this build doesn't.
func (o *Offers) placeNeeds(s spec.Spec) []string {
	var out []string

	for _, st := range s.Steps {
		if st.Agent != nil && !o.runsAt(st.Agent.Place()) {
			info, _ := catalog.PlaceByID(st.Agent.Place())
			out = append(out, "agents "+strings.ToLower(info.Label))
		}
	}

	return out
}

// stepNeeds names each kind of step, and each GitHub action, of s this build doesn't run,
// in the catalog's order.
func (o *Offers) stepNeeds(s spec.Spec) []string {
	off := o.orCatalog()

	var out []string

	for _, info := range catalog.Steps() {
		for _, st := range s.Steps {
			switch {
			case st.Kind() != info.Kind:
			case !slices.Contains(off.Steps, info.Kind):
				out = append(out, "“"+info.Label+"”")
			default:
				out = append(out, off.actionNeeds(st)...)
			}
		}
	}

	return out
}

// actionNeeds names each GitHub action of st this build doesn't send.
func (o *Offers) actionNeeds(st spec.Step) []string {
	var out []string

	for _, a := range st.GitHub {
		if !slices.Contains(o.Actions, a.Kind()) {
			out = append(out, "“"+actionLabel(a.Kind())+"”")
		}
	}

	return out
}

// actionLabel names a GitHub action as the builder does, by the catalog's label when it
// lists the action, else as the spec spells it.
func actionLabel(kind catalog.GitHubActionKind) string {
	for _, a := range catalog.GitHubActions() {
		if a.Kind == kind {
			return a.Label
		}
	}

	return map[catalog.GitHubActionKind]string{
		catalog.GitHubRebase: "Ask Dependabot to rebase", catalog.GitHubUpdateBranch: "Update branch",
		catalog.GitHubReply: "Comment a reply", catalog.GitHubLabels: "Labels",
	}[kind]
}

// needsWords says what a template needs as a sentence, "" for nothing.
func needsWords(needs []string) string {
	if len(needs) == 0 {
		return ""
	}

	return fmt.Sprintf("It needs %s, which this version of %s can't run yet.", series(needs), brand.Name)
}

// templatePrompts returns the starter prompts sp's steps run, each once, with whether
// library holds it and its nickname there.
func templatePrompts(sp spec.Spec, library []LibraryPrompt, names spec.PromptNames) []TemplatePrompt {
	out := []TemplatePrompt{}

	for _, ref := range promptRefs(sp) {
		if ref.Starter == "" || slices.ContainsFunc(out, func(p TemplatePrompt) bool { return p.Starter == ref.Starter }) {
			continue
		}

		p := TemplatePrompt{Starter: ref.Starter, Name: starterWords(ref.Starter)}

		if names != nil {
			if n := names(ref); n != "" {
				p.Name = n
			}
		}

		_, p.InLibrary = findPrompt(library, ref)
		out = append(out, p)
	}

	return out
}

// starterWords writes a starter's key as words: "fix-checks" as "Fix checks".
func starterWords(key string) string {
	return textutil.Capitalize(strings.ReplaceAll(key, "-", " "))
}

// suggestedWhy says why a template that kinds suggest fits what this Mac knows, by the
// first of them that holds, "" when none does.
func suggestedWhy(kinds []SignalKind, got TemplateSignals) string {
	for _, k := range kinds {
		switch {
		case k == SignalFortress && got.Fortress == 1:
			return "One of your clones runs go-fortress’s security checks."
		case k == SignalFortress && got.Fortress > 1:
			return fmt.Sprintf("%d of your clones run go-fortress’s security checks.", got.Fortress)
		case k == SignalDependabot && got.Dependabot > 0:
			return fmt.Sprintf("Dependabot opened %s in your inbox this week.", prsWords(got.Dependabot))
		case k == SignalBots && got.Bots > 0:
			return fmt.Sprintf("Bots opened %s in your Review requested and Assigned lists this week.",
				prsWords(got.Bots))
		}
	}

	return ""
}

// prsWords counts pull requests: "1 pull request", "9 pull requests".
func prsWords(n int) string {
	if n == 1 {
		return "1 pull request"
	}

	return fmt.Sprintf("%d pull requests", n)
}
