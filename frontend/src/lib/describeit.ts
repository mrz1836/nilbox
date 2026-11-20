// Describe it as the window words it, and how a card applies: the transcript's lines a
// turn's events make (what Claude said, a plain line for each of its tools, its notes),
// the parts of a card in the order they apply, what each changes, the words an outcome
// records for Claude's next turn, and Apply all. A card applies only through the bindings
// the builder, Prompts, and Settings already use: nothing here writes the setup another
// way.

import { brand } from '../generated/brand'
import {
  createPrompt,
  itemsByID,
  routineNames,
  saveRoutine,
  setAutopilotActionsPerDay,
  setAutopilotAgentsAtOnce,
  setAutopilotCatchUpHours,
  setAutopilotDailyCap,
  setAutopilotDefaultMode,
  setAutopilotHistoryDays,
  setAutopilotHoldOnOutage,
  setAutopilotKeepAwake,
  setAutopilotLogDays,
  setAutopilotRunsPerDay,
  setIgnoredWorkflows,
  setRepoRule,
  setTrustedBots,
  settleDescribeItCard,
  type AutopilotConfig,
  type AutopilotDraft,
  type AutopilotMatching,
  type AutopilotMode,
  type AutopilotRoutine,
  type AutopilotRunEvent,
  type AutopilotSpec,
  type DescribeAttachRef,
  type DescribeCard,
  type KitCheckedPrompt,
  type KitCheckedRoutine,
  type KitCheckedRules,
  type KitCheckedSettings,
  type KitNewPrompt,
  type KitOutcome,
  type KitOutcomeKind,
  type KitPart,
  type KitRoutineChange,
  type PromptInput,
  type RepoRule,
} from '../platform'
import { modeLabels } from './autopilot'
import { all, attempt, type Result } from './call'
import { counted, refText } from './format'

// The app's own tools, as Claude Code names them: in a turn in the app, its tool server's
// name joined to each tool's; in the user's own Claude Code, the plugin's server's, which
// names the plugin and its server (a plain name, never with an underscore) before the tool.
const inAppTools = `mcp__${brand.slug}__`
const pluginTools = `mcp__plugin_${brand.slug}_`

// appTool is the app's tool a tool's name names, without the prefix of the server it came
// through ("autopilot_check"), or '' for a tool that isn't the app's.
export function appTool(name: string): string {
  if (name.startsWith(inAppTools)) {
    return name.slice(inAppTools.length)
  }

  if (name.startsWith(pluginTools)) {
    const at = name.indexOf('__', pluginTools.length)

    return at > pluginTools.length ? name.slice(at + 2) : ''
  }

  return ''
}

// headline is the first sentence of a tool's result, without its full stop: "Checked 2
// routines: 1 problem". A tool's own words lead with what it did, and its details follow.
export function headline(text: string): string {
  const line = text.trim().split('\n', 1).join('').trim()
  const stop = line.search(/[.!?](\s|$)/)
  const sentence = stop === -1 ? line : line.slice(0, stop + 1)

  return sentence.replace(/\.$/, '')
}

// headingNames are the names a reference topic's heading may start with, which keep their
// capital in a sentence.
const headingNames: ReadonlySet<string> = new Set(['Autopilot', brand.name])

// topicOf is the reference topic a read of autopilot_reference was for: the tool's event
// names it when its text holds more than the tool's name ("triggers", or its input,
// {"topic":"triggers"}); else the result's heading says it ("# Triggers"); else ''.
function topicOf(tool: AutopilotRunEvent, result: AutopilotRunEvent | undefined): string {
  const text = tool.text.trim()

  if (text !== '' && text !== tool.tool) {
    const named = /"?topic"?\s*[:=]\s*"?([^"\s},]+)/.exec(text)

    return named?.[1] ?? text
  }

  const heading = /^#+\s+(.+)$/.exec(result?.text.trim().split('\n', 1).join('') ?? '')
  const words = heading?.[1]?.trim() ?? ''
  const lead = /^[A-Z][a-z]*\b/.exec(words)?.[0]

  // A heading's first word starts with a capital a sentence's middle doesn't have ("The
  // proposal", "A routine's definition"), unless it is a name: one capitalized within, as
  // GitHub is, or one of the app's own, as Autopilot is.
  return lead === undefined || headingNames.has(lead) ? words : words.charAt(0).toLowerCase() + words.slice(1)
}

