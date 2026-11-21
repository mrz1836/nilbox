import { act, screen, within, type BoundFunctions, type queries } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type {
  BranchResult,
  BranchRules,
  LinkedItem,
  MergedBranch,
  RemoveResult,
  Worktree,
  WorktreeRepo,
} from '../platform'
import { deferred } from '../test/async'
import { FIXED_NOW, fixedNow, renderAt } from '../test/clock'
import {
  agentSession,
  details as itemDetails,
  finishedWorktree,
  item,
  linkedItem,
  mainWorktree,
  mergedBranch,
  ownBranch,
  removeResult,
  sessionsSnapshot,
  worktree,
  worktreeCleanup,
  worktreeDetails,
  worktreeFiles,
  worktreeRepo,
  worktreesSnapshot,
} from '../test/fixtures'
import { platform, send } from '../test/platform'
import { launchPlace } from './worktrees/layout'
import { WorktreesView, type WorktreesViewProps } from './WorktreesView'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const repoPath = '/home/user/projects/example-repo'
const linkedPath = '/home/user/projects/example-repo/.claude/worktrees/pr-7'
const linkedDisplay = '~/projects/example-repo/.claude/worktrees/pr-7'
// pr7 is the fixture worktree's name: its branch.
const pr7 = 'worktree-pr-7'

// draw renders the page at the tests' clock, its listing arriving at it too.
function draw(props: Partial<WorktreesViewProps> = {}) {
  const view = renderAt(<WorktreesView clock={fixedNow} {...props} />)

  return {
    ...view,
    rerender: (next: Partial<WorktreesViewProps> = {}) => {
      view.rerender(<WorktreesView clock={fixedNow} {...next} />)
    },
  }
}

// escaped is text to match literally in a regular expression.
function escaped(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// chevron is the button that unfolds, or folds, the row of the worktree named name.
function chevron(name: string): HTMLElement {
  return screen.getByRole('button', { name: new RegExp(`^(Unfold|Fold) ${escaped(name)}$`) })
}

// row returns the queries scoped to the row of the worktree named name: its branch, or
// "detached" and its folder.
function row(name: string) {
  const li = chevron(name).closest('li')

  if (li === null) {
    throw new Error(`no row for ${name}`)
  }

  return within(li)
}

// unfold opens the drawer of the worktree named name.
async function unfold(name: string) {
  await userEvent.click(screen.getByRole('button', { name: `Unfold ${name}` }))
}

// drawer returns the queries scoped to the open drawer of the worktree named name.
function drawer(name: string) {
  return within(screen.getByRole('listitem', { name: `About ${name}` }))
}

// clone waits for one clone's section and returns the queries scoped to it.
async function clone(name: string) {
  return within(await screen.findByRole('region', { name }))
}

// heading is the button that folds a clone's rows, which names its clone.
function heading(name: string): HTMLElement {
  const section = screen.getByRole('region', { name })

  return within(section).getAllByRole('button')[0] as HTMLElement
}

// openMenu opens the ⋯ of the worktree named name and returns the queries scoped to it.
async function openMenu(name: string) {
  await userEvent.click(screen.getByRole('button', { name: `More for ${name}` }))

  return within(screen.getByRole('menu', { name: `More for ${name}` }))
}

// cleanupMenu opens the head's cleanup ⌄ and returns the queries scoped to it.
async function cleanupMenu() {
  await userEvent.click(await screen.findByRole('button', { name: 'More cleanups' }))

  return within(screen.getByRole('menu', { name: 'More cleanups' }))
}

// choose picks the item called name from the menu given.
async function choose(menu: BoundFunctions<typeof queries>, name: string) {
  await userEvent.click(menu.getByRole('menuitem', { name }))
}

// remove asks to remove the worktree named name, from its ⋯.
async function remove(name: string, item = 'Remove…') {
  await choose(await openMenu(name), item)
}

// confirmWith clicks the open dialog's button called name.
async function confirmWith(name: string) {
  await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name }))
}

// meta is the head's line under the title, which says how many and when, or how far.
function meta(): HTMLElement {
  return screen.getByRole('heading', { name: 'Worktrees' }).nextElementSibling as HTMLElement
}

// rules are the app's default branch rules, with any changes.
function rules(overrides: Partial<BranchRules> = {}): BranchRules {
  return { own: true, pullRequest: true, merged: true, protected: ['main', 'master'], ...overrides }
}

// linked is a worktree of example-repo named name, at .claude/worktrees/name, on the branch
// worktree-name, with any changes.
function linked(name: string, overrides: Partial<Worktree> = {}): Worktree {
  return worktree({
    path: `${repoPath}/.claude/worktrees/${name}`,
    displayPath: `~/projects/example-repo/.claude/worktrees/${name}`,
    branch: `worktree-${name}`,
    branches: [ownBranch({ name: `worktree-${name}` })],
    ...overrides,
  })
}

// listing has the listing hold example-repo with its main checkout and the worktrees given.
function listing(worktrees: Worktree[], overrides: Parameters<typeof worktreesSnapshot>[0] = {}) {
  platform.listWorktrees.mockResolvedValue(
    worktreesSnapshot({ repos: [worktreeRepo({ worktrees: [mainWorktree(), ...worktrees] })], ...overrides }),
  )
}

// prBranch is the pull request's own branch feature-x, checked out in the worktree.
const prBranch = ownBranch({ name: 'feature-x', kind: 'pullRequest', head: 'a'.repeat(40) })

// other is a second clone, other-tool, with its main checkout and the worktrees given.
function other(worktrees: Worktree[], overrides: Partial<WorktreeRepo> = {}): WorktreeRepo {
  return worktreeRepo({
    path: '/home/user/projects/other-tool',
    displayPath: '~/projects/other-tool',
    name: 'other-tool',
    worktrees: [
      mainWorktree({ path: '/home/user/projects/other-tool', displayPath: '~/projects/other-tool' }),
      ...worktrees,
    ],
    ...overrides,
  })
}

// settle runs fn and lets what it settles land.
async function settle(fn: () => void) {
  await act(async () => {
    fn()
    await Promise.resolve()
  })
}

