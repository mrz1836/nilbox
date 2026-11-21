// Shared wording and choices for launch settings: where a resolved setting came from, the
// agents and the models and effort levels each offers, and short summaries of prompts.

import type {
  Agent,
  AgentID,
  AgentInfo,
  Choice,
  DefaultPrompts,
  DefaultSlot,
  ItemKind,
  LaunchDefaults,
  LaunchDefaultsInput,
  LaunchSource,
  PerAgent,
  Profile,
  Prompt,
  PromptAppliesTo,
  PromptUsage,
  Settings,
  Situation,
  SlotPlace,
} from '../platform'
import { counted } from './format'

// LaunchOptions are what every launch control shares (LaunchControls): the coding agents,
// profiles, and prompts to choose from, and the saved launch defaults. Each row's own
// resolved settings and worktree come apart from them, so a change to one item's renders
// only its row.
export interface LaunchOptions {
  // agents are the coding agents a launch can start, each with whether it is installed.
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  // launchDefaults are every saved org and repo default, for saving a launch's choices.
  launchDefaults: readonly LaunchDefaults[]
  // onDefaultsSaved reloads the launch defaults and every row's settings.
  onDefaultsSaved: () => Promise<void>
  // onManagePrompts opens the Prompts page.
  onManagePrompts: () => void
  // onSettingsSaved takes the settings a change made from a launch stored, such as a
  // repository location added for a clone found elsewhere.
  onSettingsSaved: (settings: Settings) => void
}

// sourceLabels name where a resolved launch setting came from.
const sourceLabels: Readonly<Record<LaunchSource, string>> = {
  launch: 'this launch',
  prompt: 'prompt',
  repo: 'repo default',
  org: 'org default',
  profile: 'profile',
  app: 'app default',
  agent: 'agent default',
  // The settings of an item that cannot resolve come from nowhere.
  '': '',
}

// sourceLabel names a setting's source for people, as in "repo default".
export function sourceLabel(source: LaunchSource): string {
  return sourceLabels[source]
}

// agentOf returns the agent with id among agents, or undefined when there is none.
export function agentOf<A extends Pick<Agent, 'id'>>(agents: readonly A[], id: AgentID): A | undefined {
  return agents.find((a) => a.id === id)
}

// agentName names an agent for people, as in "Codex", or by its ID when unknown.
export function agentName(agents: readonly Pick<Agent, 'id' | 'name'>[], id: AgentID): string {
  return agentOf(agents, id)?.name ?? id
}

// modelChoices lists the models an agent suggests, or none for an agent this build does
// not know.
export function modelChoices(agents: readonly Pick<Agent, 'id' | 'models'>[], id: AgentID): readonly Choice[] {
  return agentOf(agents, id)?.models ?? []
}

// anyEffortChoices lists every effort level some agent accepts, lowest first, for an
// effort shared by every agent, as prompts and launch defaults set it.
export function anyEffortChoices(agents: readonly Pick<Agent, 'effortLevels'>[]): Choice[] {
  const out: Choice[] = []

  for (const a of agents) {
    for (const level of a.effortLevels) {
      if (!out.some((c) => c.value === level.value)) {
        out.push(level)
      }
    }
  }

  const order = ['minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra']

  return out.sort((a, b) => order.indexOf(a.value) - order.indexOf(b.value))
}

// modelLabel names a model value among an agent's suggestions: an alias by its name, ""
// as the agent's default, and any other model name as written.
export function modelLabel(value: string, models: readonly Choice[] = []): string {
  if (value === '') {
    return 'Agent default'
  }

  return models.find((c) => c.value === value)?.label ?? value
}

// effortLabel names an effort level, with "" as the agent's default.
export function effortLabel(value: string, levels: readonly Choice[] = []): string {
  if (value === '') {
    return 'Agent default'
  }

  return levels.find((c) => c.value === value)?.label ?? value
}

// planPhrase words how a session starts in an agent's planning mode: "in plan mode" for
// Claude, "read-only" for Codex.
export function planPhrase(agent: Pick<Agent, 'planLabel'>): string {
  const plan = agent.planLabel.toLowerCase()

  return plan.endsWith('mode') ? `in ${plan}` : plan
}

