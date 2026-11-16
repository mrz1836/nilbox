// The routine builder's pure half: a draft and how it changes, which steps fit where they
// would sit, the fields and operators a condition can take, where the backend's problems
// and warnings belong, and the words the blocks show. The builder itself is
// components/autopilot/builder; the spec's own meaning, its sentence, and its problems are
// Go's (Describe), and this only lays them out.

import { brand } from '../generated/brand'
import type {
  AutopilotAgentStep,
  AutopilotCatalog,
  AutopilotDraft,
  AutopilotEstimate,
  AutopilotEvent,
  AutopilotField,
  AutopilotInvolving,
  AutopilotMode,
  AutopilotOp,
  AutopilotOutcome,
  AutopilotPart,
  AutopilotPlace,
  AutopilotProblem,
  AutopilotRoutine,
  AutopilotSearch,
  AutopilotSetting,
  AutopilotSpec,
  AutopilotStep,
  AutopilotStepKind,
  AutopilotTestResult,
  AutopilotTrigger,
  AutopilotWhere,
  ItemKind,
} from '../platform'
import { counted } from './format'

// NEW_ROUTINE is the key of the routine being made, before it is saved.
export const NEW_ROUTINE = -1

// bothKinds are every kind of item, in the catalog's order.
const bothKinds: readonly ItemKind[] = ['pr', 'issue']

// canonical writes a value as JSON with its keys sorted, leaving out what the spec leaves
// out when it is stored (undefined, null, empty text, and empty lists), so two drafts that
// store the same compare the same however they were edited. An empty object stays: a step
// such as Pin is one.
export function canonical(value: unknown): string {
  return JSON.stringify(trimmed(value) ?? null)
}

// trimmed is value without what the spec leaves out when it is stored, its keys sorted.
function trimmed(value: unknown): unknown {
  if (Array.isArray(value)) {
    const list = value.map(trimmed).filter((v) => v !== undefined)

    return list.length === 0 ? undefined : list
  }

  if (value !== null && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>)
        .map(([k, v]) => [k, trimmed(v)] as const)
        .filter(([, v]) => v !== undefined)
        .sort(([a], [b]) => a.localeCompare(b)),
    )
  }

  return value === null || value === '' ? undefined : value
}

// omit is value without key, as a setting put back to its default leaves the spec.
export function omit<T extends object, K extends keyof T>(value: T, key: K): Omit<T, K> {
  return Object.fromEntries(Object.entries(value).filter(([k]) => k !== key)) as Omit<T, K>
}

// routineDraft is a saved routine as the builder edits it. It names no mode: the switch and
// the mode control change those at once, and Save keeps the routine's own.
export function routineDraft(r: AutopilotRoutine): AutopilotDraft {
  return { id: r.id, name: r.name, description: r.description, spec: structuredClone(r.spec) }
}

// scratchSpec is where Start from scratch begins: your account's pull requests, when their
// checks fail, telling you. It saves as it is, and every part is there to change.
export function scratchSpec(): AutopilotSpec {
  return {
    version: 1,
    watch: { sources: [{ search: { kind: 'pr', where: ['@me'] } }] },
    trigger: { event: 'checks_failed' },
    conditions: { match: 'all', of: [] },
    steps: [{ notify: {} }],
    limits: {},
  }
}

// scratchDraft is a new routine named name, in mode, the one new routines start in.
export function scratchDraft(name: string, mode: AutopilotMode): AutopilotDraft {
  return { id: 0, name, description: '', mode, spec: scratchSpec() }
}

// newRoutineName is a name no routine has: "New routine", else "New routine 2", and so on.
export function newRoutineName(routines: readonly Pick<AutopilotRoutine, 'name'>[]): string {
  const taken = new Set(routines.map((r) => r.name.toLowerCase()))

  for (let n = 1; ; n++) {
    const name = n === 1 ? 'New routine' : `New routine ${String(n)}`

    if (!taken.has(name.toLowerCase())) {
      return name
    }
  }
}

// sameDraft reports two drafts that would save the same.
export function sameDraft(a: AutopilotDraft, b: AutopilotDraft): boolean {
  return (
    a.name.trim() === b.name.trim() &&
    (a.description ?? '').trim() === (b.description ?? '').trim() &&
    a.mode === b.mode &&
    canonical(a.spec) === canonical(b.spec)
  )
}

