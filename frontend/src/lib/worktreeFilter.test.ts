import { describe, expect, it } from 'vitest'

import type { AgentID, WorktreeRepo } from '../platform'
import { linkedItem, mainWorktree, worktree, worktreeRepo } from '../test/fixtures'
import type { WorktreeErrors } from './useWorktrees'
import { filterWorktrees, worktreeHint, worktreeSuggestions, type WorktreeFilter } from './worktreeFilter'

// The listing the tests filter: example-repo with its main worktree and three linked ones
// (a dirty pull request in Codex with commits not on GitHub, a merged one Claude works in,
// a closed issue that needs a look, whose commits couldn't be counted), other-tool with its
// main worktree and a detached one in use, and a clone whose worktrees could not be listed.
// A main worktree is never a row, so no query keeps one.

const exampleRepo = worktreeRepo({
  path: '/home/user/projects/example-repo',
  displayPath: '~/projects/example-repo',
  name: 'example-repo',
  worktrees: [
    mainWorktree({ path: '/w/example-main', displayPath: '~/projects/example-repo' }),
    worktree({
      path: '/w/feature',
      displayPath: '~/w/feature',
      branch: 'feature-x',
      dirty: true,
      dirtyCount: 2,
      unpushed: 2,
      onGitHub: false,
      stage: 'active',
      agent: 'codex',
      item: linkedItem({ number: 7, title: 'Add the widget cache', state: 'open', closedAt: '' }),
    }),
    worktree({
      path: '/w/done',
      displayPath: '~/w/done',
      branch: 'worktree-done',
      stage: 'finished',
      inUse: [{ pid: 4242, command: 'claude', agent: 'claude', by: '' }],
      item: linkedItem({ number: 9, title: 'Fix the login' }),
    }),
    worktree({
      path: '/w/crash',
      displayPath: '~/w/crash',
      branch: 'crash-fix',
      stage: 'needs-review',
      locked: true,
      unpushed: -1,
      onGitHub: false,
      item: linkedItem({ kind: 'issue', number: 12, title: 'Crash on start', state: 'closed' }),
    }),
  ],
})

const otherTool = worktreeRepo({
  path: '/home/user/projects/other-tool',
  displayPath: '~/projects/other-tool',
  name: 'other-tool',
  worktrees: [
    mainWorktree({ path: '/w/other-main', displayPath: '~/projects/other-tool', branch: 'maintenance' }),
    worktree({
      path: '/w/scratch',
      displayPath: '~/w/scratch',
      branch: '',
      detached: true,
      stage: 'in-use',
      agent: 'claude',
    }),
  ],
})

const broken = worktreeRepo({
  path: '/home/user/projects/broken',
  displayPath: '~/projects/broken',
  name: 'broken',
  error: 'git worktree list: exit status 128',
  worktrees: [],
})

const repos: readonly WorktreeRepo[] = [exampleRepo, otherTool]

const agentName = (id: AgentID) => (id === 'claude' ? 'Claude Code' : 'Codex')

// shown reads a filter as the clones it shows, each with the last part of each worktree's
// path, and how many worktrees matched.
function shown(filter: WorktreeFilter | null) {
  if (filter === null) {
    return null
  }

  return {
    repos: Object.fromEntries(
      filter.repos.map(({ repo, worktrees }) => [repo.name, worktrees.map((wt) => wt.displayPath.split('/').at(-1))]),
    ),
    matches: filter.matches,
  }
}

const allExample = ['feature', 'done', 'crash']
const allOther = ['scratch']

