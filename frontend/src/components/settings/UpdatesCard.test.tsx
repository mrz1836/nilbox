import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import { settlingWords, type RestartStep } from '../../lib/update'
import type { Settings, UpdateStatus } from '../../platform'
import { at } from '../../test/clock'
import { settings, updateLocation, updateReady, updateStatus } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { moveOf, updateOf } from '../../test/query'
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

// words are the Updates row's state, and note what it says under them: each a live region,
// read out as it changes.
function words() {
  return screen.getAllByRole('status')[0]
}

function note() {
  return screen.getAllByRole('status')[1]
}

// line is the whole status under the row's name: the words, a download's percentage or
// when the last check ended, which aren't read out as they change, and the note.
function line() {
  return words()?.closest('div')
}

const releasePage = 'https://github.com/acme-org/example-repo/releases/tag/v0.3.0'

const check = { name: 'Check for Updates' }

describe('UpdatesCard', () => {
  it('says the build is up to date, and when it last checked', () => {
    card(updateStatus())

    expect(line()).toHaveTextContent('Up to date · checked 12m ago')
    // The time ago changes as it ticks, so it stays out of the live region.
    expect(words()).toHaveTextContent(/^Up to date$/)
    expect(note()).toBeEmptyDOMElement()
    expect(screen.getByRole('button', check)).toBeEnabled()
    expect(screen.queryByRole('button', { name: 'Restart to update' })).not.toBeInTheDocument()
  })

  it('says a build that never checked hasn’t', () => {
    card(updateStatus({ lastChecked: '' }))
    expect(line()).toHaveTextContent('Not checked yet')
  })

  it('says nothing while the status loads, offering no check yet', () => {
    card(null)

    expect(words()).toBeEmptyDOMElement()
    expect(screen.queryByText('Checking…')).not.toBeInTheDocument()
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
    // The percentage climbs, so it stays out of the live region.
    expect(words()).toHaveTextContent(/^Downloading 0\.3\.0…$/)
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

  it('offers Try again and Download manually once a restart failed, as the banner does, saying why', async () => {
    const { update } = card(updateReady({ lastError: 'Couldn’t install 0.3.0: no space.' }), {
      step: 'failed',
      message: 'no space',
    })

    expect(words()).toHaveTextContent('0.3.0 is ready')
    expect(note()).toHaveTextContent('Couldn’t install 0.3.0: no space.')
    expect(screen.queryByRole('button', { name: 'Restart to update' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(update.restart).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Download manually' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(releasePage)
  })

  it('says quietly that the last update is still finishing, holding the buttons a moment', () => {
    card(updateReady(), { step: 'waiting', message: settlingWords })

    expect(note()).toHaveTextContent('The last update is still finishing. This can take a minute or two.')
    expect(note()?.firstElementChild).toHaveClass('text-muted')
    expect(screen.getByRole('button', { name: 'Restart to update' })).toBeDisabled()
  })

  it('says the update is finishing as the app quits on its own, holding the buttons', () => {
    card(updateReady({ state: 'finishing' }))

    expect(words()).toHaveTextContent('Finishing the update…')
    expect(screen.queryByRole('button', { name: 'Restart to update' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', check)).toBeDisabled()
  })

  it('offers only the release page after a restart refused for an update that fails for good', () => {
    card(updateReady(), { step: 'failed', message: '0.3.0 didn’t install before.', final: true })

    expect(screen.getByRole('button', { name: 'Download manually' })).toBeEnabled()
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Restart to update' })).not.toBeInTheDocument()
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
        location: { ...updateLocation(), movable: false },
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
          reason: 'macOS didn’t let the update replace the app, so the old one was put back.',
          log: true,
          retryable: true,
        },
      }),
    )

    expect(screen.getByText('The update to 0.3.0 didn’t install')).toBeInTheDocument()
    expect(
      screen.getByText('macOS didn’t let the update replace the app, so the old one was put back.'),
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Show log in Finder' }))
    expect(platform.showInstallLog).toHaveBeenCalledOnce()

    // The version that failed, ready again, is a Try again, never a plain Restart to update.
    expect(screen.queryByRole('button', { name: 'Restart to update' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(update.restart).toHaveBeenCalledOnce()
    await userEvent.click(screen.getByRole('button', { name: 'Download manually' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(releasePage)
  })

  it('opens the page of the version that didn’t install while no version is found since', async () => {
    const final = { version: '0.3.0', reason: 'It failed its signature check.', log: false, retryable: false }

    // With nothing found, the status names the running version's page.
    card(updateStatus({ installFailed: final }))

    await userEvent.click(screen.getByRole('button', { name: 'Download manually' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(releasePage)
  })

  it('offers Download manually alone for an install that can’t be tried again', () => {
    const final = { version: '0.3.0', reason: 'It failed its signature check.', log: false, retryable: false }

    card(updateReady({ state: 'available', installFailed: final }))

    expect(screen.getByRole('button', { name: 'Download manually' })).toBeEnabled()
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Update' })).not.toBeInTheDocument()
    expect(
      screen.getByText('Trying it again would fail the same way, so download it from its release page.'),
    ).toBeInTheDocument()
  })

  it('offers a newer version than the one that failed as any other', () => {
    card(
      updateReady({
        latest: '0.3.1',
        installFailed: { version: '0.3.0', reason: 'It didn’t quit in time.', log: false, retryable: true },
      }),
    )

    expect(screen.getByRole('button', { name: 'Restart to update' })).toBeEnabled()
    expect(screen.getByText('The update to 0.3.0 didn’t install')).toBeInTheDocument()
  })

  it('says why the log couldn’t show, and offers none that wasn’t kept', async () => {
    platform.showInstallLog.mockRejectedValue(new Error('gone', { cause: { message: 'The log is gone.' } }))
    const failed = { version: '0.3.0', reason: 'It didn’t quit in time.', log: true, retryable: true }
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

  it('says the app must be in Applications to update, offering the move and no restart there', async () => {
    const { update } = card(updateReady({ location: updateLocation('translocated') }))

    expect(screen.getByText(`Move ${brand.name} to Applications to update it`)).toBeInTheDocument()
    expect(
      screen.getByText(
        `${brand.name} can only update itself from your Applications folder. Moving it puts it in /Applications, and it reopens from there.`,
      ),
    ).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Restart to update' })).not.toBeInTheDocument()
    // Nothing downloads there, so one kept from before it moved there is only available.
    expect(line()).toHaveTextContent('0.3.0 is available')

    await userEvent.click(screen.getByRole('button', { name: 'Move to Applications' }))
    expect(update.move.move).toHaveBeenCalledOnce()
  })

  it('holds Move to Applications while the move is under way', () => {
    const status = updateStatus({ location: updateLocation('diskImage') })

    render(
      <UpdatesCard
        update={updateOf(
          status,
          { step: 'idle' },
          {
            move: moveOf(status, { dialog: { variant: 'move', work: 'moving', leaving: false, error: '' } }),
          },
        )}
        settings={settings()}
        onSaved={vi.fn()}
      />,
      { wrapper: at() },
    )

    expect(screen.getByRole('button', { name: 'Move to Applications' })).toBeDisabled()
  })

  it('offers no move where the account can’t change the Applications folder the app is in', () => {
    card(updateStatus({ location: updateLocation('applications') }))

    expect(screen.getByText(`${brand.name} can’t update itself here`)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Move to Applications' })).not.toBeInTheDocument()
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
    // The change shows at once and goes back as its save ends, which can be a render
    // after the refusal shows.
    await waitFor(() => {
      expect(select).toHaveValue('download')
    })
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