// toolWords say what one of Claude's tools did, as a plain line: "Read how Autopilot
// works", a check's or a proposal's own first words once its result is in ("Checked 2
// routines: 1 problem", "Proposed 2 routines and a setting"), or, for a tool that isn't
// the app's, its name.
export function toolWords(tool: AutopilotRunEvent, result: AutopilotRunEvent | undefined): string {
  const name = appTool(tool.tool)
  const said = result === undefined ? '' : headline(result.text)

  switch (name) {
    case 'autopilot_guide':
      return 'Read how Autopilot works'
    case 'autopilot_reference': {
      const topic = topicOf(tool, result)

      return topic === '' ? 'Read Autopilot’s reference' : `Read about ${topic}`
    }
    case 'autopilot_my_setup':
      return 'Read your setup'
    case 'autopilot_inbox_names':
      return 'Read the names in your inbox'
    case 'autopilot_check':
      return result === undefined ? 'Checking the proposal' : said === '' ? 'Checked the proposal' : said
    case 'autopilot_propose':
      return result === undefined ? 'Proposing it' : said === '' ? 'Proposed it' : said
    case 'autopilot_proposal_status':
      return 'Read what you did with its proposals'
    case '':
      return `Used ${tool.tool}`
    default:
      return `Used ${name}`
  }
}

// EntryState is how a tool came out: it worked, it failed, it hasn't answered yet while
// the turn goes, or nothing to mark.
export type EntryState = 'ok' | 'failed' | 'pending' | ''

// Entry is one line of a turn's transcript: what Claude said (markdown), a plain line for
// a tool it used, or a note (a retry, a limit, a tool it was refused).
export type Entry =
  | { kind: 'say'; key: string; text: string }
  | {
      kind: 'activity'
      key: string
      text: string
      state: EntryState
      // detail is why a read failed, from its result; '' for none.
      detail: string
      // proposed reports a proposal that went through, whose card comes with it.
      proposed: boolean
    }
  | { kind: 'note'; key: string; text: string; error: boolean }

type Activity = Extract<Entry, { kind: 'activity' }>

// speaks reports the tools whose result's first words say what they did, so a failure
// needs no detail of its own.
function speaks(tool: string): boolean {
  return tool === 'autopilot_check' || tool === 'autopilot_propose'
}

// eventKey names an event, as its turn's log does: its line and which of the line's
// events it is.
function eventKey(e: AutopilotRunEvent): string {
  return `${String(e.offset)}:${String(e.part)}`
}

// turnEntries are a turn's lines, oldest first, from its events: each tool answered by the
// result after it (in the order they were used, as the stream gives them), pending while
// the turn goes and unmarked once it ended without an answer; what Claude said, except
// the reply itself once it is in, which the transcript shows whole; and the notes. Its
// start, its thinking, and its end have no line.
export function turnEntries(events: readonly AutopilotRunEvent[], live: boolean, reply: string): Entry[] {
  const entries: Entry[] = []
  const waiting: { entry: Activity; tool: AutopilotRunEvent }[] = []

  for (const e of events) {
    const key = eventKey(e)

    switch (e.kind) {
      case 'text':
        entries.push({ kind: 'say', key, text: e.text })
        break
      case 'tool': {
        const entry: Activity = {
          kind: 'activity',
          key,
          text: toolWords(e, undefined),
          state: 'pending',
          detail: '',
          proposed: false,
        }

        entries.push(entry)
        waiting.push({ entry, tool: e })
        break
      }
      case 'toolResult': {
        const answered = waiting.shift()

        if (answered !== undefined) {
          const name = appTool(answered.tool.tool)

          answered.entry.text = toolWords(answered.tool, e)
          answered.entry.state = e.error ? 'failed' : 'ok'
          answered.entry.detail = e.error && !speaks(name) ? headline(e.text) : ''
          answered.entry.proposed = name === 'autopilot_propose' && !e.error
        }

        break
      }
      case 'denied':
        entries.push({
          kind: 'note',
          key,
          text: `Claude Code didn’t let it use ${e.tool === '' ? 'a tool' : e.tool}${e.text === '' ? '' : `: ${e.text}`}`,
          error: true,
        })
        break
      case 'retry':
      case 'limit':
        entries.push({ kind: 'note', key, text: e.text, error: e.error })
        break
      default:
        break
    }
  }

  if (!live) {
    for (const entry of entries) {
      if (entry.kind === 'activity' && entry.state === 'pending') {
        entry.state = ''
      }
    }
  }

  return withoutReply(entries, reply)
}

