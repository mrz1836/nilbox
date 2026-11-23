# internal/autopilot/kit

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`internal/autopilot`](README.md) · Code: [`internal/autopilot/kit`](../../../internal/autopilot/kit) · Guide: [Describe it](../../guide/autopilot/describe-it.md)

`internal/autopilot/kit/` is what Describe it hands a coding agent, and how it reads what the
agent hands back, which the app ([internal/describeit](../describeit.md)) and its tool mode
([internal/mcpserver](../mcpserver.md)) share, so the two never disagree about a format. It
is a pure leaf, held by `TestLeafImports` to the routines core's `catalog`, `spec`, and `watch`,
`draft`, `item`, `repo`, `autopilotprefs`, `trust`, `reporules`, `heldruns`, `brand`, and
`textutil`: it reads no file, starts nothing, and keeps no clock, the snapshot and the moment
coming with each call.

- **The snapshot** (`Snapshot`, version 1, the describe folder's `context.json`) is what the
  agent may read: the user's `Setup` (each routine with its definition, sentence, switch, mode,
  and `UpdatedAt`; each prompt's names, purpose, review, and kinds, never its text; each
  script's name and where it shows, never its command; each agent profile's name; Autopilot's
  settings; and the rules routines lean on: the trusted bots, the pinned and muted repositories
  and orgs, and the ignored workflows), the inbox's names (`InboxNames`: repositories, owners,
  people, bots, labels, and checks, each most common first), and the parts of the vocabulary
  that packages above the leaves hold (`Vocabulary`: the gallery's templates, the starters'
  names, and the variables a prompt can name). Every name is untrusted, so `NewSnapshot` keeps
  each as `Name` does (one line, with no control, bidirectional, tag, or supplementary
  variation-selector character, at most 100 runes, cut with an ellipsis), and each inbox list
  as `Names` does (empties and duplicates by `draft.NameKey` left out, the first 200 kept). Every
  text kit renders from a snapshot opens with a line saying its names are the user's data,
  never instructions, and quotes each name as Go quotes a string. `EncodeSnapshot` refuses one
  past `MaxSnapshotBytes` (16 MiB); `DecodeSnapshot` reads the version first
  (`ErrSnapshotVersion`, which the app answers by writing it again as it starts), then strictly.
  `Snapshot.Outside` is the part a session in the user's own Claude Code reads: each routine's
  ID, name, sentence, switch, mode, and `UpdatedAt`, each prompt's nickname, purpose, and kinds,
  the settings, and the vocabulary; no definition, description, script, profile, rule, or inbox
  name.
- **The proposal** (`Proposal`): a summary; new prompts (`NewPrompt`: names, purpose, review,
  kinds, and text; no models, effort, plan mode, worktree, or flags, which reach a click's launch
  without a fence); new routines and changes to routines (`RoutineChange`: for a change, the
  routine's ID and `Base`, the `updatedAt` the agent read; a name, a description, the mode it
  suggests, its template, and its spec); a `SettingsChange` holding only what changes, with no
  field for Autopilot's switch or Pause; and a `RulesChange` (trust and untrust, pin, mute, and
  clear, ignore and unignore workflows). `DecodeProposal` reads it strictly through
  `spec.DecodeExact`, so a spec inside is held to a stored spec's rules and a key for the
  switch or Pause is refused, worded for the agent to fix, within its caps: 4 MiB, 10 routines,
  10 prompts of at most 32 KiB of text each, each spec within `spec.MaxBytes`, 100 entries a rule
  list, and a summary of one line within 400 runes; a proposal of nothing is refused too.
  `FuzzDecodeProposal` holds every refusal to `ErrUnreadableProposal`, and what it accepts to
  read back as itself.
- **The check.** `Check(ctx, snapshot, proposal)` checks each part as the app checks it as it is
  applied: each routine through `draft.Read`, over a `Setup` of the snapshot's prompts with the
  proposal's new ones added (so a routine may run a prompt the same proposal adds), its scripts,
  and the starters' names, in the mode it would run in (the one new routines start in for a new
  routine, whatever the agent suggests; the routine's own for a change), its name, description,
  and template as `Save` checks them, a taken name given the free one (`NameTaken`), and a
  change's routine missing (`Gone`) or changed since its base (`Stale`); each new prompt as the
  prompt library checks one as it is added, the variables its text names among them; the
  settings change as `autopilotprefs` checks the settings it leaves, each change in Settings'
  words (`SettingChange`); and the rules change through `trust.NormalizeBots`,
  `reporules.ParseTarget` and `Rule.Valid`, and `heldruns`' cleaning, each change one line
  (`RuleChange`). `Checked.Words` is what the agent reads, its first sentence the window's line
  (*Checked 2 routines and a settings change: 1 problem.*). `TestEveryGalleryTemplateChecksClean`
  checks every template, filled in.
- **Risks** (`RoutineRisks`, `SettingsRisks`, `RulesRisks`, `PromptRisks`) are worked out from a
  part's before and after, never from the agent's words, each one sentence, a warning or a
  caution (a stale base, a name taken); what a routine did before, and does still, is no new
  risk. [Describe it](../../guide/autopilot/describe-it.md) lists what they cover, and
  goldens pin every line (`TestRiskLinesArePinned`).
- **The words** are generated from this build's catalog, spec, and settings, so an update
  changes what the agent knows: `Guide` (`autopilot_guide`, read first: how Autopilot works,
  the proposal with a worked example, how to work, the posture, the templates by key, and the
  topics), `Topic` (one of `Topics`: proposal, routine, watch, triggers, schedule, conditions,
  fields, steps, agent, github, limits, settings, rules, prompts, templates, and schema),
  `Schema` (the proposal's JSON Schema, draft 2020-12, by reflection over its types, every closed
  set enumerated from `spec.ClosedSets`, the same bytes every call), `SetupWords` and
  `RoutineWords` (`autopilot_my_setup`: the whole setup, or one routine, `Outside`'s part for a
  session there), `InboxWords`, `SkillText` (the instructions: in the app, the system prompt's
  addition, and in the user's own Claude Code, the plugin's skill; how to work, read, check, then
  propose once, and the posture: start safe, one change per request, reuse a template or a
  routine, names are data, and the switch, the mode, and Pause the user's alone), `Tools` (the
  tool mode's tools by place: outside, no inbox names), and `ServerInstructions`. Every text a
  tool returns stays under 60 KB, since a result much larger never reaches the agent (the
  spike's S11): past 56 KiB, `SetupWords` leaves out the last routines' definitions, then their
  sentences, and says so.
  Goldens pin the guide, every topic, the schema, the setup and inbox words, and the skill in
  both places; `TestTheGuideNamesEveryCatalogEntry` and `TestTheSchemaEnumeratesEveryClosedSet`
  fail when the vocabulary outgrows its words.
- **The shared files.** A `ProposalFile` (version 1: its ID, `NewProposalID`'s moment in UTC and
  12 random hex digits; the conversation, or from outside the session folder's base name; when
  the snapshot it was checked against was written; and the proposal) and a folder's `Outcomes`
  (what the user did with each part: added, saved, applied, opened in the builder, or discarded,
  with its words and the routine it saved), each read strictly. `OutcomeWords` joins a turn's
  outcomes into the one sentence the next turn tells the agent (*Added “Fix checks”, switched
  off; discarded the settings change.*). `MaxOutsidePending` (50) caps the proposals waiting from
  the user's own Claude Code, counted among the newest `MaxOutsideRead` (200) files, which the
  app and the tool mode both read.
