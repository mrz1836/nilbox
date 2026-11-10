import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { ScriptRun, TerminalID } from '../../platform'
import { at, fromNow } from '../../test/clock'
import { scriptRun } from '../../test/fixtures'
import { useScriptRuns } from '../../lib/useScriptRuns'
import { emit, platform } from '../../test/platform'
import { RunsPage } from './RunsPage'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// ago is the time minutes before the test clock's, as a run records it.
function ago(minutes: number, seconds = 0): string {
  return fromNow(-(minutes * 60_000 + seconds * 1000)).toISOString()
}

// release is a release that is going in the terminal, started two minutes ago.
const release = scriptRun({
  id: 3,
  kind: 'release',
  title: 'Release v1.4.3',
  repo: 'acme-org/sdk',
  dir: '~/projects/sdk',
  runsIn: 'terminal',
  status: 'running',
  exitCode: null,
  summary: '',
  startedAt: ago(2),
  finishedAt: '',
  canStop: false,
  canRunAgain: false,
})

// lint is a magex lint run that took 38 seconds, an hour ago.
const lint = scriptRun({ id: 1, startedAt: ago(60, 38), finishedAt: ago(60) })

// Listed is the page as the tab shows it: fed every run the tab lists.
function Listed({ terminal }: { terminal: TerminalID | null }) {
  return <RunsPage {...useScriptRuns()} terminal={terminal} />
}

// renderPage shows the page with runs listed.
function renderPage(runs: ScriptRun[] = [release, lint], terminal: TerminalID | null = 'iterm2') {
  platform.listScriptRuns.mockResolvedValue(runs)
  render(<Listed terminal={terminal} />, { wrapper: at() })
}

// list is the list of runs, once it shows.
async function list() {
  return within(await screen.findByRole('list', { name: 'Runs' }))
}

// rows lists the runs as their buttons read.
async function rows(): Promise<string[]> {
  return (await list()).getAllByRole('button').map((b) => b.textContent)
}

