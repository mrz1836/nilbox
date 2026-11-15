import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { RestartStep } from '../../lib/update'
import type { Settings, UpdateStatus } from '../../platform'
import { at } from '../../test/clock'
import { settings, updateReady, updateStatus } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { updateOf } from '../../test/query'
import { AboutSection } from './AboutSection'
import { UpdatesCard } from './UpdatesCard'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// card renders the Updates card for a status and a step of the restart, with the settings
// given, and returns the update and onSaved it was given.
function card(status: UpdateStatus | null, flow: RestartStep = { step: 'idle' }, stored: Settings | null = settings()) {
  const update = updateOf(status, flow)
  const onSaved = vi.fn()

  render(<UpdatesCard update={update} settings={stored} onSaved={onSaved} />, { wrapper: at() })

  return { update, onSaved }
}

// line is the Updates row's status, a live region.
function line() {
  return screen.getByRole('status')
}

const check = { name: 'Check for Updates' }

describe('UpdatesCard', () => {
  it('says the build is up to date, and when it last checked', () => {
    card(updateStatus())

    expect(line()).toHaveTextContent('Up to date · checked 12m ago')
    expect(screen.getByRole('button', check)).toBeEnabled()
    expect(screen.queryByRole('button', { name: 'Restart to update' })).not.toBeInTheDocument()
  })

  it('says a build that never checked hasn’t', () => {
    card(updateStatus({ lastChecked: '' }))
    expect(line()).toHaveTextContent('Not checked yet')
  })

  it('says it checks while the status loads, offering no check yet', () => {
    card(null)

    expect(line()).toHaveTextContent('Checking…')
    expect(screen.queryByRole('button', check)).not.toBeInTheDocument()
  })

  it('says a check under way, and holds Check for Updates', () => {
    card(updateStatus({ state: 'checking', checking: true }))

    expect(line()).toHaveTextContent('Checking…')
    expect(screen.getByRole('button', { name: 'Checking…' })).toBeDisabled()
  })

  it('says how far a download has got, holding the buttons', () => {
    card(updateReady({ state: 'downloading', progress: 42 }))

    expect(line()).toHaveTextContent('Downloading 0.3.0… 42%')
    expect(screen.getByRole('button', check)).toBeDisabled()
  })

  it('offers Restart to update for a version ready, which runs the window’s restart', async () => {
    const { update } = card(updateReady())

    expect(line()).toHaveTextContent('0.3.0 is ready')

    await userEvent.click(screen.getByRole('button', { name: 'Restart to update' }))
    expect(update.restart).toHaveBeenCalledOnce()
  })

  it('offers Update for a version available', async () => {
    const { update } = card(updateReady({ state: 'available', mode: 'notify' }))

    expect(line()).toHaveTextContent('0.3.0 is available')

    await userEvent.click(screen.getByRole('button', { name: 'Update' }))
    expect(update.restart).toHaveBeenCalledOnce()
  })

  it('holds Restart to update while the restart is under way', () => {
    card(updateReady(), { step: 'restarting' })

    expect(screen.getByRole('button', { name: 'Restart to update' })).toBeDisabled()
    expect(screen.getByRole('button', check)).toBeDisabled()
  })

  it('lets the buttons go again once a restart failed, saying why under the version', () => {
    card(updateReady({ lastError: 'Couldn’t install 0.3.0: no space.' }), { step: 'failed', message: 'no space' })

    expect(line()).toHaveTextContent('0.3.0 is ready')
    expect(line()).toHaveTextContent('Couldn’t install 0.3.0: no space.')
    expect(screen.getByRole('button', { name: 'Restart to update' })).toBeEnabled()
  })

  it('says quietly that the last update is still finishing, holding the buttons a moment', () => {
    card(updateReady(), { step: 'waiting', message: 'The last update is still finishing. Try again in a moment.' })

    const note = screen.getByText('The last update is still finishing. Try again in a moment.')

    expect(note).toHaveClass('text-muted')
    expect(screen.getByRole('button', { name: 'Restart to update' })).toBeDisabled()
  })

  it('says what to do after a held quit, offering no restart, which can’t work in this run', () => {
    card(updateReady({ lastError: `Couldn’t restart into 0.3.0, because ${brand.name} didn’t quit.` }), {
      step: 'reopen',
      message: `Quit ${brand.name} and open it again, then restart to update.`,
    })

    expect(line()).toHaveTextContent('0.3.0 is ready')
    expect(screen.getByText(`Quit ${brand.name} and open it again, then restart to update.`)).toHaveClass(
      'text-warning',
    )
    expect(line()).not.toHaveTextContent('didn’t quit')
    expect(screen.queryByRole('button', { name: 'Restart to update' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', check)).toBeEnabled()
  })

  it('says the restart under way', () => {
    card(updateReady({ state: 'installing' }))

    expect(line()).toHaveTextContent('Restarting into 0.3.0…')
  })

  it('says the last check failed, and why', () => {
    card(updateStatus({ lastError: 'Couldn’t reach the releases: offline.' }))

    expect(line()).toHaveTextContent('Last check failed: Couldn’t reach the releases: offline.')
  })

  it('says updates are off in a build made from source, holding the choice', () => {
    card(
      updateStatus({
        flavor: 'source',
        enabled: false,
        state: 'off',
        off: 'Updates are off in builds made from source.',
        location: { ok: false, kind: 'elsewhere', target: '/Applications/Example.app' },
      }),
    )

    expect(line()).toHaveTextContent('Updates are off in builds made from source.')
    expect(screen.queryByRole('button', check)).not.toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'When a new version is out' })).toBeDisabled()
    expect(screen.getByText('Kept for builds that update themselves.')).toBeInTheDocument()
    // Where a build that doesn't update runs says nothing.
    expect(screen.queryByText(/to Applications/)).not.toBeInTheDocument()
  })

  it('marks a rehearsal build', () => {
    card(updateReady({ flavor: 'rehearsal' }))

    expect(screen.getByText('Rehearsal build')).toBeInTheDocument()
  })

  it('checks now, saying so while it does, and shows what it found in place', async () => {
    let answer: (status: UpdateStatus) => void = () => undefined
    platform.checkForUpdates.mockReturnValue(
      new Promise((resolve) => {
        answer = resolve
      }),
    )
    const { update } = card(updateStatus())

    await userEvent.click(screen.getByRole('button', check))
    expect(screen.getByRole('button', { name: 'Checking…' })).toBeDisabled()

    answer(updateReady())
    await waitFor(() => {
      expect(screen.getByRole('button', check)).toBeEnabled()
    })
    expect(update.reload).toHaveBeenCalledOnce()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('says why a check couldn’t run', async () => {
    platform.checkForUpdates.mockRejectedValue(new Error('off', { cause: { message: 'Updates are off.' } }))
    card(updateStatus())

    await userEvent.click(screen.getByRole('button', check))

    expect(await screen.findByRole('alert')).toHaveTextContent('Updates are off.')
    expect(screen.getByRole('button', check)).toHaveAccessibleDescription(/Updates are off\./)
  })

  it('says the last launch’s update didn’t install, and why, and shows its log in Finder', async () => {
    const { update } = card(
      updateReady({
        installFailed: {
          version: '0.3.0',
          reason: 'The new version failed its signature check, so the old one was put back.',
          log: true,
        },
      }),
    )

    expect(screen.getByText('The update to 0.3.0 didn’t install')).toBeInTheDocument()
    expect(
      screen.getByText('The new version failed its signature check, so the old one was put back.'),
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Show log in Finder' }))
    expect(platform.showInstallLog).toHaveBeenCalledOnce()

    // The version that failed, ready again, is a Try again, never a plain Restart to update.
    expect(screen.queryByRole('button', { name: 'Restart to update' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(update.restart).toHaveBeenCalledOnce()
  })

  it('offers a newer version than the one that failed as any other', () => {
    card(updateReady({ latest: '0.3.1', installFailed: { version: '0.3.0', reason: 'It wouldn’t open.', log: false } }))

    expect(screen.getByRole('button', { name: 'Restart to update' })).toBeEnabled()
    expect(screen.getByText('The update to 0.3.0 didn’t install')).toBeInTheDocument()
  })

  it('says why the log couldn’t show, and offers none that wasn’t kept', async () => {
    platform.showInstallLog.mockRejectedValue(new Error('gone', { cause: { message: 'The log is gone.' } }))
    const failed = { version: '0.3.0', reason: 'It wouldn’t open.', log: true }
    const { unmount } = render(
      <UpdatesCard update={updateOf(updateReady({ installFailed: failed }))} settings={null} onSaved={vi.fn()} />,
      { wrapper: at() },
    )

    await userEvent.click(screen.getByRole('button', { name: 'Show log in Finder' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('The log is gone.')
    unmount()

    card(updateReady({ installFailed: { ...failed, log: false } }))
    expect(screen.queryByRole('button', { name: 'Show log in Finder' })).not.toBeInTheDocument()
  })

  it('says the app must be in Applications to update, and offers no restart there', () => {
    card(updateReady({ location: { ok: false, kind: 'translocated', target: '/Applications/Example.app' } }))

    expect(screen.getByText(`Move ${brand.name} to Applications to update it`)).toBeInTheDocument()
    expect(
      screen.getByText(
        `${brand.name} can only update itself from your Applications folder. Move it to /Applications, then open it from there.`,
      ),
    ).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Restart to update' })).not.toBeInTheDocument()
  })

  it('chooses what happens when a new version is out, and reads the status again', async () => {
    const stored = settings({ updateMode: 'notify' })
    platform.setUpdateMode.mockResolvedValue(stored)
    const { update, onSaved } = card(updateStatus())
    const select = screen.getByRole('combobox', { name: 'When a new version is out' })

    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual([
      'Download it and tell me',
      'Just tell me',
      'Don’t check',
    ])
    expect(select).toHaveValue('download')
    expect(select).toHaveAccessibleDescription(
      `${brand.name} looks a minute after it opens, then every 6 hours. Check for Updates works with every choice.`,
    )

    await userEvent.selectOptions(select, 'notify')

    expect(platform.setUpdateMode).toHaveBeenCalledExactlyOnceWith('notify')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledExactlyOnceWith(stored)
    })
    expect(update.reload).toHaveBeenCalledOnce()
  })

  it('reads the choice from the settings, not the status', () => {
    card(updateStatus({ mode: 'download' }), { step: 'idle' }, settings({ updateMode: 'off' }))

    expect(screen.getByRole('combobox', { name: 'When a new version is out' })).toHaveValue('off')
  })

  it('says why a choice was refused, and shows the stored one again', async () => {
    platform.setUpdateMode.mockRejectedValue(new Error('locked', { cause: { message: 'The settings are locked.' } }))
    const { update } = card(updateStatus())
    const select = screen.getByRole('combobox', { name: 'When a new version is out' })

    await userEvent.selectOptions(select, 'off')

    expect(await screen.findByRole('alert')).toHaveTextContent('The settings are locked.')
    expect(select).toHaveValue('download')
    expect(update.reload).not.toHaveBeenCalled()
  })

  it('leaves the choice out until the settings load', () => {
    card(updateStatus(), { step: 'idle' }, null)

    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
  })
})

describe('AboutSection’s updates', () => {
  it('shows the Updates card under the version, given the update', async () => {
    render(<AboutSection update={updateOf(updateReady())} settings={settings()} onSaved={vi.fn()} />, {
      wrapper: at(),
    })

    expect(await screen.findByRole('button', { name: 'Restart to update' })).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'When a new version is out' })).toBeInTheDocument()
  })

  it('leaves the card out without the update', () => {
    render(<AboutSection />, { wrapper: at() })

    expect(screen.queryByRole('button', check)).not.toBeInTheDocument()
  })

  it('shows the card without the settings, leaving the choice out', async () => {
    render(<AboutSection update={updateOf(updateStatus())} />, { wrapper: at() })

    expect(await screen.findByRole('button', check)).toBeInTheDocument()
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
  })
})