describe('WorktreesView head', () => {
  it('lists again only when Check again is clicked, never on a timer', async () => {
    draw()
    await clone('example-repo')

    vi.useFakeTimers()
    act(() => {
      vi.advanceTimersByTime(30 * 60_000)
    })
    vi.useRealTimers()

    expect(platform.listWorktrees).toHaveBeenCalledOnce()
    expect(platform.on).not.toHaveBeenCalledWith('inbox:updated', expect.any(Function))

    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ repos: [], cleanCount: 0 }))
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))

    expect(await screen.findByText('No clone under ~/projects has linked worktrees.')).toBeInTheDocument()
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('counts the linked worktrees and the clones, and says when they were checked', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [worktreeRepo({ worktrees: [mainWorktree(), worktree(), linked('issue-9')] }), other([linked('pr-3')])],
      }),
    )
    draw()

    expect(await screen.findByText('3 worktrees in 2 clones · checked')).toHaveAttribute(
      'title',
      'The clones under ~/projects',
    )
    expect(meta()).toHaveTextContent('3 worktrees in 2 clones · checked just now')
    expect(meta().querySelector('time')).toHaveAttribute('dateTime', '2026-06-15T12:00:00.000Z')
  })

  it('names every repository location, and the ones it couldn’t read', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        roots: ['~/projects', '~/work', '/Volumes/ext/src'],
        unread: ['/Volumes/ext/src'],
        repos: [worktreeRepo({ worktrees: [mainWorktree(), worktree()] })],
      }),
    )
    draw()

    expect(await screen.findByText('1 worktree in 1 clone · checked')).toHaveAttribute(
      'title',
      'The clones in ~/projects and 2 other locations:\n~/projects\n~/work\n/Volumes/ext/src',
    )
    expect(screen.getByRole('note')).toHaveTextContent(
      'Couldn’t read /Volumes/ext/src, so its clones aren’t listed. Check it in Settings → Repositories.',
    )

    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({ roots: ['~/projects', '~/work', '/Volumes/ext/src'], repos: [], cleanCount: 0 }),
    )
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))

    expect(
      await screen.findByText('No clone in ~/projects and 2 other locations has linked worktrees.'),
    ).toBeInTheDocument()
    expect(meta()).toHaveTextContent('0 worktrees in 0 clones · checked just now')
    expect(screen.queryByRole('note')).not.toBeInTheDocument()
  })

  it('says it is listing until the first listing arrives, holding the place of the clones', async () => {
    const first = deferred<ReturnType<typeof worktreesSnapshot>>()
    platform.listWorktrees.mockReturnValue(first.promise)
    draw()

    expect(within(meta()).getByRole('status')).toHaveTextContent('Checking the worktrees…')
    expect(screen.getByRole('button', { name: 'Check again' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Check again' })).toHaveAttribute('title', 'A listing is under way')
    // Nothing is listed yet, so there is nothing to clean up.
    expect(screen.queryByRole('button', { name: 'More cleanups' })).not.toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Worktrees' }).querySelector('[aria-hidden="true"]')).not.toBeNull()

    send('worktrees:progress', { phase: 'listing', done: 3, total: 12 })
    expect(within(meta()).getByRole('status')).toHaveTextContent('Listing worktrees · 3 of 12 clones')

    await settle(() => {
      first.resolve(worktreesSnapshot({ repos: [], cleanCount: 0 }))
    })

    expect(await screen.findByText('No clone under ~/projects has linked worktrees.')).toBeInTheDocument()
    expect(meta()).toHaveTextContent('0 worktrees in 0 clones · checked just now')
    expect(within(meta()).queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Check again' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Check again' })).not.toHaveAttribute('title')
    // With nothing to clean up, the ⌄ still offers the merged branches.
    expect((await cleanupMenu()).getByRole('menuitem', { name: 'Clean up merged branches…' })).toBeEnabled()
  })

  it('says how far a quiet listing has got, as after a background sweep, and drops what it heard after', async () => {
    const { rerender } = draw({ reloadKey: 1 })
    await clone('example-repo')

    const quiet = deferred<ReturnType<typeof worktreesSnapshot>>()
    platform.listWorktrees.mockReturnValueOnce(quiet.promise)
    rerender({ reloadKey: 2 })

    const status = await within(meta()).findByRole('status')
    expect(status).toHaveTextContent('Checking the worktrees…')
    // A quiet listing leaves the actions alone, and the rows where they are.
    expect(screen.getByRole('button', { name: 'Check again' })).toBeEnabled()
    expect(chevron(pr7)).toBeInTheDocument()

    send('worktrees:progress', { phase: 'clones', done: 0, total: 0 })
    expect(status).toHaveTextContent('Finding the clones under ~/projects…')

    send('worktrees:progress', { phase: 'checking', done: 1, total: 4 })
    expect(status).toHaveTextContent('Checking each worktree · 1 of 4 worktrees')
    expect(status.querySelector('[style]')).toHaveStyle({ width: '25%' })

    await settle(() => {
      quiet.resolve(worktreesSnapshot())
    })

    expect(within(meta()).queryByRole('status')).not.toBeInTheDocument()

    platform.listWorktrees.mockReturnValueOnce(new Promise(() => undefined))
    rerender({ reloadKey: 3 })

    expect(await within(meta()).findByRole('status')).toHaveTextContent('Checking the worktrees…')
  })

  it('holds the actions while a listing it was asked for runs', async () => {
    draw()
    await clone('example-repo')

    const again = deferred<ReturnType<typeof worktreesSnapshot>>()
    platform.listWorktrees.mockReturnValue(again.promise)
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))

    expect(screen.getByRole('button', { name: 'Check again' })).toBeDisabled()
    expect((await cleanupMenu()).getByRole('menuitem', { name: 'Remove 1 clean worktree…' })).toBeDisabled()
    await userEvent.keyboard('{Escape}')

    await settle(() => {
      again.resolve(worktreesSnapshot())
    })

    expect(await screen.findByRole('button', { name: 'Check again' })).toBeEnabled()
  })

  it('shows a listing failure and recovers on Check again', async () => {
    platform.listWorktrees.mockRejectedValue(new Error('scan the clone root: no such directory'))
    draw()

    expect(await screen.findByRole('alert')).toHaveTextContent('scan the clone root: no such directory')
    expect(within(meta()).queryByRole('status')).not.toBeInTheDocument()
    // The merged branches don't need the listing.
    expect(screen.getByRole('button', { name: 'More cleanups' })).toBeInTheDocument()

    platform.listWorktrees.mockResolvedValue(worktreesSnapshot())
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))

    expect(await screen.findByRole('region', { name: 'example-repo' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('lists again when reloadKey changes and reports each listing', async () => {
    const onSnapshot = vi.fn()
    const snapshot = worktreesSnapshot({ finishedCount: 1 })
    platform.listWorktrees.mockResolvedValue(snapshot)
    const { rerender } = draw({ reloadKey: 1, onSnapshot })
    await clone('example-repo')

    await vi.waitFor(() => {
      expect(onSnapshot).toHaveBeenCalledWith(snapshot)
    })

    rerender({ reloadKey: 1, onSnapshot: vi.fn() })
    expect(platform.listWorktrees).toHaveBeenCalledOnce()

    rerender({ reloadKey: 2, onSnapshot })
    await vi.waitFor(() => {
      expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
    })
    await vi.waitFor(() => {
      expect(onSnapshot).toHaveBeenCalledTimes(2)
    })
  })

  it('says when GitHub could not be asked, and when the processes can’t be listed', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        linkError: 'Could not read the pull requests and issues from GitHub: offline.',
        sessionsUnknown: true,
      }),
    )
    draw()
    await clone('example-repo')

    const [link, sessions] = screen.getAllByRole('note')
    expect(link).toHaveTextContent(
      'Could not read the pull requests and issues from GitHub: offline. Worktrees are shown without their pull requests and issues, so none counts as finished.',
    )
    expect(sessions).toHaveTextContent(
      'This system can’t list the running processes, so no worktree shows a session, the automatic cleanup removes none, and no worktree can be force-removed.',
    )
    expect(screen.queryByRole('button', { name: /^Clean up \d+ finished$/ })).not.toBeInTheDocument()
  })
})

describe('WorktreesView cleanups', () => {
  it('offers Clean up N finished, with the other cleanups under its ⌄', async () => {
    listing(
      [finishedWorktree(), linked('pr-8', { item: linkedItem({ number: 8 }), stage: 'finished', canCleanUp: true })],
      {
        finishedCount: 2,
        cleanCount: 1,
      },
    )
    draw()
    await clone('example-repo')

    const primary = screen.getByRole('button', { name: 'Clean up 2 finished' })
    expect(primary).toHaveAttribute(
      'title',
      'Removes the 2 worktrees whose work is done and on GitHub; asks first, with a box for each',
    )

    const menu = await cleanupMenu()
    expect(menu.getAllByRole('menuitem').map((m) => m.textContent)).toEqual([
      'Remove 1 clean worktree…',
      'Clean up merged branches…',
    ])
    await userEvent.keyboard('{Escape}')

    await userEvent.click(primary)
    expect(screen.getByRole('dialog', { name: 'Clean up finished worktrees?' })).toBeInTheDocument()
  })

  it('offers the cleanups under Clean up ⌄ while none is finished, saying why each can’t run', async () => {
    listing([linked('scratch', { dirty: true, dirtyCount: 1, canRemove: false, canForceRemove: true })], {
      cleanCount: 0,
    })
    draw()
    await clone('example-repo')

    expect(screen.getByRole('button', { name: 'More cleanups' })).toHaveTextContent('Clean up')
    const menu = await cleanupMenu()
    const finished = menu.getByRole('menuitem', { name: 'Clean up finished…' })
    expect(finished).toBeDisabled()
    expect(finished).toHaveAccessibleDescription(
      'None is finished: no worktree whose pull request merged or issue closed has all its work on GitHub',
    )
    expect(menu.getByRole('menuitem', { name: 'Remove clean worktrees…' })).toBeDisabled()
    expect(menu.getByRole('menuitem', { name: 'Clean up merged branches…' })).toBeEnabled()
  })
})

describe('WorktreesView chips', () => {
  // states is a listing with a worktree in each state a chip counts: two dirty, one with
  // a commit not on GitHub, one that needs a look, one finished, and one something runs in.
  function states() {
    listing([
      linked('dirty-1', { dirty: true, dirtyCount: 2, canRemove: false }),
      linked('dirty-2', { dirty: true, dirtyCount: 1, canRemove: false }),
      linked('ahead', { unpushed: 1, onGitHub: false, canRemove: false }),
      linked('review', { item: linkedItem({ number: 9 }), stage: 'needs-review', unpushed: 2, onGitHub: false }),
      finishedWorktree(),
      linked('busy', { inUse: [{ pid: 4242, command: 'zsh', agent: '', by: '' }], stage: 'in-use' }),
    ])
  }

  it('counts the worktrees in each state, leaving out a state none is in, and writes its filter', async () => {
    states()
    const onQuery = vi.fn()
    const { rerender } = draw({ onQuery })
    await clone('example-repo')

    const chips = within(screen.getByRole('group', { name: 'Filter by state' }))
    expect(chips.getAllByRole('button').map((b) => b.textContent)).toEqual([
      '1 running',
      '2 uncommitted',
      '2 not on GitHub',
      '1 needs a look',
      '1 ready to clean up',
    ])

    const dirty = chips.getByRole('button', { name: '2 uncommitted' })
    expect(dirty).toHaveAttribute('aria-pressed', 'false')
    expect(dirty).toHaveAttribute('title', 'Show the 2 worktrees that have uncommitted files (is:dirty)')

    await userEvent.click(dirty)
    expect(onQuery).toHaveBeenLastCalledWith('is:dirty')

    rerender({ onQuery, query: 'is:dirty' })

    const on = within(screen.getByRole('group', { name: 'Filter by state' })).getByRole('button', {
      name: '2 uncommitted',
    })
    expect(on).toHaveAttribute('aria-pressed', 'true')
    expect(on).toHaveAttribute('title', 'Clear the filter')

    await userEvent.click(on)
    expect(onQuery).toHaveBeenLastCalledWith('')
  })

  it('shows no chips without a way to write the filter, nor while no worktree is in a state', async () => {
    states()
    const { unmount } = draw()
    await clone('example-repo')

    expect(screen.queryByRole('group', { name: 'Filter by state' })).not.toBeInTheDocument()
    unmount()

    listing([linked('plain', { stage: 'active' })])
    draw({ onQuery: vi.fn() })
    await clone('example-repo')

    expect(screen.queryByRole('group', { name: 'Filter by state' })).not.toBeInTheDocument()
  })
})

