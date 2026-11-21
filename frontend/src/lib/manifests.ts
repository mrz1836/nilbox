// The Deps page as the window words it: the groups a clone falls in by how long ago its
// dependencies last changed, the color each age takes, the rows' order, search, and kind
// filter, the words of a row and of its drawer's modules, how far a read of the clones has
// got, what the rail counts, and a repository's upgrade sessions. The backend reads every
// clone (internal/manifests); this file groups, sorts, and words what it read, and decides
// nothing else.

import type {
  AgentSession,
  DepsSort,
  ManifestKind,
  ManifestsClone,
  ManifestsModule,
  ManifestsPage,
  ManifestsProgress,
  ManifestTool,
} from '../platform'
import { counted, formatCount, joinList } from './format'
import { itemKey } from './itemWorktrees'
import { formatAgo, formatFull } from './time'
import type { Tone } from './worktreeParts'

// DepsGroup is one of the page's groups, by how long ago a clone's stalest module last
// changed: over three months (stale), one to three months (aging), or under a month
// (fresh).
export type DepsGroup = 'stale' | 'aging' | 'fresh'

// depsGroups are the page's groups in order: what each is called, and whether it starts
// open. Only Stale starts open, as only Release's unreleased changes do: it is what the
// page is for.
export const depsGroups: readonly { id: DepsGroup; title: string; open: boolean }[] = [
  { id: 'stale', title: 'Stale', open: true },
  { id: 'aging', title: 'Aging', open: false },
  { id: 'fresh', title: 'Fresh', open: false },
]

// groupAges are the ages each group holds, as its legend names them.
const groupAges: Readonly<Record<DepsGroup, string>> = {
  stale: 'over 3 months',
  aging: '1–3 months',
  fresh: 'under a month',
}

// monthMs is a month, thirty days, as the app's relative times count one (lib/time), so a
// clone in Fresh reads its age in days or weeks, one in Aging 1mo to 3mo, and one in Stale
// 3mo or more.
const monthMs = 30 * 24 * 60 * 60 * 1000

// ageGroup puts a clone in its group by its stalest module's age, iso (RFC 3339), as of
// now: Stale once that is over three months ago, Aging from a month ago, and Fresh under
// a month, a time ahead of now (a clock set wrong) included. A clone whose age isn't known
// (iso '', as when its read failed or no module's history could be read) goes in Stale
// too: nothing vouches that its dependencies are fresh, and Stale, which starts open,
// keeps its row and what went wrong in view, last in the Oldest order, where Fresh, which
// starts folded, would hide it.
export function ageGroup(now: Date, iso: string): DepsGroup {
  const at = Date.parse(iso)

  if (Number.isNaN(at)) {
    return 'stale'
  }

  const age = now.getTime() - at

  if (age > 3 * monthMs) {
    return 'stale'
  }

  return age >= monthMs ? 'aging' : 'fresh'
}

// groupTones color each group's edges and its legend's dot, hotter the longer ago the
// dependencies changed.
const groupTones: Readonly<Record<DepsGroup, Tone>> = { stale: 'caution', aging: 'warning', fresh: 'success' }

// staleTone is the color of a clone's edge by its stalest module's age, iso, as of now: its
// group's, or muted for an age that isn't known, which says nothing of how stale it is.
export function staleTone(now: Date, iso: string): Tone {
  return Number.isNaN(Date.parse(iso)) ? 'muted' : groupTones[ageGroup(now, iso)]
}

// groupsAt is each clone's group as of now, in the clones' order, as one string of their
// names, for a page to read the clock by (useNowAs): it then renders again only once a
// clone moves to another group as the days pass, never for a tick that moves none.
export function groupsAt(now: Date, clones: readonly Pick<ManifestsClone, 'oldest'>[]): string {
  return clones.map((c) => ageGroup(now, c.oldest)).join(' ')
}

// staleCount counts the clones in Stale as of now, which the rail shows beside Deps.
export function staleCount(now: Date, clones: readonly Pick<ManifestsClone, 'oldest'>[] | undefined): number {
  return clones?.filter((c) => ageGroup(now, c.oldest) === 'stale').length ?? 0
}

// LegendEntry is one color a group's legend explains: its tone and its words.
export interface LegendEntry {
  tone: Tone
  words: string
}

