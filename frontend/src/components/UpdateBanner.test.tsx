import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import { settlingWords, type RestartStep } from '../lib/update'
import type { UpdateStatus } from '../platform'
import { updateReady, updateStatus } from '../test/fixtures'
import { platform } from '../test/platform'
import { updateOf } from '../test/query'
import { UpdateBanner } from './UpdateBanner'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const releasePage = 'https://github.com/acme-org/example-repo/releases/tag/v0.3.0'

// shown renders the banner for a status and a step of the restart, and returns the update
// it was given, with its moves as mocks, the onDetails it was given, and a rerender with
// another step.
function shown(status: UpdateStatus | null, flow: RestartStep = { step: 'idle' }) {
  const update = updateOf(status, flow)
  const onDetails = vi.fn()
  const { rerender } = render(<UpdateBanner update={update} onDetails={onDetails} />)

  return {
    update,
    onDetails,
    showStep: (next: RestartStep) => {
      rerender(<UpdateBanner update={updateOf(status, next)} onDetails={onDetails} />)
    },
  }
}

// buttons are the banner's buttons, by their words.
function buttons() {
  return within(banner())
    .queryAllByRole('button')
    .map((b) => b.getAttribute('aria-label') ?? b.textContent)
}

// banner is the banner's live region.
function banner() {
  return screen.getByRole('status', { name: `${brand.name} update` })
}