describe('WorktreesView clones', () => {
  it('heads each clone with its owner and name, its path, its main checkout, and its lanes counted', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [
              mainWorktree(),
              finishedWorktree(),
              linked('scratch', { dirty: true, dirtyCount: 1, canRemove: false }),
              linked('gone', { prunable: true, canRemove: false }),
              linked('also-gone', { prunable: true, canRemove: false }),
            ],
          }),
          other([linked('pr-3', { item: null })], {
            worktrees: [
              mainWorktree({
                path: '/home/user/projects/other-tool',
                displayPath: '~/projects/other-tool',
                dirty: true,
                dirtyCount: 3,
              }),
              linked('pr-3', { path: '/home/user/projects/other-tool/.claude/worktrees/pr-3' }),
            ],
          }),
          worktreeRepo({
            path: '/home/user/projects/broken-repo',
            displayPath: '~/projects/broken-repo',
            name: 'broken-repo',
            error: 'fatal: not a git repository',
            worktrees: [],
          }),
        ],
      }),
    )
    draw()
    await clone('example-repo')

    const example = heading('example-repo')
    expect(example).toHaveAttribute('aria-expanded', 'true')
    expect(example).toHaveTextContent(
      'acme-org/example-repo ~/projects/example-repo Main checkout: on main · clean 1 at risk 1 ready 2 problems',
    )
    expect(within(example).getByText('acme-org/')).toHaveClass('text-muted')
    expect(within(example).getByTitle('The main checkout is on main, with nothing uncommitted')).toHaveClass(
      'text-muted',
    )

    // A clone none of whose worktrees is for an item has no owner to say; a main checkout
    // with uncommitted files is amber, and a clone whose worktrees are all open counts none.
    const tool = heading('other-tool')
    expect(tool).toHaveTextContent(/^other-tool ~\/projects\/other-tool Main checkout: on main · 3 uncommitted$/)
    expect(within(tool).getByTitle('The main checkout is on main, with 3 uncommitted files')).toHaveClass(
      'text-warning',
    )

    // A clone git couldn't read says why, and has no rows.
    const broken = within(screen.getByRole('region', { name: 'broken-repo' }))
    expect(broken.getByTitle('fatal: not a git repository')).toHaveClass('text-danger')
    expect(broken.queryByRole('list')).not.toBeInTheDocument()
  })

  it('lists each clone’s worktrees in attention order, newest commit first, without its main checkout', async () => {
    const unix = (hours: number) => FIXED_NOW.getTime() / 1000 - hours * 3600
    listing([
      finishedWorktree({ lastCommitUnix: unix(1) }),
      linked('open-old', { stage: 'active', lastCommitUnix: unix(30) }),
      linked('gone', { prunable: true, canRemove: false, lastCommitUnix: unix(1) }),
      linked('dirty', { dirty: true, dirtyCount: 1, canRemove: false, lastCommitUnix: unix(5) }),
      linked('open-new', { stage: 'active', lastCommitUnix: unix(2) }),
      linked('review', { item: linkedItem({ number: 9 }), stage: 'needs-review', unpushed: 1, onGitHub: false }),
      linked('busy', { inUse: [{ pid: 4242, command: 'zsh', agent: '', by: '' }], stage: 'in-use' }),
    ])
    draw()

    const list = within((await clone('example-repo')).getByRole('list', { name: 'Worktrees of example-repo' }))
    expect(list.getAllByRole('button', { name: /^Unfold / }).map((b) => b.getAttribute('aria-label'))).toEqual([
      'Unfold worktree-busy',
      'Unfold worktree-review',
      'Unfold worktree-dirty',
      'Unfold worktree-open-new',
      'Unfold worktree-open-old',
      `Unfold ${pr7}`,
      'Unfold worktree-gone',
    ])
    expect(screen.queryByRole('button', { name: 'Unfold main' })).not.toBeInTheDocument()
  })

  it('folds a clone’s rows away from its heading, keeping it folded across listings', async () => {
    draw()
    await clone('example-repo')

    await userEvent.click(heading('example-repo'))

    expect(heading('example-repo')).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('list', { name: 'Worktrees of example-repo' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))
    await vi.waitFor(() => {
      expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
    })
    expect(heading('example-repo')).toHaveAttribute('aria-expanded', 'false')

    await userEvent.click(heading('example-repo'))
    expect(chevron(pr7)).toBeInTheDocument()
  })

  it('prunes only a clone git holds entries of gone worktrees for', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo(),
          other(
            [
              linked('pr-3', {
                path: '/home/user/projects/other-tool/.claude/worktrees/pr-3',
                displayPath: '~/projects/other-tool/.claude/worktrees/pr-3',
                branch: 'worktree-pr-3',
                prunable: true,
                canRemove: false,
              }),
            ],
            { hasStale: true },
          ),
        ],
      }),
    )
    draw()

    expect((await clone('example-repo')).queryByRole('button', { name: 'Prune' })).not.toBeInTheDocument()

    const prune = (await clone('other-tool')).getByRole('button', { name: 'Prune' })
    expect(prune).toHaveAttribute(
      'title',
      'Clears git’s entries for worktrees whose folders are gone; touches no worktree that exists, and no branch',
    )
    await userEvent.click(prune)

    expect(platform.pruneWorktrees).toHaveBeenCalledWith('/home/user/projects/other-tool')
    expect(await screen.findByRole('status', { name: 'Pruned ~/projects/other-tool' })).toHaveTextContent(
      'The entries of worktrees whose folders are gone were cleared.',
    )
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('shows a prune failure on its clone', async () => {
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ repos: [worktreeRepo({ hasStale: true })] }))
    platform.pruneWorktrees.mockRejectedValue(new Error('prune ~/projects/example-repo: exit status 128'))
    draw()

    await userEvent.click((await clone('example-repo')).getByRole('button', { name: 'Prune' }))

    expect(await (await clone('example-repo')).findByRole('alert')).toHaveTextContent(
      'prune ~/projects/example-repo: exit status 128',
    )
    expect(row(pr7).queryByRole('alert')).not.toBeInTheDocument()
  })
})

describe('WorktreesView drawers', () => {
  it('unfolds several drawers, keeps them unfolded across listings, and reads each one’s details once a listing', async () => {
    listing([worktree(), linked('issue-9'), linked('gone', { prunable: true, canRemove: false })])
    draw()
    await clone('example-repo')

    await unfold(pr7)
    await unfold('worktree-issue-9')

    expect(drawer(pr7).getByText(/Safe to remove/)).toBeInTheDocument()
    expect(drawer('worktree-issue-9').getByText(/Safe to remove/)).toBeInTheDocument()
    await vi.waitFor(() => {
      expect(platform.worktreeDetails).toHaveBeenCalledTimes(2)
    })
    expect(platform.worktreeDetails).toHaveBeenCalledWith(repoPath, linkedPath)
    expect(platform.worktreeDetails).toHaveBeenCalledWith(repoPath, `${repoPath}/.claude/worktrees/issue-9`)

    // Folding and unfolding again in one listing reads nothing more; a gone folder has
    // nothing to read.
    await userEvent.click(screen.getByRole('button', { name: `Fold ${pr7}` }))
    await unfold(pr7)
    await unfold('worktree-gone')
    expect(drawer('worktree-gone').getByText(/^Its folder is gone, so/)).toBeInTheDocument()
    expect(platform.worktreeDetails).toHaveBeenCalledTimes(2)

    // A new listing reads each open drawer's again, once.
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))
    await vi.waitFor(() => {
      expect(platform.worktreeDetails).toHaveBeenCalledTimes(4)
    })
    expect(drawer(pr7).getByText(/Safe to remove/)).toBeInTheDocument()
    expect(drawer('worktree-issue-9').getByText(/Safe to remove/)).toBeInTheDocument()
    expect(drawer('worktree-gone').getByText(/^Its folder is gone, so/)).toBeInTheDocument()
  })

  it('names what works in a worktree by the session the window follows there', async () => {
    const managedPath = `/home/user/projects/.${brand.slug}/worktrees/example-repo/pr-7`
    const managedDisplay = `~/projects/.${brand.slug}/worktrees/example-repo/pr-7`
    listing([
      worktree({
        path: managedPath,
        displayPath: managedDisplay,
        legacy: false,
        inUse: [{ pid: 4242, command: '2.1.319', agent: 'claude', by: '' }],
        stage: 'in-use',
        canRemove: false,
      }),
    ])
    const { rerender } = draw({ agentName: () => 'Claude' })
    await clone('example-repo')

    expect(row(pr7).getByTitle(/^Claude Code 2\.1\.319, pid 4242/)).toHaveTextContent('Claude 2.1.319 · pid 4242')

    rerender({ agentName: () => 'Claude', sessions: sessionsSnapshot([agentSession({ dir: managedDisplay })]) })

    expect(row(pr7).getByTitle(/^Claude Code 2\.1\.319, pid 4242, needs approval/)).toHaveTextContent(
      'Claude 2.1.319 · needs approval · 5m',
    )

    // A session the window no longer follows names nothing.
    rerender({ agentName: () => 'Claude', sessions: sessionsSnapshot([agentSession({ dir: managedDisplay })], false) })
    expect(row(pr7).getByTitle(/^Claude Code 2\.1\.319, pid 4242$/)).toBeInTheDocument()
  })

  it('shows what the inbox read of a worktree’s item, and launches on one it lists', async () => {
    listing([worktree({ item: linkedItem({ state: 'open', closedAt: '' }) })])
    const launchFor = vi.fn((linkedTo: LinkedItem) => <button type="button">Open #{linkedTo.number}</button>)
    const { rerender } = draw({ launchFor })
    await clone('example-repo')

    // The inbox doesn't list it, so there's no launch, and nothing it read.
    expect(row(pr7).queryByRole('button', { name: 'Open #7' })).not.toBeInTheDocument()
    expect(launchFor).not.toHaveBeenCalled()

    rerender({
      launchFor,
      inboxItems: [
        item({ isDraft: true, details: itemDetails({ checks: { ...itemDetails().checks, state: 'FAILURE' } }) }),
      ],
    })

    expect(row(pr7).getByRole('button', { name: 'Open #7' })).toBeInTheDocument()
    expect(launchFor).toHaveBeenCalledWith(expect.objectContaining({ number: 7 }), launchPlace)
    expect(row(pr7).getByTitle('Checks failing, as the inbox read them')).toBeInTheDocument()
    expect(row(pr7).getByText('draft')).toBeInTheDocument()
  })
})

