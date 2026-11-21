import { act, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { SettingsSection } from '../../lib/settingsIndex'
import { useManifests, type ManifestsState } from '../../lib/useManifests'
import type { OnSection } from '../../lib/useSections'
import type { DepsSort, ManifestsClone, ManifestsPage } from '../../platform'
import { deferred } from '../../test/async'
import { fromNow, isoAgo, renderAt } from '../../test/clock'
import { agentSession, manifestsClone, manifestsModule, manifestsPage, sessionsSnapshot } from '../../test/fixtures'
import { platform, send } from '../../test/platform'
import { DepsPage, type DepsPageProps } from './DepsPage'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const dayMs = 24 * 60 * 60 * 1000

// daysAgo is the time days before FIXED_NOW, as RFC 3339 text.
function daysAgo(days: number): string {
  return fromNow(-days * dayMs).toISOString()
}

// goOnly and jsOnly are the modules of a clone with a Go module alone, or a JavaScript
// package alone.
const goOnly = [manifestsModule()]
const jsOnly = [manifestsModule({ kind: 'js', path: 'package.json', lockfile: 'package-lock.json', goModule: '' })]

// clone is acme-org/<name>'s clone, its stalest module changed at oldest ('' for an age
// that isn't known), with change made.
function clone(name: string, oldest: string, change: Partial<ManifestsClone> = {}): ManifestsClone {
  return manifestsClone({ repo: `acme-org/${name}`, dir: `~/projects/${name}`, oldest, ...change })
}

// groupedPage lists a clone in each group: acme-org/api-server and a clone that couldn't
// be read in Stale, acme-org/web-console's JavaScript in Aging, and acme-org/cli's Go in
// Fresh.
function groupedPage(change: Partial<ManifestsPage> = {}): ManifestsPage {
  return manifestsPage({
    clones: [
      manifestsClone(),
      clone('web-console', daysAgo(40), { modules: jsOnly, tools: ['npm'] }),
      clone('cli', daysAgo(3), { modules: goOnly, tools: ['magex'] }),
      clone('broken', '', { modules: [], tools: [], error: 'Couldn’t read this clone’s files at origin/master.' }),
    ],
    modules: 4,
    ...change,
  })
}

// read is the page's state over page as read, with no read under way.
function read(page: ManifestsPage, change: Partial<ManifestsState> = {}): ManifestsState {
  return {
    page,
    error: '',
    refreshing: false,
    refreshError: '',
    progress: null,
    refresh: () => Promise.resolve(),
    ...change,
  }
}

type Props = Omit<DepsPageProps, 'manifests'>

// Page is the page as the tab shows it, reading the clones itself.
function Page(props: Props) {
  return <DepsPage manifests={useManifests(true)} {...props} />
}

// handlers are the page's callbacks, each a mock.
function handlers() {
  return {
    onUpgrade: vi.fn<(clone: ManifestsClone) => void>(),
    onOpenSettings: vi.fn<(section: SettingsSection) => void>(),
    onSection: vi.fn<OnSection>(),
    onDepsSort: vi.fn<(sort: DepsSort) => void>(),
  }
}

// renderPage shows the page reading the clones itself, with props on top, and returns its
// handlers.
function renderPage(props: Partial<Props> = {}) {
  const calls = handlers()

  renderAt(<Page where="under ~/projects" {...calls} {...props} />)

  return calls
}

// renderRead shows the page over state, with props on top, and returns its handlers and a
// way to render it again with the same props.
function renderRead(state: ManifestsState, props: Partial<Props> = {}) {
  const calls = handlers()
  const page = () => <DepsPage manifests={state} where="under ~/projects" {...calls} {...props} />
  const view = renderAt(page())

  return {
    ...calls,
    rerender: () => {
      view.rerender(page())
    },
  }
}

// group is a group's heading button, by its title.
function group(title: RegExp) {
  return screen.getByRole('button', { name: title })
}

// rows are the repositories a group's list shows, top to bottom.
function rows(title: string): string[] {
  return within(screen.getByRole('list', { name: title }))
    .getAllByRole('button', { name: /^Unfold / })
    .map((b) => b.getAttribute('aria-label')?.replace('Unfold acme-org/', '') ?? '')
}

// sorts is the page's choice of order.
function sorts() {
  return within(screen.getByRole('radiogroup', { name: 'Sort the clones' }))
}

// heldRead makes the next read of the clones wait until the test answers it, and returns
// the answer.
function heldRead(): (page: ManifestsPage) => void {
  const d = deferred<ManifestsPage>()

  platform.listManifests.mockImplementationOnce(() => d.promise)

  return d.resolve
}

describe('DepsPage', () => {
  it('says it reads the clones and how far it has got, then what it read and what it left out', async () => {
    const answer = heldRead()

    renderPage()

    expect(screen.getByRole('status')).toHaveTextContent('Reading your clones…')

    send('manifests:progress', { phase: 'clones', done: 0, total: 0 })
    expect(screen.getByRole('status')).toHaveTextContent('Finding your clones…')

    send('manifests:progress', { phase: 'reading', done: 3, total: 12 })
    expect(screen.getByRole('status')).toHaveTextContent('Reading go.mod and package.json · 3 of 12 clones')

    act(() => {
      answer(manifestsPage())
    })

    expect(await screen.findByText(/^1 clone · 2 modules · read/)).toHaveTextContent(
      '1 clone · 2 modules · read 1m ago',
    )
    expect(screen.getByText('1m ago')).toHaveAttribute('dateTime', '2026-06-15T11:59:00Z')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByText('1 muted and 1 not on GitHub left out')).toBeInTheDocument()
    expect(rows('Stale')).toEqual(['api-server'])
    expect(platform.listManifests).toHaveBeenCalledExactlyOnceWith(false)
  })

  it('lists Stale open, then Aging and Fresh folded, each heading saying what its edges mean', async () => {
    const { onSection } = renderRead(read(groupedPage({ muted: 0, withoutGitHub: 0, withoutModules: 0 })))

    expect(group(/^Stale · 2: over 3 months, age unknown$/)).toHaveAttribute('aria-expanded', 'true')
    expect(rows('Stale')).toEqual(['api-server', 'broken'])
    expect(group(/^Aging · 1: 1–3 months$/)).toHaveAttribute('aria-expanded', 'false')
    expect(group(/^Fresh · 1: under a month$/)).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('list', { name: 'Aging' })).not.toBeInTheDocument()
    expect(screen.queryByText(/left out/)).not.toBeInTheDocument()

    await userEvent.click(group(/^Aging/))

    expect(onSection).toHaveBeenCalledExactlyOnceWith('deps.aging', true)
    expect(group(/^Aging/)).toHaveAttribute('aria-expanded', 'true')
    expect(rows('Aging')).toEqual(['web-console'])

    await userEvent.click(group(/^Stale/))

    expect(onSection).toHaveBeenLastCalledWith('deps.stale', false)
    expect(screen.queryByRole('list', { name: 'Stale' })).not.toBeInTheDocument()
  })

  it('keeps each group open or folded as the user left it, across starts', () => {
    renderRead(read(groupedPage()), { sections: { 'deps.stale': false, 'deps.fresh': true } })

    expect(group(/^Stale/)).toHaveAttribute('aria-expanded', 'false')
    expect(group(/^Aging/)).toHaveAttribute('aria-expanded', 'false')
    expect(group(/^Fresh/)).toHaveAttribute('aria-expanded', 'true')
    expect(rows('Fresh')).toEqual(['cli'])
  })

  it('opens and folds the groups without settings to keep them in', async () => {
    renderAt(
      <DepsPage
        manifests={read(groupedPage())}
        where="under ~/projects"
        onUpgrade={vi.fn()}
        onOpenSettings={vi.fn()}
      />,
    )

    await userEvent.click(group(/^Fresh/))
    expect(rows('Fresh')).toEqual(['cli'])

    await userEvent.click(sorts().getByRole('radio', { name: 'A–Z' }))
    expect(sorts().getByRole('radio', { name: 'A–Z' })).toBeChecked()
  })

  it('searches every group, open while it searches, and says when nothing matches', async () => {
    renderRead(read(groupedPage()))

    await userEvent.type(screen.getByRole('searchbox', { name: 'Search repositories' }), 'WEB')

    expect(rows('Aging')).toEqual(['web-console'])
    expect(group(/^Aging · 1/)).toBeDisabled()
    expect(screen.queryByRole('button', { name: /^Stale/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Fresh/ })).not.toBeInTheDocument()

    await userEvent.clear(screen.getByRole('searchbox', { name: 'Search repositories' }))
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search repositories' }), 'nothing-like-it')

    expect(screen.getByText('No repository matching “nothing-like-it”.')).toBeInTheDocument()
  })

  describe('the kind chips', () => {
    // chips are the chips that filter by kind.
    function chips() {
      return within(screen.getByRole('group', { name: 'Filter by kind' }))
    }

    it('keeps the rows to the clones with a module of the chip’s kind, one kind at a time', async () => {
      renderRead(read(groupedPage()), { sections: { 'deps.aging': true, 'deps.fresh': true } })

      const go = chips().getByRole('button', { name: 'Go 2' })
      const js = chips().getByRole('button', { name: 'JS 2' })

      expect(go).toHaveAttribute('aria-pressed', 'false')
      expect(go).toHaveAttribute('title', 'Show only the 2 clones with a Go module')

      await userEvent.click(js)

      expect(js).toHaveAttribute('aria-pressed', 'true')
      expect(js).toHaveAttribute('title', 'Show every clone')
      expect(rows('Stale')).toEqual(['api-server'])
      expect(rows('Aging')).toEqual(['web-console'])
      expect(screen.queryByRole('list', { name: 'Fresh' })).not.toBeInTheDocument()

      await userEvent.click(go)

      expect(go).toHaveAttribute('aria-pressed', 'true')
      expect(js).toHaveAttribute('aria-pressed', 'false')
      expect(rows('Fresh')).toEqual(['cli'])
      expect(screen.queryByRole('list', { name: 'Aging' })).not.toBeInTheDocument()

      await userEvent.click(go)

      expect(go).toHaveAttribute('aria-pressed', 'false')
      expect(rows('Stale')).toEqual(['api-server', 'broken'])
    })

    it('counts what the search leaves, and keeps a chip that is on when it leaves none', async () => {
      renderRead(read(groupedPage()))

      await userEvent.click(chips().getByRole('button', { name: 'JS 2' }))
      await userEvent.type(screen.getByRole('searchbox', { name: 'Search repositories' }), 'cli')

      expect(chips().getByRole('button', { name: 'Go 1' })).toHaveAttribute('aria-pressed', 'false')
      expect(chips().getByRole('button', { name: 'JS 0' })).toHaveAttribute('aria-pressed', 'true')
      expect(screen.getByText('No repository matching “cli”.')).toBeInTheDocument()
    })

    it('offers no chip for a kind no clone has', () => {
      renderRead(read(manifestsPage({ clones: [clone('cli', daysAgo(200), { modules: goOnly })] })))

      expect(chips().getByRole('button', { name: 'Go 1' })).toBeInTheDocument()
      expect(chips().queryByRole('button', { name: /^JS/ })).not.toBeInTheDocument()
    })

    it('says when no clone of the chip’s kind is stale', async () => {
      renderRead(read(manifestsPage({ clones: [clone('cli', daysAgo(3), { modules: [...goOnly, ...jsOnly] })] })))

      expect(group(/^Stale · 0$/)).toHaveAttribute('aria-expanded', 'true')
      expect(screen.getByText('No clone has stale dependencies.')).toBeInTheDocument()

      await userEvent.click(chips().getByRole('button', { name: 'JS 1' }))

      expect(screen.getByText('No clone with a JavaScript package has stale dependencies.')).toBeInTheDocument()
    })
  })

  it('says no clone is stale while it leaves every clone out, naming why', () => {
    renderRead(read(manifestsPage({ clones: [], modules: 0, muted: 2, withoutGitHub: 0, withoutModules: 5 })))

    expect(screen.getByText('2 muted left out · 5 with no go.mod or package.json')).toBeInTheDocument()
    expect(group(/^Stale · 0$/)).toBeInTheDocument()
    expect(screen.getByText('No clone has stale dependencies.')).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: 'Filter by kind' })).not.toBeInTheDocument()
  })

  it('offers to change the repository locations while there are no clones', async () => {
    const empty = manifestsPage({ clones: [], modules: 0, muted: 0, withoutGitHub: 0, withoutModules: 0 })
    const { onOpenSettings } = renderRead(read(empty), { where: 'in ~/projects and 2 other locations' })

    expect(
      screen.getByText(
        'No clones in ~/projects and 2 other locations yet. Deps lists the repositories you have cloned there.',
      ),
    ).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Stale/ })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Change the repository locations' }))
    expect(onOpenSettings).toHaveBeenCalledExactlyOnceWith('repositories')
  })

  it('says why the clones couldn’t be read at all', async () => {
    platform.listManifests.mockRejectedValue(new Error('the clone root is gone'))
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t read the clones: the clone root is gone')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByRole('group', { name: 'Filter by kind' })).not.toBeInTheDocument()
  })

  it('keeps the last page when a read fails, saying how old it is', () => {
    renderRead(read(manifestsPage({ checkedAt: isoAgo({ m: 14 }), error: 'Couldn’t list the clones in ~/projects.' })))

    const note = screen.getByRole('note')

    expect(note).toHaveTextContent('Couldn’t read the clones · 14m old')
    expect(note).toHaveAttribute('title', 'Couldn’t list the clones in ~/projects.')
    expect(rows('Stale')).toEqual(['api-server'])
  })

  it('reads every clone again on request, saying how far it has got, and why when that fails', async () => {
    renderPage()

    await screen.findByRole('list', { name: 'Stale' })

    const answer = heldRead()

    await userEvent.click(screen.getByRole('button', { name: 'Read again' }))

    expect(platform.listManifests).toHaveBeenLastCalledWith(true)
    expect(screen.getByRole('button', { name: 'Reading…' })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('Reading your clones…')

    send('manifests:progress', { phase: 'reading', done: 1, total: 4 })
    expect(screen.getByRole('status')).toHaveTextContent('Reading go.mod and package.json · 1 of 4 clones')
    expect(rows('Stale')).toEqual(['api-server'])

    await act(async () => {
      answer(manifestsPage({ clones: [clone('cli', daysAgo(200))] }))
      await Promise.resolve()
    })

    expect(await screen.findByRole('button', { name: 'Read again' })).toBeEnabled()
    expect(rows('Stale')).toEqual(['cli'])
    expect(screen.queryByRole('status')).not.toBeInTheDocument()

    platform.listManifests.mockRejectedValue(new Error('the clone root is gone'))
    await userEvent.click(screen.getByRole('button', { name: 'Read again' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t read the clones again: the clone root is gone')
    expect(rows('Stale')).toEqual(['cli'])
  })

  it('says how far a read has got over the page it has, and leaves out a time it lacks', () => {
    renderRead(read(manifestsPage({ checkedAt: '' }), { progress: { phase: 'clones', done: 0, total: 0 } }))

    expect(screen.getByRole('status')).toHaveTextContent('Finding your clones…')
    expect(rows('Stale')).toEqual(['api-server'])
  })

  it('counts the clones it read when it has no time for the read', () => {
    renderRead(read(manifestsPage({ checkedAt: '' })))

    // The whole of the line: no "read" time after the counts.
    expect(screen.getByText('1 clone · 2 modules')).toBeInTheDocument()
    expect(document.querySelector('time[dateTime=""]')).toBeNull()
  })

  describe('the sort', () => {
    // sortPage lists four stale clones whose ages, names, and module counts each order them
    // differently, two of them named in capitals.
    function sortPage(change: (c: ManifestsClone) => ManifestsClone = (c) => c): ManifestsPage {
      return manifestsPage({
        clones: [
          clone('Web-kit', daysAgo(100), { modules: goOnly }),
          clone('api', daysAgo(400), { modules: [...goOnly, ...jsOnly] }),
          clone('zeta', daysAgo(150), { modules: [...goOnly, ...jsOnly, ...jsOnly] }),
          clone('Sdk-kit', daysAgo(300), { modules: goOnly }),
        ].map(change),
      })
    }

    it('lists each group oldest first until another order is picked, and stores each choice', async () => {
      const { onDepsSort } = renderRead(read(sortPage()))

      expect(sorts().getByRole('radio', { name: 'Oldest' })).toBeChecked()
      expect(sorts().getByRole('radio', { name: 'Oldest' }).closest('label')).toHaveAttribute(
        'title',
        'The dependencies that changed longest ago first',
      )
      expect(rows('Stale')).toEqual(['api', 'Sdk-kit', 'zeta', 'Web-kit'])

      await userEvent.click(sorts().getByRole('radio', { name: 'A–Z' }))
      expect(rows('Stale')).toEqual(['api', 'Sdk-kit', 'Web-kit', 'zeta'])

      await userEvent.click(sorts().getByRole('radio', { name: 'Modules' }))
      expect(rows('Stale')).toEqual(['zeta', 'api', 'Sdk-kit', 'Web-kit'])

      expect(onDepsSort.mock.calls).toEqual([['name'], ['modules']])
    })

    it('starts on the order the settings remember, and keeps the one picked while they store it', async () => {
      const { rerender } = renderRead(read(sortPage()), { depsSort: 'name' })

      expect(sorts().getByRole('radio', { name: 'A–Z' })).toBeChecked()
      expect(rows('Stale')).toEqual(['api', 'Sdk-kit', 'Web-kit', 'zeta'])

      await userEvent.click(sorts().getByRole('radio', { name: 'Modules' }))
      rerender()

      expect(sorts().getByRole('radio', { name: 'Modules' })).toBeChecked()
      expect(rows('Stale')).toEqual(['zeta', 'api', 'Sdk-kit', 'Web-kit'])
    })

    it('keeps the pinned clones first in every order', async () => {
      renderRead(read(sortPage((c) => (c.repo === 'acme-org/Web-kit' ? { ...c, pinned: true } : c))))

      expect(rows('Stale')).toEqual(['Web-kit', 'api', 'Sdk-kit', 'zeta'])

      await userEvent.click(sorts().getByRole('radio', { name: 'Modules' }))
      expect(rows('Stale')).toEqual(['Web-kit', 'zeta', 'api', 'Sdk-kit'])
    })
  })

  describe('the rows', () => {
    it('opens a clone’s upgrade sheet from its row, and from its drawer', async () => {
      const { onUpgrade } = renderRead(read(manifestsPage()))

      await userEvent.click(screen.getByRole('button', { name: 'Upgrade acme-org/api-server’s dependencies…' }))
      expect(onUpgrade).toHaveBeenCalledExactlyOnceWith(manifestsClone())

      await userEvent.click(screen.getByRole('button', { name: 'Unfold acme-org/api-server' }))
      await userEvent.click(screen.getByRole('button', { name: 'Upgrade dependencies…' }))

      expect(onUpgrade).toHaveBeenCalledTimes(2)
      expect(onUpgrade).toHaveBeenLastCalledWith(manifestsClone())
    })

    it('unfolds several rows at once, each staying unfolded across reads', async () => {
      renderPage()

      platform.listManifests.mockResolvedValue(
        manifestsPage({ clones: [manifestsClone(), clone('cli', daysAgo(200))], modules: 4 }),
      )
      await screen.findByRole('list', { name: 'Stale' })
      await userEvent.click(screen.getByRole('button', { name: 'Read again' }))
      await userEvent.click(await screen.findByRole('button', { name: 'Unfold acme-org/cli' }))
      await userEvent.click(screen.getByRole('button', { name: 'Unfold acme-org/api-server' }))

      expect(screen.getByRole('listitem', { name: 'About acme-org/cli' })).toBeInTheDocument()
      expect(screen.getByRole('listitem', { name: 'About acme-org/api-server' })).toBeInTheDocument()

      await userEvent.click(screen.getByRole('button', { name: 'Read again' }))

      expect(await screen.findByRole('button', { name: 'Fold acme-org/cli' })).toHaveAttribute('aria-expanded', 'true')

      await userEvent.click(screen.getByRole('button', { name: 'Fold acme-org/cli' }))
      expect(screen.queryByRole('listitem', { name: 'About acme-org/cli' })).not.toBeInTheDocument()
      expect(screen.getByRole('listitem', { name: 'About acme-org/api-server' })).toBeInTheDocument()
    })

    it('shows the upgrade an agent works on in a clone, named as the agents are', async () => {
      platform.listSessions.mockResolvedValue(
        sessionsSnapshot([
          agentSession({
            repo: 'acme-org/api-server',
            itemId: 0,
            kind: '',
            number: 0,
            title: 'Upgrade dependencies',
            state: 'working',
            detail: '',
            stateAt: isoAgo({ m: 4 }),
          }),
        ]),
      )
      renderPage()

      const row = (await screen.findByRole('button', { name: 'Unfold acme-org/api-server' })).closest('li')

      expect(await within(row as HTMLElement).findByText(/Claude · working/)).toBeInTheDocument()
      expect(row).toHaveTextContent('Upgrade: Claude · working · 4m')
    })

    it('reads the upgrades again when the shell says a session changed', async () => {
      renderPage()

      await screen.findByRole('list', { name: 'Stale' })
      expect(screen.queryByText(/Upgrade:/)).not.toBeInTheDocument()

      platform.listSessions.mockResolvedValue(
        sessionsSnapshot([
          agentSession({
            repo: 'acme-org/api-server',
            itemId: 0,
            kind: '',
            number: 0,
            state: 'idle',
            stateAt: isoAgo({ m: 1 }),
          }),
        ]),
      )
      send('sessions:updated')

      expect(await screen.findByText(/Claude · idle/)).toBeInTheDocument()
    })
  })
})
