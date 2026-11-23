# internal/autopilot/draft

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`internal/autopilot`](README.md) · Code: [`internal/autopilot/draft`](../../../internal/autopilot/draft) · Guide: [Building a routine](../../guide/autopilot/building-a-routine.md)

`internal/autopilot/draft/` is the one validator of a routine's draft: the builder's
`Describe` and `Save`'s checks in the root package, and whatever checks a routine outside the
app's own process ([kit](kit.md), and so [the tool mode](../mcpserver.md)),
all read a draft through it, so a routine reads the same wherever it is checked. It is a pure
leaf, held by `TestLeafImports` to the routines core's `catalog`, `spec`, and `watch`, `item`,
`agent` (what each agent can keep in the background), `fence`, `autopilotprefs`, `brand`,
`textutil`, and `uierr`: it reads no database, starts nothing, and never asks GitHub. What a
draft's checks read beyond the draft comes through the `Setup` it is handed (`Library`, the
prompts as a routine names them; `Scripts`, the user's own, read only for a spec with a script
step; `StarterNames`, for the sentence; `PastRules`, whether the past-the-rules switch is on, for a
warning), a read that fails skipping its checks, and what this
build runs through `Offers` (nil is the catalog's own, `CatalogOffers`; a test hands in fakes).

- **A reading.** `Read(ctx, spec, mode, setup, offers)` returns what the builder shows
  (`Reading`): the spec as `Save` stores it (`Encode`: a spec of version 0 at this build's,
  normalized and encoded; `Load` loads it back as every load checks a stored one, returning a
  `spec.ValidationError`'s problems rather than failing), the spec's own problems or else
  `Problems`' (an agent step set to run where this build runs none, `Offers.placeProblems`; a
  background agent step whose own agent can't keep its tools preset; a prompt the library
  doesn't have, a reply where an agent's prompt goes or the other way about, or one that
  doesn't fit the kinds the routine acts on, as one written for no kind of item, a prompt for
  repositories, fits none (`kindsProblem`), `StepPromptProblems`, which the engine runs again
  before each start; a script the user no longer has under the name it was picked by), the
  steps it runs only in Dry run (`Offers.NotOffered`), its warnings, and, only while it has no
  problem, its sentence. A spec past the size a spec may be is an error, worded.
- **Names.** `CheckName` (one line of plain text, at most 80 runes, none of them `Hidden`: a
  control or a bidirectional character), `CheckDescription` (at most 500, line breaks allowed),
  and `CheckTemplate` check a routine's own fields as `Save` does. `NameKey` is the fold every
  comparison of routine names uses and the `name_key` column stores, and `FreeName` gives a
  taken name the first free "<name> 2", cut to fit (`FitName`), as the root does for a copy, an
  import, or a restore, and kit's check for a proposed routine.
- **The rest it shares** with the root: `ActsOn` (the kinds a routine acts on: those both its
  watch and its trigger take), `Offers.Needs` and `NeedsWords` (what a template needs that this
  build doesn't run), `Offers.PreferredPlace`, `RenamedWords`, and `Series`.
- **The root's side.** The autopilot package keeps its bound types (`Described`, `MyScript`)
  and aliases the leaf's unbound ones (`LibraryPrompt`, `ReplyFor`, `Offers`, the caps, and
  the errors), so no generated binding moved; `serviceSetup` is its live `Setup` (the prompt
  library and the user's scripts, a read that fails logged). `TestReadingMatchesDescribe` holds
  `Describe` and `draft.Read` over a `Setup` of plain slices to one reading of every draft the
  package's tests read and of every template, filled in.