describe('WorktreesView changes', () => {
  it('cleans up a finished worktree from its verb once confirmed, with its branch ticked, and says what it did', async () => {
    listing(
      [
        finishedWorktree({
          locked: true,
          lockReason: 'claude session pr-7 (pid 4242)',
          agentLock: true,
          staleLock: true,
          canUnlock: true,
          canRemove: false,
        }),
      ],
      { finishedCount: 1 },
    )
    platform.cleanUpWorktree.mockResolvedValue(removeResult({ removed: true }))
    draw()
    await clone('example-repo')

    expect(row(pr7).queryByRole('button', { name: 'Unlock' })).not.toBeInTheDocument()
    await userEvent.click(row(pr7).getByRole('button', { name: 'Clean up' }))

    const dialog = screen.getByRole('dialog', { name: 'Clean up this worktree?' })
    expect(dialog).toHaveTextContent(
      `This deletes the folder ${linkedDisplay}, whose work is finished: it has no uncommitted changes, and every ` +
        'commit on it is on GitHub.',
    )
    expect(dialog).toHaveTextContent('Made for pull request #7, merged.')
    const box = within(dialog).getByRole('checkbox', { name: /^Delete branch worktree-pr-7/ })
    expect(box).toBeChecked()
    expect(box).toHaveAccessibleName(/the branch made for this worktree · at fc1c397$/)
    expect(platform.cleanUpWorktree).not.toHaveBeenCalled()

    await confirmWith('Clean up')

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.cleanUpWorktree).toHaveBeenCalledWith(repoPath, linkedPath, ['worktree-pr-7'])
    expect(await screen.findByRole('status', { name: 'Cleaned up example-repo/pr-7' })).toHaveTextContent(
      'Its branch worktree-pr-7 was deleted.',
    )
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('says when a cleaned-up worktree kept its branch, and shows a refusal on its row', async () => {
    listing([finishedWorktree({ ownBranch: false, branch: 'feat/x', branches: [] })])
    platform.cleanUpWorktree.mockResolvedValueOnce(removeResult({ deletedBranches: [] }))
    draw()
    await clone('example-repo')

    await userEvent.click(row('feat/x').getByRole('button', { name: 'Clean up' }))
    const dialog = screen.getByRole('dialog', { name: 'Clean up this worktree?' })
    expect(dialog).toHaveTextContent(
      'Its branch feat/x stays: it is neither the branch made for this worktree nor its pull request’s.',
    )
    expect(within(dialog).queryByRole('checkbox')).not.toBeInTheDocument()

    await confirmWith('Clean up')
    expect(platform.cleanUpWorktree).toHaveBeenCalledWith(repoPath, linkedPath, [])
    expect(await screen.findByRole('status', { name: 'Cleaned up example-repo/pr-7' })).toHaveTextContent(
      'No branch was deleted.',
    )

    platform.cleanUpWorktree.mockRejectedValueOnce(
      new Error('the worktree is not finished: it has uncommitted changes'),
    )
    await vi.waitFor(() => {
      expect(row('feat/x').getByRole('button', { name: 'Clean up' })).toBeEnabled()
    })
    await userEvent.click(row('feat/x').getByRole('button', { name: 'Clean up' }))
    await confirmWith('Clean up')

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'the worktree is not finished: it has uncommitted changes',
    )
    expect(screen.getByRole('alert').closest('li')?.previousElementSibling).toBe(chevron('feat/x').closest('li'))
  })

  it('unlocks a worktree whose session has ended, then lists again, and shows a refusal on its row', async () => {
    const stale = worktree({
      locked: true,
      lockReason: 'claude session pr-7 (pid 4242)',
      agentLock: true,
      staleLock: true,
      canUnlock: true,
      canRemove: false,
    })
    listing([stale], { cleanCount: 0 })
    draw()
    await clone('example-repo')

    platform.listWorktrees.mockResolvedValue(worktreesSnapshot())
    await userEvent.click(row(pr7).getByRole('button', { name: 'Unlock' }))

    expect(platform.unlockWorktree).toHaveBeenCalledWith(repoPath, linkedPath)
    expect(await screen.findByRole('status', { name: 'Unlocked example-repo/pr-7' })).toHaveTextContent(
      'The lock its ended session left was released, so it can be removed.',
    )
    await vi.waitFor(() => {
      expect(row(pr7).queryByRole('button', { name: 'Unlock' })).not.toBeInTheDocument()
    })

    listing([stale], { cleanCount: 0 })
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))
    platform.unlockWorktree.mockRejectedValue(
      new Error('the agent session that locked the worktree may still be running'),
    )
    await userEvent.click(await row(pr7).findByRole('button', { name: 'Unlock' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'the agent session that locked the worktree may still be running',
    )
  })

  it('removes a clean worktree from its ⋯ once confirmed, saying what becomes of its branch, then lists again', async () => {
    draw()
    await clone('example-repo')
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({ repos: [worktreeRepo({ worktrees: [mainWorktree()] })], cleanCount: 0 }),
    )

    await remove(pr7)

    const dialog = screen.getByRole('dialog', { name: 'Remove this worktree?' })
    expect(dialog).toHaveTextContent(`This deletes the folder ${linkedDisplay}, which has no uncommitted changes.`)
    expect(within(dialog).getByRole('checkbox', { name: /^Delete branch worktree-pr-7/ })).toBeChecked()

    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(platform.removeWorktree).not.toHaveBeenCalled()

    await remove(pr7)
    await confirmWith('Remove')

    expect(platform.removeWorktree).toHaveBeenCalledWith(repoPath, linkedPath, ['worktree-pr-7'])
    expect(await screen.findByRole('status', { name: 'Removed example-repo/pr-7' })).toHaveTextContent(
      'Its branch worktree-pr-7 was deleted.',
    )
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
    await vi.waitFor(() => {
      expect(screen.queryByRole('button', { name: `Unfold ${pr7}` })).not.toBeInTheDocument()
    })
    expect((await cleanupMenu()).getByRole('menuitem', { name: 'Remove clean worktrees…' })).toBeDisabled()
  })

  it('removes from the drawer too', async () => {
    draw()
    await clone('example-repo')

    await unfold(pr7)
    await userEvent.click(drawer(pr7).getByRole('button', { name: 'Remove…' }))

    expect(screen.getByRole('dialog', { name: 'Remove this worktree?' })).toBeInTheDocument()
  })

  it("shows git's refusal on the row", async () => {
    platform.removeWorktree.mockRejectedValue(
      new Error(`git refused to remove ${linkedDisplay}: has uncommitted changes`),
    )
    draw()
    await clone('example-repo')

    await remove(pr7)
    await confirmWith('Remove')

    expect(await screen.findByRole('alert')).toHaveTextContent(
      `git refused to remove ${linkedDisplay}: has uncommitted changes`,
    )
    expect(platform.listWorktrees).toHaveBeenCalledOnce()
    expect((await openMenu(pr7)).getByRole('menuitem', { name: 'Remove…' })).toBeEnabled()
  })

  it('says a branch with commits not on GitHub stays, and that the removal kept it', async () => {
    listing([
      worktree({
        onGitHub: false,
        unpushed: 2,
        item: linkedItem({ kind: 'issue', number: 202, state: 'open' }),
        branches: [ownBranch({ deletable: false, reason: "2 commits on it aren't on GitHub" })],
      }),
    ])
    platform.removeWorktree.mockResolvedValue(
      removeResult({ deletedBranches: [], reason: 'kept the branch worktree-pr-7, which has 2 commits not on GitHub' }),
    )
    draw()
    await clone('example-repo')

    await remove(pr7)

    const dialog = screen.getByRole('dialog', { name: 'Remove this worktree?' })
    const box = within(dialog).getByRole('checkbox', { name: /^Delete branch worktree-pr-7/ })
    expect(box).not.toBeChecked()
    expect(box).toBeDisabled()
    expect(dialog).toHaveTextContent("Stays: 2 commits on it aren't on GitHub.")
    expect(dialog).toHaveTextContent('Made for issue #202, open.')

    await confirmWith('Remove')

    expect(platform.removeWorktree).toHaveBeenCalledWith(repoPath, linkedPath, [])
    expect(await screen.findByRole('status', { name: 'Removed example-repo/pr-7' })).toHaveTextContent(
      'No branch was deleted. Kept the branch worktree-pr-7, which has 2 commits not on GitHub.',
    )
  })

  // removals are worktrees to remove, what the confirmation says of their branches, and
  // the branches the removal deletes, as ticked to start with.
  const removals: {
    name: string
    overrides: Partial<Worktree>
    branchRules?: BranchRules
    text: string
    branches: string[]
  }[] = [
    {
      name: 'its own branch, ticked as the rules say',
      overrides: {},
      text: 'Delete branch worktree-pr-7the branch made for this worktree · at fc1c397',
      branches: ['worktree-pr-7'],
    },
    {
      name: 'its own branch, unticked when the rules keep it',
      overrides: {},
      branchRules: rules({ own: false }),
      text: 'Delete branch worktree-pr-7',
      branches: [],
    },
    {
      name: 'the pull request’s branch as well',
      overrides: { branch: 'feature-x', branches: [ownBranch(), prBranch] },
      text: 'Delete branch feature-xthe pull request’s branch, checked out here · at aaaaaaa',
      branches: ['worktree-pr-7', 'feature-x'],
    },
    {
      name: 'the pull request’s branch, unticked when the rules keep it',
      overrides: { branch: 'feature-x', branches: [prBranch] },
      branchRules: rules({ pullRequest: false }),
      text: 'Delete branch feature-x',
      branches: [],
    },
    {
      name: 'a branch it did not make',
      overrides: { branch: 'feature', ownBranch: false, branches: [] },
      text: 'Its branch feature stays: it is neither the branch made for this worktree nor its pull request’s.',
      branches: [],
    },
    {
      name: 'no branch',
      overrides: { branch: '', detached: true, branches: [] },
      text: 'It has no branch.',
      branches: [],
    },
  ]

  it.each(removals)(
    'says what removing a worktree with $name does, and deletes what is ticked',
    async ({ overrides, branchRules, text, branches }) => {
      const wt = worktree(overrides)
      listing([wt])
      draw({ branchRules })
      await clone('example-repo')

      await remove(wt.detached ? 'pr-7' : wt.branch)

      expect(screen.getByRole('dialog', { name: 'Remove this worktree?' })).toHaveTextContent(text)

      await confirmWith('Remove')

      expect(platform.removeWorktree).toHaveBeenCalledWith(repoPath, linkedPath, branches)
    },
  )

  it('reports a removal that deleted no branch, as the user unticked it', async () => {
    platform.removeWorktree.mockResolvedValue(removeResult({ deletedBranches: [] }))
    draw()
    await clone('example-repo')

    await remove(pr7)
    await userEvent.click(screen.getByRole('checkbox', { name: /^Delete branch worktree-pr-7/ }))
    await confirmWith('Remove')

    expect(platform.removeWorktree).toHaveBeenCalledWith(repoPath, linkedPath, [])
    expect(await screen.findByRole('status', { name: 'Removed example-repo/pr-7' })).toHaveTextContent(
      'No branch was deleted.',
    )
  })

  it('names every branch a removal deleted', async () => {
    platform.removeWorktree.mockResolvedValue(removeResult({ deletedBranches: ['worktree-pr-7', 'feature-x'] }))
    draw()
    await clone('example-repo')

    await remove(pr7)
    await confirmWith('Remove')

    expect(await screen.findByRole('status', { name: 'Removed example-repo/pr-7' })).toHaveTextContent(
      'Its branches worktree-pr-7 and feature-x were deleted.',
    )
  })

  it('force removes a worktree with uncommitted files from its ⋯ once confirmed', async () => {
    listing(
      [worktree({ dirty: true, dirtyCount: 3, files: worktreeFiles(3), canRemove: false, canForceRemove: true })],
      { cleanCount: 0 },
    )
    platform.worktreeDetails.mockResolvedValue(worktreeDetails({ files: worktreeFiles(3), fileCount: 3 }))
    draw()
    await clone('example-repo')

    await remove(pr7, 'Force remove…')

    const dialog = screen.getByRole('dialog', { name: 'Force remove this worktree?' })
    expect(within(dialog).getByRole('region', { name: 'Files it discards' })).toHaveTextContent('Discards 3 files')
    expect(within(dialog).getByRole('checkbox', { name: /^Delete branch worktree-pr-7/ })).toBeChecked()

    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.forceRemoveWorktree).not.toHaveBeenCalled()

    await remove(pr7, 'Force remove…')
    // Its details are read once a listing, however often it asks.
    expect(platform.worktreeDetails).toHaveBeenCalledExactlyOnceWith(repoPath, linkedPath)
    await confirmWith('Force remove')

    expect(platform.forceRemoveWorktree).toHaveBeenCalledWith(repoPath, linkedPath, ['worktree-pr-7'])
    expect(platform.removeWorktree).not.toHaveBeenCalled()
    expect(await screen.findByRole('status', { name: 'Force removed example-repo/pr-7' })).toHaveTextContent(
      'Its branch worktree-pr-7 was deleted.',
    )
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })
})

