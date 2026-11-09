// Filtering the Worktrees view with the toolbar's search field (see lib/query): which
// clones and worktrees a query keeps, and what the field suggests while it is typed.
// Everything it reads is in the view's latest listing, so filtering runs no git and asks
// GitHub nothing.

import type { AgentID, Worktree, WorktreeRepo } from '../platform'
import {
  fold,
  haystack,
  isEmpty,
  matchesFilter,
  matchesTerms,
  parseQuery,
  suggest,
  type Facet,
  type Query,
  type QuerySpec,
  type Suggestions,
} from './query'
import type { WorktreeErrors } from './useWorktrees'

// worktreeSpec is what the Worktrees view's filter understands: the clone, the branch,
// the agent that worked there, and the states below. A worktree can be in several states
// at once, so repeated is: filters must all match.
export const worktreeSpec: QuerySpec = { keys: ['repo', 'branch', 'agent', 'is'], multi: ['is'] }

// worktreeHint says what words match, at the foot of the field's suggestions.
export const worktreeHint = 'Matches branches, paths, repos, #numbers, titles · -word excludes'

// states are the is: values, each with whether a worktree is in it: the worktree's own
// state, then its pull request or issue's kind and state.
const states: readonly (readonly [string, (wt: Worktree) => boolean])[] = [
  ['dirty', (wt) => wt.dirty],
  ['locked', (wt) => wt.locked],
  ['in-use', (wt) => wt.inUse.length > 0 || wt.stage === 'in-use'],
  ['finished', (wt) => wt.stage === 'finished'],
  ['review', (wt) => wt.stage === 'needs-review'],
  ['pr', (wt) => wt.item?.kind === 'pr'],
  ['issue', (wt) => wt.item?.kind === 'issue'],
  ['open', (wt) => wt.item?.state === 'open'],
  ['merged', (wt) => wt.item?.state === 'merged'],
  ['closed', (wt) => wt.item?.state === 'closed'],
]

// knownStates are the is: values, all known, so is:pr is never a part of another.
const knownStates: ReadonlySet<string> = new Set(states.map(([name]) => name))

// facets are what the field offers on an empty query. The states a worktree is seldom in,
// and the agents, show only while some worktree is.
const facets: readonly Facet[] = [
  { label: 'Dirty', key: 'is', value: 'dirty' },
  { label: 'Locked', key: 'is', value: 'locked', optional: true },
  { label: 'In use', key: 'is', value: 'in-use', optional: true },
  { label: 'Ready to clean up', key: 'is', value: 'finished', optional: true },
  { label: 'Needs a look', key: 'is', value: 'review', optional: true },
  { label: 'Repository', key: 'repo', noun: ['repo', 'repos'] },
  { label: 'Branch', key: 'branch', noun: ['branch', 'branches'] },
  { label: 'Agent', key: 'agent', noun: ['agent', 'agents'], optional: true },
]

// nouns name each key's values, for the suggestions list.
const nouns: Readonly<Record<string, string>> = {
  repo: 'Repositories',
  branch: 'Branches',
  agent: 'Agents',
  is: 'States',
}

// FilteredRepo is a clone as the filter shows it: the clone, and the worktrees of it the
// filter keeps.
export interface FilteredRepo {
  repo: WorktreeRepo
  worktrees: readonly Worktree[]
}

// WorktreeFilter is what a query keeps: the clones to show, each with its worktrees, and
// how many worktrees matched.
export interface WorktreeFilter {
  repos: readonly FilteredRepo[]
  matches: number
}

export interface WorktreeFilterOptions {
  // agentName names a coding agent for people, so agent:codex and the word "Codex" both
  // find its worktrees. Without it, an agent is known by its ID alone.
  agentName?: ((id: AgentID) => string) | undefined
  // errors are the failures of actions taken in the view, which keep their clone or
  // worktree in sight.
  errors?: WorktreeErrors | undefined
}

// Entry is one worktree as the filter reads it.
interface Entry {
  clone: WorktreeRepo
  wt: Worktree
  // hay is what its words match.
  hay: string
  // number is its pull request or issue's number, if it has one.
  number: number | undefined
  // repo, branch, agent, and states are its values for each key, folded: the agents by
  // their IDs and their names.
  repo: readonly string[]
  branch: readonly string[]
  agent: readonly string[]
  states: readonly string[]
  // agents are the IDs of the agents that worked in it, for the agent: suggestions.
  agents: readonly AgentID[]
}

// haveOf is a worktree's values for a key, folded.
function haveOf(e: Entry, key: string): readonly string[] {
  switch (key) {
    case 'repo':
      return e.repo
    case 'branch':
      return e.branch
    case 'agent':
      return e.agent
    default:
      return e.states
  }
}

// agentsOf lists the agents of a worktree: the one last launched there, then those of
// the sessions working in it, each once.
function agentsOf(wt: Worktree): AgentID[] {
  return [...new Set([wt.agent, ...wt.inUse.map((s) => s.agent)].filter((id) => id !== ''))]
}

// branchOf is a worktree's branch, or '' when its HEAD is detached.
function branchOf(wt: Worktree): string {
  return wt.detached ? '' : wt.branch
}