// withoutReply leaves out what Claude said last when it is the reply, or the reply's
// start, cut short: the transcript shows the reply whole.
function withoutReply(entries: Entry[], reply: string): Entry[] {
  const said = reply.trim()

  if (said === '') {
    return entries
  }

  const last = entries.findLastIndex((e) => e.kind === 'say')
  const entry = entries[last]

  if (entry?.kind !== 'say' || !said.startsWith(entry.text.trim())) {
    return entries
  }

  return entries.filter((_, i) => i !== last)
}

// lastSeq is the newest event followed, 0 for none: a page read from a turn's log may
// number none.
export function lastSeq(events: readonly AutopilotRunEvent[]): number {
  return events.reduce((high, e) => Math.max(high, e.seq), 0)
}

// CardPart is one part of a card, as a row shows it and a card applies it: a new prompt, a
// new routine or a change to one, with the routine as the agent wrote it (change), a change
// to Autopilot's settings, or to the rules routines lean on. Each is numbered as the
// proposal numbers it: a prompt and a routine by their place in its lists, the settings and
// the rules 0.
export type CardPart =
  | { part: 'prompt'; index: number; checked: KitCheckedPrompt }
  | { part: 'routine'; index: number; checked: KitCheckedRoutine; change: KitRoutineChange | undefined }
  | { part: 'settings'; index: number; checked: KitCheckedSettings }
  | { part: 'rules'; index: number; checked: KitCheckedRules }

export type PromptPart = Extract<CardPart, { part: 'prompt' }>
export type RoutinePart = Extract<CardPart, { part: 'routine' }>
export type SettingsPart = Extract<CardPart, { part: 'settings' }>
export type RulesPart = Extract<CardPart, { part: 'rules' }>

// cardParts are a card's parts in the order it applies them: its prompts, so a routine may
// run one the same card adds, then its routines, its settings, and its rules.
export function cardParts(card: DescribeCard): CardPart[] {
  const { checked } = card

  return [
    ...checked.prompts.map((p): CardPart => ({ part: 'prompt', index: p.index, checked: p })),
    ...checked.routines.map((r): CardPart => ({
      part: 'routine',
      index: r.index,
      checked: r,
      change: card.proposal.routines?.[r.index],
    })),
    ...(checked.settings === null ? [] : [{ part: 'settings' as const, index: 0, checked: checked.settings }]),
    ...(checked.rules === null ? [] : [{ part: 'rules' as const, index: 0, checked: checked.rules }]),
  ]
}

// partKey names a part within its card.
export function partKey(p: Pick<CardPart, 'part' | 'index'>): string {
  return `${p.part}:${String(p.index)}`
}

// isEdit reports a routine's part that changes one of the user's routines, rather than
// adding one.
export function isEdit(p: RoutinePart): boolean {
  return p.checked.before !== null || p.checked.gone || (p.change?.id ?? 0) > 0
}

