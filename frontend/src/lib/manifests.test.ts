import { describe, expect, it, vi } from 'vitest'

import type { AgentSession, ManifestsClone } from '../platform'
import { FIXED_NOW, fromNow } from '../test/clock'
import { agentSession, manifestsClone, manifestsModule, manifestsPage } from '../test/fixtures'
import {
  ageGroup,
  ageTitle,
  cloneNotes,
  depsGroups,
  depsText,
  groupsAt,
  hasKind,
  kindTitle,
  leftOutText,
  legendOf,
  matchClones,
  modulesText,
  modulesTitle,
  moduleTitle,
  noStaleText,
  pageText,
  readStep,
  sortClones,
  staleCount,
  staleTone,
  tipWords,
  toolsText,
  upgradeSessions,
  versionText,
} from './manifests'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const dayMs = 24 * 60 * 60 * 1000

// daysAgo is the time days before FIXED_NOW, ms earlier still, as RFC 3339 text.
function daysAgo(days: number, ms = 0): string {
  return fromNow(-(days * dayMs + ms)).toISOString()
}

// jsModule is web/package.json as the fixture's clone holds it, with change made.
function jsModule(change: Parameters<typeof manifestsModule>[0] = {}) {
  return manifestsModule({
    kind: 'js',
    path: 'web/package.json',
    lockfile: 'pnpm-lock.yaml',
    goModule: '',
    goVersion: '',
    direct: 0,
    indirect: 0,
    name: 'web-console',
    packageManager: 'pnpm',
    deps: 48,
    devDeps: 12,
    ...change,
  })
}

// aged is a clone of repo whose stalest module changed at oldest, '' for an age that isn't
// known.
function aged(repo: string, oldest: string, change: Partial<ManifestsClone> = {}): ManifestsClone {
  return manifestsClone({ repo, dir: `~/projects/${repo.split('/')[1] ?? repo}`, oldest, ...change })
}

// repos are the repositories of clones, in order.
function repos(clones: readonly ManifestsClone[]): string[] {
  return clones.map((c) => c.repo)
}

describe('the groups', () => {
  it('starts with Stale open, and Aging and Fresh folded', () => {
    expect(depsGroups.map((g) => [g.id, g.title, g.open])).toEqual([
      ['stale', 'Stale', true],
      ['aging', 'Aging', false],
      ['fresh', 'Fresh', false],
    ])
  })

  it('puts a clone in its group by its age, at the boundaries of a month and three', () => {
    expect(ageGroup(FIXED_NOW, daysAgo(30, -1))).toBe('fresh')
    expect(ageGroup(FIXED_NOW, daysAgo(30))).toBe('aging')
    expect(ageGroup(FIXED_NOW, daysAgo(90))).toBe('aging')
    expect(ageGroup(FIXED_NOW, daysAgo(90, 1))).toBe('stale')
    expect(ageGroup(FIXED_NOW, '2025-04-15T12:00:00Z')).toBe('stale')
    expect(ageGroup(FIXED_NOW, FIXED_NOW.toISOString())).toBe('fresh')
  })

  it('puts a clone whose age isn’t known in Stale, and one from the future in Fresh', () => {
    expect(ageGroup(FIXED_NOW, '')).toBe('stale')
    expect(ageGroup(FIXED_NOW, 'not a time')).toBe('stale')
    expect(ageGroup(FIXED_NOW, daysAgo(-2))).toBe('fresh')
  })

  it('colors each age hotter the longer ago it is, and an unknown one muted', () => {
    expect(staleTone(FIXED_NOW, daysAgo(3))).toBe('success')
    expect(staleTone(FIXED_NOW, daysAgo(45))).toBe('warning')
    expect(staleTone(FIXED_NOW, daysAgo(400))).toBe('caution')
    expect(staleTone(FIXED_NOW, '')).toBe('muted')
  })

  it('names each clone’s group in order, and counts the stale ones', () => {
    const clones = [aged('acme-org/a', daysAgo(200)), aged('acme-org/b', daysAgo(40)), aged('acme-org/c', '')]

    expect(groupsAt(FIXED_NOW, [...clones, aged('acme-org/d', daysAgo(1))])).toBe('stale aging stale fresh')
    expect(groupsAt(FIXED_NOW, [])).toBe('')
    expect(staleCount(FIXED_NOW, clones)).toBe(2)
    expect(staleCount(fromNow(60 * dayMs), clones)).toBe(3)
    expect(staleCount(FIXED_NOW, undefined)).toBe(0)
  })

  it('gives each group’s legend its ages, and an unknown age apart', () => {
    const known = aged('acme-org/a', daysAgo(200))
    const unknown = aged('acme-org/b', '')

    expect(legendOf('stale', [known, unknown])).toEqual([
      { tone: 'caution', words: 'over 3 months' },
      { tone: 'muted', words: 'age unknown' },
    ])
    expect(legendOf('stale', [unknown])).toEqual([{ tone: 'muted', words: 'age unknown' }])
    expect(legendOf('aging', [aged('acme-org/c', daysAgo(40))])).toEqual([{ tone: 'warning', words: '1–3 months' }])
    expect(legendOf('fresh', [aged('acme-org/d', daysAgo(2))])).toEqual([{ tone: 'success', words: 'under a month' }])
    expect(legendOf('fresh', [])).toEqual([])
  })
})

