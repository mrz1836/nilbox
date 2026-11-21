import { act, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { AgentSession, ManifestsClone } from '../../platform'
import { deferred } from '../../test/async'
import { fromNow, isoAgo, renderAt } from '../../test/clock'
import { agents, agentSession, manifestsClone, manifestsModule } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { DepsLegend, DepsRow, depsListClass, type DepsRowProps } from './DepsRow'
import { GroupHeading } from './GroupHeading'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const dayMs = 24 * 60 * 60 * 1000

// daysAgo is the time days before FIXED_NOW, as RFC 3339 text.
function daysAgo(days: number): string {
  return fromNow(-days * dayMs).toISOString()
}

// upgradeDir is the worktree an upgrade of acme-org/api-server works in.
const upgradeDir = `~/projects/.${brand.slug}/worktrees/api-server/deps-20260615`

// upgrade is an upgrade of acme-org/api-server Claude works on, as a repository's launch
// records it: no item, the prompt's name for its title, in its deps worktree.
function upgrade(change: Partial<AgentSession> = {}): AgentSession {
  return agentSession({
    id: 'u1',
    repo: 'acme-org/api-server',
    itemId: 0,
    kind: '',
    number: 0,
    title: 'Upgrade dependencies',
    url: 'https://github.com/acme-org/api-server',
    dir: upgradeDir,
    worktree: 'deps-20260615',
    state: 'working',
    detail: '',
    stateAt: isoAgo({ m: 4 }),
    ...change,
  })
}

type RowProps = Partial<Omit<DepsRowProps, 'clone' | 'onToggle' | 'onUpgrade'>>

// renderRow renders one clone's row in a group's list, as the page lists it, the clock at
// FIXED_NOW, and returns it with its handlers and a way to render it again.
function renderRow(clone: ManifestsClone = manifestsClone(), props: RowProps = {}) {
  const onToggle = vi.fn<() => void>()
  const onUpgrade = vi.fn<() => void>()
  const row = (change: RowProps = {}) => (
    <ul className={depsListClass}>
      <DepsRow clone={clone} open={false} onToggle={onToggle} onUpgrade={onUpgrade} {...props} {...change} />
    </ul>
  )
  const view = renderAt(row())

  return {
    row: () => screen.getAllByRole('listitem')[0] as HTMLElement,
    onToggle,
    onUpgrade,
    rerender: (change: RowProps) => {
      view.rerender(row(change))
    },
  }
}

// edge is a row's staleness edge, its first cell.
function edge(row: HTMLElement): Element | null {
  return row.firstElementChild
}

describe('DepsRow', () => {
  it('lays a clone out on one line: its edge, repository, modules, tools, stalest manifest, and age', () => {
    const { row } = renderRow()
    const r = within(row())

    expect(row().parentElement).toHaveClass('@container', 'grid')
    expect(row()).toHaveClass('group/row', 'grid-cols-subgrid', 'col-span-full')
    expect(edge(row())).toHaveAttribute('data-tone', 'caution')
    expect(edge(row())).toHaveClass('bg-caution')
    expect(r.getByText('api-server')).toHaveClass('font-medium')
    expect(r.getByText('acme-org/')).toHaveClass('text-muted')
    expect(r.getByText('api-server').parentElement).toHaveAttribute(
      'title',
      'acme-org/api-server\n~/projects/api-server',
    )
    expect(r.getByText('Go 1 · JS 1')).toHaveAttribute('title', '1 Go module and 1 JavaScript package')
    expect(r.getByText('magex · pnpm')).toBeInTheDocument()
    expect(r.getByText('go.mod').parentElement).toHaveAttribute('title', 'go.mod, the one that changed longest ago')
    expect(r.getByText('1y ago')).toHaveAttribute('dateTime', '2025-04-15T12:00:00Z')
    expect(r.getByText('1y ago').parentElement).toHaveAttribute(
      'title',
      'Last changed Tuesday, April 15, 2025 at 12:00 PM\nchore(deps): bump the go modules (#41)\nAs of origin/master, fetched 3d ago',
    )
    expect(r.queryByRole('img', { name: 'Pinned' })).not.toBeInTheDocument()
    expect(r.getByRole('button', { name: 'Upgrade acme-org/api-server’s dependencies…' })).toHaveTextContent('Upgrade…')

    const chevron = r.getByRole('button', { name: 'Unfold acme-org/api-server' })

    expect(chevron).toHaveAttribute('aria-expanded', 'false')
    expect(chevron).not.toHaveAttribute('aria-controls')
    expect(screen.queryByRole('listitem', { name: 'About acme-org/api-server' })).not.toBeInTheDocument()
  })

  it('names the stalest manifest alone when it is the clone’s only one', () => {
    const { row } = renderRow(manifestsClone({ modules: [manifestsModule()] }))

    expect(within(row()).getByText('go.mod').parentElement).toHaveAttribute('title', 'go.mod')
  })

  it.each<[string, string, string, string]>([
    ['of one to three months', daysAgo(40), 'warning', 'bg-warning'],
    ['under a month', daysAgo(3), 'success', 'bg-success'],
    ['not known', '', 'muted', 'bg-muted/30'],
  ])('colors the edge of an age %s', (_, oldest, tone, fill) => {
    const { row } = renderRow(manifestsClone({ oldest }))

    expect(edge(row())).toHaveAttribute('data-tone', tone)
    expect(edge(row())).toHaveClass(fill)
  })

  it('shows no age for a clone whose age isn’t known, and says why', () => {
    const { row } = renderRow(
      manifestsClone({
        oldest: '',
        modules: [
          manifestsModule({ changedAt: '', subject: '', problem: 'Couldn’t read when this go.mod last changed.' }),
        ],
      }),
    )

    expect(row().querySelector('time')).toBeNull()
    expect(within(row()).getByText('age unknown').parentElement).toHaveAttribute(
      'title',
      'Couldn’t read when this go.mod last changed.',
    )
  })

  it('marks a repository the user pinned', () => {
    const { row } = renderRow(manifestsClone({ pinned: true }))

    expect(within(row()).getByRole('img', { name: 'Pinned' })).toHaveAttribute(
      'title',
      'Pinned: unpin it in Settings → Repositories',
    )
  })

  it('notes how the clone was read, each with its sentence, a comma apart for a screen reader', () => {
    const { row } = renderRow(
      manifestsClone({
        tip: 'HEAD',
        atHead: true,
        atHeadWhy: 'This clone has no origin/master.',
        shallow: true,
        truncated: true,
      }),
    )
    const r = within(row())

    expect(row()).toHaveTextContent('read at HEAD, shallow, not all read')
    expect(r.getByText('read at HEAD').parentElement).toHaveAttribute('title', 'This clone has no origin/master.')
    expect(r.getByText('shallow').parentElement).toHaveAttribute(
      'title',
      'A shallow clone: its history stops at its shallow commits, so its dependencies may be older than shown.',
    )
    expect(r.getByText('not all read')).toBeInTheDocument()
  })

  it('says why a clone couldn’t be read in its stalest manifest’s place, with nothing else to note', () => {
    const error = 'Couldn’t read this clone’s files at origin/master: object not found.'
    const { row } = renderRow(manifestsClone({ modules: [], oldest: '', tools: [], shallow: true, error }))
    const r = within(row())

    expect(r.getByText(error).parentElement).toHaveAttribute('title', error)
    expect(r.getByText(error).parentElement).toHaveClass('text-warning')
    expect(r.queryByText('shallow')).not.toBeInTheDocument()
    expect(row().querySelector('time')).toBeNull()
    expect(edge(row())).toHaveAttribute('data-tone', 'muted')
  })

  it('leaves the manifest’s place empty for a clone that holds none', () => {
    const { row } = renderRow(manifestsClone({ modules: [], oldest: '', tools: [] }))

    expect(row()).toHaveTextContent(/^acme-org\/api-serverage unknownUpgrade…$/)
  })

  describe('an upgrade under way', () => {
    it('follows the newest upgrade an agent works on, as a card’s session tag does', () => {
      const { row } = renderRow(manifestsClone(), {
        sessions: [upgrade(), upgrade({ id: 'u0', state: 'ended', endedAt: isoAgo({ h: 1 }) })],
        agents: agents(),
      })
      const tag = within(row())
        .getByText(/Claude · working/)
        .closest('[title]')

      expect(tag).toHaveTextContent('Upgrade: Claude · working · 4m')
      expect(tag).toHaveAttribute('title', `Upgrade dependencies: Claude working\nIn ${upgradeDir}`)
      expect(tag).toHaveClass('text-info')
    })

    it('says what an agent waits for, and keeps a comma between it and the notes', () => {
      const { row } = renderRow(manifestsClone({ shallow: true }), {
        sessions: [upgrade({ state: 'needs_approval', detail: 'Permission prompt', stateAt: isoAgo({ m: 2 }) })],
        agents: agents(),
      })
      const tag = within(row())
        .getByText(/Claude · needs approval/)
        .closest('[title]')

      expect(tag).toHaveAttribute(
        'title',
        `Upgrade dependencies: Claude needs approval: Permission prompt\nIn ${upgradeDir}`,
      )
      expect(row()).toHaveTextContent('Upgrade: Claude · needs approval · 2m, shallow')
    })

    it('shows an upgrade that ended in the last day, and none that ended before', () => {
      const ended = upgrade({ state: 'ended', stateAt: isoAgo({ h: 2 }), endedAt: isoAgo({ h: 2 }) })
      const { row, rerender } = renderRow(manifestsClone(), { sessions: [ended], agents: agents() })

      expect(row()).toHaveTextContent('Upgrade: Claude · ended · 2h')

      rerender({ sessions: [{ ...ended, endedAt: daysAgo(2) }] })
      expect(row()).not.toHaveTextContent('Upgrade:')
    })

    it('names the agent by its ID until the agents are listed, and leaves out a time it lacks', () => {
      const { row } = renderRow(manifestsClone(), { sessions: [upgrade({ agent: 'codex', stateAt: '' })] })

      expect(row()).toHaveTextContent('Upgrade: codex · working')
      expect(row()).not.toHaveTextContent('working ·')
    })
  })

  describe('folding', () => {
    it('unfolds from a click on its empty space, and leaves its controls to themselves', async () => {
      const { row, onToggle, onUpgrade } = renderRow()
      const r = within(row())

      await userEvent.click(r.getByText('magex · pnpm'))
      expect(onToggle).toHaveBeenCalledOnce()

      await userEvent.click(r.getByRole('button', { name: 'Upgrade acme-org/api-server’s dependencies…' }))
      expect(onUpgrade).toHaveBeenCalledOnce()
      expect(onToggle).toHaveBeenCalledOnce()

      await userEvent.click(r.getByRole('button', { name: 'Unfold acme-org/api-server' }))
      expect(onToggle).toHaveBeenCalledTimes(2)
    })

    it('leaves the row as it was after a drag that selects its words', async () => {
      const { row, onToggle } = renderRow()

      vi.spyOn(window, 'getSelection').mockReturnValue({ toString: () => 'api-server' } as Selection)
      await userEvent.click(within(row()).getByText('api-server'))

      expect(onToggle).not.toHaveBeenCalled()
    })

    it('unfolds from a click where the window can’t say what is selected', async () => {
      const { row, onToggle } = renderRow()

      vi.spyOn(window, 'getSelection').mockReturnValue(null)
      await userEvent.click(within(row()).getByText('api-server'))

      expect(onToggle).toHaveBeenCalledOnce()
    })

    it('unfolds with → and folds with ←, from its chevron', async () => {
      const { row, onToggle, rerender } = renderRow()

      within(row()).getByRole('button', { name: 'Unfold acme-org/api-server' }).focus()
      await userEvent.keyboard('{ArrowLeft}')
      expect(onToggle).not.toHaveBeenCalled()
      await userEvent.keyboard('{ArrowRight}')
      expect(onToggle).toHaveBeenCalledOnce()

      rerender({ open: true })
      within(row()).getByRole('button', { name: 'Fold acme-org/api-server' }).focus()
      await userEvent.keyboard('{ArrowRight}')
      expect(onToggle).toHaveBeenCalledOnce()
      await userEvent.keyboard('{ArrowLeft}')
      expect(onToggle).toHaveBeenCalledTimes(2)
      await userEvent.keyboard('{Enter}')
      expect(onToggle).toHaveBeenCalledTimes(3)
    })

    it('moves to the rows around it with ↓ and ↑, across groups', async () => {
      const shared = { open: false, onToggle: vi.fn(), onUpgrade: vi.fn() }
      const clone = (name: string) => manifestsClone({ repo: `acme-org/${name}`, dir: `~/projects/${name}` })

      renderAt(
        <>
          <ul className={depsListClass}>
            <DepsRow clone={clone('api')} {...shared} />
            <DepsRow clone={clone('cli')} {...shared} />
          </ul>
          <ul className={depsListClass}>
            <DepsRow clone={clone('web')} {...shared} />
          </ul>
        </>,
      )

      const first = screen.getByRole('button', { name: 'Unfold acme-org/api' })

      first.focus()
      await userEvent.keyboard('{ArrowUp}')
      expect(first).toHaveFocus()
      await userEvent.keyboard('{ArrowDown}')
      expect(screen.getByRole('button', { name: 'Unfold acme-org/cli' })).toHaveFocus()
      await userEvent.keyboard('{ArrowDown}')

      const last = screen.getByRole('button', { name: 'Unfold acme-org/web' })

      expect(last).toHaveFocus()
      await userEvent.keyboard('{ArrowDown}')
      expect(last).toHaveFocus()
      await userEvent.keyboard('{ArrowUp}')
      expect(screen.getByRole('button', { name: 'Unfold acme-org/cli' })).toHaveFocus()
      await userEvent.keyboard('{Home}')
      expect(screen.getByRole('button', { name: 'Unfold acme-org/cli' })).toHaveFocus()
    })
  })
})

describe('DepsDrawer', () => {
  // drawer is the open row's drawer.
  function drawer() {
    return within(screen.getByRole('listitem', { name: 'About acme-org/api-server' }))
  }

  // lines are the drawer's module lines.
  function lines(): HTMLElement[] {
    return within(drawer().getByRole('list', { name: 'The modules of acme-org/api-server' })).getAllByRole('listitem')
  }

  it('unfolds under its row, a line for each module, stalest first', () => {
    renderRow(manifestsClone(), { open: true })

    const chevron = screen.getByRole('button', { name: 'Fold acme-org/api-server' })
    const about = screen.getByRole('listitem', { name: 'About acme-org/api-server' })

    expect(chevron).toHaveAttribute('aria-expanded', 'true')
    expect(chevron).toHaveAttribute('aria-controls', about.id)
    expect(screen.getAllByRole('listitem')[0]).toHaveAttribute('data-open')

    const [goMod, web] = lines() as [HTMLElement, HTMLElement]
    const go = within(goMod)

    expect(go.getByText('go')).toHaveAttribute('title', 'A Go module')
    expect(go.getByText('go.mod')).toHaveAttribute('title', 'go.mod\ngithub.com/acme-org/api-server\nlockfile go.sum')
    expect(go.getByText('go 1.22')).toBeInTheDocument()
    expect(go.getByText('4 direct · 9 indirect')).toBeInTheDocument()
    expect(go.getByText('chore(deps): bump the go modules (#41)')).toHaveAttribute(
      'title',
      'chore(deps): bump the go modules (#41)',
    )
    expect(go.getByText('1y ago').parentElement).toHaveAttribute(
      'title',
      'Last changed Tuesday, April 15, 2025 at 12:00 PM',
    )

    const js = within(web)

    expect(js.getByText('js')).toHaveAttribute('title', 'A JavaScript package')
    expect(js.getByText('web/package.json')).toBeInTheDocument()
    expect(js.getByText('pnpm')).toBeInTheDocument()
    expect(js.getByText('48 deps · 12 dev')).toBeInTheDocument()
    expect(js.getByText('chore(web): refresh the lockfile (#52)')).toBeInTheDocument()
    expect(js.getByText('4mo ago')).toBeInTheDocument()
  })

  it('says why a module couldn’t be read in full, under its line, and leaves out an age it lacks', () => {
    const problem = 'This package.json isn’t a JSON object, so it wasn’t read.'

    renderRow(
      manifestsClone({
        modules: [
          manifestsModule(),
          manifestsModule({ kind: 'js', path: 'web/package.json', changedAt: '', subject: '', problem }),
        ],
      }),
      { open: true },
    )

    const web = within(lines()[1] as HTMLElement)

    expect(web.getByText(problem)).toHaveClass('text-warning')
    expect(lines()[1]?.querySelector('time')).toBeNull()
  })

  it('names the clone’s workspace files', () => {
    renderRow(manifestsClone({ workspaces: ['go.work', 'pnpm-workspace.yaml'] }), { open: true })

    expect(drawer().getByText('go.work · pnpm-workspace.yaml').parentElement).toHaveTextContent(
      'Workspaces: go.work · pnpm-workspace.yaml',
    )
  })

  it('says why nothing was read of a clone that couldn’t be', () => {
    const error = 'Couldn’t work out which commit of this clone to read: not a git repository.'

    renderRow(manifestsClone({ modules: [], oldest: '', error }), { open: true })

    expect(drawer().getByText(error)).toHaveClass('text-warning')
    expect(drawer().queryByRole('list')).not.toBeInTheDocument()
  })

  it('says no manifest was read of a clone with neither modules nor an error', () => {
    renderRow(manifestsClone({ modules: [], oldest: '' }), { open: true })

    expect(drawer().getByText('No go.mod or package.json was read.')).toBeInTheDocument()
  })

  it('opens the clone in the terminal, and shows a module’s manifest in Finder', async () => {
    renderRow(manifestsClone(), { open: true })

    await userEvent.click(drawer().getByRole('button', { name: 'Open in Terminal' }))
    expect(platform.openManifestsTerminal).toHaveBeenCalledExactlyOnceWith('~/projects/api-server')

    await userEvent.click(drawer().getByRole('button', { name: 'Reveal web/package.json in Finder' }))
    expect(platform.revealManifest).toHaveBeenCalledExactlyOnceWith('~/projects/api-server', 'web/package.json')
    expect(drawer().queryByRole('alert')).not.toBeInTheDocument()
  })

  it('holds Terminal and Finder while one opens, and says why one couldn’t', async () => {
    const opening = deferred()

    platform.openManifestsTerminal.mockReturnValue(opening.promise)
    renderRow(manifestsClone(), { open: true })

    await userEvent.click(drawer().getByRole('button', { name: 'Open in Terminal' }))

    expect(drawer().getByRole('button', { name: 'Open in Terminal' })).toBeDisabled()
    expect(drawer().getByRole('button', { name: 'Reveal go.mod in Finder' })).toBeDisabled()

    await act(async () => {
      opening.resolve()
      await opening.promise
    })
    expect(drawer().getByRole('button', { name: 'Open in Terminal' })).toBeEnabled()

    platform.revealManifest.mockRejectedValue(new Error('Finder can’t show a file here.'))
    await userEvent.click(drawer().getByRole('button', { name: 'Reveal go.mod in Finder' }))

    expect(await drawer().findByRole('alert')).toHaveTextContent('Finder can’t show a file here.')
  })

  it('opens the upgrade sheet from Upgrade dependencies…', async () => {
    const { onUpgrade, onToggle } = renderRow(manifestsClone(), { open: true })

    await userEvent.click(drawer().getByRole('button', { name: 'Upgrade dependencies…' }))

    expect(onUpgrade).toHaveBeenCalledOnce()
    expect(onToggle).not.toHaveBeenCalled()
  })
})

describe('DepsLegend', () => {
  it('names the ages a group’s edges stand for in its heading, a dot of each color before them', () => {
    const rows = [manifestsClone(), manifestsClone({ repo: 'acme-org/web', dir: '~/projects/web', oldest: '' })]

    renderAt(
      <GroupHeading
        id="stale"
        title="Stale"
        count={rows.length}
        open
        disabled={false}
        onToggle={vi.fn()}
        note={<DepsLegend group="stale" rows={rows} />}
      />,
    )

    const heading = screen.getByRole('button', { name: 'Stale · 2: over 3 months, age unknown' })

    expect(heading.querySelector('.bg-caution')).not.toBeNull()
    expect(heading.querySelector('.bg-muted\\/30')).not.toBeNull()
  })

  it('names one age alone', () => {
    renderAt(
      <GroupHeading
        id="fresh"
        title="Fresh"
        count={1}
        open={false}
        disabled={false}
        onToggle={vi.fn()}
        note={<DepsLegend group="fresh" rows={[manifestsClone({ oldest: daysAgo(2) })]} />}
      />,
    )

    expect(screen.getByRole('button', { name: 'Fresh · 1: under a month' })).toBeInTheDocument()
  })
})
