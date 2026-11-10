import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { IntegrationTool } from '../../platform'
import { at } from '../../test/clock'
import { goPreCommitTool, integrationTool, integrationTools, scriptRun } from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import ScriptsView from './ScriptsView'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

function renderView() {
  const onManageSettings = vi.fn<() => void>()

  render(<ScriptsView terminal="iterm2" onManageSettings={onManageSettings} />, { wrapper: at() })

  return { onManageSettings }
}

// rail is the tab's rail.
function rail() {
  return within(screen.getByRole('navigation', { name: 'Scripts' }))
}

// tools lists the tools' lines at the rail's foot, once they show.
async function tools(): Promise<string[]> {
  const list = await rail().findByRole('list', { name: 'Tools' })

  return within(list)
    .getAllByRole('listitem')
    .map((li) => li.textContent)
}

describe('ScriptsView', () => {
  it('opens on Runs, the one page built so far', async () => {
    renderView()

    expect(rail().getAllByRole('button', { name: /^Runs/ })).toHaveLength(1)
    expect(rail().getByRole('button', { name: /^Runs/ })).toHaveAttribute('aria-current', 'page')
    expect(await screen.findByRole('heading', { name: 'Runs', level: 2 })).toBeInTheDocument()
    expect(await screen.findByRole('list', { name: 'Runs' })).toBeInTheDocument()
    expect(rail().queryByRole('button', { name: /Release|Hooks|Repo scripts/ })).not.toBeInTheDocument()

    await userEvent.click(rail().getByRole('button', { name: /^Runs/ }))
    expect(rail().getByRole('button', { name: /^Runs/ })).toHaveAttribute('aria-current', 'page')
  })

  it('counts the runs in progress on Runs, the orphaned ones and those in the terminal too', async () => {
    platform.listScriptRuns.mockResolvedValue([
      scriptRun({ id: 4, status: 'running' }),
      scriptRun({ id: 3, status: 'orphaned' }),
      scriptRun({ id: 2, status: 'running', runsIn: 'terminal' }),
      scriptRun({ id: 1 }),
    ])
    renderView()

    const runs = await rail().findByRole('button', { name: 'Runs (3 runs in progress)' })

    expect(runs).toHaveAttribute('title', '3 runs in progress')
    expect(within(runs).getByText('3')).toBeInTheDocument()

    platform.listScriptRuns.mockResolvedValue([scriptRun({ id: 4, status: 'running' }), scriptRun({ id: 1 })])
    act(() => {
      emit('scriptruns:updated')
    })

    expect(await rail().findByRole('button', { name: 'Runs (1 run in progress)' })).toBeInTheDocument()
  })

  it('lists the runs once for the rail and Runs alike, and once again for each change', async () => {
    renderView()

    await screen.findByRole('list', { name: 'Runs' })
    expect(platform.listScriptRuns).toHaveBeenCalledExactlyOnceWith({ kinds: [], repo: '' })

    platform.listScriptRuns.mockResolvedValue([scriptRun({ id: 2, status: 'running' }), scriptRun()])
    act(() => {
      emit('scriptruns:updated')
    })

    expect(await rail().findByRole('button', { name: 'Runs (1 run in progress)' })).toBeInTheDocument()
    expect(within(screen.getByRole('list', { name: 'Runs' })).getAllByRole('button')).toHaveLength(2)
    expect(platform.listScriptRuns).toHaveBeenCalledTimes(2)
  })

  it('counts nothing while no run is going', async () => {
    renderView()

    await screen.findByRole('list', { name: 'Runs' })

    expect(rail().getByRole('button', { name: 'Runs' })).not.toHaveAttribute('title')
  })

  it('lists each tool’s status at its foot', async () => {
    platform.listIntegrations.mockResolvedValue(
      integrationTools({
        tools: [
          integrationTool({ state: 'update', latest: { ...integrationTool().latest, version: '1.30.0' } }),
          goPreCommitTool(),
        ],
      }),
    )
    renderView()

    expect(rail().getByText('Looking for the tools…')).toBeInTheDocument()
    expect(await tools()).toEqual(['magex 1.29.51.30.0 available', 'go-pre-commit 1.12.3'])
  })

  it.each<[string, Partial<IntegrationTool>, string]>([
    ['too old', { state: 'tooOld' }, 'magex 1.29.5 is too old'],
    ['won’t run', { state: 'wontRun' }, 'magex won’t run'],
    ['missing', { state: 'missing', on: false }, 'magex isn’t on this Mac'],
    ['off', { switch: 'off', on: false }, 'mage-x is off'],
    ['off, with an update', { switch: 'off', on: false, state: 'update' }, 'mage-x is off'],
  ])('says when a tool is %s', async (_, change, words) => {
    platform.listIntegrations.mockResolvedValue(integrationTools({ tools: [integrationTool(change)] }))
    renderView()

    expect(await tools()).toEqual([words])
  })

  it('says why the tools couldn’t be listed', async () => {
    platform.listIntegrations.mockRejectedValue(new Error('database is locked'))
    renderView()

    expect(await rail().findByRole('alert')).toHaveTextContent('Couldn’t list the tools: database is locked')
    expect(rail().queryByText('Looking for the tools…')).not.toBeInTheDocument()
  })

  it('reads the tools again when they change', async () => {
    renderView()

    expect(await tools()).toHaveLength(2)

    platform.listIntegrations.mockResolvedValue(integrationTools({ tools: [integrationTool()] }))
    act(() => {
      emit('tools:updated')
    })

    await waitFor(async () => {
      expect(await tools()).toEqual(['magex 1.29.5'])
    })
  })

  it('opens the tools’ settings from its foot', async () => {
    const { onManageSettings } = renderView()

    await userEvent.click(rail().getByRole('button', { name: 'Manage in Settings' }))

    expect(onManageSettings).toHaveBeenCalledOnce()
    await screen.findByRole('list', { name: 'Runs' })
  })

  it('names the terminal runs went to before the settings load', async () => {
    platform.listScriptRuns.mockResolvedValue([scriptRun({ runsIn: 'terminal' })])
    render(<ScriptsView onManageSettings={vi.fn()} />, { wrapper: at() })

    expect(await screen.findByText(/In your terminal/)).toBeInTheDocument()
  })
})