describe('RunsPage', () => {
  it('lists every run, newest first: its status, title, where it ran, when, how long, and how it ended', async () => {
    renderPage()

    expect(screen.getByText('Listing the runs…')).toBeInTheDocument()
    expect(await rows()).toEqual([
      'RunningRelease v1.4.32m agoacme-org/sdk · In iTerm2',
      `Donemagex lint1h agoacme-org/example-repo · In ${brand.name} · 38 sFinished.`,
    ])
    expect(platform.listScriptRuns).toHaveBeenCalledExactlyOnceWith({ kinds: [], repo: '' })
  })

  it('names the terminal as the user’s until the settings say which', async () => {
    renderPage([release], null)

    expect((await list()).getByRole('button')).toHaveTextContent('acme-org/sdk · In your terminal')
  })

  it('says nothing has run yet, with nothing to clear', async () => {
    renderPage([])

    expect(
      await screen.findByText('Nothing has run yet. Releases, checks, and scripts you run show here.'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clear history' })).toBeDisabled()
  })

  it('offers nothing to clear while every run is still going', async () => {
    renderPage([release])

    await list()

    expect(screen.getByRole('button', { name: 'Clear history' })).toBeDisabled()
  })

  it('says why the runs couldn’t be listed', async () => {
    platform.listScriptRuns.mockRejectedValue(new Error('database is locked'))
    render(<Listed terminal="iterm2" />, { wrapper: at() })

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t list the runs: database is locked')
    expect(screen.queryByText('Listing the runs…')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clear history' })).toBeDisabled()
  })

  it('filters by kind, Hooks taking the checks too, from the runs it listed', async () => {
    const checks = scriptRun({ id: 2, kind: 'checks', title: 'Checks · all files' })

    renderPage([release, checks, lint])
    await list()

    await userEvent.click(screen.getByRole('radio', { name: 'Hooks' }))
    expect(await rows()).toEqual([expect.stringContaining('Checks · all files')])

    await userEvent.click(screen.getByRole('radio', { name: 'Release' }))
    expect(await rows()).toEqual([expect.stringContaining('Release v1.4.3')])

    await userEvent.click(screen.getByRole('radio', { name: 'Scripts' }))
    expect(await rows()).toEqual([expect.stringContaining('magex lint')])

    await userEvent.click(screen.getByRole('radio', { name: 'All' }))
    expect(await rows()).toHaveLength(3)

    // Filtering asks nothing more.
    expect(platform.listScriptRuns).toHaveBeenCalledExactlyOnceWith({ kinds: [], repo: '' })
  })

  it('says when a filter lists nothing, leaving the history to clear', async () => {
    renderPage()
    await list()

    await userEvent.click(screen.getByRole('radio', { name: 'Hooks' }))

    expect(screen.getByText('No hooks actions or checks have run yet.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clear history' })).toBeEnabled()
  })

  it('searches by repository, trimmed and in any case', async () => {
    renderPage()
    await list()

    await userEvent.type(screen.getByRole('searchbox', { name: 'Search repositories' }), ' SDK ')
    expect(await rows()).toEqual([expect.stringContaining('Release v1.4.3')])

    await userEvent.type(screen.getByRole('searchbox', { name: 'Search repositories' }), 'x')
    expect(screen.getByText('No run in a repository matching “SDK x”.')).toBeInTheDocument()
    expect(platform.listScriptRuns).toHaveBeenCalledOnce()
  })

  it('lists the runs again whenever they change', async () => {
    renderPage([lint])
    await list()

    platform.listScriptRuns.mockResolvedValue([release, lint])
    act(() => {
      emit('scriptruns:updated')
    })

    await waitFor(async () => {
      expect(await rows()).toHaveLength(2)
    })
  })

  it('opens a run in the panel beside the list, and closes it', async () => {
    platform.getScriptRun.mockResolvedValue(lint)
    renderPage()

    const button = (await list()).getByRole('button', { name: /magex lint/ })

    await userEvent.click(button)

    expect(await screen.findByRole('heading', { name: 'magex lint' })).toBeInTheDocument()
    expect(button).toHaveAttribute('aria-current', 'true')
    expect(platform.getScriptRun).toHaveBeenCalledExactlyOnceWith(1)

    await userEvent.click(screen.getByRole('button', { name: 'Close the run' }))

    expect(screen.queryByRole('heading', { name: 'magex lint' })).not.toBeInTheDocument()
    expect(button).not.toHaveAttribute('aria-current')
  })

  it('opens the run Run again started', async () => {
    platform.getScriptRun.mockImplementation((id) => Promise.resolve(scriptRun({ id, title: `run ${String(id)}` })))
    renderPage()

    await userEvent.click((await list()).getByRole('button', { name: /magex lint/ }))
    await userEvent.click(await screen.findByRole('button', { name: 'Run again' }))

    expect(await screen.findByRole('heading', { name: 'run 2' })).toBeInTheDocument()
  })

  describe('Clear history', () => {
    it('asks first, then forgets the ended runs, closing an ended run’s panel', async () => {
      platform.getScriptRun.mockResolvedValue(lint)
      renderPage()

      await userEvent.click((await list()).getByRole('button', { name: /magex lint/ }))
      await screen.findByRole('heading', { name: 'magex lint' })

      await userEvent.click(screen.getByRole('button', { name: 'Clear history' }))
      const dialog = within(screen.getByRole('dialog', { name: 'Clear the run history?' }))

      expect(
        dialog.getByText('This forgets every run that has ended, with its log. Runs still going stay.'),
      ).toBeInTheDocument()

      platform.listScriptRuns.mockResolvedValue([release])
      await userEvent.click(dialog.getByRole('button', { name: 'Clear history' }))

      expect(platform.clearScriptRunHistory).toHaveBeenCalledOnce()
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      await waitFor(async () => {
        expect(await rows()).toHaveLength(1)
      })
      expect(screen.queryByRole('heading', { name: 'magex lint' })).not.toBeInTheDocument()
    })

    it('keeps the panel of a run still going', async () => {
      platform.getScriptRun.mockResolvedValue(release)
      renderPage()

      await userEvent.click((await list()).getByRole('button', { name: /Release v1\.4\.3/ }))
      await screen.findByRole('heading', { name: 'Release v1.4.3' })

      await userEvent.click(screen.getByRole('button', { name: 'Clear history' }))
      await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Clear history' }))

      expect(platform.clearScriptRunHistory).toHaveBeenCalledOnce()
      expect(screen.getByRole('heading', { name: 'Release v1.4.3' })).toBeInTheDocument()
    })

    it('keeps the panel of a run still going that the filter hides', async () => {
      platform.getScriptRun.mockResolvedValue(release)
      renderPage()

      await userEvent.click((await list()).getByRole('button', { name: /Release v1\.4\.3/ }))
      await screen.findByRole('heading', { name: 'Release v1.4.3' })
      await userEvent.type(screen.getByRole('searchbox', { name: 'Search repositories' }), 'example')
      await userEvent.click(screen.getByRole('radio', { name: 'Scripts' }))
      expect(await rows()).toEqual([expect.stringContaining('magex lint')])

      await userEvent.click(screen.getByRole('button', { name: 'Clear history' }))
      await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Clear history' }))

      expect(platform.clearScriptRunHistory).toHaveBeenCalledOnce()
      expect(screen.getByRole('heading', { name: 'Release v1.4.3' })).toBeInTheDocument()
    })

    it('forgets nothing when the user changes their mind', async () => {
      renderPage()
      await list()

      await userEvent.click(screen.getByRole('button', { name: 'Clear history' }))
      await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))

      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      expect(platform.clearScriptRunHistory).not.toHaveBeenCalled()
    })

    it('says why the history couldn’t be cleared', async () => {
      platform.clearScriptRunHistory.mockRejectedValue(new Error('database is locked'))
      renderPage()
      await list()

      await userEvent.click(screen.getByRole('button', { name: 'Clear history' }))
      await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Clear history' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
      expect(await rows()).toHaveLength(2)
    })
  })
})