describe('WorktreesView bulk cleanups', () => {
  it('confirms clean up finished, listing each worktree and its branches, and reports each outcome', async () => {
    const pr8Path = `${repoPath}/.claude/worktrees/pr-8`
    const pr8 = finishedWorktree({
      path: pr8Path,
      displayPath: '~/projects/example-repo/.claude/worktrees/pr-8',
      branch: 'feature-x',
      item: linkedItem({ number: 8, headRef: 'feature-x' }),
      branches: [ownBranch({ name: 'worktree-pr-8' }), prBranch],
    })
    listing([finishedWorktree(), pr8], { finishedCount: 2 })
    platform.cleanUpFinishedWorktrees.mockResolvedValue([
      removeResult(),
      removeResult({
        path: pr8Path,
        displayPath: '~/projects/example-repo/.claude/worktrees/pr-8',
        removed: false,
        deletedBranches: [],
        item: 'acme-org/example-repo#8',
        reason: 'the worktree changed since it was checked; refresh and try again',
      }),
    ])
    draw()
    await clone('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Clean up 2 finished' }))

    const dialog = screen.getByRole('dialog', { name: 'Clean up finished worktrees?' })
    expect(dialog).toHaveTextContent('The pull request or issue of each of these is finished')
    const listed = within(within(dialog).getByRole('list', { name: 'Worktrees' }))
    expect(listed.getByRole('checkbox', { name: `${linkedDisplay}Made for pull request #7, merged` })).toBeChecked()
    expect(listed.getByRole('checkbox', { name: /pr-8Made for pull request #8, merged$/ })).toBeChecked()
    const prBox = within(
      listed.getByRole('list', { name: 'Branches of ~/projects/example-repo/.claude/worktrees/pr-8' }),
    ).getByRole('checkbox', { name: /^Delete branch feature-x/ })
    expect(prBox).toHaveAccessibleName(/the pull request’s branch, checked out here · at aaaaaaa$/)
    expect(prBox).toBeChecked()

    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(platform.cleanUpFinishedWorktrees).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Clean up 2 finished' }))
    await userEvent.click(screen.getByRole('checkbox', { name: /^Delete branch feature-x/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Clean up 2 worktrees' }))

    expect(platform.cleanUpFinishedWorktrees).toHaveBeenCalledExactlyOnceWith([
      { repoPath, path: linkedPath, branches: ['worktree-pr-7'] },
      { repoPath, path: pr8Path, branches: ['worktree-pr-8'] },
    ])
    const results = await screen.findByRole('status', { name: 'Cleaned up 1 of 2 finished worktrees' })
    expect(results).toHaveTextContent('1 branch deleted · 1 left in place, as noted below.')
    // What a change did stays in the head, at most a third of the window tall, scrolling
    // past it, so the clones keep the rest however long its notes run.
    const shown = results.closest('[data-results]')
    expect(shown).toHaveClass('max-h-[33vh]', 'overflow-y-auto')
    expect(shown?.parentElement).toContainElement(screen.getByRole('button', { name: 'Check again' }))
    expect(results).toHaveTextContent(
      '~/projects/example-repo/.claude/worktrees/pr-8: the worktree changed since it was checked; refresh and try again',
    )
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('leaves out a worktree unticked, holding its branches, and confirms nothing with none ticked', async () => {
    listing([finishedWorktree()], { finishedCount: 1 })
    draw({ branchRules: rules({ own: false }) })
    await clone('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Clean up 1 finished' }))

    const dialog = within(screen.getByRole('dialog', { name: 'Clean up finished worktrees?' }))
    const branch = dialog.getByRole('checkbox', { name: /^Delete branch worktree-pr-7/ })
    expect(branch).not.toBeChecked()
    expect(branch).toBeEnabled()

    await userEvent.click(dialog.getByRole('checkbox', { name: /^~\/projects/ }))

    expect(branch).toBeDisabled()
    expect(dialog.getByRole('button', { name: 'Clean up 0 worktrees' })).toBeDisabled()

    await userEvent.click(dialog.getByRole('checkbox', { name: /^~\/projects/ }))
    await userEvent.click(branch)
    await confirmWith('Clean up 1 worktree')

    expect(platform.cleanUpFinishedWorktrees).toHaveBeenCalledExactlyOnceWith([
      { repoPath, path: linkedPath, branches: ['worktree-pr-7'] },
    ])
  })

  it('reports a clean up finished that had nothing to do or failed', async () => {
    listing([finishedWorktree()], { finishedCount: 1 })
    draw()
    await clone('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Clean up 1 finished' }))
    await userEvent.click(screen.getByRole('button', { name: 'Clean up 1 worktree' }))
    expect(
      await screen.findByRole('status', { name: 'There were no finished worktrees to remove' }),
    ).toBeInTheDocument()

    platform.cleanUpFinishedWorktrees.mockRejectedValue(
      new Error('GitHub could not be asked which worktrees are finished'),
    )
    await vi.waitFor(() => {
      expect(screen.getByRole('button', { name: 'Clean up 1 finished' })).toBeEnabled()
    })
    await userEvent.click(screen.getByRole('button', { name: 'Clean up 1 finished' }))
    await userEvent.click(screen.getByRole('button', { name: 'Clean up 1 worktree' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('GitHub could not be asked which worktrees are finished')
  })

  it('confirms remove all clean from the ⌄, listing each worktree, and reports each outcome', async () => {
    const issue9 = linked('issue-9')
    listing([worktree(), issue9], { cleanCount: 2 })
    platform.removeAllCleanWorktrees.mockResolvedValue([
      removeResult({ deletedBranches: [], item: '' }),
      removeResult({
        path: issue9.path,
        displayPath: issue9.displayPath,
        removed: false,
        deletedBranches: [],
        item: '',
        reason: 'has uncommitted changes',
      }),
    ])
    draw()
    await clone('example-repo')

    await choose(await cleanupMenu(), 'Remove 2 clean worktrees…')

    const dialog = screen.getByRole('dialog', { name: 'Remove clean worktrees?' })
    expect(dialog).toHaveTextContent('Each of these has no uncommitted changes and is unlocked.')
    expect(within(dialog).getAllByRole('checkbox', { checked: true })).toHaveLength(4)

    await userEvent.keyboard('{Escape}')
    expect(platform.removeAllCleanWorktrees).not.toHaveBeenCalled()

    await choose(await cleanupMenu(), 'Remove 2 clean worktrees…')
    await userEvent.click(screen.getByRole('button', { name: 'Remove 2 worktrees' }))

    expect(platform.removeAllCleanWorktrees).toHaveBeenCalledExactlyOnceWith([
      { repoPath, path: linkedPath, branches: ['worktree-pr-7'] },
      { repoPath, path: issue9.path, branches: ['worktree-issue-9'] },
    ])
    const results = await screen.findByRole('status', { name: 'Removed 1 of 2 clean worktrees' })
    expect(results).toHaveTextContent('No branch was deleted · 1 left in place, as noted below.')
    expect(results).toHaveTextContent('~/projects/example-repo/.claude/worktrees/issue-9: has uncommitted changes')
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('reports a remove all clean that had nothing to do, failed, or did it all', async () => {
    draw()
    await clone('example-repo')

    await choose(await cleanupMenu(), 'Remove 1 clean worktree…')
    await userEvent.click(screen.getByRole('button', { name: 'Remove 1 worktree' }))

    expect(await screen.findByRole('status', { name: 'There were no clean worktrees to remove' })).toBeInTheDocument()

    platform.removeAllCleanWorktrees.mockRejectedValueOnce(new Error('scan the clone root: permission denied'))
    await vi.waitFor(() => {
      expect(screen.getByRole('button', { name: 'Check again' })).toBeEnabled()
    })
    await choose(await cleanupMenu(), 'Remove 1 clean worktree…')
    await userEvent.click(screen.getByRole('button', { name: 'Remove 1 worktree' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('scan the clone root: permission denied')

    platform.removeAllCleanWorktrees.mockResolvedValueOnce([removeResult({ deletedBranches: [] })])
    await choose(await cleanupMenu(), 'Remove 1 clean worktree…')
    await userEvent.click(screen.getByRole('button', { name: 'Remove 1 worktree' }))

    const results = await screen.findByRole('status', { name: 'Removed 1 clean worktree' })
    expect(within(results).queryByRole('listitem')).not.toBeInTheDocument()
  })
})

describe('WorktreesView history', () => {
  // history waits for the recently cleaned-up list and returns the queries scoped to it.
  async function history() {
    return within(await screen.findByRole('region', { name: 'Recently cleaned up' }))
  }

  it('lists what was cleaned up under the clones, whatever the filter', async () => {
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])
    draw({ query: 'nothing-here' })

    const list = await history()
    expect(list.getByRole('heading')).toHaveTextContent('Recently cleaned up · 1everything in them was on GitHub')
    expect(list.getAllByRole('listitem')).toHaveLength(1)
    expect(list.getByRole('listitem')).toHaveTextContent(/^example-repo \/ pr-7/)
  })

  it('is left out when nothing was cleaned up', async () => {
    draw()
    await clone('example-repo')

    expect(platform.recentCleanups).toHaveBeenCalledOnce()
    expect(screen.queryByRole('region', { name: 'Recently cleaned up' })).not.toBeInTheDocument()
  })

  it('clears the list, and shows a clear that failed', async () => {
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])
    platform.clearCleanups.mockRejectedValueOnce(new Error('database is locked'))
    draw()

    await userEvent.click((await history()).getByRole('button', { name: 'Clear' }))
    expect(await (await history()).findByRole('alert')).toHaveTextContent('database is locked')

    await userEvent.click((await history()).getByRole('button', { name: 'Clear' }))

    expect(platform.clearCleanups).toHaveBeenCalledTimes(2)
    await vi.waitFor(() => {
      expect(screen.queryByRole('region', { name: 'Recently cleaned up' })).not.toBeInTheDocument()
    })
  })

  it('lists again after a change, so a cleanup shows at once', async () => {
    listing([finishedWorktree()])
    platform.cleanUpWorktree.mockResolvedValue(removeResult())
    draw()
    await clone('example-repo')

    platform.recentCleanups.mockResolvedValue([worktreeCleanup({ automatic: false })])
    await userEvent.click(row(pr7).getByRole('button', { name: 'Clean up' }))
    await confirmWith('Clean up')

    expect((await history()).getByText('by you')).toBeInTheDocument()
  })

  it('scrolls to the list once when asked to', async () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView')
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])
    const { rerender } = draw({ focusHistory: true })

    await history()
    await vi.waitFor(() => {
      expect(scroll).toHaveBeenCalledExactlyOnceWith({ block: 'start' })
    })

    rerender({ focusHistory: true, reloadKey: 1 })
    await vi.waitFor(() => {
      expect(platform.recentCleanups).toHaveBeenCalledTimes(2)
    })
    expect(scroll).toHaveBeenCalledExactlyOnceWith({ block: 'start' })
  })

  it('scrolls to the list again when asked again, as a page kept between visits is', async () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView')
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])
    const { rerender } = draw({ focusHistory: true })

    await history()
    await vi.waitFor(() => {
      expect(scroll).toHaveBeenCalledOnce()
    })

    rerender()
    rerender({ focusHistory: true })

    expect(scroll).toHaveBeenCalledTimes(2)
  })

  it('does not scroll when not asked to', async () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView')
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])
    draw()

    await history()
    expect(scroll).not.toHaveBeenCalled()
  })

  it('logs a list that could not be read and still lists the worktrees', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.recentCleanups.mockRejectedValue(new Error('database is locked'))
    draw()

    await clone('example-repo')
    await vi.waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not read the recently cleaned-up worktrees', expect.any(Error))
    })
    expect(screen.queryByRole('region', { name: 'Recently cleaned up' })).not.toBeInTheDocument()
    error.mockRestore()
  })
})

