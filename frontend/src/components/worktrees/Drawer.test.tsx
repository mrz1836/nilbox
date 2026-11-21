import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { DetailsRead } from '../../lib/useWorktreeDetails'
import type { ItemFacts } from '../../lib/worktreeParts'
import type { AgentSession, Worktree, WorktreeDetails, WorktreeRepo } from '../../platform'
import { FIXED_NOW, isoAgo, renderAt } from '../../test/clock'
import {
  agentName,
  agentSession,
  copiedWorktree,
  copyComparison,
  item,
  linkedItem,
  managed,
  ownBranch,
  partlyCopiedWorktree,
  recordedWorktree,
  worktree,
  worktreeCommit,
  worktreeCopies,
  worktreeDetails,
  worktreeFiles,
  worktreeRepo,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import { Drawer, type DrawerProps } from './Drawer'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const now = FIXED_NOW
const minute = 60
const hour = 60 * minute
const day = 24 * hour

// unixAgo is the time s seconds before the clock, in Unix seconds.
function unixAgo(s: number): number {
  return Math.floor(now.getTime() / 1000) - s
}

const er = worktreeRepo()

// row is example-repo's pr-7, the app's, for its pull request, open, clean, and at its head.
function row(overrides: Partial<Worktree> = {}): Worktree {
  return worktree({
    path: `/home/user/projects/.${brand.slug}/worktrees/example-repo/pr-7`,
    displayPath: managed('pr-7'),
    legacy: false,
    stage: 'active',
    item: linkedItem({ state: 'open', closedAt: '' }),
    lastCommitUnix: unixAgo(3 * hour),
    ...overrides,
  })
}

// read is details read for this listing.
function read(overrides: Partial<WorktreeDetails> = {}): DetailsRead {
  return { state: 'read', details: worktreeDetails(overrides) }
}

const claudeHere = { pid: 47890, command: '2.1.328', agent: 'claude', by: '' } as const

const running = row({
  branch: 'feat/inspect-point',
  inUse: [claudeHere],
  stage: 'in-use',
  dirty: true,
  dirtyCount: 30,
  files: worktreeFiles(12),
  unpushed: 1,
  onGitHub: false,
  canRemove: false,
})

const handLocked = row({
  displayPath: managed('pr-254'),
  locked: true,
  lockReason: 'kept for the release branch',
  stage: '',
  canRemove: false,
})

// working is the session the app follows in a worktree, at work since twelve minutes ago.
const working: AgentSession = agentSession({ state: 'working', launchedAt: isoAgo({ s: 12 * minute }) })

// renderDrawer draws wt's drawer, its details as read, with every handler a mock.
function renderDrawer(wt: Worktree, options: Partial<DrawerProps> = {}) {
  const handlers = {
    onTerminal: vi.fn<() => void>(),
    onReveal: vi.fn<() => void>(),
    onCleanUp: vi.fn<(repo: WorktreeRepo, wt: Worktree) => void>(),
    onUnlock: vi.fn<(repo: WorktreeRepo, wt: Worktree) => void>(),
    onRemove: vi.fn<(repo: WorktreeRepo, wt: Worktree) => void>(),
    onForceRemove: vi.fn<(repo: WorktreeRepo, wt: Worktree) => void>(),
    onPrune: vi.fn<(repo: WorktreeRepo) => void>(),
  }

  renderAt(
    <ul>
      <Drawer
        id="drawer"
        repo={er}
        worktree={wt}
        read={undefined}
        session={undefined}
        facts={undefined}
        agentName={agentName}
        disabled={false}
        working={false}
        opening={false}
        {...handlers}
        {...options}
      />
    </ul>,
  )

  const li = screen.getAllByRole('listitem')[0] as HTMLElement

  return { ...handlers, li, drawer: within(li) }
}

// column is the drawer's column headed name.
function column(name: string) {
  const heading = screen.getByRole('heading', { name: new RegExp(`^${name}`) })

  return within(heading.parentElement as HTMLElement)
}

// about is the About column's value for term.
function about(term: string): HTMLElement {
  return column('About').getByText(term, { selector: 'dt' }).nextElementSibling as HTMLElement
}

// noHead is pull request #7, open, with no head the listing knows.
const noHead = linkedItem({ state: 'open', closedAt: '', headOid: '' })

describe('Drawer safety', () => {
  it.each<[string, Worktree, Partial<DrawerProps>, string]>([
    [
      'success',
      row(),
      {},
      'Safe to remove: no uncommitted files, and every commit is on GitHub (it’s at PR #7’s head).',
    ],
    [
      'warning',
      row({ dirty: true, dirtyCount: 7, unpushed: 2, onGitHub: false, canRemove: false, canForceRemove: true }),
      {},
      'Removing it would discard 7 uncommitted files, counting untracked ones.Its 2 commits not on GitHub stay in the clone on worktree-pr-7, which removing it keeps.',
    ],
    [
      'info',
      running,
      { session: working },
      'Can’t be removed while Claude runs in it (pid 47890, started 11:48, 12m ago).Once it ends, removing it would discard 30 uncommitted files. Its 1 commit not on GitHub stays in the clone on feat/inspect-point, which removing it keeps.',
    ],
    [
      'danger',
      handLocked,
      {},
      `Locked by hand, so it can’t be removed here. Unlock it first:git -C ~/projects/example-repo worktree unlock ${managed('pr-254')}CopyAfter that, nothing in it would be lost: every commit is on GitHub.`,
    ],
  ])('says what removing it costs, in the %s tone', (tone, wt, options, words) => {
    const { li } = renderDrawer(wt, options)

    const band = li.querySelector('[data-tone]')
    expect(band).toHaveAttribute('data-tone', tone)
    expect(band).toHaveTextContent(words)
  })

  it('says what removing it costs as its details count it, once read, and offers the removal that agrees', () => {
    const { li } = renderDrawer(row(), { read: read({ files: worktreeFiles(3), fileCount: 3, unpushed: 1 }) })

    const band = li.querySelector('[data-tone]')
    expect(band).toHaveAttribute('data-tone', 'warning')
    expect(band).toHaveTextContent(
      'Removing it would discard 3 uncommitted files, counting untracked ones.Its 1 commit not on GitHub stays in the clone on worktree-pr-7, which removing it keeps.',
    )
    expect(column('Changes').getByRole('heading')).toHaveTextContent('Changes3')
    // The files it has now call for Force remove…, which the backend, having listed it
    // clean, didn't allow: the read holds the removal back until a listing checks again.
    expect(screen.queryByRole('button', { name: 'Remove…' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Force remove…' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Force remove…' })).toHaveAttribute(
      'title',
      'It can’t be force-removed right now: Check again',
    )
  })

  it('holds Remove… back for one that reads clean now but listed with files, until it’s checked again', () => {
    renderDrawer(row({ dirty: true, dirtyCount: 3, files: worktreeFiles(3), canRemove: false, canForceRemove: true }), {
      read: read(),
    })

    expect(screen.queryByRole('button', { name: 'Force remove…' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Remove…' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Remove…' })).toHaveAttribute(
      'title',
      'It can’t be removed right now: Check again',
    )
  })

  it('says a worktree at its pull request’s head loses nothing, however many commits no remote has', () => {
    // A fork's pull request: its head is on GitHub, though no remote-tracking branch has it.
    // Details lists HEAD first.
    const atHead = row({ unpushed: 3, onGitHub: true, stage: 'finished', canCleanUp: true })
    const { li } = renderDrawer(atHead, {
      read: read({
        unpushed: 3,
        commits: [
          worktreeCommit({ oid: atHead.head }),
          worktreeCommit({ oid: '1'.repeat(40) }),
          worktreeCommit({ oid: '2'.repeat(40) }),
        ],
      }),
    })

    expect(li.querySelector('[data-tone]')).toHaveAttribute('data-tone', 'success')
    expect(li.querySelector('[data-tone]')).toHaveTextContent(/^Safe to remove/)
    expect(li.querySelector('[data-tone]')).not.toHaveTextContent(/aren’t on GitHub/)

    const commits = column('Not on GitHub')
    expect(commits.getByRole('heading')).toHaveTextContent(/^Not on GitHub$/)
    expect(commits.queryByText('Keep the widget cache warm')).not.toBeInTheDocument()
    expect(commits.getByText('None: every commit is on GitHub (PR #7’s head).')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clean up' })).toBeEnabled()
  })

  it('counts the commits as not on GitHub once HEAD moved off its pull request’s head', () => {
    const atHead = row({ unpushed: 3, onGitHub: true })
    renderDrawer(atHead, { read: read({ unpushed: 4, commits: [worktreeCommit()] }) })

    const commits = column('Not on GitHub')
    expect(commits.getByRole('heading')).toHaveTextContent('Not on GitHub4')
    expect(commits.getByText('Keep the widget cache warm')).toBeInTheDocument()
  })

  it('sets a branch’s name in code', () => {
    const { li } = renderDrawer(row({ unpushed: 2, onGitHub: false }))

    expect(within(li.querySelector('[data-tone]') as HTMLElement).getAllByText('worktree-pr-7')[0]?.tagName).toBe(
      'CODE',
    )
  })

  it('copies the command a hand lock needs', async () => {
    const { li } = renderDrawer(handLocked)

    await userEvent.click(within(li.querySelector('[data-tone]') as HTMLElement).getByRole('button', { name: 'Copy' }))

    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith(
      `git -C ~/projects/example-repo worktree unlock ${managed('pr-254')}`,
    )
  })
})

describe('Drawer changes', () => {
  it('says a clean worktree has nothing uncommitted', () => {
    renderDrawer(row())

    expect(column('Changes').getByText('Nothing uncommitted.')).toBeInTheDocument()
    expect(column('Changes').queryByRole('button', { name: 'Copy list' })).not.toBeInTheDocument()
  })

  it('lists each file with its status, a dot when staged, and its folder muted', () => {
    const files = [
      { code: ' M', path: 'web/src/lib/storage.ts' },
      { code: '??', path: 'web/src/lib/searchHistory.ts' },
      { code: 'A ', path: 'docs/limits.md' },
      { code: 'MM', path: 'web/src/pages/Inbox.tsx' },
      { code: 'UU', path: 'go.mod' },
      { code: 'T ', path: 'bin/tool' },
    ]
    renderDrawer(row({ dirty: true, dirtyCount: 6, files, canRemove: false, canForceRemove: true }))

    const changes = column('Changes')
    expect(changes.getByRole('heading')).toHaveTextContent('Changes6Copy list')

    const line = (path: string) => within(changes.getByTitle(path).parentElement as HTMLElement)
    expect(line('web/src/lib/storage.ts').getByText('M')).toHaveAttribute('title', 'Modified (git status “ M”)')
    expect(line('web/src/lib/storage.ts').getByText('M')).toHaveClass('text-warning')
    expect(line('web/src/lib/searchHistory.ts').getByText('U')).toHaveAttribute(
      'title',
      'Untracked: new, not added (git status “??”)',
    )
    expect(line('docs/limits.md').getByText('A')).toHaveClass('text-success')
    expect(line('docs/limits.md').getByTitle('Staged')).toHaveClass('bg-success')
    expect(line('web/src/pages/Inbox.tsx').getByText('M')).toHaveAttribute(
      'title',
      'Modified, partly staged (git status “MM”)',
    )
    expect(line('go.mod').getByText('C')).toHaveClass('text-danger')
    expect(line('bin/tool').getByText('T')).toHaveClass('text-muted')
    expect(changes.getAllByText('web/src/lib/')[0]).toHaveClass('text-muted')
    expect(changes.getByTitle('web/src/lib/storage.ts')).toHaveTextContent('web/src/lib/storage.ts')
  })

  it('lists a path git lists twice once for each status', () => {
    // React warns when siblings share a key.
    const errors = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    // git rm --cached leaves the file deleted in the index and untracked in the folder.
    const files = [
      { code: 'D ', path: 'notes.txt' },
      { code: '??', path: 'notes.txt' },
    ]
    renderDrawer(row({ dirty: true, dirtyCount: 2, files, canRemove: false, canForceRemove: true }), {
      read: read({ files, fileCount: 2 }),
    })

    const lines = column('Changes').getAllByRole('listitem')
    expect(lines.map((li) => li.textContent)).toEqual(['Dnotes.txt', 'Unotes.txt'])
    expect(errors.mock.calls.flat().join(' ')).not.toMatch(/same key/)
    errors.mockRestore()
  })

  it('copies the list with each file’s status', async () => {
    renderDrawer(row({ dirty: true, dirtyCount: 2, files: worktreeFiles(2), canRemove: false }))

    await userEvent.click(column('Changes').getByRole('button', { name: 'Copy list' }))

    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith(' M src/file-001.ts\n?? src/file-002.ts')
  })

  it.each<[string, DetailsRead | undefined]>([
    ['before it asks', undefined],
    ['while it reads', { state: 'reading' }],
  ])('lists the first twelve, and says it reads the rest, %s', (_label, details) => {
    renderDrawer(row({ dirty: true, dirtyCount: 30, files: worktreeFiles(12), canRemove: false }), { read: details })

    const changes = column('Changes')
    expect(changes.getAllByRole('listitem')).toHaveLength(12)
    expect(changes.getByText('reading the other 18…')).toBeInTheDocument()
    expect(changes.getByRole('button', { name: 'Copy list' })).toBeDisabled()
    expect(changes.getByRole('button', { name: 'Copy list' })).toHaveAttribute(
      'title',
      'The rest of the list is still being read',
    )
    expect(changes.queryByRole('button', { name: /^Show/ })).not.toBeInTheDocument()
  })

  it('lists every file once read, on Show all, and the first twelve again on Show fewer', async () => {
    renderDrawer(row({ dirty: true, dirtyCount: 30, files: worktreeFiles(12), canRemove: false }), {
      read: read({ files: worktreeFiles(30), fileCount: 30 }),
    })

    const changes = column('Changes')
    expect(changes.getAllByRole('listitem')).toHaveLength(12)
    expect(changes.queryByText(/reading the other/)).not.toBeInTheDocument()
    expect(changes.getByRole('button', { name: 'Copy list' })).toBeEnabled()

    const all = changes.getByRole('button', { name: 'Show all 30' })
    expect(all).toHaveAttribute('aria-expanded', 'false')
    await userEvent.click(all)
    expect(changes.getAllByRole('listitem')).toHaveLength(30)
    expect(changes.queryByText(/are listed here/)).not.toBeInTheDocument()

    await userEvent.click(changes.getByRole('button', { name: 'Show fewer' }))
    expect(changes.getAllByRole('listitem')).toHaveLength(12)
  })

  it('says where the rest are past the first 200', async () => {
    renderDrawer(row({ dirty: true, dirtyCount: 1204, files: worktreeFiles(12), canRemove: false }), {
      read: read({ files: worktreeFiles(200), fileCount: 1204 }),
    })

    const changes = column('Changes')
    expect(changes.getByRole('heading')).toHaveTextContent('Changes1,204')
    expect(changes.queryByText(/are listed here/)).not.toBeInTheDocument()

    await userEvent.click(changes.getByRole('button', { name: 'Show the first 200' }))

    expect(changes.getAllByRole('listitem')).toHaveLength(200)
    expect(changes.getByText(/^Only the first 200 are listed here/).parentElement).toHaveTextContent(
      'Only the first 200 are listed here; 1,004 more are in Terminal (git status)',
    )
  })

  it.each<[string, DetailsRead]>([
    ['the details couldn’t be read', { state: 'failed', error: 'Couldn’t read the worktree: it is busy.' }],
    ['the files couldn’t be read', read({ fileCount: -1, errors: ['Couldn’t read the worktree: it is busy.'] })],
  ])('keeps the listing’s files when %s, and says why', (_label, details) => {
    renderDrawer(row({ dirty: true, dirtyCount: 30, files: worktreeFiles(12), canRemove: false }), { read: details })

    const changes = column('Changes')
    expect(changes.getAllByRole('listitem')).toHaveLength(12)
    expect(changes.getByText(/^Only the first 12/).parentElement).toHaveTextContent(
      'Only the first 12 are listed here; 18 more are in Terminal (git status)',
    )
    expect(changes.getByRole('button', { name: 'Copy list' })).toBeEnabled()
    expect(screen.getByText('Couldn’t read the worktree: it is busy.')).toBeInTheDocument()
  })

  it('shows the files as they are now once read', () => {
    renderDrawer(row({ dirty: true, dirtyCount: 3, files: worktreeFiles(3), canRemove: false }), {
      read: read({ fileCount: 0 }),
    })

    expect(column('Changes').getByText('Nothing uncommitted.')).toBeInTheDocument()
  })

  it.each([
    ['a folder that’s gone', row({ prunable: true, dirtyCount: 3, files: worktreeFiles(3) }), 'Its folder is gone.'],
    [
      'one git couldn’t read',
      row({ error: 'git status failed', dirtyCount: 3, files: worktreeFiles(3) }),
      'Couldn’t be read.',
    ],
  ])('lists no files for %s', (_label, wt, words) => {
    renderDrawer(wt)

    expect(column('Changes').getByText(words)).toBeInTheDocument()
    expect(column('Changes').queryByRole('button', { name: 'Copy list' })).not.toBeInTheDocument()
  })
})

describe('Drawer commits', () => {
  const ahead = row({ unpushed: 2, onGitHub: false, item: noHead })

  it('stands a line in for each commit while it reads them, four at most', () => {
    renderDrawer(ahead)
    expect(screen.getByRole('list', { name: 'Reading the commits' }).children).toHaveLength(2)
  })

  it('stands in for no more than four', () => {
    renderDrawer(row({ unpushed: 9, onGitHub: false }), { read: { state: 'reading' } })
    expect(screen.getByRole('list', { name: 'Reading the commits' }).children).toHaveLength(4)
  })

  it('lists the commits once read: each one’s short ID, subject, and age', () => {
    renderDrawer(ahead, {
      read: read({
        unpushed: 2,
        commits: [
          worktreeCommit(),
          worktreeCommit({
            oid: 'b81c4d0e5f60718293a4b5c6d7e8f90123456789',
            subject: 'test: cover a reload',
            unix: unixAgo(day),
          }),
        ],
      }),
    })

    const commits = column('Not on GitHub')
    expect(commits.getByRole('heading')).toHaveTextContent('Not on GitHub2')

    const first = within(commits.getAllByRole('listitem')[0] as HTMLElement)
    expect(first.getByText('0b1c397')).toHaveAttribute('title', '0b1c3975011e66491785ace891c893e4fc716588')
    expect(first.getByText('Keep the widget cache warm')).toHaveAttribute('title', 'Keep the widget cache warm')
    expect(first.getByText('2h')).toBeInTheDocument()
    expect(commits.getByText('test: cover a reload')).toBeInTheDocument()
    expect(commits.getByText('1d')).toBeInTheDocument()
  })

  it('says where the commits past the first 200 are', () => {
    const commits = Array.from({ length: 200 }, (_, i) => worktreeCommit({ oid: i.toString(16).padStart(40, '0') }))
    renderDrawer(row({ unpushed: 350, onGitHub: false, item: noHead }), { read: read({ unpushed: 350, commits }) })

    const notOnGitHub = column('Not on GitHub')
    expect(notOnGitHub.getByRole('heading')).toHaveTextContent('Not on GitHub350')
    expect(notOnGitHub.getAllByRole('listitem')).toHaveLength(200)
    expect(notOnGitHub.getByText(/^Only the first 200 are listed here/)).toHaveTextContent(
      'Only the first 200 are listed here; 150 more are in Terminal (git log)',
    )
  })

  it('says nothing of the rest when every commit is listed', () => {
    renderDrawer(ahead, {
      read: read({ unpushed: 2, commits: [worktreeCommit(), worktreeCommit({ oid: '1'.repeat(40) })] }),
    })

    expect(column('Not on GitHub').queryByText(/are listed here/)).not.toBeInTheDocument()
  })

  it('counts them as they are now once read', () => {
    renderDrawer(ahead, { read: read({ unpushed: 0 }) })

    const commits = column('Not on GitHub')
    expect(commits.getByRole('heading')).toHaveTextContent(/^Not on GitHub$/)
    expect(commits.getByText('None: every commit is on GitHub.')).toBeInTheDocument()
  })

  it.each<[string, DetailsRead]>([
    ['the details couldn’t be read', { state: 'failed', error: 'Couldn’t read the worktree.' }],
    ['the commits couldn’t be read', read({ unpushed: -1, errors: ['Couldn’t read the commits not on GitHub.'] })],
    ['the commits couldn’t be listed', read({ unpushed: 2, errors: ['Couldn’t read the commits not on GitHub.'] })],
  ])('says it couldn’t list them when %s', (_label, details) => {
    renderDrawer(ahead, { read: details })

    const commits = column('Not on GitHub')
    expect(commits.getByRole('heading')).toHaveTextContent('Not on GitHub2')
    expect(commits.getByText('Couldn’t list them.')).toBeInTheDocument()
  })

  it('stands one line in for commits the listing couldn’t count while it reads them', () => {
    renderDrawer(row({ unpushed: -1, onGitHub: false }))

    expect(screen.getByRole('list', { name: 'Reading the commits' }).children).toHaveLength(1)
    expect(column('Not on GitHub').queryByText('Couldn’t count them.')).not.toBeInTheDocument()
  })

  it('says it couldn’t count commits the listing couldn’t', () => {
    const uncounted = row({ unpushed: -1, onGitHub: false })
    renderDrawer(uncounted, { read: read({ unpushed: -1 }) })

    expect(column('Not on GitHub').getByText('Couldn’t count them.')).toBeInTheDocument()
  })

  it('counts commits the listing couldn’t once read', () => {
    renderDrawer(row({ unpushed: -1, onGitHub: false, item: noHead }), { read: read() })

    expect(column('Not on GitHub').getByText('None: every commit is on GitHub.')).toBeInTheDocument()
  })

  it('says a worktree at its pull request’s head is on GitHub', () => {
    renderDrawer(row())

    expect(column('Not on GitHub').getByText('None: every commit is on GitHub (PR #7’s head).')).toBeInTheDocument()
  })

  it.each([
    ['a folder that’s gone', row({ prunable: true, unpushed: 2, onGitHub: false })],
    ['one git couldn’t read', row({ error: 'git status failed', unpushed: -1, onGitHub: false })],
  ])('lists no commits for %s', (_label, wt) => {
    renderDrawer(wt)

    const commits = column('Not on GitHub')
    expect(commits.getByRole('heading')).toHaveTextContent(/^Not on GitHub$/)
    expect(commits.getByText('Couldn’t be read.')).toBeInTheDocument()
  })
})

describe('Drawer copies', () => {
  // two is a sub-agent's worktree whose two commits not on GitHub by their commit IDs each
  // have an identical copy on origin/feature-x, pull request #42's branch.
  const two = copiedWorktree({
    unpushed: 2,
    copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 2, identical: 2, pr: 42 }),
  })

  // copiesOf are two's commits as Details lists them, HEAD first, each with its copy, and
  // stillCopied how Details' comparison of them went: as the listing's did.
  const copiesOf = [
    worktreeCommit({ oid: two.head, copy: 'identical', copyOid: '3'.repeat(40) }),
    worktreeCommit({
      oid: '1'.repeat(40),
      subject: 'Test the widget cache',
      copy: 'identical',
      copyOid: '4'.repeat(40),
    }),
  ]
  const stillCopied = copyComparison({ identical: 2 })

  it('says nothing is lost, headed with the pull request the commits were copied onto', () => {
    const { li } = renderDrawer(two)

    expect(li.querySelector('[data-tone]')).toHaveAttribute('data-tone', 'success')
    expect(li.querySelector('[data-tone]')).toHaveTextContent(
      'Safe to remove: no uncommitted files, and every commit’s change is on GitHub, copied onto PR #42’s branch.Removing it keeps its branch, worktree-agent-a1b2c3, with the original commits.',
    )

    const copies = column('Copied to PR #42')
    expect(copies.getByRole('heading')).toHaveTextContent(/^Copied to PR #42$/)
    expect(copies.getByText(/^Each has an identical copy on/)).toHaveTextContent(
      'Each has an identical copy on origin/feature-x, under another commit ID.',
    )
    expect(copies.getByText('origin/feature-x').tagName).toBe('CODE')
    // They wait for Details, a line for each.
    expect(copies.getByRole('list', { name: 'Reading the commits' }).children).toHaveLength(2)
    expect(screen.queryByRole('heading', { name: /^Not on GitHub/ })).not.toBeInTheDocument()
  })

  it('lists each commit with its identical copy once Details compared them again', () => {
    renderDrawer(two, { read: read({ unpushed: 2, commits: copiesOf, copies: stillCopied }) })

    const copies = column('Copied to PR #42')
    const [first, second] = copies.getAllByRole('listitem').map((li) => within(li))
    expect(first?.getByText('Keep the widget cache warm')).toBeInTheDocument()
    expect(first?.getByTitle(`An identical copy of it is on GitHub: ${'3'.repeat(40)}`)).toHaveTextContent(
      '= copied as 3333333',
    )
    expect(first?.getByTitle(/^An identical copy/)).toHaveClass('text-muted')
    expect(second?.getByTitle(/^An identical copy/)).toHaveTextContent('= copied as 4444444')
    expect(copies.queryByText(/are listed here/)).not.toBeInTheDocument()
  })

  it('says where the copied commits past the first 200 are', () => {
    renderDrawer(copiedWorktree(), {
      read: read({ unpushed: 10, commits: copiesOf.slice(0, 1), copies: copyComparison() }),
    })

    expect(column('Copied to PR #42').getByText(/^Only the first 1 are listed here/)).toHaveTextContent(
      'Only the first 1 are listed here; 9 more are in Terminal (git log)',
    )
  })

  it('says where the copies are, with no list, when Details couldn’t be read', () => {
    renderDrawer(two, { read: { state: 'failed', error: 'Couldn’t read the worktree.' } })

    const copies = column('Copied to PR #42')
    expect(copies.getByText(/^Each has an identical copy on/)).toBeInTheDocument()
    expect(copies.queryByRole('list')).not.toBeInTheDocument()
  })

  it('heads copies on a branch no pull request is known to head with that branch, in its own case', () => {
    renderDrawer(
      copiedWorktree({
        item: null,
        linkedBy: '',
        copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 10, identical: 10 }),
      }),
    )

    const heading = screen.getByRole('heading', { name: 'Copied to origin/feature-x' })
    expect(within(heading).getByText('origin/feature-x')).toHaveClass('normal-case')
  })

  it('says a branch that’s gone, whose copies the app recorded, went into the merged pull request', () => {
    const { li } = renderDrawer(recordedWorktree(), {
      read: read({ unpushed: 10, commits: [worktreeCommit({ oid: recordedWorktree().head })] }),
    })

    expect(li.querySelector('[data-tone]')).toHaveTextContent(
      'Safe to remove: no uncommitted files, and every commit’s change went into PR #42, merged 2d ago.',
    )

    const copies = column('Copied to PR #42')
    expect(copies.getByText(/is gone, but the app recorded/)).toHaveTextContent(
      'origin/feature-x is gone, but the app recorded each one’s identical copy there, and PR #42 has them.',
    )
    expect(copies.getByText('Keep the widget cache warm')).toBeInTheDocument()
    expect(copies.queryByText(/copied as/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clean up' })).toHaveAttribute(
      'title',
      'Removes the worktree, whose every commit’s change is on GitHub; asks which branches go with it',
    )
  })

  it('marks no commit a record vouches for, whatever the branch holds now', () => {
    const merged = recordedWorktree({
      copies: worktreeCopies({
        ref: 'origin/feature-x',
        branch: 'feature-x',
        count: 10,
        identical: 10,
        recorded: true,
        pr: 42,
      }),
    })
    const { li } = renderDrawer(merged, {
      read: read({
        unpushed: 10,
        commits: [worktreeCommit({ oid: merged.head, copy: 'edited', copyOid: '5'.repeat(40) })],
        copies: copyComparison({ identical: 0, edited: 1 }),
      }),
    })

    expect(li.querySelector('[data-tone]')).toHaveTextContent(
      'Safe to remove: no uncommitted files, and every commit’s change went into PR #42, merged 2d ago; origin/feature-x no longer holds all of them.',
    )

    const copies = column('Copied to PR #42')
    expect(copies.getByText(/no longer holds them all/)).toHaveTextContent(
      'origin/feature-x no longer holds them all, but the app recorded each one’s identical copy there, and PR #42 has them.',
    )
    expect(copies.getByText('Keep the widget cache warm')).toBeInTheDocument()
    expect(copies.queryByText(/edited copy/)).not.toBeInTheDocument()
  })

  it('names the pull request recorded copies went into, though the worktree is linked to another', () => {
    renderDrawer(recordedWorktree({ item: linkedItem(), linkedBy: 'name' }))

    expect(screen.getByRole('heading', { name: 'Copied to PR #42' })).toBeInTheDocument()
    expect(about('For')).toHaveTextContent(/^PR #7/)
    expect(screen.getByText(/is gone, but the app recorded/)).toHaveTextContent(
      'origin/feature-x is gone, but the app recorded each one’s identical copy there, and PR #42 has them.',
    )
  })

  it('says recorded copies went into their pull request when its number isn’t known', () => {
    renderDrawer(
      recordedWorktree({
        item: null,
        linkedBy: '',
        copies: worktreeCopies({
          ref: 'origin/feature-x',
          branch: 'feature-x',
          count: 10,
          identical: 10,
          recorded: true,
          gone: true,
        }),
      }),
    )

    expect(screen.getByRole('heading', { name: 'Copied to origin/feature-x' })).toBeInTheDocument()
    expect(screen.getByText(/is gone, but the app recorded/)).toHaveTextContent(
      'origin/feature-x is gone, but the app recorded each one’s identical copy there, and its pull request has them.',
    )
  })

  it('marks a commit with only an edited copy, in amber, among those not on GitHub', () => {
    const { li } = renderDrawer(partlyCopiedWorktree(), {
      read: read({
        unpushed: 1,
        commits: [worktreeCommit({ oid: partlyCopiedWorktree().head, copy: 'edited', copyOid: '5'.repeat(40) })],
        copies: copyComparison({ identical: 0, edited: 1 }),
      }),
    })

    expect(li.querySelector('[data-tone]')).toHaveAttribute('data-tone', 'warning')
    expect(li.querySelector('[data-tone]')).toHaveTextContent('1 has only an edited copy on origin/feature-x.')

    const commits = column('Not on GitHub')
    expect(commits.getByRole('heading')).toHaveTextContent('Not on GitHub1')
    const mark = commits.getByText(/^≈ edited copy/)
    expect(mark).toHaveTextContent('≈ edited copy 5555555')
    expect(mark).toHaveClass('text-caution')
    expect(mark).toHaveAttribute(
      'title',
      `Only an edited copy of it is on GitHub, ${'5'.repeat(40)}: the same author, time, and subject, with another change`,
    )
  })

  it('counts the commits as not on GitHub once their copies changed since they were compared', () => {
    const { li } = renderDrawer(two, {
      read: read({
        unpushed: 2,
        commits: [
          worktreeCommit({ oid: two.head, copy: 'identical', copyOid: '3'.repeat(40) }),
          worktreeCommit({ oid: '1'.repeat(40), copy: 'edited', copyOid: '4'.repeat(40) }),
        ],
        copies: copyComparison({ identical: 1, edited: 1 }),
      }),
    })

    expect(li.querySelector('[data-tone]')).toHaveAttribute('data-tone', 'warning')
    expect(li.querySelector('[data-tone]')).toHaveTextContent(
      '1 has an identical copy on origin/feature-x, 1 only an edited one.',
    )
    expect(column('Not on GitHub').getByRole('heading')).toHaveTextContent('Not on GitHub2')
    expect(column('Not on GitHub').getByText(/^≈ edited copy/)).toBeInTheDocument()
  })

  it('counts the commits as not on GitHub once their branch is gone, nothing recorded', () => {
    const { li } = renderDrawer(two, {
      read: read({
        unpushed: 2,
        commits: [worktreeCommit({ oid: two.head }), worktreeCommit({ oid: '1'.repeat(40) })],
        copies: copyComparison({ ref: '', branch: '', identical: 0 }),
      }),
    })

    expect(li.querySelector('[data-tone]')).toHaveAttribute('data-tone', 'warning')
    expect(li.querySelector('[data-tone]')).toHaveTextContent(
      'Its folder is clean, but 2 commits on worktree-agent-a1b2c3 aren’t on GitHub.',
    )
    expect(screen.queryByRole('heading', { name: /^Copied to/ })).not.toBeInTheDocument()
  })

  it('counts the commits as not on GitHub when Details couldn’t compare them, saying why', () => {
    const { li } = renderDrawer(two, {
      read: read({
        unpushed: 2,
        commits: copiesOf.map((c) => ({ ...c, copy: '' as const, copyOid: '' })),
        copies: copyComparison({
          compared: false,
          ref: '',
          branch: '',
          identical: 0,
          note: 'Couldn’t compare its commits with the pushed branches. git refused it.',
        }),
      }),
    })

    expect(li.querySelector('[data-tone]')).toHaveAttribute('data-tone', 'warning')
    expect(li.querySelector('[data-tone]')).toHaveTextContent(
      'Couldn’t compare its commits with the pushed branches. git refused it.',
    )
    expect(column('Not on GitHub').getByRole('heading')).toHaveTextContent('Not on GitHub2')
  })

  it('counts the commits as not on GitHub once HEAD moved since they were compared', () => {
    const { li } = renderDrawer(two, { read: read({ unpushed: 2, commits: [worktreeCommit({ copy: 'edited' })] }) })

    expect(li.querySelector('[data-tone]')).toHaveAttribute('data-tone', 'warning')
    expect(li.querySelector('[data-tone]')).toHaveTextContent(
      'Its folder is clean, but 2 commits on worktree-agent-a1b2c3 aren’t on GitHub.',
    )
    expect(column('Not on GitHub').getByRole('heading')).toHaveTextContent('Not on GitHub2')
    expect(screen.queryByRole('heading', { name: /^Copied to/ })).not.toBeInTheDocument()
  })

  it.each<[string, Partial<Worktree>, string, RegExp]>([
    ['made for', {}, 'For', /^PR #7/],
    ['on the branch of', { linkedBy: 'branch' }, 'On the branch of', /^PR #7/],
    ['with its commits copied onto', { linkedBy: 'copy' }, 'Copied onto', /^PR #7/],
  ])('says the pull request a worktree is %s', (_label, link, term, value) => {
    renderDrawer(row(link))

    expect(about(term)).toHaveTextContent(value)
  })
})

describe('Drawer branch', () => {
  // branchLine is the line of the branch block named key.
  function branchLine(key: 'Branch' | 'Head' | 'Base'): HTMLElement {
    return column('Not on GitHub').getByText(key).parentElement as HTMLElement
  }

  it('shows the branch, which Copy puts on the clipboard, and the head with its age', async () => {
    renderDrawer(row())

    expect(branchLine('Branch')).toHaveTextContent('worktree-pr-7')
    expect(branchLine('Head')).toHaveTextContent('fc1c397 · 3h ago')
    expect(within(branchLine('Head')).getByText('fc1c397')).toHaveAttribute(
      'title',
      'fc1c3975011e66491785ace891c893e4fc716587',
    )

    await userEvent.click(within(branchLine('Branch')).getByRole('button', { name: 'Copy the branch name' }))
    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith('worktree-pr-7')
  })

  it('says a detached worktree has no branch, and shows its head alone without a commit time', () => {
    renderDrawer(row({ detached: true, branch: '', lastCommitUnix: 0 }))

    expect(branchLine('Branch')).toHaveTextContent('none: detached')
    expect(within(branchLine('Branch')).queryByRole('button')).not.toBeInTheDocument()
    expect(branchLine('Head')).toHaveTextContent(/^Headfc1c397$/)
  })

  it('stands in for the base while it reads it', () => {
    renderDrawer(row())

    expect(within(branchLine('Base')).getByRole('img', { name: 'Reading the base' })).toBeInTheDocument()
  })

  it.each<[string, DetailsRead, string]>([
    ['level with it', read(), 'origin/main · up to date'],
    ['behind it', read({ behind: 3 }), 'origin/main · 3 behind'],
    ['an unknown way behind it', read({ behind: -1 }), 'origin/main'],
    ['no base the clone names', read({ base: '' }), 'not known'],
    ['details that couldn’t be read', { state: 'failed', error: 'Couldn’t read it.' }, 'not known'],
  ])('shows the base once read, %s', (_label, details, words) => {
    renderDrawer(row(), { read: details })

    expect(branchLine('Base')).toHaveTextContent(`Base${words}`)
  })

  it('reads no base for one git couldn’t read', () => {
    renderDrawer(row({ error: 'git status failed' }))

    expect(branchLine('Base')).toHaveTextContent('Basenot known')
  })

  it('shows no branch for a folder that’s gone', () => {
    renderDrawer(row({ prunable: true }))

    expect(column('Not on GitHub').queryByText('Branch')).not.toBeInTheDocument()
  })
})

describe('Drawer about', () => {
  it('says what it’s for, with its checks, review, and draft as the inbox read them', async () => {
    const facts: ItemFacts = { item: item(), checks: 'SUCCESS', review: 'APPROVED', draft: true }
    renderDrawer(row(), { facts })

    expect(about('For')).toHaveTextContent('PR #7 Add the widget cache open · checks passed · approved · draft')

    await userEvent.click(within(about('For')).getByRole('button', { name: 'PR #7' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/example-repo/pull/7')
  })

  it('says how long ago its item merged, and changes requested', () => {
    const facts: ItemFacts = { item: item(), checks: '', review: 'CHANGES_REQUESTED', draft: false }
    renderDrawer(row({ item: linkedItem({ closedAt: isoAgo({ s: 2 * day }) }) }), { facts })

    expect(about('For')).toHaveTextContent('PR #7 Add the widget cache merged 2d ago · changes requested')
  })

  it('says only what the inbox read', () => {
    renderDrawer(row(), { facts: { item: item(), checks: 'PENDING', review: '', draft: false } })

    expect(about('For')).toHaveTextContent(/open · checks running$/)
  })

  it('says merged alone when it doesn’t know when', () => {
    renderDrawer(row({ item: linkedItem({ closedAt: '' }) }))

    expect(about('For')).toHaveTextContent(/merged$/)
  })

  it('says a worktree for nothing is for no pull request or issue', () => {
    renderDrawer(row({ item: null }))

    expect(about('For')).toHaveTextContent('no pull request or issue')
  })

  it.each<[string, Worktree, AgentSession | undefined, string]>([
    ['a followed session', running, working, 'Claude Code 2.1.328 · pid 47890 · working · started 11:48 (12m ago)'],
    [
      'a program with no version',
      row({ inUse: [{ pid: 60311, command: 'codex', agent: 'codex', by: '' }], stage: 'in-use' }),
      undefined,
      'Codex · pid 60311',
    ],
    [
      'a routine’s agent',
      row({ inUse: [{ pid: 61022, command: '2.1.328', agent: 'claude', by: '⚡ Fix checks' }], stage: 'in-use' }),
      working,
      'Claude Code 2.1.328 · pid 61022 · working · started 11:48 (12m ago) · for ⚡ Fix checks, in the background',
    ],
    [
      'a process with none',
      row({ inUse: [{ pid: 0, command: 'node', agent: '', by: '' }], stage: 'in-use' }),
      undefined,
      'node',
    ],
    [
      'a live lock',
      row({
        locked: true,
        agentLock: true,
        lockReason: 'claude session pr-7 (pid 70113 start Mon Jun 15 11:30:03 2026)',
        stage: 'in-use',
      }),
      undefined,
      'Claude, by its lock · pid 70113 · started 11:30 (29m ago)',
    ],
    [
      'a lock it can’t read',
      row({ locked: true, agentLock: true, lockReason: 'held', stage: 'in-use' }),
      undefined,
      'Claude, by its lock',
    ],
  ])('says what works in it: %s', (_label, wt, session, words) => {
    renderDrawer(wt, { session })

    expect(about('Session')).toHaveTextContent(words)
  })

  it('says nothing works in it when only a past session did', () => {
    renderDrawer(row({ agent: 'claude', lastLaunchedAt: isoAgo({ s: hour }) }))

    expect(column('About').queryByText('Session')).not.toBeInTheDocument()
  })

  it('counts its launches, with the last', () => {
    renderDrawer(row({ agent: 'codex', lastLaunchedAt: isoAgo({ s: 2 * hour }), launches: 3 }))

    expect(about('Launches')).toHaveTextContent('3 launches · last Codex, 2h ago')
  })

  it('counts a launch it doesn’t know the agent of', () => {
    renderDrawer(row({ launches: 1 }))

    expect(about('Launches')).toHaveTextContent(/^1 launch$/)
  })

  it('says who made it, and when once read', () => {
    renderDrawer(row(), { read: read() })

    expect(about('Made by')).toHaveTextContent(`${brand.name} · 1d ago`)
  })

  it('stands in for when it was made while it reads it', () => {
    renderDrawer(row())

    expect(within(about('Made by')).getByRole('img', { name: 'Reading when it was made' })).toBeInTheDocument()
  })

  it('says nothing of when for one it has no record of, or can’t read', () => {
    renderDrawer(row({ error: 'git status failed' }))
    expect(about('Made by')).toHaveTextContent(new RegExp(`^${brand.name}$`))
  })

  it('says nothing of when for one made before it kept records', () => {
    renderDrawer(row(), { read: read({ createdAt: '' }) })
    expect(about('Made by')).toHaveTextContent(new RegExp(`^${brand.name}$`))
  })

  it('shows the path, which Copy puts on the clipboard', async () => {
    const wt = row()
    renderDrawer(wt)

    expect(about('Path')).toHaveTextContent(managed('pr-7'))
    await userEvent.click(within(about('Path')).getByRole('button', { name: 'Copy the path' }))

    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith(wt.path)
  })

  it.each<[string, Partial<Worktree>, string]>([
    [
      'by hand, with why',
      { lockReason: 'kept for the release branch' },
      'By hand: “kept for the release branch” · the app never unlocks it',
    ],
    ['by hand', {}, 'By hand · the app never unlocks it'],
    [
      'by a live session',
      { agentLock: true, lockReason: 'claude session pr-7 (pid 70113 start Mon Jun 15 11:30:03 2026)' },
      'Claude session pr-7 · pid 70113 · live, started 11:30',
    ],
    ['by a session whose reason it can’t read', { agentLock: true, lockReason: 'held' }, 'Claude session · live'],
    [
      'by an ended session',
      {
        agentLock: true,
        staleLock: true,
        canUnlock: true,
        lockReason: 'claude session pr-7 (pid 66120 start Mon Jun 15 07:14:09 2026)',
      },
      'Claude session pr-7 · pid 66120 · ended, started 4h ago · Unlock releases it',
    ],
    [
      'by an ended session of a finished worktree',
      { agentLock: true, staleLock: true, canCleanUp: true, lockReason: 'claude session pr-7 (pid 7)' },
      'Claude session pr-7 · pid 7 · ended · Clean up releases it',
    ],
  ])('says who holds its lock: %s', (_label, overrides, words) => {
    renderDrawer(row({ locked: true, ...overrides }))

    expect(about('Lock')).toHaveTextContent(words)
  })

  it('shows no lock for an unlocked one', () => {
    renderDrawer(row())

    expect(column('About').queryByText('Lock')).not.toBeInTheDocument()
  })

  it('says which branches a removal would delete, and why one stays', () => {
    renderDrawer(
      row({
        branches: [
          ownBranch(),
          ownBranch({
            name: 'feat/x',
            kind: 'pullRequest',
            deletable: false,
            reason: '1 commit on it isn’t on GitHub',
          }),
        ],
      }),
    )

    expect(about('Removal')).toHaveTextContent('worktree-pr-7 can go with it')
    expect(about('Removal')).toHaveTextContent('feat/x stays: 1 commit on it isn’t on GitHub')
  })

  it.each([
    ['a detached one has no branch to delete', row({ detached: true, branches: [] }), 'It has no branch to delete.'],
    [
      'another’s branch stays',
      row({ branch: 'hotfix/login-loop', branches: [] }),
      'Its branch stays: it’s neither the branch made for it nor its pull request’s.',
    ],
  ])('says %s', (_label, wt, words) => {
    renderDrawer(wt)

    expect(about('Removal')).toHaveTextContent(words)
  })

  it('says nothing of a removal for a folder that’s gone', () => {
    renderDrawer(row({ prunable: true }))

    expect(column('About').queryByText('Removal')).not.toBeInTheDocument()
  })
})

describe('Drawer errors', () => {
  it('says what of its details couldn’t be read', () => {
    renderDrawer(row(), {
      read: read({ errors: ['Couldn’t read the changed files: git failed.', 'Couldn’t read the base: no origin.'] }),
    })

    expect(screen.getByText('Couldn’t read the changed files: git failed.')).toBeInTheDocument()
    expect(screen.getByText('Couldn’t read the base: no origin.')).toBeInTheDocument()
  })

  it('says nothing when every read worked', () => {
    renderDrawer(row(), { read: read() })

    expect(screen.queryByText(/^Couldn’t read/)).not.toBeInTheDocument()
  })
})

describe('Drawer footer', () => {
  it('opens the worktree in Terminal or Finder, and copies its path, then Remove…', async () => {
    const wt = row()
    const { drawer, onTerminal, onReveal, onRemove } = renderDrawer(wt)

    const buttons = drawer
      .getAllByRole('button')
      .slice(-4)
      .map((b) => b.textContent)
    expect(buttons).toEqual(['Open in Terminal', 'Reveal in Finder', 'Copy path', 'Remove…'])

    await userEvent.click(drawer.getByRole('button', { name: 'Open in Terminal' }))
    await userEvent.click(drawer.getByRole('button', { name: 'Reveal in Finder' }))
    await userEvent.click(drawer.getByRole('button', { name: 'Copy path' }))

    expect(onTerminal).toHaveBeenCalledOnce()
    expect(onReveal).toHaveBeenCalledOnce()
    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith(wt.path)

    const remove = drawer.getByRole('button', { name: 'Remove…' })
    expect(remove).toHaveAttribute('title', 'Removes the folder; asks which branches go with it')
    await userEvent.click(remove)
    expect(onRemove).toHaveBeenCalledExactlyOnceWith(er, wt)
  })

  it('waits while Terminal or Finder opens', () => {
    const { drawer } = renderDrawer(row(), { opening: true })

    expect(drawer.getByRole('button', { name: 'Open in Terminal' })).toBeDisabled()
    expect(drawer.getByRole('button', { name: 'Reveal in Finder' })).toBeDisabled()
  })

  it('offers no Terminal or Finder for one git couldn’t read', () => {
    const { drawer } = renderDrawer(row({ error: 'git status failed', canRemove: false }))

    expect(drawer.queryByRole('button', { name: 'Open in Terminal' })).not.toBeInTheDocument()
    expect(drawer.getByRole('button', { name: 'Copy path' })).toBeInTheDocument()
    expect(drawer.getByRole('button', { name: 'Remove…' })).toHaveAttribute('title', 'git couldn’t read it')
    expect(drawer.getByRole('button', { name: 'Remove…' })).toBeDisabled()
  })

  it('offers Prune for a folder that’s gone, in place of everything else', async () => {
    const { drawer, onPrune } = renderDrawer(row({ prunable: true, canRemove: false }))

    expect(drawer.queryByRole('button', { name: 'Open in Terminal' })).not.toBeInTheDocument()
    expect(drawer.queryByRole('button', { name: 'Copy path' })).not.toBeInTheDocument()
    expect(drawer.queryByRole('button', { name: /Remove…$/ })).not.toBeInTheDocument()

    const prune = drawer.getByRole('button', { name: 'Prune example-repo' })
    expect(prune).toHaveAttribute(
      'title',
      'Clears git’s entries for worktrees whose folders are gone; touches no worktree that exists, and no branch',
    )
    await userEvent.click(prune)
    expect(onPrune).toHaveBeenCalledExactlyOnceWith(er)
  })

  it('cleans up a finished worktree from its footer too, but never launches there', async () => {
    const wt = row({ item: linkedItem(), stage: 'finished', canCleanUp: true })
    const { drawer, onCleanUp } = renderDrawer(wt)

    await userEvent.click(drawer.getByRole('button', { name: 'Clean up' }))

    expect(onCleanUp).toHaveBeenCalledExactlyOnceWith(er, wt)
    expect(drawer.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()
  })

  it('unlocks a lock an ended session left from its footer', async () => {
    const wt = row({ locked: true, agentLock: true, staleLock: true, canUnlock: true, canRemove: false })
    const { drawer, onUnlock } = renderDrawer(wt)

    await userEvent.click(drawer.getByRole('button', { name: 'Unlock' }))

    expect(onUnlock).toHaveBeenCalledExactlyOnceWith(er, wt)
    expect(drawer.getByRole('button', { name: 'Remove…' })).toHaveAttribute(
      'title',
      'A Claude session that has ended still locks it: Unlock releases it',
    )
  })

  it('holds the verb back while a change takes the worktree', () => {
    const { drawer } = renderDrawer(row({ stage: 'finished', canCleanUp: true }), { working: true })

    expect(drawer.queryByRole('button', { name: 'Clean up' })).not.toBeInTheDocument()
  })

  it('force removes one with uncommitted files, in red, saying what it discards', async () => {
    const wt = row({ dirty: true, dirtyCount: 3, files: worktreeFiles(3), canRemove: false, canForceRemove: true })
    const { drawer, onForceRemove } = renderDrawer(wt)

    expect(drawer.queryByRole('button', { name: 'Remove…' })).not.toBeInTheDocument()

    const force = drawer.getByRole('button', { name: 'Force remove…' })
    expect(force).toHaveClass('text-danger')
    expect(force).toHaveAttribute('title', 'Discards 3 uncommitted files; lists them and asks first')
    await userEvent.click(force)

    expect(onForceRemove).toHaveBeenCalledExactlyOnceWith(er, wt)
  })

  it('says why Force remove… can’t run', () => {
    const { drawer } = renderDrawer(running, { session: working })

    expect(drawer.getByRole('button', { name: 'Force remove…' })).toBeDisabled()
    expect(drawer.getByRole('button', { name: 'Force remove…' })).toHaveAttribute('title', 'Claude runs in it')
  })

  it('holds every change while one runs', () => {
    const { drawer } = renderDrawer(row({ stage: 'finished', canCleanUp: true }), { disabled: true })

    expect(drawer.getByRole('button', { name: 'Clean up' })).toBeDisabled()
    expect(drawer.getByRole('button', { name: 'Remove…' })).toBeDisabled()
    expect(drawer.getByRole('button', { name: 'Remove…' })).toHaveAttribute(
      'title',
      'Removes the folder; asks which branches go with it',
    )
  })

  it('names itself for its worktree', () => {
    renderDrawer(row({ detached: true, branch: '' }))

    expect(screen.getByRole('listitem', { name: 'About pr-7' })).toHaveAttribute('id', 'drawer')
  })

  it('shows the hand lock’s danger band over a footer that offers no removal', () => {
    const { drawer } = renderDrawer(handLocked)

    expect(drawer.getByRole('button', { name: 'Remove…' })).toHaveAttribute('title', 'It’s locked by hand')
  })
})
