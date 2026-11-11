import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { ReleaseRequest } from '../../lib/release'
import type { SettingsSection } from '../../lib/settingsIndex'
import { useIntegrations } from '../../lib/useIntegrations'
import { useScriptRuns } from '../../lib/useScriptRuns'
import type { IntegrationTool, TerminalID } from '../../platform'
import { at } from '../../test/clock'
import {
  goPreCommitTool,
  hookRepo,
  hooksList,
  hooksRun,
  integrationTool,
  integrationTools,
  releaseCandidate,
  releaseCandidates,
  releaseCheck,
  releaseOffer,
  releaseRun,
  scriptRun,
} from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import ScriptsView from './ScriptsView'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

interface TabProps {
  terminal?: TerminalID | null
  request?: ReleaseRequest | null
  onOpenSettings: (section: SettingsSection) => void
}

// Tab is the tab as the window shows it: fed the tools and the runs the window lists.
function Tab({ terminal = 'iterm2', request = null, onOpenSettings }: TabProps) {
  return (
    <ScriptsView
      terminal={terminal}
      runsIn="app"
      cloneRoot="~/projects"
      integrations={useIntegrations()}
      runs={useScriptRuns()}
      request={request}
      onOpenSettings={onOpenSettings}
    />
  )
}

function renderView(props: Partial<TabProps> = {}) {
  const onOpenSettings = vi.fn<(section: SettingsSection) => void>()
  const view = render(<Tab onOpenSettings={onOpenSettings} {...props} />, { wrapper: at() })

  return { onOpenSettings, view }
}

// magexOff lists mage-x and go-pre-commit switched off, so the tab has Runs alone.
function magexOff() {
  platform.listIntegrations.mockResolvedValue(
    integrationTools({
      tools: [integrationTool({ switch: 'off', on: false }), goPreCommitTool({ switch: 'off', on: false })],
    }),
  )
}

