import { describe, expect, it } from 'vitest'

import type { PerAgent, PromptUsage } from '../platform'
import { agents, claudeAgent, codexAgent } from '../test/fixtures'
import {
  agentName,
  anyEffortChoices,
  effortLabel,
  kindPlural,
  modelLabel,
  pinSummary,
  planToggleLabel,
  isAgentPrompt,
  promptApplies,
  routinePlaces,
  routinesUsing,
  sourceLabel,
  usageCount,
  usagePlaces,
  usageSummary,
} from './launch'

const noUsage: PromptUsage = {
  defaults: [],
  slots: [],
  orgs: [],
  repos: [],
  slotPlaces: [],
  kinds: [],
  replies: [],
  routines: [],
}

describe('labels', () => {
  it('names where a setting came from', () => {
    expect(sourceLabel('repo')).toBe('repo default')
    expect(sourceLabel('agent')).toBe('agent default')
    expect(sourceLabel('launch')).toBe('this launch')
  })

  it('names models and effort levels', () => {
    expect(modelLabel('opus', claudeAgent().models)).toBe('Opus')
    expect(modelLabel('claude-opus-5-5', claudeAgent().models)).toBe('claude-opus-5-5')
    expect(modelLabel('')).toBe('Agent default')
    expect(effortLabel('xhigh', claudeAgent().effortLevels)).toBe('Extra high')
    expect(effortLabel('')).toBe('Agent default')
    expect(effortLabel('turbo', claudeAgent().effortLevels)).toBe('turbo')
  })

  it('names kinds in the plural', () => {
    expect(kindPlural('pr')).toBe('pull requests')
    expect(kindPlural('issue')).toBe('issues')
  })
})

describe('isAgentPrompt', () => {
  it('leaves out review replies', () => {
    expect(isAgentPrompt({ purpose: 'agent' })).toBe(true)
    expect(isAgentPrompt({ purpose: 'reply' })).toBe(false)
  })
})

describe('promptApplies', () => {
  it('matches a prompt to the kinds it is for', () => {
    expect(promptApplies({ appliesTo: 'any' }, 'issue')).toBe(true)
    expect(promptApplies({ appliesTo: 'pr' }, 'pr')).toBe(true)
    expect(promptApplies({ appliesTo: 'pr' }, 'issue')).toBe(false)
  })

  it('keeps a prompt for repositories out of every item’s launch, and offers a repository nothing else', () => {
    expect(promptApplies({ appliesTo: 'repo' }, 'pr')).toBe(false)
    expect(promptApplies({ appliesTo: 'repo' }, 'issue')).toBe(false)
    expect(promptApplies({ appliesTo: 'repo' }, '')).toBe(true)
    expect(promptApplies({ appliesTo: 'any' }, '')).toBe(false)
    expect(promptApplies({ appliesTo: 'pr' }, '')).toBe(false)
    expect(promptApplies({ appliesTo: 'issue' }, '')).toBe(false)
  })
})

