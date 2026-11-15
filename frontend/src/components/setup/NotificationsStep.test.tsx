import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { Query } from '../../lib/useQuery'
import type { BannerStatus, Settings } from '../../platform'
import { settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { queryOf } from '../../test/query'
import { NotificationsStep } from './NotificationsStep'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// Stateful holds the settings as the window does, taking each one a setter returns.
function Stateful({ stored, banners }: { stored: Settings; banners: Query<BannerStatus> }) {
  const [shown, setShown] = useState(stored)

  return <NotificationsStep settings={shown} onSaved={setShown} banners={banners} />
}

// renderStep shows the step with the banners as status says, over stored settings.
function renderStep(status: BannerStatus | undefined, stored: Settings = settings()) {
  const banners = queryOf(status)

  render(<Stateful stored={stored} banners={banners} />)

  return { banners }
}

describe('NotificationsStep', () => {
  it('turns banners on with a sample, which makes macOS ask once', async () => {
    const { banners } = renderStep('off')

    expect(screen.getByText(/turned off for this app in System Settings/)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Turn on banners' }))

    expect(platform.sendTestNotification).toHaveBeenCalledOnce()
    expect(await screen.findByText(/Sent. If no banner showed/)).toBeInTheDocument()
    expect(banners.refetch).toHaveBeenCalled()
  })

  it('says banners are on, and sends a test', async () => {
    renderStep('on')

    expect(screen.getByText('Banners are on.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Send a test' }))

    expect(platform.sendTestNotification).toHaveBeenCalledOnce()
  })

  it('explains a build that can’t show banners', () => {
    renderStep('unavailable')

    expect(screen.getByText(/Banners need the packaged app/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Turn on banners' })).toBeDisabled()
  })

  it('waits for the status before it offers anything', () => {
    renderStep(undefined)

    expect(screen.getByRole('button', { name: 'Turn on banners' })).toBeDisabled()
  })

  it('says why a test could not be sent', async () => {
    platform.sendTestNotification.mockRejectedValue(new Error('the notification center did not answer'))
    renderStep('off')

    await userEvent.click(screen.getByRole('button', { name: 'Turn on banners' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('the notification center did not answer')
  })

  it.each([
    ['Tell me when an agent needs me', 'session_needs_you'],
    ['Tell me when an agent finishes its turn', 'session_finished'],
  ] as const)('turns on “%s”, which starts off', async (label, kind) => {
    renderStep('on')

    const toggle = screen.getByRole('switch', { name: label })
    expect(toggle).not.toBeChecked()

    await userEvent.click(toggle)

    expect(platform.setNotificationBanner).toHaveBeenCalledWith(kind, true)
  })

  it('turns a session’s banner off', async () => {
    renderStep('on', settings({ bannersOff: [] }))

    await userEvent.click(screen.getByRole('switch', { name: 'Tell me when an agent needs me' }))

    expect(platform.setNotificationBanner).toHaveBeenCalledWith('session_needs_you', false)
  })

  it('keeps the app in the menu bar, saying what closing the window does', async () => {
    renderStep('on')

    const menuBar = screen.getByRole('switch', { name: `Keep ${brand.name} in the menu bar` })
    expect(menuBar).toBeChecked()
    expect(screen.getByText(/Closing the window keeps/)).toBeInTheDocument()

    await userEvent.click(menuBar)

    expect(platform.setMenuBar).toHaveBeenCalledWith(false)
    expect(await screen.findByText('Closing the window quits the app.')).toBeInTheDocument()
  })

  it('mirrors the GitHub notifications, and clears them only while they are read', async () => {
    renderStep('on')

    const section = within(screen.getByRole('region', { name: 'GitHub notifications' }))
    const auto = section.getByRole('switch', { name: 'Clear them automatically' })

    await userEvent.click(auto)
    expect(platform.setGitHubNotificationSwitch).toHaveBeenCalledWith('auto', true)

    await userEvent.click(section.getByRole('switch', { name: 'Mirror my GitHub notifications' }))
    expect(platform.setGitHubNotificationSwitch).toHaveBeenCalledWith('read', false)
    expect(auto).toBeDisabled()
  })

  it('says why a change was refused', async () => {
    platform.setMenuBar.mockRejectedValue(new Error('the settings are locked'))
    renderStep('on')

    await userEvent.click(screen.getByRole('switch', { name: `Keep ${brand.name} in the menu bar` }))

    expect(await screen.findByRole('alert')).toHaveTextContent('the settings are locked')
  })
})