// watchChanged reports a change to what a routine watches, fires on, checks, or fires once
// per: what the question about current matches is asked for. Once per decides the
// occurrence each match is for, so a change of it makes every current match a new one.
export function watchChanged(before: AutopilotSpec, after: AutopilotSpec): boolean {
  return (
    canonical(before.watch) !== canonical(after.watch) ||
    canonical(before.trigger) !== canonical(after.trigger) ||
    canonical(before.conditions) !== canonical(after.conditions) ||
    (before.limits.oncePer ?? '') !== (after.limits.oncePer ?? '')
  )
}

// testedKeys are what a test's findings hold for: its matches and records for the routine,
// what it watches, fires on, checks, and fires once per (key); its searches' notes and cost
// for what it watches (watch); and its warnings, which are by step, for its steps too
// (steps).
export function testedKeys(id: number, spec: AutopilotSpec): { key: string; watch: string; steps: string } {
  return {
    key: canonical({
      id,
      watch: spec.watch,
      trigger: spec.trigger,
      conditions: spec.conditions,
      oncePer: spec.limits.oncePer,
    }),
    watch: canonical(spec.watch),
    steps: canonical(spec.steps),
  }
}

// defaultSchedule is when On a schedule fires until it is changed: every day at 09:00.
export const defaultSchedule: NonNullable<AutopilotTrigger['schedule']> = { every: 'day', at: '09:00' }

// newTrigger is the trigger event as When starts it, with what it needs to fire filled in
// as the block shows it: On a schedule's every day at 09:00.
export function newTrigger(catalog: AutopilotCatalog, event: AutopilotEvent): AutopilotTrigger {
  const info = catalog.triggers.find((t) => t.event === event)

  return info?.options.some((o) => o.id === 'schedule') === true
    ? { event, schedule: { ...defaultSchedule } }
    : { event }
}

// prOnlyInvolving is how a search involves you that only a pull request can: asking for
// your review.
const prOnlyInvolving: AutopilotInvolving = 'reviewRequested'

// involvingFor are the ways a search of kind can involve you: an issue's leave out asking
// for your review.
export function involvingFor(kind: AutopilotSearch['kind']): AutopilotInvolving[] {
  const all: AutopilotInvolving[] = ['assigned', 'reviewRequested', 'mentioned']

  return kind === 'issue' ? all.filter((v) => v !== prOnlyInvolving) : all
}

// withKind is search looking for kind ('' for both): a search of issues drops what only a
// pull request has (its base and head branches, its state, and asking for your review),
// which the block no longer shows for it.
export function withKind(search: AutopilotSearch, kind: AutopilotSearch['kind'] | ''): AutopilotSearch {
  if (kind !== 'pr' && kind !== 'issue') {
    return omit(search, 'kind')
  }

  if (kind === 'pr') {
    return { ...search, kind }
  }

  const involving = (search.involving ?? []).filter((v) => v !== prOnlyInvolving)
  const rest = omit(omit(omit(omit(search, 'base'), 'headPrefix'), 'state'), 'involving')

  return { ...rest, kind, ...(involving.length > 0 ? { involving } : {}) }
}

// stepKinds are the keys a step may set, one each, in the catalog's order.
const stepKinds: readonly AutopilotStepKind[] = [
  'notify',
  'pin',
  'snooze',
  'dismiss',
  'restore',
  'markDone',
  'github',
  'agent',
  'waitForChecks',
  'merge',
  'postReview',
  'tellMe',
]

// stepKindOf is the kind of step a step is, or undefined for one that sets none.
export function stepKindOf(st: AutopilotStep): AutopilotStepKind | undefined {
  return stepKinds.find((k) => st[k] !== undefined && st[k] !== null)
}

// defaultTools is the tools preset an agent step runs with when it names none
// (catalog.DefaultPreset).
export const defaultTools = 'editTestPush'

// toolsOf is an agent step's tools preset, its default when it sets none.
export function toolsOf(a: AutopilotAgentStep): string {
  const tools = a.fence?.tools ?? ''

  return tools === '' ? defaultTools : tools
}

// placeOf is where an agent step runs, the background when it says nothing.
export function placeOf(a: AutopilotAgentStep): 'background' | 'terminal' {
  return a.where === 'terminal' ? 'terminal' : 'background'
}

// Prior is what the steps before one hold, for what may follow them.
interface Prior {
  agent: AutopilotAgentStep | undefined
  wait: boolean
  waitOnPush: boolean
}

