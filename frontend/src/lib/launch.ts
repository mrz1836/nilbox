// Shared wording and choices for launch settings: where a resolved setting came from, the
// agents and the models and effort levels each offers, and short summaries of prompts.

import type {
  Agent,
  AgentID,
  Choice,
  ItemKind,
  LaunchSource,
  PerAgent,
  Prompt,
  PromptAppliesTo,
  PromptUsage,
} from '../platform'
import { counted } from './format'

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
}

// promptApplies reports whether a prompt can launch an item of kind.
export function promptApplies(prompt: Pick<Prompt, 'appliesTo'>, kind: ItemKind): boolean {
  return prompt.appliesTo === 'any' || prompt.appliesTo === kind
}

// kindPlural names a kind of item in the plural.
export function kindPlural(kind: ItemKind): string {
  return kind === 'pr' ? 'pull requests' : 'issues'
}

// usageCount is how many places use a prompt as a default: the app's settings for each
// kind, and every org and repository.
export function usageCount(usage: PromptUsage): number {
  return usage.defaults.length + usage.orgs.length + usage.repos.length
}

// usageSummary says where a prompt is a default, as in "App default for pull requests ·
// 2 orgs · 1 repo", or "Not a default anywhere".
export function usageSummary(usage: PromptUsage): string {
  const parts: string[] = []

  if (usage.defaults.length > 0) {
    parts.push(`App default for ${usage.defaults.map(kindPlural).join(' and ')}`)
  }

  const orgs = usage.orgs.length
  const repos = usage.repos.length

  if (orgs > 0) {
    parts.push(counted(orgs, 'org', 'orgs'))
  }

  if (repos > 0) {
    parts.push(counted(repos, 'repo', 'repos'))
  }

  return parts.length === 0 ? 'Not a default anywhere' : parts.join(' · ')
}

// usagePlaces lists every place a prompt is a default, for a tooltip or a dialog.
export function usagePlaces(usage: PromptUsage): string[] {
  return [
    ...usage.defaults.map((kind) => `App default for ${kindPlural(kind)}`),
    ...usage.orgs.map((org) => `${org} (all repositories)`),
    ...usage.repos,
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