// changedId is the routine a change changes, 0 for a new one.
export function changedId(p: RoutinePart): number {
  return p.change?.id ?? p.checked.before?.id ?? 0
}

// promptName is a new prompt by the name a launch menu shows it: its nickname, else its
// name.
function promptName(p: KitNewPrompt): string {
  return p.nickname === '' ? p.name : p.nickname
}

// changedName is the name of the routine a change changes, as Claude read it, else as the
// change names it.
function changedName(p: RoutinePart): string {
  return p.checked.before?.name ?? p.checked.name
}

// partLabel names a part in a sentence: "the routine “Fix checks”", "the changes to “Fix
// checks”", "the prompt “Fix it”", "the settings change", "the rules change".
export function partLabel(p: CardPart): string {
  switch (p.part) {
    case 'prompt':
      return `the prompt “${promptName(p.checked.prompt)}”`
    case 'routine':
      return isEdit(p) ? `the changes to “${changedName(p)}”` : `the routine “${p.checked.name}”`
    case 'settings':
      return 'the settings change'
    case 'rules':
      return 'the rules change'
  }
}

// settleWords are what an outcome records of a part, as Claude's next turn hears it:
// `Added “Fix checks”, switched off`, `Saved the changes to “Fix checks”`, `Added the
// prompt “Fix it”`, `Applied the settings change`, `Opened “Fix checks” in the builder`,
// `Discarded the rules change`.
export function settleWords(p: CardPart, kind: KitOutcomeKind): string {
  if (kind === 'discarded') {
    return `Discarded ${partLabel(p)}`
  }

  switch (p.part) {
    case 'prompt':
      return `Added the prompt “${promptName(p.checked.prompt)}”`
    case 'routine':
      if (kind === 'builder') {
        return `Opened “${p.checked.name}” in the builder`
      }

      return kind === 'saved' ? `Saved the changes to “${p.checked.name}”` : `Added “${p.checked.name}”, switched off`
    case 'settings':
    case 'rules':
      return `Applied ${partLabel(p)}`
  }
}

// outcomeText is what a row says once it is dealt with, in place of its buttons: "Added,
// switched off", "Changes saved", "Added to your prompts", "Applied", "Opened in the
// builder", "Discarded".
export function outcomeText(part: KitPart, kind: KitOutcomeKind): string {
  switch (kind) {
    case 'added':
      return part === 'prompt' ? 'Added to your prompts' : 'Added, switched off'
    case 'saved':
      return 'Changes saved'
    case 'applied':
      return 'Applied'
    case 'builder':
      return 'Opened in the builder'
    case 'discarded':
      return 'Discarded'
  }
}

// Settled is what the user did with a part: what it records, and the routine a routine's
// part added or saved, 0 for none.
export interface Settled {
  kind: KitOutcomeKind
  words: string
  routineId: number
}

// recorded is the outcome the card holds for a part, undefined while no one dealt with it.
export function recorded(card: DescribeCard, p: Pick<CardPart, 'part' | 'index'>): Settled | undefined {
  const o: KitOutcome | undefined = card.outcomes.find((x) => x.part === p.part && x.index === p.index)

  return o === undefined ? undefined : { kind: o.kind, words: o.words, routineId: o.routineId ?? 0 }
}

// partProblems are what keeps a part from being applied, each a sentence: its checks'
// problems, and, for a change, a routine that is gone.
export function partProblems(p: CardPart): string[] {
  switch (p.part) {
    case 'routine':
      return [
        ...(p.checked.gone ? [`The routine it changes is gone.`] : []),
        ...p.checked.problems.map((x) => x.message),
      ]
    default:
      return p.checked.problems
  }
}

// promptInput is a new prompt as the library's Create takes it: its names, what it is for,
// and its text, with no models, effort, plan mode, worktree, or extra flags, so it takes the
// library's defaults for each.
export function promptInput(p: KitNewPrompt): PromptInput {
  return {
    name: p.name,
    nickname: p.nickname,
    appliesTo: p.appliesTo,
    body: p.body,
    purpose: p.purpose,
    replyAction: p.replyAction ?? '',
    models: {},
    effort: '',
    planMode: null,
    worktree: null,
    extraFlags: {},
  }
}