// legendOf is the legend of a group's rows, for its heading: the group's ages, in the color
// its rows' edges take, while any row's age is known, then "age unknown", muted, while any
// row's isn't. A group with no rows has none.
export function legendOf(group: DepsGroup, rows: readonly Pick<ManifestsClone, 'oldest'>[]): LegendEntry[] {
  const known = rows.filter((r) => !Number.isNaN(Date.parse(r.oldest))).length

  return [
    ...(known > 0 ? [{ tone: groupTones[group], words: groupAges[group] }] : []),
    ...(known < rows.length ? [{ tone: 'muted' as const, words: 'age unknown' }] : []),
  ]
}

// matchClones keeps the clones whose "owner/name" holds the search, trimmed and in any
// case, in their order.
export function matchClones(clones: readonly ManifestsClone[], search: string): ManifestsClone[] {
  const want = search.trim().toLowerCase()

  return clones.filter((c) => c.repo.toLowerCase().includes(want))
}

// hasKind reports a clone with a module of kind: a Go module, or a JavaScript package.
export function hasKind(clone: Pick<ManifestsClone, 'modules'>, kind: ManifestKind): boolean {
  return clone.modules.some((m) => m.kind === kind)
}

// kindNames name each kind of module where a row counts them, and its filter's chip.
export const kindNames: Readonly<Record<ManifestKind, string>> = { go: 'Go', js: 'JS' }

// kindWords name a module of each kind in a sentence: one, and many.
const kindWords: Readonly<Record<ManifestKind, readonly [one: string, many: string]>> = {
  go: ['Go module', 'Go modules'],
  js: ['JavaScript package', 'JavaScript packages'],
}

// moduleKinds are the kinds of module, in the order a row counts them and the page's chips
// filter by them.
export const moduleKinds: readonly ManifestKind[] = ['go', 'js']

// kindTitle is the tooltip of a kind's chip, n being how many of the clones listed have a
// module of that kind: showing only those, or, while it is on, every clone again.
export function kindTitle(kind: ManifestKind, n: number, on: boolean): string {
  return on ? 'Show every clone' : `Show only the ${counted(n, 'clone', 'clones')} with a ${kindWords[kind][0]}`
}

// noStaleText says Stale holds nothing, of the clones with a module of kind, while a chip
// filters the rows by it.
export function noStaleText(kind: ManifestKind | null): string {
  return kind === null
    ? 'No clone has stale dependencies.'
    : `No clone with a ${kindWords[kind][0]} has stale dependencies.`
}

// kindCounts counts each kind's modules, the kinds a clone has none of left out.
function kindCounts(modules: readonly Pick<ManifestsModule, 'kind'>[]): { kind: ManifestKind; n: number }[] {
  return moduleKinds.map((kind) => ({ kind, n: modules.filter((m) => m.kind === kind).length })).filter((c) => c.n > 0)
}

// modulesText counts a clone's modules by kind, as its row does: "Go 3 · JS 1".
export function modulesText(modules: readonly Pick<ManifestsModule, 'kind'>[]): string {
  return kindCounts(modules)
    .map(({ kind, n }) => `${kindNames[kind]} ${formatCount(n)}`)
    .join(' · ')
}

// modulesTitle counts them in words, for the row's tooltip: "3 Go modules and 1
// JavaScript package".
export function modulesTitle(modules: readonly Pick<ManifestsModule, 'kind'>[]): string {
  return joinList(kindCounts(modules).map(({ kind, n }) => counted(n, ...kindWords[kind])))
}

// toolsText names the tools a clone is set up for, as its row does: "magex · pnpm".
export function toolsText(tools: readonly ManifestTool[]): string {
  return tools.filter((t) => t !== '').join(' · ')
}

// tipWords says what of a clone was read, as of now, for its age's tooltip: origin's
// default branch as the clone last fetched it, "As of origin/master, fetched 3d ago", or
// its HEAD, and why.
export function tipWords(now: Date, clone: Pick<ManifestsClone, 'tip' | 'atHead' | 'atHeadWhy' | 'fetchedAt'>): string {
  if (clone.atHead) {
    return clone.atHeadWhy === '' ? 'Read at HEAD' : `Read at HEAD: ${clone.atHeadWhy}`
  }

  const fetched = formatAgo(now, clone.fetchedAt)

  return fetched === '' ? `As of ${clone.tip}` : `As of ${clone.tip}, fetched ${fetched}`
}

// ageTitle is the tooltip of a clone's age, as of now: when its stalest module last
// changed, in full, that change's subject, and what of the clone was read (tipWords).
export function ageTitle(now: Date, clone: ManifestsClone): string {
  return [`Last changed ${formatFull(clone.oldest)}`, clone.modules[0]?.subject ?? '', tipWords(now, clone)]
    .filter((line) => line !== '')
    .join('\n')
}

