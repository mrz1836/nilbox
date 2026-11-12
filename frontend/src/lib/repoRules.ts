import type { RepoRule, RepoRules } from '../platform'

// The pinned and muted repositories and orgs, as Settings → Repositories and each card's
// menu show them. The backend (internal/reporules) is the rule's owner: these read the
// rules the settings hold in the same way, a repository's own rule before its org's, and
// match a target ignoring case, as GitHub does.

// AppliedRule is the rule that applies to a repository, and the target it comes from: the
// repository itself ("owner/name") or its org ("owner"), as stored. target is '' for none.
export interface AppliedRule {
  rule: RepoRule
  target: string
}

// noRule applies to a repository with no rule of its own or its org's.
const noRule: AppliedRule = { rule: '', target: '' }

// targetKey is a target as rules compare it: lower case.
export function targetKey(target: string): string {
  return target.toLowerCase()
}

// orgOf is the owner of a repository written "owner/name".
export function orgOf(repo: string): string {
  return repo.split('/', 1).join('')
}

// find returns the target in list that names key, as stored.
function find(list: readonly string[], key: string): string | undefined {
  return list.find((target) => targetKey(target) === key)
}

// ruleFor is the rule that applies to a repository ("owner/name"): its own, else its
// org's, else none.
export function ruleFor(rules: RepoRules, repo: string): AppliedRule {
  for (const key of [targetKey(repo), targetKey(orgOf(repo))]) {
    const pinned = find(rules.pinned, key)

    if (pinned !== undefined) {
      return { rule: 'pinned', target: pinned }
    }

    const muted = find(rules.muted, key)

    if (muted !== undefined) {
      return { rule: 'muted', target: muted }
    }
  }

  return noRule
}

// ownRule is the rule set for a target itself, ignoring its org's: what the target's row in
// Settings shows.
export function ownRule(rules: RepoRules, target: string): RepoRule {
  const key = targetKey(target)

  if (find(rules.pinned, key) !== undefined) {
    return 'pinned'
  }

  return find(rules.muted, key) === undefined ? '' : 'muted'
}

// sortTargets orders targets as the backend stores them: ignoring case, an org before its
// repositories.
function sortTargets(list: readonly string[]): string[] {
  const keyOf = (target: string) => (target.includes('/') ? targetKey(target) : `${targetKey(target)}/`)

  return [...list].sort((a, b) => (keyOf(a) < keyOf(b) ? -1 : keyOf(a) > keyOf(b) ? 1 : 0))
}

// withRepoRule returns the rules with rule set for target, as the backend's Set stores
// them: the target out of both lists, then in the rule's own unless it is ''.
export function withRepoRule(rules: RepoRules, target: string, rule: RepoRule): RepoRules {
  const key = targetKey(target)
  const pinned = rules.pinned.filter((t) => targetKey(t) !== key)
  const muted = rules.muted.filter((t) => targetKey(t) !== key)

  return {
    pinned: sortTargets(rule === 'pinned' ? [...pinned, target] : pinned),
    muted: sortTargets(rule === 'muted' ? [...muted, target] : muted),
  }
}

// RuleRow is one row of the rules as Settings lists them: an org, or a repository with the
// org it is in, and the rule set for it.
export interface RuleRow {
  target: string
  org: string
  rule: Exclude<RepoRule, ''>
}

// ruleRows lists every target with a rule, each org first and then its repositories, the
// orgs in order, as the launch defaults are listed. A repository whose org has no rule of
// its own sits under its org's name all the same.
export function ruleRows(rules: RepoRules): RuleRow[] {
  const rows: RuleRow[] = [
    ...rules.pinned.map((target) => ({ target, org: orgOf(target), rule: 'pinned' as const })),
    ...rules.muted.map((target) => ({ target, org: orgOf(target), rule: 'muted' as const })),
  ]
  const order = (row: RuleRow) => `${targetKey(row.org)}\u0000${row.target.includes('/') ? targetKey(row.target) : ''}`

  return rows.sort((a, b) => (order(a) < order(b) ? -1 : order(a) > order(b) ? 1 : 0))
}

// ownerPattern and repoPattern are the names GitHub allows (internal/repo): an owner of 1
// to 39 letters, digits, hyphens, and underscores, starting with a letter or digit, and a
// repository of 1 to 100 letters, digits, '.', '_', and '-', never "." or "..", nor
// starting with '-'.
const ownerPattern = /^[A-Za-z0-9][A-Za-z0-9_-]{0,38}$/
const repoPattern = /^(?!-)(?!\.{1,2}$)[A-Za-z0-9._-]{1,100}$/

// parseTarget reads a target as the user typed it: "owner" or "owner/name", trimmed, a
// trailing slash dropped, or a link to the org or repository on GitHub. It returns the
// target, or null for one GitHub could not have.
export function parseTarget(text: string): string | null {
  const trimmed = text
    .trim()
    .replace(/^https?:\/\/(www\.)?github\.com\//i, '')
    .replace(/\/+$/, '')
  const [owner = '', name, ...rest] = trimmed.split('/')

  if (rest.length > 0 || !ownerPattern.test(owner)) {
    return null
  }

  if (name === undefined) {
    return owner
  }

  return repoPattern.test(name) ? `${owner}/${name}` : null
}
