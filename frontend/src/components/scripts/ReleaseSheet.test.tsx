import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { ReleaseCandidate, ReleasePrepareRequest, RunsIn } from '../../platform'
import { at } from '../../test/clock'
import { releaseCandidate, releaseCheck, releaseOffer, releaseResult, releaseRun } from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import { ReleaseSheet, type ReleaseTarget } from './ReleaseSheet'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

interface Options {
  target?: Partial<ReleaseTarget>
  candidate?: ReleaseCandidate | undefined
  runsIn?: RunsIn
}

function renderSheet(options: Options = {}) {
  const { target = {}, runsIn = 'app' } = options
  const candidate = 'candidate' in options ? options.candidate : releaseCandidate()
  const handlers = {
    onOpenSettings: vi.fn<() => void>(),
    onWatch: vi.fn<(id: number) => void>(),
    onClose: vi.fn<() => void>(),
  }

  render(
    <ReleaseSheet
      target={{ repo: 'acme-org/sdk', dir: '~/projects/sdk', branch: '', ...target }}
      candidate={candidate}
      runsIn={runsIn}
      terminal="iterm2"
      {...handlers}
    />,
    { wrapper: at() },
  )

  return handlers
}

// sheet is the open sheet, once its offer shows.
async function sheet() {
  const dialog = await screen.findByRole('dialog', { name: 'Release acme-org/sdk' })

  await within(dialog).findByRole('list', { name: 'Checks' })

  return within(dialog)
}

// lastRequest is the last Prepare the sheet made.
function lastRequest(): ReleasePrepareRequest | undefined {
  return platform.prepareRelease.mock.lastCall?.[0]
}

// offerFor answers Prepare with the offer for its bump, as Decide works one out.
function offerFor(req: ReleasePrepareRequest) {
  const versions = { patch: 'v1.4.3', minor: 'v1.5.0', major: 'v2.0.0' } as const
  const version = versions[req.bump]
  const major = req.bump === 'major'
  const confirmed = major && req.typed === 'v2.0.0'

  return Promise.resolve(
    releaseOffer({
      bump: req.bump,
      version,
      button: `Release ${version}${major ? ' (major)' : ''}${req.runsIn === 'terminal' ? ' in iTerm2' : ''}`,
      ready: !major || confirmed,
      major: { ...releaseOffer().major, confirmed },
    }),
  )
}

