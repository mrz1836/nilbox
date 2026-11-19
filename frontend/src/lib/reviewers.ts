// The reviewer picker's words and arithmetic: what to say beside each person, how typing
// filters and orders them, and what an edit sends, people and teams apart. Everything here is
// local: the picker asks GitHub nothing until the user applies.

import type { ReviewerCandidate, ReviewersRequest, TeamCandidate } from '../platform'
import { counted } from './format'
import { formatAgo } from './time'

// teamPrefix starts a team's key in the picker, so a team and a person whose login is its
// slug never collide.
const teamPrefix = 'team:'

// teamKey is the picker's key for the team with slug.
export function teamKey(slug: string): string {
  return `${teamPrefix}${slug}`
}

// isTeamKey reports a picker key that names a team.
export function isTeamKey(key: string): boolean {
  return key.startsWith(teamPrefix)
}

// teamName is a team as the sheet names it, "org/slug", with org the repository's owner.
export function teamName(repo: string, slug: string): string {
  const owner = repo.split('/')[0] ?? ''

  return owner === '' ? slug : `${owner}/${slug}`
}

// candidateHint says, in a few words, why the picker offers someone: why GitHub suggests
// them, else what they did on the repository and when they were last seen there, as in
// "Reviewed 12 here · 3d ago", or '' when the app knows nothing more.
export function candidateHint(c: ReviewerCandidate, now: Date): string {
  if (c.suggestion === 'changed_files') {
    return 'Changed these files'
  }

  if (c.suggestion === 'commented') {
    return 'Reviewed these files'
  }

  const did =
    c.reviewed > 0
      ? `Reviewed ${String(c.reviewed)} here`
      : c.authored > 0
        ? `Opened ${String(c.authored)} here`
        : c.committed > 0
          ? `Committed to ${counted(c.committed, 'pull request', 'pull requests')} here`
          : ''
  const when = c.lastAt === '' ? '' : formatAgo(now, c.lastAt)

  return [did, when].filter((s) => s !== '').join(' · ')
}

// matchRank is how well query, lower-cased and trimmed, matches any of fields: 0 when one
// starts with it, 1 when one holds it, and -1 when none does. An empty query matches all as
// well as each other.
export function matchRank(query: string, fields: readonly string[]): number {
  if (query === '') {
    return 0
  }

  const lower = fields.map((f) => f.toLowerCase())

  if (lower.some((f) => f.startsWith(query) || f.split(/[\s/_-]/).some((w) => w.startsWith(query)))) {
    return 0
  }

  return lower.some((f) => f.includes(query)) ? 1 : -1
}

// filtered is items that match query (matchRank), those with a field that starts with it
// first, each kind in the order given.
export function filtered<T>(items: readonly T[], query: string, fields: (item: T) => readonly string[]): T[] {
  const q = query.trim().toLowerCase()

  return items
    .map((item, at) => ({ item, at, rank: matchRank(q, fields(item)) }))
    .filter((r) => r.rank >= 0)
    .toSorted((a, b) => a.rank - b.rank || a.at - b.at)
    .map((r) => r.item)
}

// personFields are what typing finds a person by: their login and their name.
export function personFields(c: { login: string; name: string }): string[] {
  return [c.login, c.name]
}

// teamFields are what typing finds a team by: its slug and its name.
export function teamFields(t: TeamCandidate): string[] {
  return [t.slug, t.name]
}

// reviewersRequest is what the picker sends for the pull request with itemId: the keys to
// add and to take off, people and teams apart.
export function reviewersRequest(itemId: number, add: readonly string[], remove: readonly string[]): ReviewersRequest {
  const people = (keys: readonly string[]) => keys.filter((k) => !isTeamKey(k))
  const teams = (keys: readonly string[]) => keys.filter(isTeamKey).map((k) => k.slice(teamPrefix.length))

  return { itemId, add: people(add), remove: people(remove), addTeams: teams(add), removeTeams: teams(remove) }
}

// applyLabel is the picker's button for an edit that asks add and takes remove off, as in
// "Request 2 · Remove 1", or Apply while there is neither.
export function applyLabel(add: readonly string[], remove: readonly string[]): string {
  const parts = [
    add.length > 0 ? `Request ${String(add.length)}` : '',
    remove.length > 0 ? `Remove ${String(remove.length)}` : '',
  ].filter((s) => s !== '')

  return parts.length === 0 ? 'Apply' : parts.join(' · ')
}

// learnedNote says where the picker's people come from: the contributors to repo, as the
// app last learned them from pulls of its pull requests, as in "Contributors to
// acme-org/api · from 50 pull requests, 2d ago", or from the pull requests the user viewed
// in the app, before it learned more.
export function learnedNote(repo: string, pulls: number, readAt: string, now: Date): string {
  if (readAt === '' || pulls === 0) {
    return `Contributors to ${repo} · from the pull requests you’ve viewed`
  }

  return `Contributors to ${repo} · from ${counted(pulls, 'pull request', 'pull requests')}, ${formatAgo(now, readAt)}`
}