// DepsNoteKind is what a note about a clone is about: an age no module's history gave, a
// clone read at HEAD rather than at origin's default branch, a shallow clone, or one with
// more modules than were read.
export type DepsNoteKind = 'unknown' | 'head' | 'shallow' | 'truncated'

// DepsNote is one thing to know about how a clone was read: what it is about, the few
// words its row shows, and the sentence its tooltip says.
export interface DepsNote {
  kind: DepsNoteKind
  text: string
  title: string
}

// cloneNotes says what to know about how a clone was read, in order: an age that isn't
// known, with why when a module says, then a read at HEAD, a shallow clone, and more
// modules than were read. A clone that couldn't be read has none: its error says it all.
export function cloneNotes(clone: ManifestsClone): DepsNote[] {
  if (clone.error !== '') {
    return []
  }

  const notes: DepsNote[] = []

  if (Number.isNaN(Date.parse(clone.oldest))) {
    notes.push({
      kind: 'unknown',
      text: 'age unknown',
      title:
        clone.modules.find((m) => m.problem !== '')?.problem ??
        'No commit that changed its go.mod or package.json files was found.',
    })
  }

  if (clone.atHead) {
    notes.push({
      kind: 'head',
      text: 'read at HEAD',
      title:
        clone.atHeadWhy === '' ? 'This clone was read at its HEAD, not at origin’s default branch.' : clone.atHeadWhy,
    })
  }

  if (clone.shallow) {
    notes.push({
      kind: 'shallow',
      text: 'shallow',
      title: 'A shallow clone: its history stops at its shallow commits, so its dependencies may be older than shown.',
    })
  }

  if (clone.truncated) {
    notes.push({
      kind: 'truncated',
      text: 'not all read',
      title:
        'This clone holds more go.mod and package.json files than Deps reads; only those nearest its top were read.',
    })
  }

  return notes
}

// versionText is what a module is built with, as its drawer line says: a Go module's go
// directive, "go 1.22", or a JavaScript package's manager, "pnpm"; '' when it names none.
export function versionText(m: Pick<ManifestsModule, 'kind' | 'goVersion' | 'packageManager'>): string {
  if (m.kind === 'go') {
    return m.goVersion === '' ? '' : `go ${m.goVersion}`
  }

  return m.packageManager
}

// depsText counts the dependencies a module names, as its drawer line says: a Go module's
// requirements, "4 direct · 9 indirect", or a JavaScript package's, "48 deps · 12 dev",
// with its peer and optional ones when it has any. One that names none says so, unless its
// manifest wasn't read, which its problem says instead.
export function depsText(
  m: Pick<
    ManifestsModule,
    'kind' | 'direct' | 'indirect' | 'deps' | 'devDeps' | 'peerDeps' | 'optionalDeps' | 'problem'
  >,
): string {
  const parts =
    m.kind === 'go'
      ? [`${formatCount(m.direct)} direct`, `${formatCount(m.indirect)} indirect`]
      : [
          `${formatCount(m.deps)} deps`,
          ...(m.devDeps > 0 ? [`${formatCount(m.devDeps)} dev`] : []),
          ...(m.peerDeps > 0 ? [`${formatCount(m.peerDeps)} peer`] : []),
          ...(m.optionalDeps > 0 ? [`${formatCount(m.optionalDeps)} optional`] : []),
        ]
  const total = m.direct + m.indirect + m.deps + m.devDeps + m.peerDeps + m.optionalDeps

  if (total > 0) {
    return parts.join(' · ')
  }

  return m.problem === '' ? 'no dependencies' : ''
}

// moduleTitle says what a module is, for its path's tooltip, a line each: its manifest's
// path, its module path or package name, its toolchain, its lockfile (or that it has
// none), and its workspaces' patterns.
export function moduleTitle(m: ManifestsModule): string {
  return [
    m.path,
    m.kind === 'go' ? m.goModule : m.name,
    m.toolchain === '' ? '' : `toolchain ${m.toolchain}`,
    m.lockfile === '' ? 'no lockfile' : `lockfile ${m.lockfile}`,
    m.workspaces.length === 0 ? '' : `workspaces ${m.workspaces.join(', ')}`,
  ]
    .filter((line) => line !== '')
    .join('\n')
}

// compare orders two values as Go's cmp.Compare does: -1 when x comes first, 1 when y
// does, and 0 when they're equal.
function compare<T extends string | number>(x: T, y: T): number {
  if (x < y) {
    return -1
  }

  return x > y ? 1 : 0
}