describe('usage', () => {
  it('summarizes where a prompt is a default', () => {
    expect(usageSummary(noUsage)).toBe('Not a default anywhere')
    expect(usageSummary({ ...noUsage, defaults: ['pr'], orgs: ['acme-org'], repos: ['a/b', 'a/c'] })).toBe(
      'App default for pull requests · 1 org · 2 repos',
    )
    expect(usageSummary({ ...noUsage, defaults: ['pr', 'issue'] })).toBe('App default for pull requests and issues')
  })

  it('names the situations a prompt is a default for', () => {
    const one = { org: 'acme-org', repo: '', slot: 'checks_failing' } as const
    const two = { org: '', repo: 'acme-org/api', slot: 'rereview' } as const

    expect(usageSummary({ ...noUsage, slots: ['checks_failing'] })).toBe('App default when checks fail')
    expect(usageSummary({ ...noUsage, slots: ['conflicts', 'changes_requested'], slotPlaces: [one] })).toBe(
      'App default when it has conflicts and when changes are requested · acme-org’s prompt when checks fail',
    )
    expect(usageSummary({ ...noUsage, orgs: ['acme-org'], slotPlaces: [one, two] })).toBe(
      '1 org · A situation’s prompt in 2 places',
    )

    const usage: PromptUsage = { ...noUsage, slots: ['rereview'], repos: ['acme-org/api'], slotPlaces: [one, two] }

    expect(usageCount(usage)).toBe(4)
    expect(usagePlaces(usage)).toEqual([
      'App default when you’re asked to review again',
      'acme-org/api',
      'acme-org’s prompt when checks fail',
      'acme-org/api’s prompt when you’re asked to review again',
    ])
  })

  it('counts and lists the places', () => {
    const usage: PromptUsage = { ...noUsage, defaults: ['issue'], orgs: ['acme-org'], repos: ['acme-org/api'] }

    expect(usageCount(usage)).toBe(3)
    expect(usagePlaces(usage)).toEqual(['App default for issues', 'acme-org (all repositories)', 'acme-org/api'])
  })

  it('says which routines run a prompt, by name for one', () => {
    expect(routinesUsing(noUsage)).toBe('')
    expect(routinesUsing({ ...noUsage, routines: ['Triage bugs'] })).toBe('Used by “Triage bugs”')
    expect(routinesUsing({ ...noUsage, routines: ['Fix checks', 'Triage bugs'] })).toBe('Used by 2 routines')
    expect(routinePlaces({ ...noUsage, routines: ['Fix checks'] })).toEqual(['The routine “Fix checks”'])
    // A routine is no default, so it counts in no place.
    expect(usageCount({ ...noUsage, routines: ['Fix checks'] })).toBe(0)
  })
})

describe('pinSummary', () => {
  it('lists the settings a prompt sets, each model under its agent', () => {
    const all = agents()

    expect(pinSummary({ models: {}, effort: '', planMode: null, worktree: null, extraArgs: {} }, all)).toEqual([])
    expect(
      pinSummary(
        {
          models: { claude: 'sonnet', codex: 'gpt-6-sol' },
          effort: 'ultra',
          planMode: true,
          worktree: false,
          extraArgs: { claude: ['--advisor', 'fable'], codex: ['--search'] },
        },
        all,
      ),
    ).toEqual(['Claude: Sonnet', 'Codex: GPT-6-Sol', 'ultra effort', 'plan mode', 'no worktree', '3 flags'])
    expect(pinSummary({ models: {}, effort: '', planMode: false, worktree: true, extraArgs: {} }, all)).toEqual([
      'no plan mode',
      'worktree',
    ])
  })

  it('names a setting for an agent this build does not know by its ID', () => {
    // A prompt imported from a newer build can carry an agent this one does not have.
    const models = { 'example-agent': 'big' } as PerAgent<string>
    const extraArgs = { 'example-agent': ['--fast'], codex: null } as PerAgent<string[] | null>

    expect(pinSummary({ models, effort: '', planMode: null, worktree: null, extraArgs }, agents())).toEqual([
      'example-agent: big',
      '1 flag',
    ])
  })

  it('shows a model no agent suggests as stored', () => {
    expect(
      pinSummary({ models: { codex: 'gpt-x' }, effort: '', planMode: null, worktree: null, extraArgs: {} }, agents()),
    ).toEqual(['Codex: gpt-x'])
  })
})

describe('agent choices', () => {
  it('lists every agent’s effort levels once, in order', () => {
    expect(anyEffortChoices(agents()).map((c) => c.value)).toEqual(['low', 'medium', 'high', 'xhigh', 'max', 'ultra'])
  })

  it('names models and efforts by their labels, and unknown ones as stored', () => {
    const codex = codexAgent()

    expect(modelLabel('gpt-6-luna', codex.models)).toBe('GPT-6-Luna')
    expect(effortLabel('ultra', codex.effortLevels)).toBe('Ultra')
    // Claude has no ultra level, so it reads as stored.
    expect(effortLabel('ultra', claudeAgent().effortLevels)).toBe('ultra')
  })

  it('names an agent, and says plan mode in its words', () => {
    expect(agentName(agents(), 'codex')).toBe('Codex')
    // An agent this build does not know is named by its ID.
    expect(agentName(agents(), '')).toBe('')
    expect(agentName([], 'codex')).toBe('codex')
    expect(planToggleLabel(codexAgent())).toBe('Start read-only')
    expect(planToggleLabel(claudeAgent())).toBe('Start in plan mode')
  })
})