describe('the rows', () => {
  it('searches the clones by owner and name, trimmed and in any case', () => {
    const clones = [manifestsClone(), manifestsClone({ repo: 'acme-org/web-console', dir: '~/projects/web-console' })]

    expect(repos(matchClones(clones, '  WEB '))).toEqual(['acme-org/web-console'])
    expect(matchClones(clones, '')).toHaveLength(2)
    expect(matchClones(clones, 'nothing-like-it')).toEqual([])
  })

  it('knows the clones with a module of each kind', () => {
    expect(hasKind(manifestsClone(), 'go')).toBe(true)
    expect(hasKind(manifestsClone(), 'js')).toBe(true)
    expect(hasKind(manifestsClone({ modules: [manifestsModule()] }), 'js')).toBe(false)
    expect(hasKind(manifestsClone({ modules: [] }), 'go')).toBe(false)
  })

  it('words the chips and an empty Stale, by the kind they keep', () => {
    expect(kindTitle('go', 160, false)).toBe('Show only the 160 clones with a Go module')
    expect(kindTitle('js', 1, false)).toBe('Show only the 1 clone with a JavaScript package')
    expect(kindTitle('go', 160, true)).toBe('Show every clone')
    expect(noStaleText(null)).toBe('No clone has stale dependencies.')
    expect(noStaleText('js')).toBe('No clone with a JavaScript package has stale dependencies.')
  })

  it('counts a clone’s modules by kind, in a few words and in a sentence', () => {
    const modules = [manifestsModule(), manifestsModule({ path: 'tools/go.mod' }), jsModule(), manifestsModule()]

    expect(modulesText(modules)).toBe('Go 3 · JS 1')
    expect(modulesTitle(modules)).toBe('3 Go modules and 1 JavaScript package')
    expect(modulesText([jsModule(), jsModule({ path: 'docs/package.json' })])).toBe('JS 2')
    expect(modulesTitle([jsModule(), jsModule({ path: 'docs/package.json' })])).toBe('2 JavaScript packages')
    expect(modulesText([manifestsModule()])).toBe('Go 1')
    expect(modulesTitle([manifestsModule()])).toBe('1 Go module')
    expect(modulesText([])).toBe('')
    expect(modulesTitle([])).toBe('')
  })

  it('names the tools a clone is set up for', () => {
    expect(toolsText(['magex', 'pnpm'])).toBe('magex · pnpm')
    expect(toolsText(['', 'npm'])).toBe('npm')
    expect(toolsText([])).toBe('')
  })

  it('says what of a clone was read: origin’s default branch as last fetched, or HEAD and why', () => {
    expect(tipWords(FIXED_NOW, manifestsClone())).toBe('As of origin/master, fetched 3d ago')
    expect(tipWords(FIXED_NOW, manifestsClone({ fetchedAt: '' }))).toBe('As of origin/master')
    expect(tipWords(FIXED_NOW, manifestsClone({ tip: 'origin/main', fetchedAt: FIXED_NOW.toISOString() }))).toBe(
      'As of origin/main, fetched just now',
    )
    expect(
      tipWords(FIXED_NOW, manifestsClone({ tip: 'HEAD', atHead: true, atHeadWhy: 'This clone has no origin/main.' })),
    ).toBe('Read at HEAD: This clone has no origin/main.')
    expect(tipWords(FIXED_NOW, manifestsClone({ tip: 'HEAD', atHead: true }))).toBe('Read at HEAD')
  })

  it('says when a clone’s stalest module changed, with what, and what was read, for the age’s tooltip', () => {
    expect(ageTitle(FIXED_NOW, manifestsClone())).toBe(
      'Last changed Tuesday, April 15, 2025 at 12:00 PM\nchore(deps): bump the go modules (#41)\nAs of origin/master, fetched 3d ago',
    )
    expect(ageTitle(FIXED_NOW, manifestsClone({ modules: [manifestsModule({ subject: '' })] }))).toBe(
      'Last changed Tuesday, April 15, 2025 at 12:00 PM\nAs of origin/master, fetched 3d ago',
    )
    expect(ageTitle(FIXED_NOW, manifestsClone({ modules: [], fetchedAt: '' }))).toBe(
      'Last changed Tuesday, April 15, 2025 at 12:00 PM\nAs of origin/master',
    )
  })

  it('notes how a clone was read, in order, and nothing for one read in full at origin', () => {
    expect(cloneNotes(manifestsClone())).toEqual([])
    expect(
      cloneNotes(
        manifestsClone({
          oldest: '',
          modules: [manifestsModule({ changedAt: '', problem: 'Couldn’t read when this go.mod last changed.' })],
          tip: 'HEAD',
          atHead: true,
          atHeadWhy: 'This clone has no origin/master.',
          shallow: true,
          truncated: true,
        }),
      ),
    ).toEqual([
      { kind: 'unknown', text: 'age unknown', title: 'Couldn’t read when this go.mod last changed.' },
      { kind: 'head', text: 'read at HEAD', title: 'This clone has no origin/master.' },
      {
        kind: 'shallow',
        text: 'shallow',
        title:
          'A shallow clone: its history stops at its shallow commits, so its dependencies may be older than shown.',
      },
      {
        kind: 'truncated',
        text: 'not all read',
        title:
          'This clone holds more go.mod and package.json files than Deps reads; only those nearest its top were read.',
      },
    ])
  })

  it('says why an age isn’t known, or that no commit was found, and why a clone was read at HEAD', () => {
    expect(cloneNotes(manifestsClone({ oldest: '', modules: [manifestsModule({ changedAt: '' })] }))).toEqual([
      {
        kind: 'unknown',
        text: 'age unknown',
        title: 'No commit that changed its go.mod or package.json files was found.',
      },
    ])
    expect(cloneNotes(manifestsClone({ tip: 'HEAD', atHead: true }))).toEqual([
      { kind: 'head', text: 'read at HEAD', title: 'This clone was read at its HEAD, not at origin’s default branch.' },
    ])
  })

  it('notes nothing of a clone that couldn’t be read: its error says it', () => {
    expect(
      cloneNotes(
        manifestsClone({ modules: [], oldest: '', shallow: true, error: 'Couldn’t read this clone’s files at HEAD.' }),
      ),
    ).toEqual([])
  })
})