// entryOf reads one worktree of a clone for matching.
function entryOf(repo: WorktreeRepo, wt: Worktree, name: (id: AgentID) => string): Entry {
  const agents = agentsOf(wt)
  const named = agents.flatMap((id) => [id, name(id)])
  const item = wt.item
  const branch = branchOf(wt)

  return {
    clone: repo,
    wt,
    hay: haystack([
      branch,
      wt.displayPath,
      repo.name,
      item === null ? '' : `${item.owner}/${item.name}`,
      item === null ? '' : `#${String(item.number)}`,
      item?.title ?? '',
      ...named,
    ]),
    number: item?.number,
    repo: [fold(repo.name)],
    branch: branch === '' ? [] : [fold(branch)],
    agent: named.map(fold),
    states: states.filter(([, holds]) => holds(wt)).map(([state]) => state),
    agents,
  }
}

// Index is every worktree of the listing read for matching, with the values each key
// knows, which a filter value that names one matches exactly.
interface Index {
  entries: readonly Entry[]
  known: (key: string) => ReadonlySet<string>
}

// indexOf reads a listing for matching.
function indexOf(repos: readonly WorktreeRepo[], options: WorktreeFilterOptions): Index {
  const name = options.agentName ?? ((id: AgentID) => id)
  const entries = repos.flatMap((repo) => repo.worktrees.map((wt) => entryOf(repo, wt, name)))
  const sets = {
    repo: new Set(entries.flatMap((e) => e.repo)),
    branch: new Set(entries.flatMap((e) => e.branch)),
    agent: new Set(entries.flatMap((e) => e.agent)),
  }

  return {
    entries,
    known: (key) => (key === 'repo' || key === 'branch' || key === 'agent' ? sets[key] : knownStates),
  }
}

// matchesEntry reports whether a worktree matches a query.
function matchesEntry(e: Entry, query: Query, known: Index['known']): boolean {
  return (
    matchesTerms(query.terms, e.hay, e.number) &&
    query.filters.every((f) => matchesFilter(f, haveOf(e, f.key), known(f.key)))
  )
}

// namesRepo reports a query that names a clone itself: words that match the clone's name
// or folder, and repo: filters that match its name, with nothing it excludes and nothing
// only a worktree could match. Such a clone shows every worktree it has.
function namesRepo(repo: WorktreeRepo, query: Query, known: Index['known']): boolean {
  const name = [fold(repo.name)]

  return (
    query.terms.every((t) => !t.negated && !t.numberOnly) &&
    matchesTerms(query.terms, haystack([repo.name, repo.displayPath])) &&
    query.filters.every((f) => f.key === 'repo' && matchesFilter(f, name, known('repo')))
  )
}

// filterWorktrees keeps the clones and worktrees a query matches, or returns null for a
// query that filters nothing. A clone the query names shows all its worktrees; any other
// shows those that match, and a clone with none is left out. A clone or worktree showing
// an error stays in sight whatever the query, without counting as a match.
export function filterWorktrees(
  repos: readonly WorktreeRepo[],
  query: string,
  options: WorktreeFilterOptions = {},
): WorktreeFilter | null {
  const parsed = parseQuery(query, worktreeSpec)

  if (isEmpty(parsed)) {
    return null
  }

  const { entries, known } = indexOf(repos, options)
  const { errors } = options
  const kept: FilteredRepo[] = []
  let matches = 0

  for (const repo of repos) {
    const whole = namesRepo(repo, parsed, known)
    const matched = new Set(
      entries.filter((e) => e.clone === repo && (whole || matchesEntry(e, parsed, known))).map((e) => e.wt),
    )
    const worktrees = repo.worktrees.filter(
      (wt) => matched.has(wt) || wt.error !== '' || (errors?.worktree(wt.path) ?? '') !== '',
    )
    const failed = repo.error !== '' || (errors?.repo(repo.path) ?? '') !== ''

    matches += matched.size

    if (worktrees.length > 0 || failed) {
      kept.push({ repo, worktrees })
    }
  }

  return { repos: kept, matches }
}

// valuesOf lists a worktree's values for a key as the suggestions show them: the agents
// by their IDs, which agent: takes as typed.
function valuesOf(e: Entry, key: string): readonly string[] {
  switch (key) {
    case 'repo':
      return [e.clone.name]
    case 'branch':
      return [branchOf(e.wt)].filter((b) => b !== '')
    case 'agent':
      return e.agents
    default:
      return e.states
  }
}

// worktreeSuggestions offers what fits where the caret is in a query: the states, clones,
// branches, and agents to narrow by on an empty query, or the values of the key being
// typed, each counted among the worktrees the rest of the query keeps.
export function worktreeSuggestions(
  repos: readonly WorktreeRepo[],
  src: string,
  caret: number,
  options: WorktreeFilterOptions = {},
): Suggestions {
  const { entries, known } = indexOf(repos, options)

  return suggest(src, caret, {
    spec: worktreeSpec,
    items: entries,
    matches: (e, query) => matchesEntry(e, query, known),
    values: valuesOf,
    facets,
    hint: worktreeHint,
    nouns,
  })
}