// CardDraft is a routine as a card saves it, whose mode it always names.
export type CardDraft = AutopilotDraft & { mode: AutopilotMode }

// newRoutineDraft is a new routine as a card adds it: the name and spec checked as a save
// stores them, the description and template the agent wrote, and the mode chosen.
export function newRoutineDraft(p: RoutinePart, mode: AutopilotMode): CardDraft {
  return {
    id: 0,
    name: p.checked.name,
    description: p.change?.description ?? '',
    mode,
    spec: p.checked.spec,
    templateKey: p.change?.template ?? '',
  }
}

// changeDraft is a change to the routine r as a card saves it: r's own mode (a change never
// touches a routine's mode or switch), the name and spec checked as a save stores them,
// and the description the agent wrote, or r's own when it wrote none.
export function changeDraft(r: Pick<AutopilotRoutine, 'id' | 'mode' | 'description'>, p: RoutinePart): CardDraft {
  const description = p.change?.description ?? ''

  return {
    id: r.id,
    name: p.checked.name,
    description: description === '' ? r.description : description,
    mode: r.mode,
    spec: p.checked.spec,
  }
}

// listsOnly reports a spec that watches the inbox's lists alone, whose matches the stored
// items tell exactly; a search's are Test's to read.
export function listsOnly(spec: AutopilotSpec): boolean {
  return (
    spec.watch.sources.length > 0 &&
    spec.watch.sources.every((s) => s.search == null && s.lists !== undefined && s.lists !== null && s.lists.length > 0)
  )
}

// storedMatchWords say what a routine matches among the items the inbox stores now:
// "Matches 2 pull requests and 1 issue in your inbox now", with what it can't tell yet,
// or that it matches nothing there.
export function storedMatchWords(m: AutopilotMatching): string {
  const unknown =
    m.unknown > 0 ? `; ${counted(m.unknown, 'more', 'more')} can’t be told until what it checks is read` : ''

  if (m.count === 0) {
    return m.unknown > 0
      ? `${counted(m.unknown, 'item', 'items')} in your inbox may match now, once what it checks is read`
      : 'Matches nothing in your inbox now'
  }

  const parts = [
    m.pullRequests > 0 ? counted(m.pullRequests, 'pull request', 'pull requests') : '',
    m.issues > 0 ? counted(m.issues, 'issue', 'issues') : '',
  ].filter((part) => part !== '')
  const what = parts.length === 0 ? counted(m.count, 'item', 'items') : parts.join(' and ')

  return `Matches ${m.incomplete ? 'at least ' : ''}${what} in your inbox now${unknown}`
}

// claudeModeHint says what Claude suggests beside the mode a new routine starts in, as a
// hint that never changes it, as a template's suggestion does; '' when it suggests that
// mode, or none.
export function claudeModeHint(suggested: AutopilotMode | undefined, mode: AutopilotMode): string {
  if (suggested === undefined || suggested === mode || !(suggested in modeLabels)) {
    return ''
  }

  return `Claude suggests ${modeLabels[suggested]}. It starts in ${modeLabels[mode]}, as new routines do: change it here if you like.`
}

// settingSetters are Autopilot's settings a card can change, each through its own setter,
// from the settings once the change is applied, in the order Settings lists them: the
// history before its logs, which never outlast it.
const settingSetters: readonly (readonly [string, (after: AutopilotConfig) => PromiseLike<unknown>])[] = [
  ['defaultMode', (a) => setAutopilotDefaultMode(a.defaultMode)],
  ['agentsAtOnce', (a) => setAutopilotAgentsAtOnce(a.agentsAtOnce)],
  ['runsPerDay', (a) => setAutopilotRunsPerDay(a.runsPerDay)],
  ['actionsPerDay', (a) => setAutopilotActionsPerDay(a.actionsPerDay)],
  ['dailyCap', (a) => setAutopilotDailyCap(a.dailyCap)],
  ['keepAwake', (a) => setAutopilotKeepAwake(a.keepAwake)],
  ['holdOnOutage', (a) => setAutopilotHoldOnOutage(a.holdOnOutage)],
  ['catchUpHours', (a) => setAutopilotCatchUpHours(a.catchUpHours)],
  ['historyDays', (a) => setAutopilotHistoryDays(a.historyDays)],
  ['logDays', (a) => setAutopilotLogDays(a.logDays)],
]