describe('the drawer’s lines', () => {
  it('names a module’s Go version or package manager', () => {
    expect(versionText(manifestsModule())).toBe('go 1.22')
    expect(versionText(manifestsModule({ goVersion: '' }))).toBe('')
    expect(versionText(jsModule())).toBe('pnpm')
    expect(versionText(jsModule({ packageManager: '' }))).toBe('')
  })

  it('counts a module’s dependencies by kind', () => {
    expect(depsText(manifestsModule())).toBe('4 direct · 9 indirect')
    expect(depsText(manifestsModule({ direct: 1200, indirect: 0 }))).toBe('1,200 direct · 0 indirect')
    expect(depsText(jsModule())).toBe('48 deps · 12 dev')
    expect(depsText(jsModule({ deps: 0, devDeps: 0, peerDeps: 2, optionalDeps: 1 }))).toBe(
      '0 deps · 2 peer · 1 optional',
    )
    expect(depsText(jsModule({ devDeps: 0 }))).toBe('48 deps')
  })

  it('says a module names no dependencies, unless its manifest wasn’t read', () => {
    expect(depsText(manifestsModule({ direct: 0, indirect: 0 }))).toBe('no dependencies')
    expect(depsText(jsModule({ deps: 0, devDeps: 0 }))).toBe('no dependencies')
    expect(
      depsText(jsModule({ deps: 0, devDeps: 0, problem: 'This package.json isn’t a JSON object, so it wasn’t read.' })),
    ).toBe('')
  })

  it('says what a module is, a line each, for its path’s tooltip', () => {
    expect(moduleTitle(manifestsModule({ toolchain: 'go1.23.4' }))).toBe(
      'go.mod\ngithub.com/acme-org/api-server\ntoolchain go1.23.4\nlockfile go.sum',
    )
    expect(moduleTitle(jsModule({ lockfile: '', workspaces: ['apps/*', 'packages/*'] }))).toBe(
      'web/package.json\nweb-console\nno lockfile\nworkspaces apps/*, packages/*',
    )
    expect(moduleTitle(jsModule({ name: '' }))).toBe('web/package.json\nlockfile pnpm-lock.yaml')
  })
})