describe('WorktreesView merged branches', () => {
  const otherPath = '/home/user/projects/other-repo'

  // merged is a merged branch named name, in example-repo unless overrides say otherwise.
  function merged(name: string, number: number, overrides: Partial<MergedBranch> = {}): MergedBranch {
    const base = mergedBranch()

    return mergedBranch({
      branch: { ...base.branch, name, head: String(number).repeat(40).slice(0, 40) },
      pullRequest: { ...base.pullRequest, number, title: `Change ${String(number)}` },
      ...overrides,
    })
  }

  // busy is a merged branch the clone has checked out, so it can't go.
  const busy = merged('busy', 187, {
    branch: { ...mergedBranch().branch, name: 'busy', deletable: false, reason: 'it is the clone’s current branch' },
  })

  // result is the outcome of deleting one merged branch.
  function result(name: string, overrides: Partial<BranchResult> = {}): BranchResult {
    return {
      repoPath,
      displayRepo: '~/projects/example-repo',
      name,
      head: 'fc1c3975011e66491785ace891c893e4fc716587',
      deleted: true,
      reason: '',
      ...overrides,
    }
  }

  // askMerged opens the merged branches' confirmation from the head's ⌄.
  async function askMerged() {
    await choose(await cleanupMenu(), 'Clean up merged branches…')

    return within(screen.getByRole('dialog', { name: 'Delete merged branches?' }))
  }

  async function openMerged(branchRules?: BranchRules) {
    draw({ branchRules })
    await clone('example-repo')

    return askMerged()
  }

  // cloneRow returns the row of one clone in the dialog: its box, its toggle, and the
  // queries scoped to it.
  function cloneRow(dialog: BoundFunctions<typeof queries>, displayRepo: string) {
    const toggle = dialog.getByRole('button', { name: displayRepo })
    const li = toggle.closest('li')

    if (li === null) {
      throw new Error(`no row shows ${displayRepo}`)
    }

    return { toggle, box: dialog.getByRole('checkbox', { name: `Every branch in ${displayRepo}` }), row: within(li) }
  }

  it('checks the branches against GitHub, groups them by clone, closed, and deletes those ticked', async () => {
    const answer = deferred<MergedBranch[]>()
    platform.mergedBranches.mockReturnValue(answer.promise)
    platform.deleteBranches.mockResolvedValue([
      result('feature-x'),
      result('feature-z', {
        repoPath: otherPath,
        displayRepo: '~/projects/other-repo',
        deleted: false,
        reason: 'the branch moved since it was checked; refresh and try again',
      }),
    ])

    const dialog = await openMerged()
    const checking = dialog.getByRole('status')
    expect(checking).toHaveTextContent('Checking your clones’ branches against GitHub…')
    expect(checking).toHaveTextContent('then GitHub is asked which of their pull requests merged')
    expect(dialog.getByRole('button', { name: 'Delete branches' })).toBeDisabled()

    await settle(() => {
      answer.resolve([
        busy,
        mergedBranch(),
        merged('feature-y', 185),
        merged('feature-z', 186, { repoPath: otherPath, displayRepo: '~/projects/other-repo' }),
      ])
    })

    expect(await dialog.findByText('4 branches in 2 clones · 3 ticked · 1 can’t be deleted')).toBeInTheDocument()

    const example = cloneRow(dialog, '~/projects/example-repo')
    expect(example.toggle).toHaveAttribute('aria-expanded', 'false')
    expect(example.row.getByText('3 branches · 2 ticked')).toBeInTheDocument()
    expect(example.row.getByRole('img', { name: '1 branch can’t be deleted' })).toBeInTheDocument()
    expect(example.box).toBeChecked()

    await userEvent.click(example.toggle)

    const branches = within(dialog.getByRole('list', { name: 'Branches in ~/projects/example-repo' }))
    expect(branches.getByRole('checkbox', { name: /^feature-x/ })).toHaveAccessibleName(
      /merged 3d ago · at fc1c397#184 Add locktimes$/,
    )
    expect(branches.getByRole('checkbox', { name: /^busy/ })).toBeDisabled()
    expect(branches.getByText('Stays: it is the clone’s current branch.')).toBeInTheDocument()

    await userEvent.click(branches.getByRole('checkbox', { name: /^feature-y/ }))
    expect(example.box).toBePartiallyChecked()

    await userEvent.click(dialog.getByRole('button', { name: 'Delete 2 branches' }))

    expect(platform.deleteBranches).toHaveBeenCalledExactlyOnceWith([
      { repoPath, name: 'feature-x', head: 'fc1c3975011e66491785ace891c893e4fc716587' },
      { repoPath: otherPath, name: 'feature-z', head: '1861861861861861861861861861861861861861' },
    ])

    const results = await screen.findByRole('status', { name: 'Deleted 1 of 2 branches' })
    expect(results).toHaveTextContent('Across 1 clone · 1 kept, as noted below.')
    expect(within(results).getByRole('list', { name: 'Branches kept' })).toHaveTextContent(
      '~/projects/other-repo: feature-z kept: the branch moved since it was checked; refresh and try again',
    )
    expect(within(results).getByRole('list', { name: 'Branches deleted' })).toHaveTextContent(
      '~/projects/example-repo: feature-x at fc1c397',
    )
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('ticks and unticks a whole clone, and every branch at once', async () => {
    platform.mergedBranches.mockResolvedValue([
      mergedBranch(),
      merged('feature-y', 185),
      merged('feature-z', 186, { repoPath: otherPath, displayRepo: '~/projects/other-repo' }),
    ])
    const dialog = await openMerged(rules({ merged: false }))

    const example = cloneRow(dialog, '~/projects/example-repo')
    expect(await dialog.findByText('3 branches in 2 clones · 0 ticked')).toBeInTheDocument()
    expect(example.box).not.toBeChecked()

    await userEvent.click(example.box)
    expect(dialog.getByRole('button', { name: 'Delete 2 branches' })).toBeEnabled()

    await userEvent.click(dialog.getByRole('button', { name: 'Select all' }))
    expect(dialog.getByRole('button', { name: 'Delete 3 branches' })).toBeEnabled()

    await userEvent.click(dialog.getByRole('button', { name: 'Select none' }))
    expect(dialog.getByRole('button', { name: 'Delete 0 branches' })).toBeDisabled()
  })

  it('deletes a branch with commits not on GitHub only once asked for, ticked, and confirmed', async () => {
    const stale = merged('stale-work', 190, {
      branch: {
        ...mergedBranch().branch,
        name: 'stale-work',
        head: '1901901901901901901901901901901901901901',
        deletable: false,
        reason: 'it has 3 commits that aren’t in pull request #190 or on GitHub',
      },
      unpushed: 3,
    })
    platform.mergedBranches.mockResolvedValue([busy, mergedBranch(), stale])
    platform.deleteBranches.mockResolvedValue([result('feature-x'), result('stale-work')])
    const dialog = await openMerged()

    expect(await dialog.findByText('3 branches in 1 clone · 1 ticked · 2 can’t be deleted')).toBeInTheDocument()
    await userEvent.click(cloneRow(dialog, '~/projects/example-repo').toggle)

    await userEvent.click(dialog.getByRole('checkbox', { name: /Also offer the 1 branch with commits not on GitHub/ }))
    await userEvent.click(dialog.getByRole('checkbox', { name: /^stale-work/ }))
    await userEvent.click(dialog.getByRole('button', { name: 'Delete 2 branches' }))

    const confirm = within(await screen.findByRole('dialog', { name: 'Delete 1 branch with commits not on GitHub?' }))
    expect(platform.deleteBranches, 'nothing yet').not.toHaveBeenCalled()
    await userEvent.click(confirm.getByRole('button', { name: 'Delete 2 branches' }))

    expect(platform.deleteBranches).toHaveBeenCalledExactlyOnceWith([
      { repoPath, name: 'feature-x', head: 'fc1c3975011e66491785ace891c893e4fc716587' },
      { repoPath, name: 'stale-work', head: '1901901901901901901901901901901901901901' },
    ])
  })

  it('says when no branch belongs to a merged pull request', async () => {
    const dialog = await openMerged()

    expect(
      await dialog.findByText(/No local branch in your clones belongs to a merged pull request/),
    ).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.deleteBranches).not.toHaveBeenCalled()
  })

  it('shows why the branches could not be checked, and why they could not be deleted', async () => {
    platform.mergedBranches.mockRejectedValueOnce(new Error('GitHub is not available: offline'))
    const dialog = await openMerged()

    expect(await dialog.findByRole('alert')).toHaveTextContent(
      'Couldn’t check the branches: GitHub is not available: offline',
    )
    await userEvent.keyboard('{Escape}')

    platform.mergedBranches.mockResolvedValue([mergedBranch()])
    platform.deleteBranches.mockRejectedValue(new Error('scan the clone root: permission denied'))
    await askMerged()
    await userEvent.click(await screen.findByRole('button', { name: 'Delete 1 branch' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('scan the clone root: permission denied')
  })

  it('reports deleting every branch ticked, and copies the commands that bring them back', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.mergedBranches.mockResolvedValue([mergedBranch(), merged("it's-done", 185)])
    platform.deleteBranches.mockResolvedValue([
      result('feature-x'),
      result("it's-done", { head: '1851851851851851851851851851851851851851' }),
    ])
    await openMerged()

    await userEvent.click(await screen.findByRole('button', { name: 'Delete 2 branches' }))

    const results = await screen.findByRole('status', { name: 'Deleted 2 branches' })
    expect(results).toHaveTextContent('Across 1 clone.')
    expect(within(results).queryByRole('list', { name: 'Branches kept' })).not.toBeInTheDocument()
    expect(results).toHaveTextContent('The 2 branches deleted, with the commit each pointed at')

    platform.copyText.mockRejectedValueOnce(new Error('clipboard refused'))
    await userEvent.click(within(results).getByRole('button', { name: 'Copy restore commands' }))
    await vi.waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not copy the restore commands', expect.any(Error))
    })

    await userEvent.click(within(results).getByRole('button', { name: 'Copy restore commands' }))

    expect(platform.copyText).toHaveBeenLastCalledWith(
      `git -C ${repoPath} branch feature-x fc1c3975011e66491785ace891c893e4fc716587\n` +
        `git -C ${repoPath} branch 'it'\\''s-done' 1851851851851851851851851851851851851851`,
    )
    expect(await within(results).findByRole('button', { name: 'Copied' })).toBeInTheDocument()
    error.mockRestore()
  })
})

describe('WorktreesView summaries', () => {
  it('reports what each change did, or why it failed, and puts the result away when dismissed', async () => {
    const onSummary = vi.fn()
    draw({ onSummary })
    await clone('example-repo')

    await remove(pr7)
    await confirmWith('Remove')

    const card = await screen.findByRole('status', { name: 'Removed example-repo/pr-7' })
    expect(onSummary).toHaveBeenLastCalledWith({
      title: 'Removed example-repo/pr-7',
      detail: 'Its branch worktree-pr-7 was deleted.',
      tone: 'success',
    })

    await userEvent.click(within(card).getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByRole('status', { name: 'Removed example-repo/pr-7' })).not.toBeInTheDocument()

    platform.removeWorktree.mockRejectedValueOnce(new Error('git refused: has uncommitted changes'))
    await remove(pr7)
    await confirmWith('Remove')
    await vi.waitFor(() => {
      expect(onSummary).toHaveBeenLastCalledWith({
        title: 'Couldn’t remove example-repo/pr-7',
        detail: 'git refused: has uncommitted changes',
        tone: 'error',
      })
    })

    platform.removeAllCleanWorktrees.mockRejectedValueOnce(new Error('scan the clone root: permission denied'))
    await choose(await cleanupMenu(), 'Remove 1 clean worktree…')
    await confirmWith('Remove 1 worktree')
    await vi.waitFor(() => {
      expect(onSummary).toHaveBeenLastCalledWith({
        title: 'Couldn’t remove the clean worktrees',
        detail: 'scan the clone root: permission denied',
        tone: 'error',
      })
    })

    platform.mergedBranches.mockResolvedValueOnce([mergedBranch()])
    platform.deleteBranches.mockRejectedValueOnce(new Error('offline'))
    await choose(await cleanupMenu(), 'Clean up merged branches…')
    await userEvent.click(await screen.findByRole('button', { name: 'Delete 1 branch' }))
    await vi.waitFor(() => {
      expect(onSummary).toHaveBeenLastCalledWith({
        title: 'Couldn’t delete the branches',
        detail: 'offline',
        tone: 'error',
      })
    })
  })

  it('names a clean-up that failed, and reports a bulk action and a deletion of branches', async () => {
    const onSummary = vi.fn()
    listing([finishedWorktree()], { finishedCount: 1 })
    platform.cleanUpWorktree.mockRejectedValueOnce(new Error('the worktree is not finished'))
    platform.cleanUpFinishedWorktrees.mockResolvedValueOnce([removeResult()])
    platform.mergedBranches.mockResolvedValueOnce([mergedBranch()])
    platform.deleteBranches.mockResolvedValueOnce([
      {
        repoPath,
        displayRepo: '~/projects/example-repo',
        name: 'feature-x',
        head: 'fc1c3975011e66491785ace891c893e4fc716587',
        deleted: true,
        reason: '',
      },
    ])
    draw({ onSummary })
    await clone('example-repo')

    await userEvent.click(row(pr7).getByRole('button', { name: 'Clean up' }))
    await confirmWith('Clean up')
    await vi.waitFor(() => {
      expect(onSummary).toHaveBeenLastCalledWith(
        expect.objectContaining({ title: 'Couldn’t clean up example-repo/pr-7', tone: 'error' }),
      )
    })

    await userEvent.click(screen.getByRole('button', { name: 'Clean up 1 finished' }))
    await confirmWith('Clean up 1 worktree')
    await vi.waitFor(() => {
      expect(onSummary).toHaveBeenLastCalledWith({
        title: 'Cleaned up 1 finished worktree',
        detail: '1 branch deleted.',
        tone: 'success',
      })
    })

    await choose(await cleanupMenu(), 'Clean up merged branches…')
    await userEvent.click(await screen.findByRole('button', { name: 'Delete 1 branch' }))
    await vi.waitFor(() => {
      expect(onSummary).toHaveBeenLastCalledWith({
        title: 'Deleted 1 branch',
        detail: 'Across 1 clone.',
        tone: 'success',
      })
    })
  })
})

describe('WorktreesView filter', () => {
  // filtered is example-repo with its main worktree, pr-7, and a dirty worktree, and
  // other-tool with its main worktree and pr-3; the history holds one cleanup.
  function filtered() {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [
              mainWorktree(),
              worktree(),
              linked('scratch', {
                branch: 'scratch',
                dirty: true,
                dirtyCount: 1,
                canRemove: false,
                canForceRemove: true,
              }),
            ],
          }),
          other([
            linked('pr-3', {
              path: '/home/user/projects/other-tool/.claude/worktrees/pr-3',
              displayPath: '~/projects/other-tool/.claude/worktrees/pr-3',
            }),
          ]),
        ],
      }),
    )
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])
  }

  it('shows only the clones and worktrees that match, counting what each clone shows, open', async () => {
    filtered()
    const { rerender } = draw({ query: 'is:dirty' })

    const example = await clone('example-repo')
    expect(heading('example-repo')).toHaveTextContent(/1 of 2$/)
    expect(heading('example-repo')).toBeDisabled()
    expect(example.getByRole('button', { name: 'Unfold scratch' })).toBeInTheDocument()
    expect(example.queryByRole('button', { name: `Unfold ${pr7}` })).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'other-tool' })).not.toBeInTheDocument()
    expect(screen.queryByText(/^No worktree matches/)).not.toBeInTheDocument()

    // The bulk actions and the cleanup history still cover everything.
    expect((await cleanupMenu()).getByRole('menuitem', { name: 'Remove 1 clean worktree…' })).toBeEnabled()
    await userEvent.keyboard('{Escape}')
    expect(await screen.findByRole('region', { name: 'Recently cleaned up' })).toBeInTheDocument()

    // A query that names a clone shows all of its linked worktrees.
    rerender({ query: 'other-tool' })
    expect(heading('other-tool')).toHaveTextContent(/1 of 1$/)
    expect(screen.queryByRole('region', { name: 'example-repo' })).not.toBeInTheDocument()

    // Clearing the query shows everything again, the clones' lanes counted.
    rerender({ query: '' })
    expect(heading('example-repo')).toHaveTextContent(/1 at risk$/)
    expect(heading('example-repo')).toBeEnabled()
    expect(screen.getByRole('region', { name: 'other-tool' })).toBeInTheDocument()
  })

  it('says at the top when no worktree matches, keeping a worktree that shows an error, with a way back', async () => {
    platform.pruneWorktrees.mockRejectedValue(new Error('prune: exit status 128'))
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            hasStale: true,
            worktrees: [mainWorktree(), worktree({ canRemove: false, error: 'could not read the status: exit 128' })],
          }),
        ],
      }),
    )
    const onQuery = vi.fn()
    const { rerender } = draw({ onQuery })

    // A prune fails, so its clone shows an error too.
    await userEvent.click((await clone('example-repo')).getByRole('button', { name: 'Prune' }))
    expect(await (await clone('example-repo')).findByRole('alert')).toHaveTextContent('prune: exit status 128')

    rerender({ onQuery, query: '  nothing-here ' })

    const message = screen.getByText(/^No worktree matches “nothing-here”\./)
    expect(heading('example-repo')).toHaveTextContent(/1 of 1$/)
    expect(chevron(pr7)).toBeInTheDocument()
    // The message comes before the clones.
    expect(message.compareDocumentPosition(screen.getByRole('region', { name: 'example-repo' }))).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    )

    await userEvent.click(within(message).getByRole('button', { name: 'Clear the filter' }))
    expect(onQuery).toHaveBeenLastCalledWith('')

    // Without a way to write the filter, there's no way back to offer.
    rerender({ query: 'nothing-here' })
    expect(screen.getByText('No worktree matches “nothing-here”.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Clear the filter' })).not.toBeInTheDocument()
  })

  it('says nothing of a filter while no clone has worktrees', async () => {
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ repos: [], cleanCount: 0 }))
    draw({ query: 'anything' })

    expect(await screen.findByText('No clone under ~/projects has linked worktrees.')).toBeInTheDocument()
    expect(screen.queryByText(/^No worktree matches/)).not.toBeInTheDocument()
  })
})