// planToggleLabel words an agent's planning mode as a launch option: "Start in plan mode"
// for Claude, "Start read-only" for Codex.
export function planToggleLabel(agent: Pick<Agent, 'planLabel'>): string {
  return `Start ${planPhrase(agent)}`
}

// perAgentEntries lists a per-agent setting's entries that are set, in the agents' order
// and then any other agent's.
export function perAgentEntries<T>(
  values: PerAgent<T>,
  agents: readonly Pick<Agent, 'id'>[],
): [Exclude<AgentID, ''>, T][] {
  const known = agents.map((a) => a.id).filter((id): id is Exclude<AgentID, ''> => id !== '')
  const ids = [...known, ...Object.keys(values).filter((id) => !known.includes(id as Exclude<AgentID, ''>))] as Exclude<
    AgentID,
    ''
  >[]
  const out: [Exclude<AgentID, ''>, T][] = []

  for (const id of ids) {
    const v = values[id]

    if (v !== undefined) {
      out.push([id, v])
    }
  }

  return out
}

// appliesToLabels name what a prompt is for.
export const appliesToLabels: Readonly<Record<PromptAppliesTo, string>> = {
  pr: 'Pull requests',
  issue: 'Issues',
  any: 'Both',
  repo: 'Repositories',
}

// isAgentPrompt reports a prompt a launch can use: every prompt but a review reply, which
// the review sheet fills in as a review's text and never launches an agent.
export function isAgentPrompt(prompt: Pick<Prompt, 'purpose'>): boolean {
  return prompt.purpose !== 'reply'
}

// promptApplies reports whether a prompt can launch an item of kind.
export function promptApplies(prompt: Pick<Prompt, 'appliesTo'>, kind: ItemKind): boolean {
  return prompt.appliesTo === 'any' || prompt.appliesTo === kind
}

// kindPlural names a kind of item in the plural.
export function kindPlural(kind: ItemKind): string {
  return kind === 'pr' ? 'pull requests' : 'issues'
}

// PullSituation is a situation a pull request can be in, whose prompt the launch defaults
// set: every Situation but '', the item's own prompt.
export type PullSituation = Exclude<Situation, ''>

// situations are the situations a pull request can be in, in the order the settings list
// them.
export const situations: readonly PullSituation[] = ['checks_failing', 'conflicts', 'changes_requested', 'rereview']

// situationInfo is each situation's words and fields: when words it in a sentence, as in
// "Prompt when checks fail"; who is the pull request it is about, for the settings; app is
// its field in the app's default prompts, and field its prompt's in an org's or a
// repository's launch defaults.
export const situationInfo = {
  checks_failing: {
    when: 'when checks fail',
    who: 'your pull request whose checks fail',
    app: 'checksFailing',
    field: 'checksPromptId',
  },
  conflicts: {
    when: 'when it has conflicts',
    who: 'your pull request that conflicts with its base',
    app: 'conflicts',
    field: 'conflictsPromptId',
  },
  changes_requested: {
    when: 'when changes are requested',
    who: 'your pull request a reviewer asked to change',
    app: 'changesRequested',
    field: 'changesPromptId',
  },
  rereview: {
    when: 'when you’re asked to review again',
    who: 'a pull request you’re asked to review that changed since your review',
    app: 'rereview',
    field: 'rereviewPromptId',
  },
} as const satisfies Readonly<
  Record<PullSituation, { when: string; who: string; app: keyof DefaultPrompts; field: keyof LaunchDefaultsInput }>
>

// slotWhen words the situation a default slot of the usage is for, as in "when checks
// fail". The usage names a slot only for a situation's default.
function slotWhen(slot: DefaultSlot): string {
  return situationInfo[slot as PullSituation].when
}

// usageCount is how many places use a prompt as a default: the app's settings for each
// kind and each situation, and every org and repository.
export function usageCount(usage: PromptUsage): number {
  return usage.defaults.length + usage.slots.length + usage.orgs.length + usage.repos.length + usage.slotPlaces.length
}