describe('sortClones', () => {
  // sortRows are four clones whose ages, names, and module counts each order them
  // differently, one whose age isn't known and two named in capitals.
  function sortRows(): ManifestsClone[] {
    return [
      aged('acme-org/Web-kit', daysAgo(40), { modules: [manifestsModule()] }),
      aged('acme-org/api', daysAgo(400), { modules: [manifestsModule(), jsModule()] }),
      aged('acme-org/zeta', '', { modules: [manifestsModule(), jsModule(), jsModule({ path: 'b/package.json' })] }),
      aged('acme-org/Sdk-kit', daysAgo(200), { modules: [manifestsModule(), jsModule()] }),
    ]
  }

  it('lists the dependencies that changed longest ago first, one whose age isn’t known last', () => {
    expect(repos(sortClones(sortRows(), 'oldest'))).toEqual([
      'acme-org/api',
      'acme-org/Sdk-kit',
      'acme-org/Web-kit',
      'acme-org/zeta',
    ])
  })

  it('lists by name in any case, or the most modules first, then the oldest', () => {
    expect(repos(sortClones(sortRows(), 'name'))).toEqual([
      'acme-org/api',
      'acme-org/Sdk-kit',
      'acme-org/Web-kit',
      'acme-org/zeta',
    ])
    expect(repos(sortClones(sortRows(), 'modules'))).toEqual([
      'acme-org/zeta',
      'acme-org/api',
      'acme-org/Sdk-kit',
      'acme-org/Web-kit',
    ])
  })

  it('breaks a tie of ages by name', () => {
    const at = daysAgo(100)

    expect(repos(sortClones([aged('acme-org/b', at), aged('acme-org/a', at)], 'oldest'))).toEqual([
      'acme-org/a',
      'acme-org/b',
    ])
  })

  it('keeps the pinned repositories first in every order', () => {
    const rows = sortRows().map((r) => (r.repo === 'acme-org/Web-kit' ? { ...r, pinned: true } : r))

    for (const sort of ['oldest', 'name', 'modules'] as const) {
      expect(sortClones(rows, sort)[0]?.repo).toBe('acme-org/Web-kit')
    }
  })

  it('keeps the order of clones it can’t tell apart, in a new array', () => {
    const first = aged('acme-org/api', daysAgo(100), { dir: '~/projects/api' })
    const second = aged('acme-org/api', daysAgo(100), { dir: '~/work/api' })
    const rows = [first, second]
    const sorted = sortClones(rows, 'name')

    expect(sorted).toEqual([first, second])
    expect(sorted).not.toBe(rows)
    expect(sortClones([second, first], 'oldest')).toEqual([second, first])
  })
})