describe('WorktreesView progress', () => {
  it('shows a removal under way on its row and in the head, then what it did until the list lands', async () => {
    draw()
    await clone('example-repo')

    const removal = deferred<RemoveResult>()
    platform.removeWorktree.mockReturnValue(removal.promise)
    await remove(pr7)
    await confirmWith('Remove')

    expect(within(meta()).getByRole('status')).toHaveTextContent('Removing example-repo/pr-7…')
    expect(chevron(pr7).closest('li')).toHaveAttribute('aria-busy', 'true')
    expect(row(pr7).getByText('Removing…')).toBeInTheDocument()
    expect(row(pr7).queryByRole('button', { name: `More for ${pr7}` })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Check again' })).toHaveAttribute('title', 'A change is under way')

    const relisting = deferred<ReturnType<typeof worktreesSnapshot>>()
    platform.listWorktrees.mockReturnValue(relisting.promise)
    await settle(() => {
      removal.resolve(removeResult())
    })

    expect(await row(pr7).findByText('Removed')).toBeInTheDocument()
    expect(chevron(pr7).closest('li')).not.toHaveAttribute('aria-busy')
    expect(within(meta()).getByRole('status')).toHaveTextContent('Checking the worktrees…')
    expect(screen.getByRole('button', { name: 'Check again' })).toHaveAttribute('title', 'A listing is under way')

    await settle(() => {
      relisting.resolve(worktreesSnapshot({ repos: [worktreeRepo({ worktrees: [mainWorktree()] })], cleanCount: 0 }))
    })

    await vi.waitFor(() => {
      expect(screen.queryByRole('button', { name: `Unfold ${pr7}` })).not.toBeInTheDocument()
    })
    expect(screen.queryByText('Removing example-repo/pr-7…')).not.toBeInTheDocument()
  })

  it('shows a bulk removal under way on each worktree it takes, then which ones stayed and why', async () => {
    const issue9 = linked('issue-9')
    listing([worktree(), issue9], { cleanCount: 2 })
    draw()
    await clone('example-repo')

    const removal = deferred<RemoveResult[]>()
    platform.removeAllCleanWorktrees.mockReturnValue(removal.promise)
    await choose(await cleanupMenu(), 'Remove 2 clean worktrees…')
    await userEvent.click(screen.getByRole('button', { name: 'Remove 2 worktrees' }))

    expect(within(meta()).getByRole('status')).toHaveTextContent('Removing 2 clean worktrees…')
    expect(row(pr7).getByText('Removing…')).toBeInTheDocument()
    expect(row('worktree-issue-9').getByText('Removing…')).toBeInTheDocument()

    platform.listWorktrees.mockReturnValue(deferred<ReturnType<typeof worktreesSnapshot>>().promise)
    await settle(() => {
      removal.resolve([
        removeResult(),
        removeResult({
          path: issue9.path,
          displayPath: issue9.displayPath,
          removed: false,
          reason: 'has uncommitted changes',
        }),
      ])
    })

    expect(await row(pr7).findByText('Removed')).toBeInTheDocument()
    // The one left keeps its color, and says why on a line under it.
    const left = chevron('worktree-issue-9').closest('li')
    expect(row('worktree-issue-9').getByText('Left in place')).toBeInTheDocument()
    expect(left).not.toHaveClass('opacity-55')
    expect(left?.nextElementSibling).toHaveTextContent('Left in place: has uncommitted changes.')
    expect(within(left?.nextElementSibling as HTMLElement).getByRole('alert')).toHaveClass('text-warning')
  })

  it('shows an unlock under way on its row', async () => {
    const stale = worktree({ locked: true, agentLock: true, staleLock: true, canUnlock: true, canRemove: false })
    listing([stale], { cleanCount: 0 })
    draw()
    await clone('example-repo')

    const unlock = deferred<undefined>()
    platform.unlockWorktree.mockReturnValue(unlock.promise)
    await userEvent.click(row(pr7).getByRole('button', { name: 'Unlock' }))

    expect(within(meta()).getByRole('status')).toHaveTextContent('Unlocking example-repo/pr-7…')
    expect(row(pr7).getByText('Unlocking…')).toBeInTheDocument()

    await settle(() => {
      unlock.resolve(undefined)
    })

    expect(await screen.findByRole('status', { name: 'Unlocked example-repo/pr-7' })).toBeInTheDocument()
  })

  it('says a prune is under way on its clone', async () => {
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ repos: [worktreeRepo({ hasStale: true })] }))
    draw()

    const prune = deferred<undefined>()
    platform.pruneWorktrees.mockReturnValue(prune.promise)
    await userEvent.click((await clone('example-repo')).getByRole('button', { name: 'Prune' }))

    expect(within(meta()).getByRole('status')).toHaveTextContent('Pruning ~/projects/example-repo…')
    expect((await clone('example-repo')).getByText('Pruning…')).toBeInTheDocument()
    expect((await clone('example-repo')).queryByRole('button', { name: 'Prune' })).not.toBeInTheDocument()

    await settle(() => {
      prune.resolve(undefined)
    })

    expect(await (await clone('example-repo')).findByRole('button', { name: 'Prune' })).toBeEnabled()
  })

  it('says the merged branches are being deleted, holding the cleanups', async () => {
    platform.mergedBranches.mockResolvedValue([mergedBranch()])
    draw()
    await clone('example-repo')

    const deletion = deferred<BranchResult[]>()
    platform.deleteBranches.mockReturnValue(deletion.promise)
    await choose(await cleanupMenu(), 'Clean up merged branches…')
    await userEvent.click(await screen.findByRole('button', { name: 'Delete 1 branch' }))

    expect(within(meta()).getByRole('status')).toHaveTextContent('Deleting 1 merged branch…')
    expect((await cleanupMenu()).getByRole('menuitem', { name: 'Clean up merged branches…' })).toBeDisabled()
    await userEvent.keyboard('{Escape}')

    await settle(() => {
      deletion.resolve([])
    })

    await vi.waitFor(() => {
      expect(screen.getByRole('button', { name: 'Check again' })).toBeEnabled()
    })
    expect((await cleanupMenu()).getByRole('menuitem', { name: 'Clean up merged branches…' })).toBeEnabled()
  })

  it('says the recently cleaned-up list is being cleared', async () => {
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])
    draw()

    const clear = deferred<undefined>()
    platform.clearCleanups.mockReturnValue(clear.promise)

    const list = within(await screen.findByRole('region', { name: 'Recently cleaned up' }))
    await userEvent.click(list.getByRole('button', { name: 'Clear' }))

    expect(list.getByRole('button', { name: 'Clearing…' })).toBeDisabled()

    await settle(() => {
      clear.resolve(undefined)
    })

    await vi.waitFor(() => {
      expect(screen.queryByRole('region', { name: 'Recently cleaned up' })).not.toBeInTheDocument()
    })
  })
})
