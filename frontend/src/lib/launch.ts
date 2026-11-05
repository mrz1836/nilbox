// Shared wording and choices for launch settings: where a resolved setting came from,
// the models and effort levels the agent offers, and short summaries of prompts.

import type { Prompt, PromptUsage } from '../platform'
import { counted } from './format'

// sourceLabels name where a resolved launch setting came from.
const sourceLabels: Readonly<Record<string, string>> = {
  launch: 'this launch',
  prompt: 'prompt',
  repo: 'repo default',
  org: 'org default',
  profile: 'profile',
  app: 'app default',
  agent: 'agent default',
}

// sourceLabel names a setting's source for people, as in "repo default".
export function sourceLabel(source: string): string {
  return sourceLabels[source] ?? source
}

export interface Choice {
  value: string
  label: string
}

// modelChoices are the model aliases the agent accepts; a full model name works too.
export const modelChoices: readonly Choice[] = [
  { value: 'fable', label: 'Fable' },
  { value: 'opus', label: 'Opus' },
  { value: 'sonnet', label: 'Sonnet' },
  { value: 'haiku', label: 'Haiku' },
]

// effortChoices are the agent's effort levels, lowest first.
export const effortChoices: readonly Choice[] = [
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
  { value: 'xhigh', label: 'Extra high' },
  { value: 'max', label: 'Max' },
]

// modelLabel names a model value: an alias by its name, "" as the agent's default, and
// a full model name as written.
export function modelLabel(value: string): string {
  if (value === '') {
    return 'Agent default'
  }

  return modelChoices.find((c) => c.value === value)?.label ?? value
}

// effortLabel names an effort level, with "" as the agent's default.
export function effortLabel(value: string): string {
  if (value === '') {
    return 'Agent default'
  }

  return effortChoices.find((c) => c.value === value)?.label ?? value
}

// appliesToLabels name what a prompt is for.
export const appliesToLabels: Readonly<Record<string, string>> = {
  pr: 'Pull requests',
  issue: 'Issues',
  any: 'Both',
}

// promptApplies reports whether a prompt can launch an item of kind ("pr" or "issue").
export function promptApplies(prompt: Pick<Prompt, 'appliesTo'>, kind: string): boolean {
  return prompt.appliesTo === 'any' || prompt.appliesTo === kind
}

// kindPlural names a kind of item in the plural.
export function kindPlural(kind: string): string {
  return kind === 'pr' ? 'pull requests' : 'issues'
}

// usageCount is how many places use a prompt as a default: the app's settings for each
// kind, and every org and repository.
export function usageCount(usage: PromptUsage): number {
  return (usage.defaults?.length ?? 0) + (usage.orgs?.length ?? 0) + (usage.repos?.length ?? 0)
}

// usageSummary says where a prompt is a default, as in "App default for pull requests ·
// 2 orgs · 1 repo", or "Not a default anywhere".
export function usageSummary(usage: PromptUsage): string {
  const parts: string[] = []
  const defaults = usage.defaults ?? []

  if (defaults.length > 0) {
    parts.push(`App default for ${defaults.map(kindPlural).join(' and ')}`)
  }

  const orgs = usage.orgs?.length ?? 0
  const repos = usage.repos?.length ?? 0

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
    ...(usage.defaults ?? []).map((kind) => `App default for ${kindPlural(kind)}`),
    ...(usage.orgs ?? []).map((org) => `${org} (all repositories)`),
    ...(usage.repos ?? []),
  ]
}

// pinSummary lists the launch settings a prompt sets, as short chips.
export function pinSummary(p: Pick<Prompt, 'model' | 'effort' | 'planMode' | 'worktree' | 'extraArgs'>): string[] {
  const pins: string[] = []

  if (p.model !== '') {
    pins.push(modelLabel(p.model))
  }

  if (p.effort !== '') {
    pins.push(`${effortLabel(p.effort).toLowerCase()} effort`)
  }

  if (p.planMode !== null) {
    pins.push(p.planMode ? 'plan mode' : 'no plan mode')
  }

  if (p.worktree !== null) {
    pins.push(p.worktree ? 'worktree' : 'no worktree')
  }

  const flags = p.extraArgs?.length ?? 0

  if (flags > 0) {
    pins.push(counted(flags, 'flag', 'flags'))
  }

  return pins
}