describe('the head', () => {
  it('counts the clones and modules listed', () => {
    expect(pageText(manifestsPage())).toBe('1 clone · 2 modules')
    expect(pageText(manifestsPage({ clones: [manifestsClone(), manifestsClone()], modules: 1 }))).toBe(
      '2 clones · 1 module',
    )
    expect(pageText(manifestsPage({ clones: [], modules: 0 }))).toBe('0 clones · 0 modules')
  })

  it('says which clones are left out, leaving out the counts that are 0', () => {
    expect(leftOutText({ muted: 14, withoutGitHub: 3, withoutModules: 37 })).toBe(
      '14 muted and 3 not on GitHub left out · 37 with no go.mod or package.json',
    )
    expect(leftOutText({ muted: 1, withoutGitHub: 0, withoutModules: 0 })).toBe('1 muted left out')
    expect(leftOutText({ muted: 0, withoutGitHub: 1200, withoutModules: 0 })).toBe('1,200 not on GitHub left out')
    expect(leftOutText({ muted: 0, withoutGitHub: 0, withoutModules: 2 })).toBe('2 with no go.mod or package.json')
    expect(leftOutText({ muted: 0, withoutGitHub: 0, withoutModules: 0 })).toBe('')
  })

  it('says how far a read of the clones has got, phase by phase', () => {
    expect(readStep(null)).toEqual({ text: 'Reading your clones…', share: null })
    expect(readStep({ phase: 'clones', done: 0, total: 0 })).toEqual({ text: 'Finding your clones…', share: null })
    expect(readStep({ phase: 'reading', done: 45, total: 180 })).toEqual({
      text: 'Reading go.mod and package.json · 45 of 180 clones',
      share: 0.25,
    })
    expect(readStep({ phase: 'reading', done: 0, total: 1 })).toEqual({
      text: 'Reading go.mod and package.json · 0 of 1 clone',
      share: 0,
    })
    expect(readStep({ phase: 'reading', done: 0, total: 0 })).toEqual({
      text: 'Reading go.mod and package.json · 0 of 0 clones',
      share: null,
    })
  })
})

describe('upgradeSessions', () => {
  // byItem holds sessions as useSessions keeps them, by item.
  function byItem(entries: [string, AgentSession[]][]): ReadonlyMap<string, readonly AgentSession[]> {
    return new Map(entries)
  }

  it('finds a repository’s upgrades, in any case, among each item’s sessions', () => {
    const upgrade = agentSession({
      id: 'u1',
      repo: 'acme-org/api-server',
      itemId: 0,
      kind: '',
      number: 0,
      title: 'Upgrade',
    })
    const item = agentSession({ id: 'i1', repo: 'acme-org/api-server', number: 7 })
    const sessions = byItem([
      ['acme-org/api-server#0', [upgrade]],
      ['acme-org/api-server#7', [item]],
    ])

    expect(upgradeSessions(sessions, 'Acme-Org/API-Server')).toEqual([upgrade])
  })

  it('finds none for a repository without one, nor among an item’s sessions', () => {
    const stray = agentSession({ id: 's1', repo: 'acme-org/api-server', itemId: 5, number: 0 })

    expect(upgradeSessions(byItem([]), 'acme-org/api-server')).toBeUndefined()
    expect(upgradeSessions(byItem([['acme-org/api-server#0', [stray]]]), 'acme-org/api-server')).toBeUndefined()
  })
})
