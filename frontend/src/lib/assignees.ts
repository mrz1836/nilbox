// The assignee pickers' words and arithmetic, both sheets': what to say beside each person,
// how they are grouped and filtered, when typing searches GitHub, and the few people the
// sheet offers to assign in one click. Everything here is local: the picker asks GitHub
// nothing until a search the app can't answer, or the user applies.

import type { AssigneeCandidate, AssigneeCandidates, AssigneeSource } from '../platform'
import { counted } from './format'
import { filtered, personFields } from './reviewers'
import { formatAgo } from './time'

// Assigned is someone an issue or a pull request is assigned to: mine says it is the user.
export interface Assigned {
  login: string
  mine: boolean
}

// ItemNoun names the kind of item a picker is for, in a sentence.
export type ItemNoun = 'issue' | 'pull request'

// assignLabel is the picker's button for an edit that assigns add and unassigns remove, as
// in "Assign 2 · Unassign 1", or Apply while there is neither.
export function assignLabel(add: readonly string[], remove: readonly string[]): string {
  const parts = [
    add.length > 0 ? `Assign ${String(add.length)}` : '',
    remove.length > 0 ? `Unassign ${String(remove.length)}` : '',
  ].filter((s) => s !== '')

  return parts.length === 0 ? 'Apply' : parts.join(' · ')
}

// worked says what someone did on the repository, as in "Reviewed 12 here", or ''.
function worked(c: AssigneeCandidate): string {
  if (c.reviewed > 0) {
    return `Reviewed ${String(c.reviewed)} here`
  }

  if (c.authored > 0) {
    return `Opened ${String(c.authored)} here`
  }

  return c.committed > 0 ? `Committed to ${counted(c.committed, 'pull request', 'pull requests')} here` : ''
}

// assigneeHint says, in a few words, why the picker offers someone to assign to an item of
// noun, as in "Opened this issue", "Assigned here 3d ago", "Reviewed 12 here · 3d ago", or
// "Works on acme-org/web · 2d ago"; '' for the user, whose row says "(you)".
export function assigneeHint(c: AssigneeCandidate, now: Date, noun: ItemNoun): string {
  const ago = (at: string) => (at === '' ? '' : formatAgo(now, at))
  const join = (...parts: string[]) => parts.filter((s) => s !== '').join(' · ')

  switch (c.source) {
    case 'you':
      return ''
    case 'author':
      return `Opened this ${noun}`
    case 'participant':
      return `Took part in this ${noun}`
    case 'assigned_before':
      return c.assignedAt === '' ? 'Assigned here before' : `Assigned here ${ago(c.assignedAt)}`
    case 'works_here':
      return join(worked(c), ago(c.lastAt))
    case 'org':
      return join(c.where === '' ? '' : `Works on ${c.where}`, ago(c.lastAt))
    case 'listed':
      return 'Can be assigned here'
    case 'found':
      return 'Found on GitHub'
  }
}

// AssigneeGroup is one section of the picker: its heading, and the people in it, in order.
export interface AssigneeGroup {
  section: string
  people: AssigneeCandidate[]
}

// sectionOf names the section a source lists its people in, for repo, "owner/name".
function sectionOf(source: AssigneeSource, repo: string): string {
  switch (source) {
    case 'you':
    case 'author':
    case 'participant':
    case 'assigned_before':
      return 'Suggested'
    case 'works_here':
      return `Works on ${repo}`
    case 'listed':
      return `Can be assigned in ${repo}`
    case 'org':
      return `In ${repo.split('/')[0] ?? repo}`
    case 'found':
      return 'Found on GitHub'
  }
}

// assigneeGroups are the picker's sections of people, in the order the app offers them,
// without anyone assigned, and with only those that match query, those that start with it
// first within each: Suggested (the user, the author, the people who took part, and those
// assigned in repo before), those who work on repo, the rest of those GitHub listed, those
// who work on the owner's other repositories, and those a search of GitHub found.
export function assigneeGroups(
  people: readonly AssigneeCandidate[],
  assigned: readonly Assigned[],
  repo: string,
  query: string,
): AssigneeGroup[] {
  const taken = new Set(assigned.map((a) => a.login.toLowerCase()))
  const out: AssigneeGroup[] = []

  for (const c of people) {
    if (taken.has(c.login.toLowerCase())) {
      continue
    }

    const section = sectionOf(c.source, repo)
    const group = out.find((g) => g.section === section)

    if (group === undefined) {
      out.push({ section, people: [c] })
    } else {
      group.people.push(c)
    }
  }

  return out
    .map((g) => ({ section: g.section, people: filtered(g.people, query, personFields) }))
    .filter((g) => g.people.length > 0)
}

// mergeFound is people with whom a search found added, each once, those already offered
// keeping why: a search answers with everyone it matched, which the list may hold already.
export function mergeFound(
  people: readonly AssigneeCandidate[],
  found: readonly AssigneeCandidate[] | undefined,
): AssigneeCandidate[] {
  const known = new Set(people.map((c) => c.login.toLowerCase()))

  return [...people, ...(found ?? []).filter((c) => !known.has(c.login.toLowerCase()))]
}

// searchesGitHub reports that typing query asks the app to search: there are words, and the
// app's last read of who can be assigned didn't name everyone, so it may not hold them. The
// app still answers from what it holds when an earlier search covers the words.
export function searchesGitHub(candidates: AssigneeCandidates | undefined, query: string): boolean {
  return query.trim() !== '' && candidates?.complete !== true
}

// assigneesNote says where the picker's people come from in repo: everyone who can be
// assigned there, as last read, or the first page of them, which typing searches past, or,
// before a read, the issues and pull requests the user viewed.
export function assigneesNote(candidates: AssigneeCandidates | undefined, repo: string, now: Date): string {
  if (candidates === undefined || candidates.readAt === '') {
    return `People from the issues and pull requests you’ve viewed in ${repo}`
  }

  const ago = formatAgo(now, candidates.readAt)

  if (candidates.complete) {
    return `Everyone who can be assigned in ${repo} · read ${ago}`
  }

  return `Some of the ${String(candidates.total)} who can be assigned in ${repo} · type to search the rest`
}

// QuickPick is someone the sheet offers to assign in one click while no one is.
export interface QuickPick {
  login: string
  // you marks the user, whose chip says "Assign yourself".
  you: boolean
  candidate: AssigneeCandidate
}

// quickPickCount is how many people the sheet offers to assign in one click.
const quickPickCount = 3

// quickPicks are the first few people the picker offers who aren't assigned: the user
// first, as "Assign yourself", then the author, then the rest in the picker's order.
export function quickPicks(candidates: AssigneeCandidates | undefined, assigned: readonly Assigned[]): QuickPick[] {
  const taken = new Set(assigned.map((a) => a.login.toLowerCase()))

  return (candidates?.people ?? [])
    .filter((c) => !taken.has(c.login.toLowerCase()))
    .slice(0, quickPickCount)
    .map((c) => ({ login: c.login, you: c.mine, candidate: c }))
}