describe('ReleaseSheet', () => {
  it('checks the clone as it opens, then shows the checks, the commits, the versions, and the button', async () => {
    renderSheet()

    expect(screen.getByRole('status')).toHaveTextContent('Checking the clone, origin, and GitHub…')

    const s = await sheet()

    expect(lastRequest()).toEqual({
      repo: 'acme-org/sdk',
      dir: '~/projects/sdk',
      branch: '',
      bump: 'patch',
      typed: '',
      allowModulePath: false,
      runsIn: 'app',
      refresh: true,
    })
    expect(s.getByText('master')).toBeInTheDocument()
    expect(s.getByText('the default branch on GitHub')).toBeInTheDocument()
    expect(within(s.getByRole('list', { name: 'Checks' })).getAllByRole('listitem')[0]).toHaveTextContent(
      'Passed: No uncommitted changes',
    )
    expect(s.getByRole('radio', { name: /Patch/ })).toBeChecked()
    expect(s.getByText(/magex switches to master, pulls/)).toBeInTheDocument()
    expect(s.getByRole('button', { name: 'Release v1.4.3' })).toBeEnabled()
    expect(s.getByRole('radio', { name: brand.name })).toBeChecked()
  })

  it('lists the commits since the newest version, three, then the rest, each opening on GitHub', async () => {
    renderSheet({ candidate: releaseCandidate({ ahead: 12 }) })

    const s = await sheet()
    const section = within(s.getByRole('region', { name: /What’s in it · 12 commits since v1\.4\.2/ }))

    expect(section.getAllByRole('listitem')).toHaveLength(3)

    await userEvent.click(section.getByRole('button', { name: 'Fix retries on 502 (#45)' }))
    expect(platform.openExternal).toHaveBeenCalledWith(
      'https://github.com/acme-org/sdk/commit/a1b2c3d4e5f60718293a4b5c6d7e8f9012345678',
    )

    await userEvent.click(section.getByRole('button', { name: /^9 more commits$/ }))
    expect(section.getAllByRole('listitem')).toHaveLength(4)

    await userEvent.click(section.getByRole('button', { name: /8 more commits on GitHub/ }))
    expect(platform.openExternal).toHaveBeenLastCalledWith('https://github.com/acme-org/sdk/compare/v1.4.2...master')
  })

  it('leaves the commits out while the page hasn’t listed the repository', async () => {
    renderSheet({ candidate: undefined })

    const s = await sheet()

    expect(s.queryByRole('heading', { name: /What’s in it/ })).not.toBeInTheDocument()
  })

  it('decides again on the facts it has for each choice, and reads them afresh only on Check again', async () => {
    platform.prepareRelease.mockImplementation(offerFor)
    platform.prepareRelease.mockImplementationOnce((req) =>
      offerFor(req).then((o) => ({
        ...o,
        checks: [releaseCheck({ id: 'dirty', state: 'block', text: '3 uncommitted changes', fixes: ['checkAgain'] })],
      })),
    )
    renderSheet()

    const s = await sheet()

    await userEvent.click(s.getByRole('radio', { name: /Minor/ }))
    await waitFor(() => {
      expect(lastRequest()).toMatchObject({ bump: 'minor', refresh: false })
    })
    expect(await s.findByRole('button', { name: 'Release v1.5.0' })).toBeInTheDocument()

    await userEvent.click(s.getByRole('radio', { name: 'iTerm2' }))
    await waitFor(() => {
      expect(lastRequest()).toMatchObject({ runsIn: 'terminal', refresh: false })
    })
    expect(await s.findByRole('button', { name: 'Release v1.5.0 in iTerm2' })).toBeInTheDocument()
    expect(s.getByText(/For this release only/)).toBeInTheDocument()

    platform.prepareRelease.mockImplementationOnce((req) =>
      offerFor(req).then((o) => ({
        ...o,
        checks: [releaseCheck({ id: 'dirty', state: 'block', text: '3 uncommitted changes', fixes: ['checkAgain'] })],
      })),
    )
    await userEvent.click(s.getByRole('radio', { name: brand.name }))
    await userEvent.click(await s.findByRole('button', { name: 'Check again' }))
    await waitFor(() => {
      expect(lastRequest()).toMatchObject({ runsIn: 'app', refresh: true })
    })
  })

  it('offers the other branch Release can release from, and says when it isn’t the default', async () => {
    const branches = [
      { name: 'master', default: true },
      { name: 'main', default: false },
    ]

    platform.prepareRelease.mockImplementation((req) =>
      Promise.resolve(
        releaseOffer({
          branch: req.branch === '' ? 'master' : req.branch,
          branches,
          branchNote: req.branch === 'main' ? 'main isn’t the default branch on GitHub (master is).' : '',
        }),
      ),
    )
    renderSheet()

    const s = await sheet()

    await userEvent.selectOptions(s.getByRole('combobox', { name: 'Branch' }), 'main')

    expect(await s.findByText('main isn’t the default branch on GitHub (master is).')).toBeInTheDocument()
    expect(lastRequest()).toMatchObject({ branch: 'main', refresh: false })
  })

  it('says Major needs a newer magex, and offers to update it in Settings', async () => {
    const offer = releaseOffer()

    platform.prepareRelease.mockResolvedValue({
      ...offer,
      bumps: offer.bumps.map((b) =>
        b.bump === 'major'
          ? { ...b, offered: false, note: 'Needs magex 1.30.0 or later', fixes: ['update' as const] }
          : b,
      ),
    })
    const { onOpenSettings } = renderSheet()

    const s = await sheet()

    expect(s.getByRole('radio', { name: /Major/ })).toBeDisabled()
    expect(s.getByRole('radio', { name: /Major/ })).toHaveAccessibleDescription('Needs magex 1.30.0 or later')

    await userEvent.click(s.getByRole('button', { name: 'Update magex in Settings' }))
    expect(onOpenSettings).toHaveBeenCalledOnce()
  })

  it('takes a major only once its version is typed exactly, and the module path is accepted', async () => {
    platform.prepareRelease.mockImplementation((req) =>
      offerFor(req).then((o) => ({
        ...o,
        major: {
          ...o.major,
          modulePath: 'github.com/acme-org/sdk',
          moduleWant: 'github.com/acme-org/sdk/v2',
          moduleText:
            'go.mod says module github.com/acme-org/sdk. Go can’t install v2.0.0 until it says github.com/acme-org/sdk/v2.',
        },
        ready: o.major.confirmed && req.allowModulePath,
      })),
    )
    renderSheet()

    const s = await sheet()

    await userEvent.click(s.getByRole('radio', { name: /Major/ }))

    const panel = within(await s.findByRole('region', { name: 'A major release is a breaking change' }))
    const field = panel.getByRole('textbox', { name: 'Type v2.0.0 to confirm' })
    const button = await s.findByRole('button', { name: 'Release v2.0.0 (major)' })

    expect(field).toHaveValue('')
    expect(button).toBeDisabled()
    expect(panel.getByText(/^go\.mod says module github\.com\/acme-org\/sdk\./).parentElement).toHaveTextContent(
      /^Blocks the release:/,
    )

    await userEvent.type(field, 'v2.0')
    expect(panel.getByText('Doesn’t match v2.0.0 yet')).toBeInTheDocument()
    expect(platform.prepareRelease.mock.calls.every(([req]) => req.typed === '')).toBe(true)

    await userEvent.type(field, '.0')
    expect(panel.getByText('Matches')).toBeInTheDocument()
    await waitFor(() => {
      expect(lastRequest()).toMatchObject({ bump: 'major', typed: 'v2.0.0' })
    })
    expect(button).toBeDisabled()

    await userEvent.click(
      panel.getByRole('checkbox', { name: /Release anyway: this module isn’t meant to be imported/ }),
    )
    await waitFor(() => {
      expect(button).toBeEnabled()
    })
    expect(lastRequest()).toMatchObject({ typed: 'v2.0.0', allowModulePath: true })

    // Choosing another version and Major again starts the field empty.
    await userEvent.click(s.getByRole('radio', { name: /Patch/ }))
    await userEvent.click(s.getByRole('radio', { name: /Major/ }))
    expect(s.getByRole('textbox', { name: 'Type v2.0.0 to confirm' })).toHaveValue('')
  })

  it('calls v1.0.0 the first stable release', async () => {
    platform.prepareRelease.mockImplementation((req) =>
      offerFor(req).then((o) => ({ ...o, major: { ...o.major, version: 'v1.0.0', firstStable: true } })),
    )
    renderSheet()

    const s = await sheet()

    await userEvent.click(s.getByRole('radio', { name: /Major/ }))

    const panel = await s.findByRole('region', { name: 'v1.0.0 is your first stable release' })

    expect(panel).toHaveTextContent(/needs a v2/)
  })

  it('shows the command it runs, to copy', async () => {
    platform.prepareRelease.mockResolvedValue(
      releaseOffer({ argv: ['/Users/example/Library/Tool Folder/magex', 'version:bump', "it's"] }),
    )
    renderSheet()

    const s = await sheet()

    await userEvent.click(s.getByText('Command'))
    await userEvent.click(s.getByRole('button', { name: /^Copy / }))

    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith(
      `'/Users/example/Library/Tool Folder/magex' version:bump 'it'\\''s'`,
    )
    expect(await s.findByText('Copied')).toBeInTheDocument()
  })

  it('offers each check’s fixes: settings, the terminal, GitHub, the commands, and the run holding the clone', async () => {
    platform.prepareRelease.mockResolvedValue(
      releaseOffer({
        ready: false,
        checks: [
          releaseCheck({ id: 'tool', state: 'block', text: 'magex 1.27.0 is older', fixes: ['update'] }),
          releaseCheck({
            id: 'localOnly',
            state: 'block',
            text: 'v1.4.3 is tagged on this Mac but isn’t on GitHub.',
            fixes: ['openTerminal'],
            commands: ['git push origin v1.4.3', 'git tag -d v1.4.3'],
          }),
          releaseCheck({
            id: 'checks',
            state: 'warn',
            text: 'Checks are failing on a1b2c3d',
            fixes: ['view'],
            link: 'https://github.com/acme-org/sdk/commit/a1b2c3d/checks',
          }),
          releaseCheck({ id: 'hooks', state: 'info', text: 'This clone has a pre-push hook.' }),
          releaseCheck({
            id: 'busy',
            state: 'block',
            text: 'A release is using acme-org/sdk.',
            fixes: ['watch'],
            runId: 4,
          }),
        ],
      }),
    )
    const { onOpenSettings, onWatch } = renderSheet()

    const s = await sheet()
    const items = within(s.getByRole('list', { name: 'Checks' })).getAllByRole('listitem')

    expect(items.map((li) => li.querySelector('.sr-only')?.textContent)).toEqual([
      'Blocks the release: ',
      'Blocks the release: ',
      'Warning: ',
      'Note: ',
      'Blocks the release: ',
    ])
    expect(s.getByRole('button', { name: 'Release v1.4.3' })).toBeDisabled()

    await userEvent.click(s.getByRole('button', { name: 'Update magex in Settings' }))
    expect(onOpenSettings).toHaveBeenCalledOnce()

    await userEvent.click(s.getByRole('button', { name: 'Open in terminal' }))
    expect(platform.openReleaseTerminal).toHaveBeenCalledExactlyOnceWith('acme-org/sdk', '~/projects/sdk')

    platform.openReleaseTerminal.mockRejectedValue(new Error('Your terminal can’t be opened here.'))
    await userEvent.click(s.getByRole('button', { name: 'Open in terminal' }))
    expect(await s.findByText('Your terminal can’t be opened here.')).toHaveAttribute('role', 'alert')

    await userEvent.click(s.getByRole('button', { name: 'Copy git push origin v1.4.3' }))
    expect(platform.copyText).toHaveBeenCalledWith('git push origin v1.4.3')

    await userEvent.click(s.getByRole('button', { name: 'View on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/sdk/commit/a1b2c3d/checks')

    await userEvent.click(s.getByRole('button', { name: 'Watch the run' }))
    expect(onWatch).toHaveBeenCalledExactlyOnceWith(4)
  })

  it('says why the clone couldn’t be checked, and checks again', async () => {
    platform.prepareRelease.mockRejectedValueOnce(new Error('no clone of the repository'))
    renderSheet()

    const dialog = within(await screen.findByRole('dialog', { name: 'Release acme-org/sdk' }))

    expect(await dialog.findByRole('alert')).toHaveTextContent('Couldn’t check the release: no clone of the repository')

    await userEvent.click(dialog.getByRole('button', { name: 'Check again' }))

    expect(await dialog.findByRole('list', { name: 'Checks' })).toBeInTheDocument()
    expect(lastRequest()).toMatchObject({ refresh: true })
  })

  it('keeps the checks shown when checking again fails, and says so', async () => {
    renderSheet()

    const s = await sheet()

    platform.prepareRelease.mockRejectedValueOnce(new Error('origin timed out'))
    await userEvent.click(s.getByRole('radio', { name: /Minor/ }))

    expect(await s.findByText('Couldn’t check again: origin timed out')).toBeInTheDocument()
    expect(s.getByRole('list', { name: 'Checks' })).toBeInTheDocument()
  })

  it('releases, following the steps as its progress moves, then shows how it came out', async () => {
    const going = (step: string, working: string) =>
      releaseRun({
        status: 'running',
        exitCode: null,
        finishedAt: '',
        summary: '',
        progress: { step, working, stopRefused: '', stopConfirm: '' },
        canStop: true,
      })

    platform.getScriptRun.mockResolvedValue(going('onBranch', 'updated'))
    const { onClose } = renderSheet()

    const s = await sheet()

    await userEvent.click(s.getByRole('button', { name: 'Release v1.4.3' }))

    expect(platform.startRelease).toHaveBeenCalledExactlyOnceWith({
      repo: 'acme-org/sdk',
      dir: '~/projects/sdk',
      branch: 'master',
      bump: 'patch',
      version: 'v1.4.3',
      head: 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678',
      original: 'feature-x',
      typed: '',
      allowModulePath: false,
      runsIn: 'app',
    })

    const dialog = within(await screen.findByRole('dialog', { name: 'Releasing acme-org/sdk v1.4.3' }))
    const steps = () =>
      within(dialog.getByRole('list', { name: 'Steps' }))
        .getAllByRole('listitem')
        .map((li) => li.textContent)

    await waitFor(() => {
      expect(steps()[1]).toBe('Now: Pulling master from origin…')
    })
    expect(dialog.getByText('Closing this doesn’t stop the release: it goes on under Runs.')).toBeInTheDocument()

    // Another run's progress reads nothing; this one's reads it again.
    const reads = platform.getScriptRun.mock.calls.length

    platform.getScriptRun.mockResolvedValue(going('tagCreated', 'tagPushed'))
    act(() => {
      emit('scriptruns:progress', { id: 99 })
    })
    expect(platform.getScriptRun).toHaveBeenCalledTimes(reads)

    act(() => {
      emit('scriptruns:progress', { id: 7 })
    })

    await waitFor(() => {
      expect(steps()).toEqual([
        'Done: Switched to master',
        'Done: Pulled master from origin',
        'Done: Created tag v1.4.3',
        'Now: Pushing to origin…',
        'Next: Checking that GitHub has it',
      ])
    })
    expect(await dialog.findByRole('button', { name: 'Stop' })).toBeInTheDocument()

    platform.getScriptRun.mockResolvedValue(releaseRun())
    act(() => {
      emit('scriptruns:updated')
    })

    expect(await dialog.findByRole('heading', { name: 'acme-org/sdk v1.4.3 is on GitHub' })).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Done' }))
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('reads afresh after Check again even when a choice replaces that read before it is answered', async () => {
    platform.prepareRelease.mockResolvedValue(releaseOffer({ checks: [releaseCheck({ fixes: ['checkAgain'] })] }))
    renderSheet()

    const s = await sheet()

    // The fresh read Check again asks for hangs, and a new choice replaces it.
    platform.prepareRelease.mockImplementationOnce(() => new Promise(() => undefined))
    await userEvent.click(s.getByRole('button', { name: 'Check again' }))
    await waitFor(() => {
      expect(lastRequest()).toMatchObject({ bump: 'patch', refresh: true })
    })

    await userEvent.click(s.getByRole('radio', { name: /Minor/ }))
    await waitFor(() => {
      expect(lastRequest()).toMatchObject({ bump: 'minor', refresh: true })
    })
    await waitFor(() => {
      expect(s.queryByText('Checking again…')).not.toBeInTheDocument()
    })

    // Once a fresh read was answered, the next choice decides on it.
    await userEvent.click(s.getByRole('radio', { name: /Patch/ }))
    await waitFor(() => {
      expect(lastRequest()).toMatchObject({ bump: 'patch', refresh: false })
    })
  })

  it('reads afresh again when the fresh read failed', async () => {
    platform.prepareRelease.mockRejectedValueOnce(new Error('origin timed out'))
    renderSheet()

    const dialog = within(await screen.findByRole('dialog', { name: 'Release acme-org/sdk' }))

    expect(await dialog.findByRole('alert')).toHaveTextContent('Couldn’t check the release: origin timed out')

    await userEvent.click(dialog.getByRole('button', { name: 'Check again' }))
    await dialog.findByRole('list', { name: 'Checks' })

    expect(platform.prepareRelease.mock.calls.map(([req]) => req.refresh)).toEqual([true, true])
  })

  it('says why a release didn’t start, and checks again, showing the version it moved to', async () => {
    platform.startRelease.mockRejectedValue(new Error('v1.4.3 was released a moment ago. The next patch is v1.4.4.'))
    renderSheet()

    const s = await sheet()

    platform.prepareRelease.mockResolvedValue(releaseOffer({ version: 'v1.4.4', button: 'Release v1.4.4' }))
    await userEvent.click(s.getByRole('button', { name: 'Release v1.4.3' }))

    expect(await s.findByRole('alert')).toHaveTextContent('v1.4.3 was released a moment ago. The next patch is v1.4.4.')
    expect(await s.findByRole('button', { name: 'Release v1.4.4' })).toBeInTheDocument()
    expect(lastRequest()).toMatchObject({ refresh: true })

    await userEvent.click(s.getByRole('radio', { name: /Minor/ }))
    expect(s.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('says when the release’s run can’t be read', async () => {
    platform.getScriptRun.mockRejectedValue(new Error('database is locked'))
    renderSheet()

    const s = await sheet()

    await userEvent.click(s.getByRole('button', { name: 'Release v1.4.3' }))

    expect(await screen.findByText('Couldn’t read the release’s run: database is locked')).toBeInTheDocument()
  })

  it('goes back to the sheet with fresh checks from a failed release’s Try again', async () => {
    platform.getScriptRun.mockResolvedValue(releaseRun({ status: 'failed' }))
    platform.getReleaseResult.mockResolvedValue(
      releaseResult({
        kind: 'failed',
        tagged: false,
        failure: {
          kind: 'dirty',
          heading: 'The clone has uncommitted changes. Nothing was tagged.',
          quote: '',
          bug: false,
        },
      }),
    )
    renderSheet()

    const s = await sheet()

    await userEvent.click(s.getByRole('button', { name: 'Release v1.4.3' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Try again' }))

    const again = await sheet()

    expect(again.getByRole('button', { name: 'Release v1.4.3' })).toBeInTheDocument()
    expect(lastRequest()).toMatchObject({ refresh: true, typed: '' })
  })

  it('closes from its close button and Cancel', async () => {
    const { onClose } = renderSheet()

    const s = await sheet()

    await userEvent.click(s.getByRole('button', { name: 'Close the release sheet' }))
    await userEvent.click(s.getByRole('button', { name: 'Cancel' }))

    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('names the release by its repository while there is no button text to show', async () => {
    platform.prepareRelease.mockResolvedValue(releaseOffer({ button: '', ready: false, branches: [], branch: '' }))
    renderSheet()

    const s = await sheet()

    expect(s.getByRole('button', { name: 'Release acme-org/sdk' })).toBeDisabled()
    expect(s.getByText('unknown')).toBeInTheDocument()
  })
})