// applySettings applies a change to Autopilot's settings, one setter for each setting it
// changes, stopping at the first that fails.
export async function applySettings(s: KitCheckedSettings): Promise<Result<void>> {
  const unknown = s.changes.find((c) => !settingSetters.some(([key]) => key === c.key))

  if (unknown !== undefined) {
    return { ok: false, error: `This build can’t set “${unknown.label}”.` }
  }

  for (const [key, set] of settingSetters) {
    if (!s.changes.some((c) => c.key === key)) {
      continue
    }

    const done = await attempt(set(s.after))

    if (!done.ok) {
      return done
    }
  }

  return { ok: true, data: undefined }
}

// repoRules are the rules a change of a repository or an org sets.
const repoRules: Readonly<Record<string, RepoRule>> = { pin: 'pinned', mute: 'muted', clear: '' }

// ruleKinds are the kinds of rule change a card applies.
const ruleKinds = ['trust', 'untrust', 'pin', 'mute', 'clear', 'ignoreWorkflows', 'unignoreWorkflows']

// applyRules applies a change to the rules routines lean on: the trusted bots as they are
// once applied, each repository's or org's rule, and the ignored workflows as they are once
// applied, stopping at the first that fails.
export async function applyRules(r: KitCheckedRules): Promise<Result<void>> {
  const unknown = r.changes.find((c) => !ruleKinds.includes(c.kind))

  if (unknown !== undefined) {
    return { ok: false, error: `This build can’t apply “${unknown.words}”.` }
  }

  const calls: (() => PromiseLike<unknown>)[] = []

  if (r.changes.some((c) => c.kind === 'trust' || c.kind === 'untrust')) {
    calls.push(() => setTrustedBots(r.after.trustedBots))
  }

  for (const c of r.changes) {
    const rule = repoRules[c.kind]

    if (rule !== undefined) {
      calls.push(() => setRepoRule(c.target, rule))
    }
  }

  if (r.changes.some((c) => c.kind === 'ignoreWorkflows' || c.kind === 'unignoreWorkflows')) {
    calls.push(() => setIgnoredWorkflows(r.after.ignoredWorkflows))
  }

  for (const call of calls) {
    const done = await attempt(call())

    if (!done.ok) {
      return done
    }
  }

  return { ok: true, data: undefined }
}

// settlePart records what the user did with a part of card, which Claude's next turn
// hears.
export function settlePart(card: DescribeCard, p: CardPart, s: Settled): Promise<Result<void>> {
  return attempt(
    settleDescribeItCard({
      ref: card.ref,
      part: p.part,
      index: p.index,
      kind: s.kind,
      words: s.words,
      routineId: s.routineId,
    }),
  )
}

// ApplyAllKit is what Apply all needs of the card it applies.
export interface ApplyAllKit {
  // settled reports a part the user already dealt with, which Apply all leaves alone.
  settled: (p: CardPart) => boolean
  // modeOf is the mode a new routine starts in, as its row chose it.
  modeOf: (p: RoutinePart) => AutopilotMode
  // saveChange saves a change to one of the user's routines, asking first when it can act
  // on what the routine matches now; an error of '' is the question closed unanswered.
  saveChange: (p: RoutinePart) => Promise<Result<AutopilotRoutine>>
  // onSettled hears each part as it is applied and recorded.
  onSettled: (p: CardPart, s: Settled) => void
}