// hooksOnly lists mage-x switched off and go-pre-commit on, so the tab has Hooks and Runs.
function hooksOnly(change: Partial<IntegrationTool> = {}) {
  platform.listIntegrations.mockResolvedValue(
    integrationTools({ tools: [integrationTool({ switch: 'off', on: false }), goPreCommitTool(change)] }),
  )
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

// request asks for acme-org/sdk's sheet, as a merge's toast does.
function request(seq: number): ReleaseRequest {
  return { repo: 'acme-org/sdk', dir: '~/projects/sdk', branch: 'master', seq }
}

describe('ScriptsView', () => {
  it('opens on Release while mage-x is on, with Hooks and Runs after it, and keeps the page used last', async () => {
    renderView()

    const release = await rail().findByRole('button', { name: /^Release/ })

    expect(release).toHaveAttribute('aria-current', 'page')
    expect(await screen.findByRole('heading', { name: 'Release', level: 2 })).toBeInTheDocument()
    expect(rail().queryByRole('button', { name: /Repo scripts/ })).not.toBeInTheDocument()
    expect(
      rail()
        .getAllByRole('button')
        .map((b) => b.textContent),
    ).toEqual([expect.stringMatching(/^Release/), expect.stringMatching(/^Hooks/), 'Runs', 'Manage in Settings'])

    await userEvent.click(rail().getByRole('button', { name: /^Runs/ }))
    expect(rail().getByRole('button', { name: /^Runs/ })).toHaveAttribute('aria-current', 'page')
    expect(await screen.findByRole('heading', { name: 'Runs', level: 2 })).toBeInTheDocument()
  })

  it('opens on Runs while mage-x is off, and leaves Release when mage-x goes off', async () => {
    magexOff()
    renderView()

    expect(await screen.findByRole('list', { name: 'Runs' })).toBeInTheDocument()
    expect(rail().getAllByRole('button', { name: /^(Release|Runs)/ })).toHaveLength(1)
    expect(rail().getByRole('button', { name: /^Runs/ })).toHaveAttribute('aria-current', 'page')
    expect(platform.listReleaseCandidates).not.toHaveBeenCalled()

    platform.listIntegrations.mockResolvedValue(integrationTools())
    act(() => {
      emit('tools:updated')
    })
    await userEvent.click(await rail().findByRole('button', { name: /^Release/ }))
    expect(await screen.findByRole('heading', { name: 'Release', level: 2 })).toBeInTheDocument()

    magexOff()
    act(() => {
      emit('tools:updated')
    })

    expect(await screen.findByRole('heading', { name: 'Runs', level: 2 })).toBeInTheDocument()
    expect(rail().queryByRole('button', { name: /^Release/ })).not.toBeInTheDocument()
  })

  it('counts the repositories with unreleased changes beside Release', async () => {
    platform.listReleaseCandidates.mockResolvedValue(
      releaseCandidates({
        repos: [releaseCandidate(), releaseCandidate({ repo: 'acme-org/cli', dir: '~/projects/cli' })],
      }),
    )
    renderView()

    const release = await rail().findByRole('button', { name: 'Release (2 repositories with unreleased changes)' })

    expect(release).toHaveAttribute('title', '2 repositories with unreleased changes')
    expect(platform.listReleaseCandidates).toHaveBeenCalledWith(false)
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
    magexOff()
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
    magexOff()
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
    ['too old, left automatic', { switch: 'auto', on: false, state: 'tooOld' }, 'magex 1.29.5 is too old'],
    ['won’t run, left automatic', { switch: 'auto', on: false, state: 'wontRun' }, 'magex won’t run'],
    ['off', { switch: 'off', on: false }, 'mage-x is off'],
    ['off, and too old', { switch: 'off', on: false, state: 'tooOld' }, 'mage-x is off'],
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

  it('opens Settings → Integrations from its foot', async () => {
    const { onOpenSettings } = renderView()

    await userEvent.click(rail().getByRole('button', { name: 'Manage in Settings' }))

    expect(onOpenSettings).toHaveBeenCalledExactlyOnceWith('integrations')
    await screen.findByRole('heading', { name: 'Release', level: 2 })
  })

  it('names the terminal runs went to before the settings load', async () => {
    magexOff()
    platform.listScriptRuns.mockResolvedValue([scriptRun({ runsIn: 'terminal' })])
    renderView({ terminal: null })

    expect(await screen.findByText(/In your terminal/)).toBeInTheDocument()
  })

  it('opens a repository’s sheet from its row, and closes it', async () => {
    renderView()

    await userEvent.click(await screen.findByRole('button', { name: 'Release acme-org/sdk…' }))

    const sheet = await screen.findByRole('dialog', { name: 'Release acme-org/sdk' })

    expect(platform.prepareRelease).toHaveBeenCalledWith(
      expect.objectContaining({ repo: 'acme-org/sdk', dir: '~/projects/sdk', branch: '', refresh: true }),
    )
    // The commits since the newest version come from the page's list.
    expect(within(sheet).getByRole('heading', { name: /What’s in it · 4 commits since v1\.4\.2/ })).toBeInTheDocument()

    await userEvent.click(within(sheet).getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    // Opening it again checks everything again.
    await userEvent.click(screen.getByRole('button', { name: 'Release acme-org/sdk…' }))
    await screen.findByRole('dialog', { name: 'Release acme-org/sdk' })
    await waitFor(() => {
      expect(platform.prepareRelease).toHaveBeenLastCalledWith(expect.objectContaining({ refresh: true }))
    })
  })

  it('opens Release and the sheet a merge asks for, once for each request', async () => {
    magexOff()
    const { view, onOpenSettings } = renderView()

    await screen.findByRole('list', { name: 'Runs' })
    platform.listIntegrations.mockResolvedValue(integrationTools())
    act(() => {
      emit('tools:updated')
    })
    await rail().findByRole('button', { name: /^Release/ })

    view.rerender(<Tab request={request(1)} onOpenSettings={onOpenSettings} />)

    const sheet = await screen.findByRole('dialog', { name: 'Release acme-org/sdk' })

    expect(platform.prepareRelease).toHaveBeenCalledWith(
      expect.objectContaining({ repo: 'acme-org/sdk', dir: '~/projects/sdk', branch: 'master', refresh: true }),
    )
    expect(rail().getByRole('button', { name: /^Release/ })).toHaveAttribute('aria-current', 'page')

    await userEvent.click(within(sheet).getByRole('button', { name: 'Close the release sheet' }))
    view.rerender(<Tab request={request(1)} onOpenSettings={onOpenSettings} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    view.rerender(<Tab request={request(2)} onOpenSettings={onOpenSettings} />)
    expect(await screen.findByRole('dialog', { name: 'Release acme-org/sdk' })).toBeInTheDocument()
  })

  it('keeps the sheet open while Open Settings goes to Settings → Integrations', async () => {
    platform.prepareRelease.mockResolvedValue(
      releaseOffer({
        checks: [releaseCheck({ id: 'tool', state: 'block', text: 'magex isn’t available', fixes: ['openSettings'] })],
      }),
    )
    const { onOpenSettings } = renderView({ request: request(1) })

    const sheet = await screen.findByRole('dialog', { name: 'Release acme-org/sdk' })

    await userEvent.click(await within(sheet).findByRole('button', { name: 'Open Settings' }))

    expect(onOpenSettings).toHaveBeenCalledExactlyOnceWith('integrations')
    expect(screen.getByRole('dialog', { name: 'Release acme-org/sdk' })).toBeInTheDocument()
  })

  it('opens a recent release’s run in Runs', async () => {
    platform.listScriptRuns.mockResolvedValue([releaseRun()])
    platform.getScriptRun.mockResolvedValue(releaseRun())
    renderView()

    const recent = await screen.findByRole('list', { name: 'Recent releases' })

    await userEvent.click(within(recent).getByRole('button', { name: /v1\.4\.3/ }))

    expect(rail().getByRole('button', { name: /^Runs/ })).toHaveAttribute('aria-current', 'page')
    expect(await screen.findByRole('heading', { name: 'Release v1.4.3', level: 2 })).toBeInTheDocument()
  })

  it('reads the list again when the shell says a release ended, without forcing GitHub itself', async () => {
    platform.getScriptRun.mockResolvedValue(releaseRun())
    renderView({ request: request(1) })

    const sheet = within(await screen.findByRole('dialog', { name: 'Release acme-org/sdk' }))

    await userEvent.click(await sheet.findByRole('button', { name: 'Release v1.4.3' }))
    expect(await sheet.findByRole('heading', { name: 'acme-org/sdk v1.4.3 is on GitHub' })).toBeInTheDocument()

    platform.listReleaseCandidates.mockResolvedValue(
      releaseCandidates({ repos: [releaseCandidate({ group: 'upToDate', version: 'v1.4.3', ahead: 0 })] }),
    )
    act(() => {
      emit('releases:updated')
    })

    await waitFor(() => {
      expect(rail().getByRole('button', { name: 'Release' })).not.toHaveAttribute('title')
    })
    expect(platform.listReleaseCandidates).not.toHaveBeenCalledWith(true)
  })

  it('reads the list again when a release ends after its sheet was closed', async () => {
    platform.getScriptRun.mockResolvedValue(releaseRun({ status: 'running', exitCode: null, finishedAt: '' }))
    renderView()

    await userEvent.click(await screen.findByRole('button', { name: 'Release acme-org/sdk…' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Release v1.4.3' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    platform.listReleaseCandidates.mockResolvedValue(
      releaseCandidates({ repos: [releaseCandidate({ group: 'upToDate', version: 'v1.4.3', ahead: 0 })] }),
    )
    act(() => {
      emit('releases:updated')
    })

    expect(await screen.findByText('No repository has unreleased changes.')).toBeInTheDocument()
  })

  it('closes the sheet and opens the run that holds the clone, from Watch', async () => {
    platform.prepareRelease.mockResolvedValue(
      releaseOffer({
        ready: false,
        checks: [
          releaseCheck({
            id: 'busy',
            state: 'block',
            text: 'A pre-commit run is using this clone.',
            fixes: ['watch'],
            runId: 9,
          }),
        ],
      }),
    )
    platform.getScriptRun.mockImplementation((id) => Promise.resolve(scriptRun({ id, title: 'Checks · all files' })))
    renderView({ request: request(1) })

    const sheet = await screen.findByRole('dialog', { name: 'Release acme-org/sdk' })

    await userEvent.click(await within(sheet).findByRole('button', { name: 'Watch the run' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Checks · all files', level: 2 })).toBeInTheDocument()
    expect(platform.getScriptRun).toHaveBeenCalledWith(9)
  })
  describe('Hooks', () => {
    it('counts the repositories that need attention beside Hooks, with an amber dot', async () => {
      renderView()

      const hooks = await rail().findByRole('button', { name: 'Hooks (4 repositories need attention)' })

      expect(hooks).toHaveAttribute('title', '4 repositories need attention')
      expect(hooks.querySelector('.text-warning')).not.toBeNull()
      expect(platform.listHooks).toHaveBeenCalledWith(false)

      platform.listHooks.mockResolvedValue(hooksList({ repos: [hookRepo()] }))
      act(() => {
        emit('hooks:updated')
      })

      await waitFor(() => {
        expect(rail().getByRole('button', { name: 'Hooks' })).not.toHaveAttribute('title')
      })
      const dot = rail().getByRole('button', { name: 'Hooks' }).querySelector('.text-warning')

      expect(dot).toBeNull()
    })

    it('counts one repository that needs attention', async () => {
      platform.listHooks.mockResolvedValue(hooksList({ repos: [hooksList().repos[0] ?? hookRepo()] }))
      renderView()

      expect(await rail().findByRole('button', { name: 'Hooks (1 repository needs attention)' })).toBeInTheDocument()
    })

    it('opens on Hooks while mage-x is off, and leaves it when go-pre-commit goes off', async () => {
      hooksOnly()
      renderView()

      expect(await screen.findByRole('heading', { name: 'Hooks', level: 2 })).toBeInTheDocument()
      expect(rail().getByRole('button', { name: /^Hooks/ })).toHaveAttribute('aria-current', 'page')
      expect(rail().queryByRole('button', { name: /^Release/ })).not.toBeInTheDocument()
      expect(platform.listReleaseCandidates).not.toHaveBeenCalled()

      magexOff()
      act(() => {
        emit('tools:updated')
      })

      expect(await screen.findByRole('heading', { name: 'Runs', level: 2 })).toBeInTheDocument()
      expect(rail().queryByRole('button', { name: /^Hooks/ })).not.toBeInTheDocument()
    })

    it('keeps Hooks once chosen, beside Release', async () => {
      renderView()

      await userEvent.click(await rail().findByRole('button', { name: /^Hooks/ }))

      expect(await screen.findByRole('heading', { name: 'Hooks', level: 2 })).toBeInTheDocument()
      expect(await screen.findByRole('list', { name: 'Needs attention' })).toBeInTheDocument()
    })

    it('says why go-pre-commit can’t be used in place of the list, reading nothing', async () => {
      hooksOnly({
        state: 'tooOld',
        copy: { ...goPreCommitTool().copy, version: { raw: '1.10.0', commit: '', built: '' } },
      })
      const { onOpenSettings } = renderView()

      expect(
        await screen.findByText('go-pre-commit 1.10.0 is older than 1.12.0, which Hooks needs.'),
      ).toBeInTheDocument()
      expect(rail().getByRole('button', { name: 'Hooks' })).toBeInTheDocument()
      expect(platform.listHooks).not.toHaveBeenCalled()

      await userEvent.click(screen.getByRole('button', { name: 'Update go-pre-commit in Settings' }))
      expect(onOpenSettings).toHaveBeenCalledExactlyOnceWith('integrations')
    })

    it('opens the run a row starts in Runs, with its result', async () => {
      hooksOnly()
      platform.getScriptRun.mockResolvedValue(hooksRun())
      renderView()

      await userEvent.click(await screen.findByRole('button', { name: 'Install the hook in acme-org/api' }))

      expect(await screen.findByRole('heading', { name: 'Install the hook', level: 2 })).toBeInTheDocument()
      expect(rail().getByRole('button', { name: /^Runs/ })).toHaveAttribute('aria-current', 'page')
      expect(
        await screen.findByRole('heading', { name: /Installed the pre-commit hook in acme-org\/api\./ }),
      ).toBeInTheDocument()
      expect(platform.getScriptRun).toHaveBeenCalledWith(11)
    })

    it('reads which clones are busy again when a run starts in one', async () => {
      hooksOnly()
      renderView()

      await screen.findByRole('list', { name: 'Needs attention' })
      expect(platform.listHooks).toHaveBeenCalledOnce()

      platform.listScriptRuns.mockResolvedValue([hooksRun({ status: 'running', exitCode: null, finishedAt: '' })])
      act(() => {
        emit('scriptruns:updated')
      })

      await waitFor(() => {
        expect(platform.listHooks).toHaveBeenCalledTimes(2)
      })
    })
  })
})