// priorOf is what the steps before the one at index hold.
function priorOf(steps: readonly AutopilotStep[], index: number): Prior {
  const prior: Prior = { agent: undefined, wait: false, waitOnPush: false }

  for (const st of steps.slice(0, index)) {
    if (st.agent !== undefined && st.agent !== null) {
      prior.agent = st.agent
    } else if (st.waitForChecks !== undefined && st.waitForChecks !== null) {
      prior.wait = true
      prior.waitOnPush = st.onlyIf === 'pushed'
    }
  }

  return prior
}

// mayPush and readOnly report an agent step in the background whose tools let it push, and
// one whose tools change nothing.
function mayPush(catalog: AutopilotCatalog, a: AutopilotAgentStep | undefined): boolean {
  return a !== undefined && placeOf(a) === 'background' && catalog.presets.some((p) => p.id === toolsOf(a) && p.mayPush)
}

function readOnly(catalog: AutopilotCatalog, a: AutopilotAgentStep | undefined): boolean {
  return (
    a !== undefined && placeOf(a) === 'background' && catalog.presets.some((p) => p.id === toolsOf(a) && p.readOnly)
  )
}

// watchKinds are the kinds of item a spec's watch holds: every kind with no source.
export function watchKinds(catalog: AutopilotCatalog, spec: AutopilotSpec): ItemKind[] {
  if (spec.watch.sources.length === 0) {
    return [...bothKinds]
  }

  const seen = new Set<ItemKind>()

  for (const src of spec.watch.sources) {
    if (src.search !== undefined && src.search !== null) {
      for (const k of src.search.kind === 'pr' || src.search.kind === 'issue' ? [src.search.kind] : bothKinds) {
        seen.add(k)
      }
    }

    for (const l of src.lists ?? []) {
      for (const k of catalog.lists.find((info) => info.id === l)?.kinds ?? []) {
        seen.add(k)
      }
    }
  }

  return bothKinds.filter((k) => seen.has(k))
}

// actsOn are the kinds of item a routine acts on: those its watch holds and its trigger
// fires for.
export function actsOn(catalog: AutopilotCatalog, spec: AutopilotSpec): ItemKind[] {
  const trigger = catalog.triggers.find((t) => t.event === spec.trigger.event)

  return watchKinds(catalog, spec).filter((k) => trigger === undefined || trigger.kinds.includes(k))
}

// askable are the routines an item's menu can ask about: the user's own that load and act
// on its kind, on or off, in the list's order.
export function askable(
  catalog: AutopilotCatalog,
  routines: readonly AutopilotRoutine[],
  kind: ItemKind,
): AutopilotRoutine[] {
  return routines.filter((r) => !r.otherAccount && r.broken === '' && actsOn(catalog, r.spec).includes(kind))
}

// runnable are the routines an item's menu can run on it now: those it can ask about that
// are on.
export function runnable(
  catalog: AutopilotCatalog,
  routines: readonly AutopilotRoutine[],
  kind: ItemKind,
): AutopilotRoutine[] {
  return askable(catalog, routines, kind).filter((r) => r.enabled)
}

// fits reports kinds holding one that applies takes, or none at all.
function fits(kinds: readonly ItemKind[], applies: readonly ItemKind[]): boolean {
  return kinds.length === 0 || kinds.some((k) => applies.includes(k))
}

// AddChoice is a kind of step + Add a step offers: its label and help, and whether this
// build runs it, which a routine that has it waits in Dry run for.
export interface AddChoice {
  kind: AutopilotStepKind
  label: string
  help: string
  offered: boolean
}

// maxSteps is the most steps a routine has.
export const maxSteps = 10

// addable are the kinds of step that fit at the end of spec's steps, in the catalog's
// order: one that acts on the items it watches, an agent step only while there is none,
// Wait for the checks only after an agent step that may push, Merge it only after a wait
// on its push, Post the agent's review only after a read-only agent step, and only a
// notice after one in the terminal. A GitHub step waits for the catalog to list an action.
export function addable(catalog: AutopilotCatalog, spec: AutopilotSpec): AddChoice[] {
  if (spec.steps.length >= maxSteps) {
    return []
  }

  const prior = priorOf(spec.steps, spec.steps.length)
  const kinds = actsOn(catalog, spec)
  const terminal = prior.agent !== undefined && placeOf(prior.agent) === 'terminal'

  return catalog.steps
    .filter((info) => {
      if (!fits(kinds, info.kinds) || (terminal && info.kind !== 'notify' && info.kind !== 'tellMe')) {
        return false
      }

      switch (info.kind) {
        case 'github':
          return catalog.githubActions.some((a) => a.offered)
        case 'agent':
          return prior.agent === undefined
        case 'waitForChecks':
          return mayPush(catalog, prior.agent)
        case 'merge':
          return prior.wait && prior.waitOnPush
        case 'postReview':
          return readOnly(catalog, prior.agent)
        default:
          return true
      }
    })
    .map((info) => ({ kind: info.kind, label: info.label, help: info.help, offered: info.offered }))
}

