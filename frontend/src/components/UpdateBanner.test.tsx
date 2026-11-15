import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { RestartStep } from '../lib/update'
import type { UpdateStatus } from '../platform'
import { updateReady, updateStatus } from '../test/fixtures'
import { platform } from '../test/platform'
import { updateOf } from '../test/query'
import { UpdateBanner } from './UpdateBanner'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const latestPage = 'https://github.com/acme-org/example-repo/releases/latest'
const releasePage = 'https://github.com/acme-org/example-repo/releases/tag/v0.3.0'

// shown renders the banner for a status and a step of the restart, and returns the update
// it was given, with its moves as mocks, and the onDetails it was given.
function shown(status: UpdateStatus | null, flow: RestartStep = { step: 'idle' }) {
  const update = updateOf(status, flow)
  const onDetails = vi.fn()

  render(<UpdateBanner update={update} onDetails={onDetails} />)

  return { update, onDetails }
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

  it('shows a download’s progress, without reading its percentage out', () => {
    shown(updateReady({ state: 'downloading', mode: 'notify', progress: 42 }), { step: 'downloading' })

    const bar = screen.getByRole('progressbar', { name: 'Download' })

    expect(banner()).toHaveTextContent('Downloading 0.3.0… 42%')
    expect(bar).toHaveAttribute('aria-valuenow', '42')
    expect(within(banner()).getByText('42%', { exact: false })).toHaveAttribute('aria-hidden', 'true')
    expect(within(banner()).queryByRole('button')).not.toBeInTheDocument()
  })

  it('shows the restart under way, and the backup it starts with', () => {
    shown(updateReady({ state: 'installing' }), { step: 'restarting' })

    expect(banner()).toHaveTextContent('Restarting into 0.3.0… Backing up your data first.')
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
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(latestPage)

    await userEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(update.dismiss).toHaveBeenCalledOnce()
    expect(screen.queryByRole('button', { name: 'Later' })).not.toBeInTheDocument()
  })

  it('says the last launch’s update didn’t install: Try again, Download manually, Details', async () => {
    const { update, onDetails } = shown(
      updateReady({ installFailed: { version: '0.3.0', reason: 'It wouldn’t open.', log: true } }),
    )

    expect(banner()).toHaveTextContent(`The update to 0.3.0 didn’t install, so ${brand.name} is still on 0.2.0.`)

    await userEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(update.restart).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Download manually' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(latestPage)

    await userEvent.click(screen.getByRole('button', { name: 'Details' }))
    expect(onDetails).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(update.dismiss).toHaveBeenCalledOnce()
  })

  it('says how to let the app install an update where it can’t, with no Restart to try', async () => {
    const { update } = shown(
      updateReady({ location: { ok: false, kind: 'diskImage', target: '/Applications/Example.app' } }),
    )

    expect(banner()).toHaveTextContent(
      `${brand.name} 0.3.0 is ready. Move ${brand.name} to your Applications folder to install it.`,
    )
    expect(screen.queryByRole('button', { name: 'Restart to update' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Later' }))
    expect(update.later).toHaveBeenCalledOnce()
    await userEvent.click(screen.getByRole('button', { name: 'What’s new' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(releasePage)
  })

  it('says an update available can’t install where the app runs, too', () => {
    shown(
      updateReady({
        state: 'available',
        mode: 'notify',
        location: { ok: false, kind: 'applications', target: '/Applications/Example.app' },
      }),
    )

    expect(banner()).toHaveTextContent(
      `${brand.name} 0.3.0 is available. Your account can’t change the folder it’s in, so it can’t install it.`,
    )
  })

  it('opens no page before the status loads', async () => {
    shown(null, { step: 'failed', message: 'The app is closing.' })

    await userEvent.click(screen.getByRole('button', { name: 'Download manually' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('')
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