describe('filterWorktrees', () => {
  it.each(['', '   ', 'repo:', 'is:', '""'])('filters nothing for %j', (query) => {
    expect(filterWorktrees(repos, query)).toBeNull()
  })

  it.each([
    // Words match the branch, the path, the clone, and the item's repository, number, and title.
    ['feature', { 'example-repo': ['feature'] }, 1],
    ['widget', { 'example-repo': ['feature'] }, 1],
    ['LOGIN', { 'example-repo': ['done'] }, 1],
    ['acme-org/example-repo', { 'example-repo': ['feature', 'done', 'crash'] }, 3],
    ['#7', { 'example-repo': ['feature'] }, 1],
    ['#1', {}, 0],
    ['12', { 'example-repo': ['crash'] }, 1],
    ['scratch', { 'other-tool': ['scratch'] }, 1],
    ['nothing-here', {}, 0],
    // A query that names a clone shows all of it; one that excludes anything does not.
    ['example-repo', { 'example-repo': allExample }, 3],
    ['projects/other', { 'other-tool': allOther }, 1],
    ['example-repo -feature', { 'example-repo': ['done', 'crash'] }, 2],
    ['repo:other', { 'other-tool': allOther }, 1],
    ['repo:example-repo,other-tool', { 'example-repo': allExample, 'other-tool': allOther }, 4],
    ['repo:example-repo repo:other-tool', { 'example-repo': allExample, 'other-tool': allOther }, 4],
    ['-repo:example-repo', { 'other-tool': allOther }, 1],
    ['repo:example-repo is:dirty', { 'example-repo': ['feature'] }, 1],
    // Branches: a value that names one matches it exactly, any other as a part of one. A
    // main checkout's branch is no row's.
    ['branch:feature-x', { 'example-repo': ['feature'] }, 1],
    ['branch:fix', { 'example-repo': ['crash'] }, 1],
    ['branch:main', {}, 0],
    ['branch:maint', {}, 0],
    ['-branch:feature-x repo:example', { 'example-repo': ['done', 'crash'] }, 2],
    // Agents, by ID or by name: the one last launched there, and those working in it.
    ['agent:codex', { 'example-repo': ['feature'] }, 1],
    ['agent:claude', { 'example-repo': ['done'], 'other-tool': ['scratch'] }, 2],
    ['agent:"claude code"', { 'example-repo': ['done'], 'other-tool': ['scratch'] }, 2],
    ['"claude code"', { 'example-repo': ['done'], 'other-tool': ['scratch'] }, 2],
    // States.
    ['is:dirty', { 'example-repo': ['feature'] }, 1],
    // Commits not on GitHub, never those that couldn't be counted.
    ['is:unpushed', { 'example-repo': ['feature'] }, 1],
    ['-is:unpushed repo:example', { 'example-repo': ['done', 'crash'] }, 2],
    ['is:locked', { 'example-repo': ['crash'] }, 1],
    ['is:in-use', { 'example-repo': ['done'], 'other-tool': ['scratch'] }, 2],
    ['is:finished', { 'example-repo': ['done'] }, 1],
    ['is:review', { 'example-repo': ['crash'] }, 1],
    ['is:pr', { 'example-repo': ['feature', 'done'] }, 2],
    ['is:issue', { 'example-repo': ['crash'] }, 1],
    ['is:open', { 'example-repo': ['feature'] }, 1],
    ['is:merged', { 'example-repo': ['done'] }, 1],
    ['is:closed', { 'example-repo': ['crash'] }, 1],
    ['is:merged,closed', { 'example-repo': ['done', 'crash'] }, 2],
    ['is:pr is:open', { 'example-repo': ['feature'] }, 1],
    ['-is:pr repo:example', { 'example-repo': ['crash'] }, 1],
    ['is:fin', { 'example-repo': ['done'] }, 1],
  ])('filters %j', (query, want, matches) => {
    expect(shown(filterWorktrees(repos, query, { agentName }))).toEqual({ repos: want, matches })
  })

  it('knows an agent by its ID alone without a way to name it', () => {
    expect(shown(filterWorktrees(repos, 'claude code'))).toEqual({ repos: {}, matches: 0 })
    expect(shown(filterWorktrees(repos, 'codex'))).toEqual({ repos: { 'example-repo': ['feature'] }, matches: 1 })
  })

  it('never keeps a main checkout, not even one that shows an error', () => {
    const failing = worktreeRepo({
      ...otherTool,
      worktrees: [mainWorktree({ path: '/w/other-main', displayPath: '~/projects/other-tool', error: 'unreadable' })],
    })
    const errors: WorktreeErrors = { worktree: () => 'remove: exit status 1', repo: () => '' }

    expect(shown(filterWorktrees([failing], 'other-tool', { errors }))).toEqual({ repos: {}, matches: 0 })
    expect(shown(filterWorktrees([failing], 'nothing-here', { errors }))).toEqual({ repos: {}, matches: 0 })
  })

  it('keeps a clone or worktree that shows an error in sight, without counting it', () => {
    const unread = worktree({ path: '/w/unread', displayPath: '~/w/unread', error: 'could not read the status' })
    const listing = [broken, worktreeRepo({ ...otherTool, worktrees: [...otherTool.worktrees, unread] }), exampleRepo]
    const errors: WorktreeErrors = {
      worktree: (path) => (path === '/w/crash' ? 'remove: exit status 1' : ''),
      repo: (path) => (path === otherTool.path ? 'prune: exit status 128' : ''),
    }

    expect(shown(filterWorktrees(listing, 'nothing-here'))).toEqual({
      repos: { broken: [], 'other-tool': ['unread'] },
      matches: 0,
    })
    expect(shown(filterWorktrees(listing, 'nothing-here', { errors }))).toEqual({
      repos: { broken: [], 'other-tool': ['unread'], 'example-repo': ['crash'] },
      matches: 0,
    })
    expect(
      shown(filterWorktrees([worktreeRepo({ ...otherTool, worktrees: [] }), exampleRepo], 'feature', { errors })),
    ).toEqual({
      repos: { 'other-tool': [], 'example-repo': ['feature', 'crash'] },
      matches: 1,
    })
  })
})