// firstPlace is where a new agent step runs: the first place this build runs agents in, or
// undefined when the catalog offers none, which leaves the spec's own default.
export function firstPlace(places: readonly Pick<AutopilotPlace, 'id' | 'offered'>[]): AutopilotWhere | undefined {
  return places.find((p) => p.offered)?.id
}

// placeWhy says why an agent step can't run in a place this build doesn't run agents in.
export function placeWhy(place: Pick<AutopilotPlace, 'label'>): string {
  return `This version of ${brand.name} can’t run an agent ${place.label.toLowerCase()} yet.`
}

// newStep is a step of kind as + Add a step adds it, at the end of steps: each setting left
// to its default, an agent step where this build runs agents (places, the catalog's), Wait
// for the checks only if the agent pushed, Merge it only if they passed, and Tell me saying
// how the steps before it ended once there are some to say.
export function newStep(
  kind: AutopilotStepKind,
  steps: readonly AutopilotStep[],
  places: readonly Pick<AutopilotPlace, 'id' | 'offered'>[] = [],
): AutopilotStep {
  switch (kind) {
    case 'snooze':
      return { snooze: {} }
    case 'agent': {
      const where = firstPlace(places)

      return { agent: { agent: 'inherit', prompt: { itemDefault: true }, ...(where === undefined ? {} : { where }) } }
    }
    case 'waitForChecks':
      return { waitForChecks: {}, onlyIf: 'pushed' }
    case 'merge':
      return { merge: { how: 'autoMergeIfItWasOn' }, onlyIf: 'passed' }
    case 'postReview':
      return { postReview: { as: 'comment' } }
    case 'tellMe':
      return { tellMe: { when: steps.some((s) => s.agent !== undefined && s.agent !== null) ? 'verdict' : 'reached' } }
    case 'notify':
      return { notify: {} }
    case 'github':
      return { github: [] }
    default:
      return { [kind]: {} }
  }
}

// onlyIfChoices are the outcomes a step at index may run only if: those of a step before
// it that can end so. Wait for the checks follows the agent's push, and Merge it runs only
// if the checks passed.
export function onlyIfChoices(
  catalog: AutopilotCatalog,
  steps: readonly AutopilotStep[],
  index: number,
): AutopilotCatalog['outcomes'] {
  if (index === 0) {
    return []
  }

  const prior = priorOf(steps, index)
  const kind = steps[index] === undefined ? undefined : stepKindOf(steps[index])

  return catalog.outcomes.filter((o) => {
    if (kind === 'waitForChecks' && o.id === 'notPushed') {
      return false
    }

    if (kind === 'merge') {
      return o.id === 'passed'
    }

    if (o.from === 'waitForChecks') {
      return prior.wait
    }

    if (o.id === 'approve' || o.id === 'requestChanges') {
      return readOnly(catalog, prior.agent)
    }

    return mayPush(catalog, prior.agent)
  })
}

// moved is steps with the one at from moved to to.
export function moved<T>(list: readonly T[], from: number, to: number): T[] {
  const out = [...list]
  const [step] = out.splice(from, 1)

  if (step !== undefined) {
    out.splice(to, 0, step)
  }

  return out
}

// fieldsFor are the fields a condition can read on the kinds of item a routine acts on, in
// the catalog's order.
export function fieldsFor(catalog: AutopilotCatalog, kinds: readonly ItemKind[]): AutopilotField[] {
  return catalog.fields.filter((f) => fits(kinds, f.kinds))
}

// choiceListOps are the list operators a field whose list holds its choices takes.
const choiceListOps: readonly string[] = ['hasAny', 'hasAll', 'hasNone', 'isEmpty', 'notEmpty']

// opsFor are the operators a field takes: its type's, and for a list of choices only the
// set operators.
export function opsFor(catalog: AutopilotCatalog, field: AutopilotField): AutopilotOp[] {
  return catalog.ops.filter(
    (o) =>
      o.type === field.type && (field.type !== 'list' || field.choices.length === 0 || choiceListOps.includes(o.id)),
  )
}