// Applied is how Apply all ended: every part applied, or the part it stopped at and why.
export type Applied = { ok: true } | { ok: false; part: CardPart; error: string }

// applyPart applies one part of a card, resolving with what it records.
async function applyPart(p: CardPart, kit: ApplyAllKit): Promise<Result<Settled>> {
  const settled = (kind: KitOutcomeKind, routineId = 0): Result<Settled> => ({
    ok: true,
    data: { kind, words: settleWords(p, kind), routineId },
  })

  switch (p.part) {
    case 'prompt': {
      const made = await attempt(createPrompt(promptInput(p.checked.prompt)))

      return made.ok ? settled('added') : made
    }
    case 'routine': {
      const saved = isEdit(p) ? await kit.saveChange(p) : await attempt(saveRoutine(newRoutineDraft(p, kit.modeOf(p))))

      return saved.ok ? settled(isEdit(p) ? 'saved' : 'added', saved.data.id) : saved
    }
    case 'settings': {
      const done = await applySettings(p.checked)

      return done.ok ? settled('applied') : done
    }
    case 'rules': {
      const done = await applyRules(p.checked)

      return done.ok ? settled('applied') : done
    }
  }
}

// applyAll applies every part of card no one has dealt with yet, in order (its prompts,
// then its routines, its settings, and its rules), each through the binding its own row
// uses, and records each as it goes. It stops at the first that fails, or that has
// problems, saying which and why; what it applied before stays applied.
export async function applyAll(card: DescribeCard, kit: ApplyAllKit): Promise<Applied> {
  for (const p of cardParts(card)) {
    if (kit.settled(p)) {
      continue
    }

    const problems = partProblems(p)

    if (problems.length > 0) {
      return { ok: false, part: p, error: problems.join(' ') }
    }

    const applied = await applyPart(p, kit)

    if (!applied.ok) {
      return { ok: false, part: p, error: applied.error }
    }

    const recorded = await settlePart(card, p, applied.data)

    kit.onSettled(p, applied.data)

    if (!recorded.ok) {
      return { ok: false, part: p, error: `It was applied, but Claude won’t hear of it: ${recorded.error}` }
    }
  }

  return { ok: true }
}

// appliedWords say where Apply all stopped, and why: "Stopped at the routine “Fix checks”:
// Another routine has that name.", or, for a question about current matches closed
// unanswered, that it stopped there.
export function appliedWords(stopped: Extract<Applied, { ok: false }>): string {
  const at = partLabel(stopped.part)

  return stopped.error === ''
    ? `Stopped at ${at}: the question about what it matches now was closed unanswered.`
    : `Stopped at ${at}: ${stopped.error}`
}

// revisedCards are the cards a later card revises, by their proposals' IDs: one that
// proposes again a routine (by its ID, or a new one by its name, ignoring case), a prompt
// (by its nickname), the settings, or the rules an earlier one proposed. The transcript
// shows each in one line.
export function revisedCards(cards: readonly DescribeCard[]): Set<string> {
  const revised = new Set<string>()
  const touches = (c: DescribeCard): Set<string> =>
    new Set([
      ...(c.proposal.routines ?? []).flatMap((r) => [
        `name:${r.name.toLowerCase()}`,
        ...((r.id ?? 0) > 0 ? [`id:${String(r.id)}`] : []),
      ]),
      ...(c.proposal.prompts ?? []).map((p) => `prompt:${p.nickname.toLowerCase()}`),
      ...(c.proposal.settings == null ? [] : ['settings']),
      ...(c.proposal.rules == null ? [] : ['rules']),
    ])

  cards.forEach((card, i) => {
    const mine = touches(card)

    if (cards.slice(i + 1).some((later) => [...touches(later)].some((t) => mine.has(t)))) {
      revised.add(card.ref.proposal)
    }
  })

  return revised
}