describe('worktreeSuggestions', () => {
  // ids reads the options a list offers, with their counts.
  function ids(src: string, caret = src.length, listing = repos) {
    const { options } = worktreeSuggestions(listing, src, caret, { agentName })

    return options.map((o) => `${o.id} ${o.count}`)
  }

  it('offers the states, clones, branches, and agents on an empty query', () => {
    const list = worktreeSuggestions(repos, '', 0)

    expect(list).toMatchObject({ label: 'Filters', auto: true, hint: worktreeHint })
    expect(list.options.map((o) => [o.label, o.detail, o.count])).toEqual([
      ['Dirty', 'is:dirty', '1'],
      ['Not on GitHub', 'is:unpushed', '1'],
      ['Locked', 'is:locked', '1'],
      ['In use', 'is:in-use', '2'],
      ['Ready to clean up', 'is:finished', '1'],
      ['Needs a look', 'is:review', '1'],
      ['Repository', 'repo:', '2 repos'],
      ['Branch', 'branch:', '3 branches'],
      ['Agent', 'agent:', '2 agents'],
    ])
  })

  it('leaves out the states no worktree is in, and the agents while none worked anywhere', () => {
    const quiet = [worktreeRepo()]

    expect(ids('', 0, quiet)).toEqual(['is:dirty 0', 'repo: 1 repo', 'branch: 1 branch'])
  })

  it('offers the values of the key being typed, counted among what the rest of the query keeps', () => {
    expect(ids('agent:')).toEqual(['agent:claude 2', 'agent:codex 1'])
    expect(ids('branch:fea')).toEqual(['branch:feature-x 1'])
    expect(ids('is:dirty repo:')).toEqual(['repo:example-repo 1'])
    expect(ids('repo: is:dirty', 'repo:'.length)).toEqual(['repo:example-repo 1'])
    expect(ids('is:')).toEqual([
      'is:in-use 2',
      'is:pr 2',
      'is:closed 1',
      'is:dirty 1',
      'is:finished 1',
      'is:issue 1',
      'is:locked 1',
      'is:merged 1',
      'is:open 1',
      'is:review 1',
      'is:unpushed 1',
    ])
    expect(ids('branch:ma')).toEqual([])
    expect(worktreeSuggestions(repos, 'branch:', 7)).toMatchObject({ label: 'Branches', auto: true, hint: '' })
  })

  it('offers the facets a word starts, waiting for ↓ once the query has words', () => {
    const list = worktreeSuggestions(repos, 'widget br', 9)

    expect(list.auto).toBe(false)
    expect(list.options.map((o) => o.id)).toEqual(['branch:'])
  })
})