describe('UpdateBanner', () => {
  it('keeps its live region, named, and shows nothing while up to date', () => {
    shown(updateStatus())

    expect(banner()).toBeEmptyDOMElement()
    expect(banner()).toHaveAttribute('aria-live', 'polite')
  })

  it('offers a version ready: Restart to update, What’s new, and Later', async () => {
    const { update } = shown(updateReady())

    expect(banner()).toHaveTextContent(`${brand.name} 0.3.0 is ready.`)

    await userEvent.click(screen.getByRole('button', { name: 'Restart to update' }))
    expect(update.restart).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'What’s new' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(releasePage)

    await userEvent.click(screen.getByRole('button', { name: 'Later' }))
    expect(update.later).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: 'Later' })).toHaveAttribute('title', 'Hide this for a day')
  })

  it('offers a version available: Update downloads it, then restarts', async () => {
    const { update } = shown(updateReady({ state: 'available', mode: 'notify' }))

    expect(banner()).toHaveTextContent(`${brand.name} 0.3.0 is available.`)

    await userEvent.click(screen.getByRole('button', { name: 'Update' }))
    expect(update.restart).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: 'What’s new' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Later' })).toBeInTheDocument()
  })

  it('shows a download’s progress, without reading its percentage out, and Later hides it', async () => {
    const { update } = shown(updateReady({ state: 'downloading', mode: 'notify', progress: 42 }), {
      step: 'downloading',
    })

    const bar = screen.getByRole('progressbar', { name: 'Download' })

    expect(banner()).toHaveTextContent('Downloading 0.3.0… 42%')
    expect(bar).toHaveAttribute('aria-valuenow', '42')
    expect(within(banner()).getByText('42%', { exact: false })).toHaveAttribute('aria-hidden', 'true')
    expect(buttons()).toEqual(['Later'])
    expect(screen.getByRole('button', { name: 'Later' })).toHaveAttribute('title', 'Hide this while it downloads')

    await userEvent.click(screen.getByRole('button', { name: 'Later' }))
    expect(update.later).toHaveBeenCalledOnce()
  })

  it('shows the restart under way, and the backup it starts with', () => {
    shown(updateReady({ state: 'installing' }), { step: 'restarting' })

    expect(banner()).toHaveTextContent('Restarting into 0.3.0… Backing up your data and preparing the update…')
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
    expect(within(banner()).queryByRole('button')).not.toBeInTheDocument()
  })

  it('shows the check a Try again starts with', () => {
    shown(updateStatus(), { step: 'checking' })

    expect(banner()).toHaveTextContent('Checking for updates…')
  })

  it('says why a restart failed: Try again, Download manually, or put it away', async () => {
    const { update } = shown(updateReady(), { step: 'failed', message: 'Couldn’t install 0.3.0: the disk is full' })

    expect(banner()).toHaveTextContent('Couldn’t install 0.3.0: the disk is full.')

    await userEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(update.restart).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Download manually' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(releasePage)

    await userEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(update.dismiss).toHaveBeenCalledOnce()
    expect(screen.queryByRole('button', { name: 'Later' })).not.toBeInTheDocument()
  })

  it('hands focus to Try again when a restart fails with nothing focused', () => {
    const { showStep } = shown(updateReady(), { step: 'restarting' })

    expect(document.body).toHaveFocus()
    showStep({ step: 'failed', message: 'Couldn’t back up your data first: the disk is full' })

    expect(screen.getByRole('button', { name: 'Try again' })).toHaveFocus()
  })

  it('hands focus to Download manually when the update can’t be tried again', () => {
    const { showStep } = shown(updateReady(), { step: 'restarting' })

    showStep({ step: 'failed', message: '0.3.0 didn’t install before', final: true })

    expect(buttons()).toEqual(['Download manually', 'Dismiss'])
    expect(screen.getByRole('button', { name: 'Download manually' })).toHaveFocus()
  })

  it('leaves focus where it is when something has it', () => {
    const { showStep } = shown(updateReady(), { step: 'restarting' })
    const elsewhere = document.createElement('input')

    document.body.append(elsewhere)
    elsewhere.focus()
    showStep({ step: 'failed', message: 'No space' })

    expect(elsewhere).toHaveFocus()
    elsewhere.remove()
  })

  it('takes no focus as the last launch’s failure shows', () => {
    shown(
      updateReady({
        installFailed: { version: '0.3.0', reason: 'It didn’t quit in time.', log: true, retryable: true },
      }),
    )

    expect(document.body).toHaveFocus()
  })

  it('lets its buttons wrap under the words in a narrow window', () => {
    shown(
      updateReady({
        installFailed: { version: '0.3.0', reason: 'It didn’t quit in time.', log: true, retryable: true },
      }),
    )

    const row = within(banner()).getByText(/didn’t install/).parentElement
    const actions = screen.getByRole('button', { name: 'Details' }).parentElement

    expect(row).toHaveClass('flex-wrap')
    expect(actions).toHaveClass('ml-auto', 'flex-wrap')
  })

  it('waits quietly while the last update finishes: no failure, nothing to click or put away', () => {
    shown(updateReady(), { step: 'waiting', message: settlingWords })

    expect(banner()).toHaveTextContent('The last update is still finishing. This can take a minute or two.')
    expect(within(banner()).queryByRole('button')).not.toBeInTheDocument()
    expect(banner().querySelector('.border-warning')).toBeNull()
  })

  it('says the update is finishing as the app quits on its own after a held quit, with nothing to click', () => {
    shown(updateReady({ state: 'finishing', bannerHidden: true }), { step: 'restarting' })

    expect(banner()).toHaveTextContent(`Finishing the update… ${brand.name} will reopen in a moment.`)
    expect(within(banner()).queryByRole('button')).not.toBeInTheDocument()
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
    expect(banner().querySelector('.border-warning')).toBeNull()
  })

  it('says the last launch’s update didn’t install: Try again, Download manually, Details', async () => {
    const { update, onDetails } = shown(
      updateReady({
        installFailed: { version: '0.3.0', reason: 'It didn’t quit in time.', log: true, retryable: true },
      }),
    )

    expect(banner()).toHaveTextContent(`The update to 0.3.0 didn’t install, so ${brand.name} is still on 0.2.0.`)

    await userEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(update.restart).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Download manually' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(releasePage)

    await userEvent.click(screen.getByRole('button', { name: 'Details' }))
    expect(onDetails).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(update.dismiss).toHaveBeenCalledOnce()
  })

  it('offers Download manually alone for an install that can’t be tried again', () => {
    const final = { version: '0.3.0', reason: 'The copy kept is damaged.', log: false, retryable: false }

    shown(updateReady({ installFailed: final }))

    expect(buttons()).toEqual(['Download manually', 'Details', 'Dismiss'])
  })

  it('names the newer version found since the last launch’s failure, and offers it as any other', async () => {
    const failure = { version: '0.3.0', reason: 'It didn’t quit in time.', log: true, retryable: true }
    const { update } = shown(updateReady({ latest: '0.3.1', installFailed: failure }))

    expect(banner()).toHaveTextContent(
      `The update to 0.3.0 didn’t install, so ${brand.name} is still on 0.2.0. 0.3.1 is ready.`,
    )
    expect(buttons()).toEqual(['Restart to update', 'Details', 'Dismiss'])
    await userEvent.click(screen.getByRole('button', { name: 'Restart to update' }))
    expect(update.restart).toHaveBeenCalledOnce()
  })

  it('names a newer version found since that is still to download, as Just tell me leaves it', () => {
    const failure = { version: '0.3.0', reason: 'It didn’t quit in time.', log: true, retryable: true }

    shown(updateReady({ state: 'available', mode: 'notify', latest: '0.3.1', installFailed: failure }))

    expect(banner()).toHaveTextContent('still on 0.2.0. 0.3.1 is available.')
    expect(buttons()).toEqual(['Update', 'Details', 'Dismiss'])
  })

  it('opens the page of the version that didn’t install while no version is found since', async () => {
    const failure = { version: '0.3.0', reason: 'It didn’t quit in time.', log: true, retryable: true }

    // With nothing found, the status names the running version's page.
    shown(updateStatus({ installFailed: failure }))

    expect(banner()).toHaveTextContent(`The update to 0.3.0 didn’t install, so ${brand.name} is still on 0.2.0.`)
    expect(buttons()).toEqual(['Try again', 'Download manually', 'Details', 'Dismiss'])
    await userEvent.click(screen.getByRole('button', { name: 'Download manually' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(releasePage)
  })

  it('says how to let the app install an update where it can’t, with no Update to try', async () => {
    // Nothing downloads there, so the version found stays available, in the default mode too.
    const { update } = shown(
      updateReady({
        state: 'available',
        location: { ok: false, kind: 'diskImage', target: '/Applications/Example.app' },
      }),
    )

    expect(banner()).toHaveTextContent(
      `${brand.name} 0.3.0 is available. Move ${brand.name} to your Applications folder to install it.`,
    )
    expect(screen.queryByRole('button', { name: 'Update' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Later' }))
    expect(update.later).toHaveBeenCalledOnce()
    await userEvent.click(screen.getByRole('button', { name: 'What’s new' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(releasePage)
  })

  it('says why where the account can’t change the folder, and calls one kept from before ready', () => {
    shown(
      updateReady({
        state: 'ready',
        mode: 'notify',
        location: { ok: false, kind: 'applications', target: '/Applications/Example.app' },
      }),
    )

    expect(banner()).toHaveTextContent(
      `${brand.name} 0.3.0 is ready. Your account can’t change the folder it’s in, so it can’t install it.`,
    )
    expect(screen.queryByRole('button', { name: 'Restart to update' })).not.toBeInTheDocument()
  })

  it('offers no page before the status loads, only Try again', () => {
    shown(null, { step: 'failed', message: 'The app is closing.' })

    expect(buttons()).toEqual(['Try again', 'Dismiss'])
  })

  it('makes its room gently as it appears, and at once with reduced motion', () => {
    shown(updateReady())

    const grower = banner().firstElementChild

    expect(grower).toHaveClass('grid', 'grid-rows-[1fr]', 'motion-safe:animate-notice-in')
    expect(grower?.firstElementChild).toHaveClass('min-h-0', 'overflow-hidden')
  })
})

describe('UpdateBanner with the restart under way and a status not loaded', () => {
  it('names the update it restarts into plainly', () => {
    shown(null, { step: 'restarting' })

    expect(banner()).toHaveTextContent('Restarting into the new version…')
  })
})
