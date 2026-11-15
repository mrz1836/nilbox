import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import { brand } from './generated/brand'
import { item, list, notification, scriptRun, snapshot, updateReady, updateStatus } from './test/fixtures'
import { emit, platform, renderApp } from './test/platform'

vi.mock('./platform', () => import('./test/platform').then((fake) => fake.platform))

// Settings and the review sheet load apart from the first paint; they are imported once
// before the tests, so no test pays their cold import inside a findBy's wait.
beforeAll(async () => {
  await Promise.all([import('./components/SettingsView'), import('./components/review/ReviewSheet')])
})

beforeEach(() => {
  platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
})

// banner is the update banner's live region.
function banner() {
  return screen.getByRole('status', { name: `${brand.name} update` })
}

// updateEntry is the history's entry for a new version, which names no item, page, or run.
const updateEntry = notification({
  id: 9,
  kind: 'update_ready',
  itemId: 0,
  itemKind: '',
  repo: '',
  number: 0,
  url: '',
  author: '',
  title: `${brand.name} 0.3.0 is ready`,
  detail: 'Restart to update when you’re ready.',
})

describe('App’s update', () => {
  it('shows the update’s banner under the toolbar, and hides it for a day on Later', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    await renderApp()

    await waitFor(() => {
      expect(banner()).toHaveTextContent(`${brand.name} 0.3.0 is ready.`)
    })

    await userEvent.click(within(banner()).getByRole('button', { name: 'Later' }))

    expect(banner()).toBeEmptyDOMElement()
    expect(platform.hideUpdateBanner).toHaveBeenCalledOnce()
  })

  it('says once which version an update installed, with What’s new, and tells the update it was said', async () => {
    platform.getUpdateStatus.mockResolvedValueOnce(updateStatus({ current: '0.3.0', justUpdated: '0.3.0' }))
    await renderApp()

    const toast = await screen.findByRole('group', { name: 'Notification' })

    expect(toast).toHaveTextContent('Updated to 0.3.0')
    await waitFor(() => {
      expect(platform.ackUpdated).toHaveBeenCalledOnce()
    })

    // The acknowledgement says it changed; the status read again tells nothing new.
    act(() => {
      emit('updates:updated')
    })

    await userEvent.click(within(toast).getByRole('button', { name: 'What’s new' }))

    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(
      'https://github.com/acme-org/example-repo/releases/tag/v0.3.0',
    )
    expect(screen.queryByRole('group', { name: 'Notification' })).not.toBeInTheDocument()
    expect(platform.ackUpdated).toHaveBeenCalledOnce()
  })

  it('holds the toast while the first-run setup shows, so it is said where it can be seen', async () => {
    platform.getSetup.mockResolvedValue({ done: false, step: 'welcome' })
    platform.getUpdateStatus.mockResolvedValue(updateStatus({ current: '0.3.0', justUpdated: '0.3.0' }))
    await renderApp()

    await screen.findByRole('dialog', { name: /Welcome/ })
    await act(async () => {
      await Promise.resolve()
    })

    expect(platform.ackUpdated).not.toHaveBeenCalled()
  })

  it('asks first while a sheet holds text not sent, and restarts once confirmed', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockReturnValue(new Promise(() => undefined))
    await renderApp()

    await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))
    const sheet = await screen.findByRole('dialog', { name: 'Add the widget cache' })
    await userEvent.type(await within(sheet).findByRole('textbox', { name: 'Your review' }), 'One question first.')
    await userEvent.keyboard('{Escape}')
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Add the widget cache' })).not.toBeInTheDocument()
    })

    await userEvent.click(within(banner()).getByRole('button', { name: 'Restart to update' }))
    const asked = await screen.findByRole('dialog', { name: 'Restart to update?' })

    expect(asked).toHaveTextContent('Restarting loses the text you haven’t sent in a sheet.')
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()

    await userEvent.click(within(asked).getByRole('button', { name: 'Restart to update' }))

    expect(platform.restartToUpdateNow).toHaveBeenCalledExactlyOnceWith(true)
    await waitFor(() => {
      expect(banner()).toHaveTextContent('Restarting into 0.3.0…')
    })
  })

  it('runs the same restart when the menu bar asks, asking first while a run goes', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockResolvedValueOnce({
      restarted: false,
      message: '',
      runsGoing: [{ id: 7, kind: 'release', title: 'Release v1.2.0', repo: 'acme-org/sdk', dir: '', stopRefused: '' }],
    })
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })

    act(() => {
      emit('updates:restart')
    })
    const asked = await screen.findByRole('dialog', { name: 'Restart to update?' })

    expect(asked).toHaveTextContent('Restarting stops Release v1.2.0 in acme-org/sdk.')

    await userEvent.click(within(asked).getByRole('button', { name: 'Not now' }))

    expect(screen.queryByRole('dialog', { name: 'Restart to update?' })).not.toBeInTheDocument()
    expect(platform.restartToUpdateNow).toHaveBeenCalledExactlyOnceWith(false)
    expect(banner()).toHaveTextContent(`${brand.name} 0.3.0 is ready.`)
  })

  it('names the runs listed when a sheet holds text too, in one question', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.listScriptRuns.mockResolvedValue([
      scriptRun({ id: 7, kind: 'release', title: 'Release v1.2.0', repo: 'acme-org/sdk', canStop: true }),
    ])
    await renderApp()

    await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))
    const sheet = await screen.findByRole('dialog', { name: 'Add the widget cache' })
    await userEvent.type(await within(sheet).findByRole('textbox', { name: 'Your review' }), 'Wait.')
    await userEvent.keyboard('{Escape}')

    act(() => {
      emit('updates:restart')
    })

    expect(await screen.findByRole('dialog', { name: 'Restart to update?' })).toHaveTextContent(
      'Restarting stops Release v1.2.0 in acme-org/sdk and loses the text you haven’t sent in a sheet.',
    )
  })

  it('shows the banner again for a click on the update’s notification, lifting Later, and marks it read', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady({ bannerHidden: true }))
    platform.showUpdateBanner.mockImplementation(() => {
      platform.getUpdateStatus.mockResolvedValue(updateReady())

      return Promise.resolve(updateReady())
    })
    platform.listNotifications.mockResolvedValue({ items: [updateEntry], unread: 1 })
    await renderApp()
    await screen.findByRole('button', { name: '1 unread notifications' })

    expect(banner()).toBeEmptyDOMElement()

    act(() => {
      emit('inbox:reveal', { itemId: 0, url: '', runId: 0, runStarted: '', notificationId: 9 })
    })

    await waitFor(() => {
      expect(banner()).toHaveTextContent(`${brand.name} 0.3.0 is ready.`)
    })
    expect(platform.showUpdateBanner).toHaveBeenCalledOnce()
    expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([9])
  })

  it('opens About for the update’s notification once no update is on its way', async () => {
    platform.showUpdateBanner.mockResolvedValue(updateStatus())
    platform.listNotifications.mockResolvedValue({ items: [{ ...updateEntry, read: true }], unread: 0 })
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })

    act(() => {
      emit('inbox:reveal', { itemId: 0, url: '', runId: 0, runStarted: '', notificationId: 9 })
    })

    expect(await screen.findByRole('region', { name: 'About' })).toBeInTheDocument()
    expect(platform.markNotificationsRead).not.toHaveBeenCalled()
  })

  it('shows the banner for the update’s entry in the history', async () => {
    platform.listNotifications.mockResolvedValue({ items: [updateEntry], unread: 1 })
    await renderApp()

    await userEvent.click(await screen.findByRole('button', { name: '1 unread notifications' }))
    // The entry's own button, not its Done.
    await userEvent.click(
      await screen.findByRole('button', { name: new RegExp(`^(?!Done: ).*${brand.name} 0\\.3\\.0 is ready`) }),
    )

    await waitFor(() => {
      expect(platform.showUpdateBanner).toHaveBeenCalledOnce()
    })
    expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([9])
  })

  it('opens About from a failed install’s Details', async () => {
    platform.getUpdateStatus.mockResolvedValue(
      updateReady({ installFailed: { version: '0.3.0', reason: 'It wouldn’t open.', log: false } }),
    )
    await renderApp()

    await waitFor(() => {
      expect(banner()).toHaveTextContent('The update to 0.3.0 didn’t install')
    })
    await userEvent.click(within(banner()).getByRole('button', { name: 'Details' }))

    const about = await screen.findByRole('region', { name: 'About' })

    expect(await within(about).findByText('It wouldn’t open.')).toBeInTheDocument()
  })
})