// usageSummary says where a prompt is a default, as in "App default for pull requests ·
// App default when checks fail · 2 orgs · 1 repo · acme-org’s prompt when it has
// conflicts", or "Not a default anywhere". Several orgs' and repositories' situations are
// counted, as in "A situation’s prompt in 3 places".
export function usageSummary(usage: PromptUsage): string {
  const parts: string[] = []

  if (usage.defaults.length > 0) {
    parts.push(`App default for ${usage.defaults.map(kindPlural).join(' and ')}`)
  }

  if (usage.slots.length > 0) {
    parts.push(`App default ${usage.slots.map(slotWhen).join(' and ')}`)
  }

  const orgs = usage.orgs.length
  const repos = usage.repos.length

  if (orgs > 0) {
    parts.push(counted(orgs, 'org', 'orgs'))
  }

  if (repos > 0) {
    parts.push(counted(repos, 'repo', 'repos'))
  }

  const [place, ...more] = usage.slotPlaces

  if (place !== undefined) {
    parts.push(
      more.length === 0 ? slotPlaceText(place) : `A situation’s prompt in ${String(usage.slotPlaces.length)} places`,
    )
  }

  return parts.length === 0 ? 'Not a default anywhere' : parts.join(' · ')
}

// slotPlaceText names an org's or a repository's situation a prompt is the default for, as
// in "acme-org’s prompt when checks fail".
function slotPlaceText(place: SlotPlace): string {
  return `${place.org === '' ? place.repo : place.org}’s prompt ${slotWhen(place.slot)}`
}

// routinesUsing says which Autopilot routines run or comment a prompt, as in "Used by
// “Triage bugs”" or "Used by 3 routines", or '' when none does.
export function routinesUsing(usage: PromptUsage): string {
  const [only] = usage.routines

  if (only === undefined) {
    return ''
  }

  return usage.routines.length === 1 ? `Used by “${only}”` : `Used by ${String(usage.routines.length)} routines`
}

// routinePlaces lists the routines that run or comment a prompt, for a tooltip or a dialog.
export function routinePlaces(usage: PromptUsage): string[] {
  return usage.routines.map((name) => `The routine “${name}”`)
}

// usagePlaces lists every place a prompt is a default, for a tooltip or a dialog: the
// app's for a kind or a situation, as in "App default when checks fail", an org's or a
// repository's for a kind, and an org's or a repository's for a situation, as in
// "acme-org’s prompt when checks fail".
export function usagePlaces(usage: PromptUsage): string[] {
  return [
    ...usage.defaults.map((kind) => `App default for ${kindPlural(kind)}`),
    ...usage.slots.map((slot) => `App default ${slotWhen(slot)}`),
    ...usage.orgs.map((org) => `${org} (all repositories)`),
    ...usage.repos,
    ...usage.slotPlaces.map(slotPlaceText),
  ]
}

// pinSummary lists the launch settings a prompt or a set of launch defaults pins, as short
// chips: each agent's model, the effort, plan mode, the worktree, and how many extra flags.
export function pinSummary(
  p: Pick<Prompt, 'models' | 'effort' | 'planMode' | 'worktree' | 'extraArgs'>,
  agents: readonly Pick<Agent, 'id' | 'name' | 'models' | 'effortLevels'>[],
): string[] {
  const pins: string[] = []

  for (const [id, model] of perAgentEntries(p.models, agents)) {
    if (model !== '') {
      pins.push(`${agentName(agents, id)}: ${modelLabel(model, modelChoices(agents, id))}`)
    }
  }

  if (p.effort !== '') {
    pins.push(`${effortLabel(p.effort, anyEffortChoices(agents)).toLowerCase()} effort`)
  }

  if (p.planMode !== null) {
    pins.push(p.planMode ? 'plan mode' : 'no plan mode')
  }

  if (p.worktree !== null) {
    pins.push(p.worktree ? 'worktree' : 'no worktree')
  }

  const flags = perAgentEntries(p.extraArgs, agents).reduce((n, [, args]) => n + (args ?? []).length, 0)

  if (flags > 0) {
    pins.push(counted(flags, 'flag', 'flags'))
  }

  return pins
}