// promptKindWords say what a new prompt is for: "Pull requests · a coding agent runs it",
// "Issues · a reply that comments".
export function promptKindWords(p: KitNewPrompt): string {
  const kinds: Readonly<Record<KitNewPrompt['appliesTo'], string>> = {
    pr: 'Pull requests',
    issue: 'Issues',
    any: 'Pull requests and issues',
  }
  const replies: Readonly<Record<string, string>> = {
    approve: 'a reply that approves',
    request_changes: 'a reply that requests changes',
    comment: 'a reply that comments',
  }
  const purpose = p.purpose === 'reply' ? (replies[p.replyAction ?? ''] ?? 'a review reply') : 'a coding agent runs it'

  return `${kinds[p.appliesTo]} · ${purpose}`
}

// costWords say what a conversation has cost: "about $0.04" for an estimate, as on the
// user's plan, "$0.04" else, '' before a turn reported any.
export function costWords(cost: number | null, estimated: boolean): string {
  if (cost === null) {
    return ''
  }

  return `${estimated ? 'about ' : ''}$${cost.toFixed(2)}`
}

// agentWords say the agent a conversation runs with: "Claude · work · opus · high", its
// profile, model, and effort each left out while it is ''.
export function agentWords(a: { profile: string; model: string; effort: string }): string {
  return ['Claude', a.profile, a.model, a.effort].filter((part) => part !== '').join(' · ')
}

// CardPlaces are where a turn's cards show among its lines: each after the proposal that
// made it, in the order they came (at, by the line's key), and those no line shows yet
// after the rest, in their order.
export interface CardPlaces<T> {
  at: ReadonlyMap<string, T>
  rest: T[]
}

// cardPlaces places a turn's cards among its lines.
export function cardPlaces<T>(entries: readonly Entry[], cards: readonly T[]): CardPlaces<T> {
  const proposals = entries.filter((e) => e.kind === 'activity' && e.proposed).slice(0, cards.length)

  return {
    at: new Map(proposals.flatMap((e, i) => cards.slice(i, i + 1).map((card) => [e.key, card] as const))),
    rest: cards.slice(proposals.length),
  }
}

// Chip is one thing the next message attaches, with the words its chip shows.
export interface Chip {
  ref: DescribeAttachRef
  label: string
}

// chipLabel is a chip's words while what it attaches can't be read: its kind and ID.
function chipLabel(ref: DescribeAttachRef): string {
  return `${ref.kind === 'routine' ? 'Routine' : 'Item'} ${String(ref.id)}`
}

// chipsFor reads the words of each chip for refs: a routine's name, an item's address
// ("acme-org/example-repo#42"), from what this Mac stores, asking GitHub nothing.
export async function chipsFor(refs: readonly DescribeAttachRef[]): Promise<Chip[]> {
  const routines = refs.some((r) => r.kind === 'routine')
  const items = refs.filter((r) => r.kind === 'item').map((r) => r.id)
  const [names, stored] = await all([
    routines ? routineNames() : Promise.resolve([]),
    items.length > 0 ? itemsByID(items) : Promise.resolve([]),
  ])

  return refs.map((ref) => {
    const label =
      ref.kind === 'routine'
        ? names.find((n) => n.id === ref.id)?.name
        : stored.map((it) => (it.id === ref.id ? refText(it) : '')).find((text) => text !== '')

    return { ref, label: label ?? chipLabel(ref) }
  })
}

// withChips adds chips to those the composer holds, each once.
export function withChips(held: readonly Chip[], more: readonly Chip[]): Chip[] {
  const has = (c: Chip) => held.some((h) => h.ref.kind === c.ref.kind && h.ref.id === c.ref.id)

  return [
    ...held,
    ...more.filter(
      (c, i) => !has(c) && more.findIndex((m) => m.ref.kind === c.ref.kind && m.ref.id === c.ref.id) === i,
    ),
  ]
}

// fallbackChips are chips for refs whose words couldn't be read.
export function fallbackChips(refs: readonly DescribeAttachRef[]): Chip[] {
  return refs.map((ref) => ({ ref, label: chipLabel(ref) }))
}