// atPath reports a problem at path or inside it: "of[1]" holds "of[1].values[0]" and
// "of[1]", never "of[10]".
function atPath(field: string, path: string): boolean {
  return path === '' || field === path || field.startsWith(`${path}.`) || field.startsWith(`${path}[`)
}

// problemsAt are the problems in part at path or inside it, '' for the whole part.
export function problemsAt(problems: readonly AutopilotProblem[], part: AutopilotPart, path = ''): AutopilotProblem[] {
  return problems.filter((p) => p.part === part && atPath(p.field, path))
}

// problemsExactly are the problems in part at path itself.
export function problemsExactly(
  problems: readonly AutopilotProblem[],
  part: AutopilotPart,
  path: string,
): AutopilotProblem[] {
  return problems.filter((p) => p.part === part && p.field === path)
}

// shownIn reports a problem one of spec's blocks shows where it is: When and Limits show
// every problem of theirs; Watch its own and each source's, at it or inside it; If its own
// (the top list's match and its count) and each condition's or group's, whatever is inside
// it; and Then its own and each step's.
function shownIn(spec: AutopilotSpec, p: AutopilotProblem): boolean {
  const inside = (count: number, at: (i: number) => string) =>
    Array.from({ length: count }, (_, i) => at(i)).some((path) => atPath(p.field, path))

  switch (p.part) {
    case 'trigger':
    case 'limits':
      return true
    case 'watch':
      return (
        p.field === '' || p.field === 'sources' || inside(spec.watch.sources.length, (i) => `sources[${String(i)}]`)
      )
    case 'conditions':
      return p.field === 'of' || p.field === 'match' || inside(spec.conditions.of.length, (i) => `of[${String(i)}]`)
    case 'steps':
      return p.field === '' || inside(spec.steps.length, (i) => `[${String(i)}]`)
    default:
      return false
  }
}

// unshown are the problems no block of spec shows, which the builder lists at the top: the
// version's, the notices', the tools', and any at a place the blocks don't draw, as an
// imported spec can hold.
export function unshown(problems: readonly AutopilotProblem[], spec: AutopilotSpec): AutopilotProblem[] {
  return problems.filter((p) => !shownIn(spec, p))
}

// openRange is a range that bounds nothing, for a setting the catalog doesn't list.
const openRange = { min: 0, max: Number.MAX_SAFE_INTEGER, default: 0, unit: '' } as const

// settingRange is a whole-number setting's bounds and default, as the catalog lists them
// among settings.
export function settingRange(
  settings: readonly AutopilotSetting[],
  id: string,
): { min: number; max: number; default: number; unit: string } {
  return settings.find((s) => s.id === id)?.range ?? openRange
}

// heldWhy says, of each kind of step this build can't run yet, what it waits for.
const heldWhy: Readonly<Partial<Record<AutopilotStepKind, string>>> = {
  agent: `until this version of ${brand.name} can run agents`,
  github: `until this version of ${brand.name} can send GitHub actions`,
  markDone: `until this version of ${brand.name} can mark GitHub notifications done`,
  waitForChecks: `until this version of ${brand.name} can follow an agent’s push`,
  merge: `until this version of ${brand.name} can merge`,
  postReview: `until this version of ${brand.name} can post an agent’s review`,
}

// dryRunWhy says why a routine with steps of kinds runs in Dry run alone, a sentence a
// kind: "Agent steps run in Dry run until this version of <brand> can run agents."
export function dryRunWhy(catalog: AutopilotCatalog, kinds: readonly AutopilotStepKind[]): string {
  return kinds
    .map((k) => {
      const label = catalog.steps.find((s) => s.kind === k)?.label ?? k
      const why = heldWhy[k] ?? `until this version of ${brand.name} can run it`

      return k === 'agent' ? `Agent steps run in Dry run ${why}.` : `“${label}” runs in Dry run ${why}.`
    })
    .join(' ')
}

// percentText writes a share of the budget: "0.2%", "<0.1%".
function percentText(percent: number): string {
  if (percent > 0 && percent < 0.1) {
    return '<0.1%'
  }

  return `${percent.toFixed(percent >= 10 ? 0 : 1)}%`
}

