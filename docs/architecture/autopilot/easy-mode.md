# Easy mode: templates, from an item, why, export and import, graduating

What the user sees is in `docs/desktop.md`'s
[Templates and quick setup](../../guide/autopilot/templates-and-quick-setup.md) and
[Export and import](../../guide/autopilot/export-and-import.md).

**The gallery** (`gallery.go`). Each template is a spec in its own file, `gallery/<key>.json`,
embedded in the build, with its words in Go (`galleryInfo`: name, icon, summary, the mode it
suggests, its `Blanks`, and the `Signals` that suggest it); `Gallery` reads them, and
`TemplateByKey` one, which the simulator's scenarios load "exactly as the gallery holds it". A
blank is a part of the spec quick setup fills: a search's Where (`BlankWhere`, at a source) or
a condition's check names (`BlankChecks`, at a condition, `Required` when the spec is invalid
until it is filled). Every template names its prompts by starter, so every Mac has them. The
bound `Templates` serves them as this Mac sees them: the sentence (empty while a required blank
is), what this build can't run of it (the draft leaf's `Offers.Needs`: an agent's place, then
each step and GitHub action the build's `Offers` don't offer, in plain words, and `NeedsWords`
as a sentence;
none, in this build, which runs every template),
the starters it runs with whether the library holds each (`TemplatePrompt.InLibrary`, which the
prompts' `AddStarter` puts back), and why it is suggested (`suggestedWhy` over
`TemplateSignals`: the clones whose CI is go-fortress's, from setup's `Signals`; Dependabot's
pull requests in the inbox this week and bots' that ask for the user's review or are assigned
to them, from the
inbox's `BotPullRequests`, `CountBotPullRequests`; core's `templateSignals`), the suggested
first. Since the needs are worked out from the offers on every read, a template is never
locked: one with a step a later catalog adds before its dispatcher would say what it needs,
and work as that step is offered, with no change to it. A routine made from one is
an ordinary routine (`Draft.TemplateKey`), in the mode new routines start in. The tests hold
every template to its file, its blanks to its spec, its starters to `prompts.Starters`, its
fixture items (it matches each match and none of its misses, and each match is in its watch),
and its sentence to `testdata/gallery/<key>.golden`; core's `TestEveryShippedTemplateSavesCleanly`
saves each through `Describe` and `Save` over the real library, held back only by where its
agent runs while the background isn't offered.

**Quick setup's readiness.** `DraftReadiness(draft)` checks what an unsaved draft needs
(`Needs`), each answered from the checks kept within `readyFor` or checked again cheaply
(never `fresh`, so never GitHub's sign-in), the place by the build's offers.

**From an item** (`fromitem.go`). `FromItem(itemID)` reads the stored item's facts and fills a
`Draft` (never saved): a search of its kind, by its author (`@me` for the viewer, a bot as
`app/<slug>`), in its owner (`@me` for the viewer's own, else `org:<owner>`); Checks fail
(with `checks.failedNames` *contains any of* the names that failed), else Merge conflicts, else
Changes requested, else Appears, as the item stands; and an agent step running the item's own
default prompt (`PromptRef.ItemDefault`) where `Offers.PreferredPlace` says (the background
once offered, else the first place that is); named apart from the user's routines
(`itemRoutineName`, `freeName`), in the mode new routines start in. Its labels and its update
type come as `Suggestions`, and the draft's decision on the item now (`Match`, `Trace`).

**Why didn't a routine run?** `Explain` (`why.go`) adds `Routine` (Autopilot off or paused, the
routine off, another account's, in Dry run, and each problem readiness keeps for it), `Watch`
(for an item not in the watch, a sentence a source, from its facts: the lists it is and isn't
in, or the first search field it doesn't fit, `searchMiss`: kind, author, where, labels, base,
head prefix, draft; a search it fits says the next refresh brings it), and `Records`, every
match of the routine on the item, newest first, at most `maxExplainRecords`.

**Export and import** (`transfer.go`). `Export(path, ids, withPrompts)` writes the routines with
ids (all for none) as a `Bundle` through `appdata.WriteFile`, with `Bundle.Prompts` holding the
prompts they name by nickname in the prompts' own format when asked (`PromptTransfer`, core's
`routinePromptTransfer` over the prompts' `RoutinePrompts`, a type of its own, never bound:
`Bundled`, `Arrivals`, `AddNew`). `PreviewImport(path)` reads a bundle (`appdata.ReadFile`, at
most `MaxBundleBytes`) and returns its SHA-256, each routine's sentence or why it won't load and
the name it arrives under, each bundled prompt's `Arrives` (new, the same, or differs, the copy
here kept), and the `MissingRef`s: a prompt neither the library nor the bundle's new prompts
hold (with the library's prompts of its purpose as choices), an agent whose program this Mac
lacks (`Profiles`, core's `routineProfiles` over the profiles: it inherits by itself, its
profiles with it), and a profile none of its agents has (with their profiles as choices).
`Import` refuses a file whose SHA-256 changed (`ErrChanged`), adds the bundle's new prompts
first, maps each missing reference as the request's `RefChoice`s say, else to inherit
(`refMapper`: the item's own default prompt, the profile a click uses, a fallback dropped; a
reply with no choice stays as it is, and the engine's prompt check holds its runs), and stores
every routine through `keptFrom` and `insertKept`, switched off, the account signed in's, at the
end of the list under a free name, one that won't load as it was.

**Graduating** (`graduate.go`, migration `0100_settings`). `List` gives a routine
`Figures.Graduate` while it is on, the account signed in's, loading, in Ask first, with a step
that asks first, and its last `GraduateAfter` (5) answered real matches since
`graduate_declined_at` (`ListRoutineAsked`: approved or skipped by the user, newest first) are
each a clean approval (`cleanApproval`: approved, done as ran, no step failed, no agent that
pushed nothing, no checks still failing). `DeclineGraduate` stamps `graduate_declined_at`, so
the count starts again; nothing changes a mode but the user's `SetMode`.