// SortKey is what the sort reads of a clone, worked out once per row rather than on every
// comparison: whether it is pinned (0) or not (1), when its stalest module changed, in
// milliseconds (Infinity when that isn't known, so it sorts after every age that is), its
// name lowered, and how many modules it holds.
interface SortKey {
  row: ManifestsClone
  rank: number
  time: number
  name: string
  modules: number
}

// byName orders clones by "owner/name", ignoring case, as the manifests service does
// (cmp.Compare of the lowered names, not the locale's collation).
function byName(a: SortKey, b: SortKey): number {
  return compare(a.name, b.name)
}

// byOldest orders clones by when their stalest module changed, the longest ago first and
// one whose age isn't known last, then by name.
function byOldest(a: SortKey, b: SortKey): number {
  return compare(a.time, b.time) || byName(a, b)
}

// byModules orders clones by how many modules they hold, the most first, then as byOldest
// does.
function byModules(a: SortKey, b: SortKey): number {
  return compare(b.modules, a.modules) || byOldest(a, b)
}

// cloneOrder is how a sort orders two clones, pinning aside.
function cloneOrder(sort: DepsSort): (a: SortKey, b: SortKey) => number {
  switch (sort) {
    case 'oldest':
      return byOldest
    case 'name':
      return byName
    case 'modules':
      return byModules
  }
}

// sortClones orders the clones as the page lists them, in a new array: the pinned
// repositories first, then each part in the chosen sort. The sort is stable, so clones it
// can't tell apart, such as two of one repository, keep their order.
export function sortClones(rows: readonly ManifestsClone[], sort: DepsSort): ManifestsClone[] {
  const order = cloneOrder(sort)
  const keys = rows.map((row): SortKey => {
    const at = Date.parse(row.oldest)

    return {
      row,
      rank: row.pinned ? 0 : 1,
      time: Number.isNaN(at) ? Infinity : at,
      name: row.repo.toLowerCase(),
      modules: row.modules.length,
    }
  })

  return keys.sort((a, b) => compare(a.rank, b.rank) || order(a, b)).map((k) => k.row)
}

// pageText counts what the page lists: "175 clones · 318 modules".
export function pageText(page: Pick<ManifestsPage, 'clones' | 'modules'>): string {
  return `${counted(page.clones.length, 'clone', 'clones')} · ${counted(page.modules, 'module', 'modules')}`
}

// leftOutText says which clones the page leaves out, the counts that are 0 left out: "14
// muted and 3 not on GitHub left out · 37 with no go.mod or package.json"; '' when none.
export function leftOutText(page: Pick<ManifestsPage, 'muted' | 'withoutGitHub' | 'withoutModules'>): string {
  const off = [
    page.muted > 0 ? `${formatCount(page.muted)} muted` : '',
    page.withoutGitHub > 0 ? `${formatCount(page.withoutGitHub)} not on GitHub` : '',
  ].filter((part) => part !== '')

  return [
    off.length > 0 ? `${off.join(' and ')} left out` : '',
    page.withoutModules > 0 ? `${formatCount(page.withoutModules)} with no go.mod or package.json` : '',
  ]
    .filter((part) => part !== '')
    .join(' · ')
}

// ReadStep is how far a read of the clones has got, in words, with the share of its phase
// done, from 0 to 1, or null for a phase that counts nothing, as ProgressStatus shows it.
export interface ReadStep {
  text: string
  share: number | null
}

// readStep words how far a read of the clones has got: finding the clones, which counts
// nothing, then reading each one's go.mod and package.json files; before the read first
// says, only that it reads.
export function readStep(progress: ManifestsProgress | null): ReadStep {
  if (progress === null) {
    return { text: 'Reading your clones…', share: null }
  }

  const { phase, done, total } = progress

  switch (phase) {
    case 'clones':
      return { text: 'Finding your clones…', share: null }
    case 'reading':
      return {
        text: `Reading go.mod and package.json · ${formatCount(done)} of ${counted(total, 'clone', 'clones')}`,
        share: total > 0 ? done / total : null,
      }
  }
}

// upgradeSessions are the sessions a repository's launch started, as Deps' Upgrade does,
// newest first, from each item's sessions (useSessions' byItem), or undefined while it has
// none. Such a launch has no item: its sessions are kept under number 0, which no item
// has, with a kind of ''.
export function upgradeSessions(
  byItem: ReadonlyMap<string, readonly AgentSession[]>,
  repo: string,
): readonly AgentSession[] | undefined {
  const sessions = byItem.get(itemKey(repo, 0))?.filter((s) => s.kind === '') ?? []

  return sessions.length === 0 ? undefined : sessions
}