// costLine says what a draft's searches add to each refresh: "About 1 point a refresh
// (0.2% of the budget)", or that watching the inbox lists alone costs nothing more; and,
// of a test of earlier searches (stale), that it is the last test's.
export function costLine(estimate: AutopilotEstimate, stale = false): string {
  const line =
    estimate.requests === 0
      ? 'Nothing more a refresh: the inbox reads its lists already.'
      : `About ${counted(estimate.pointsPerRefresh, 'point', 'points')} a refresh (${percentText(estimate.percent)} of the budget)`

  return stale ? `${line.replace(/\.$/, '')} · as last tested, before the searches changed` : line
}

// testHeadline says what a test found: "3 of 7 pull requests match now", and how many it
// can't tell yet.
export function testHeadline(result: AutopilotTestResult, kinds: readonly ItemKind[]): string {
  const noun = kinds.length === 1 ? (kinds[0] === 'pr' ? 'pull request' : 'issue') : 'item'
  const nouns = kinds.length === 1 ? (kinds[0] === 'pr' ? 'pull requests' : 'issues') : 'items'

  if (result.watched === 0) {
    return `No open ${nouns} to try it on now.`
  }

  const all = result.watched === 1 ? `1 open ${noun}` : `${String(result.watched)} open ${nouns}`
  const unknown = result.unknown > 0 ? `; ${String(result.unknown)} can’t be told yet` : ''

  return `${String(result.matched)} of ${all} ${result.matched === 1 ? 'matches' : 'match'} now${unknown}`
}

// testedLabel is Test's button: "Test (3 points)" once it spent some.
export function testedLabel(result: AutopilotTestResult | undefined): string {
  if (result === undefined || result.points === 0) {
    return result?.cached === true ? 'Test (cached)' : 'Test'
  }

  return `Test (${counted(result.points, 'point', 'points')})`
}

// botNames name the bots the sentence names by name.
const botNames: Readonly<Record<string, string>> = {
  dependabot: 'Dependabot',
  renovate: 'Renovate',
  'github-actions': 'GitHub Actions',
}

// authorLabel is how a token names an author: "You", "Dependabot", "octo-reviewer".
export function authorLabel(author: string): string {
  if (author === '@me') {
    return 'You'
  }

  if (author.startsWith('app/')) {
    const bot = author.slice(4)

    return botNames[bot] ?? bot
  }

  return author
}

// authorValue is what a typed or picked author is in a search: "@me" for you, "app/<bot>"
// for a bot, else the login.
export function authorValue(text: string, bots: readonly string[]): string {
  const t = text.trim().replace(/^@(?!me$)/, '')
  const lower = t.toLowerCase()

  if (lower === '@me' || lower === 'me' || lower === 'you') {
    return '@me'
  }

  if (lower.startsWith('app/')) {
    return lower
  }

  const bot = lower.replace(/\[bot\]$/, '')

  if (bots.some((b) => b.toLowerCase().replace(/\[bot\]$/, '') === bot) || bot !== lower) {
    return `app/${bot}`
  }

  return t
}

// placeLabel is how a token names a place: "Your account", "acme-org", "acme-org/api".
export function placeLabel(where: string): string {
  if (where === '@me') {
    return 'Your account'
  }

  const at = where.indexOf(':')

  return at < 0 ? where : where.slice(at + 1)
}

// placeValue is what a typed or picked place is in a search: "@me" for your account, a
// repository for owner/name, an org for any other name, and as typed when it names its
// qualifier (user:octo-user).
export function placeValue(text: string, viewer: string): string {
  const t = text.trim()
  const lower = t.toLowerCase()

  if (lower === '@me' || lower === 'your account' || (viewer !== '' && lower === viewer.toLowerCase())) {
    return '@me'
  }

  if (/^(user|org|repo):/.test(lower)) {
    return t.replace(/^[^:]+/, (q) => q.toLowerCase())
  }

  return t.includes('/') ? `repo:${t}` : `org:${t}`
}

// outcomeLabel is how an Only if menu says an outcome: "only if it pushed".
export function outcomeLabel(catalog: AutopilotCatalog, id: AutopilotOutcome): string {
  return `only if ${catalog.outcomes.find((o) => o.id === id)?.label ?? id}`
}

// lengthParts reads a duration condition's length, "30m", into its number and unit.
export function lengthParts(value: string): { n: string; unit: 'm' | 'h' | 'd' } {
  const unit = value.at(-1)

  return {
    n: value.slice(0, -1),
    unit: unit === 'm' || unit === 'h' ? unit : 'd',
  }
}
