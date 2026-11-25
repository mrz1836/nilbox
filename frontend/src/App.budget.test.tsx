import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { brand } from './generated/brand'
import { autopilotTarget, item, list, notification, snapshot } from './test/fixtures'
import { platform, renderApp, send } from './test/platform'

vi.mock('./platform', () => import('./test/platform').then((fake) => fake.platform))

beforeEach(() => {
  platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
})

// drainingEntry is the history's entry for a budget draining fast, which names no item,
// page, or run.
const drainingEntry = notification({
  id: 12,
  kind: 'budget_draining',
  itemId: 0,
  itemKind: '',
  repo: '',
  number: 0,
  url: '',
  author: '',
  title: `Something signed in as you, not ${brand.name}, used 1,800 GraphQL points in 10 minutes.`,
  detail: 'At this pace it runs out around 10:05 PM, before it refills at 10:34 PM.',
})

describe('App’s API budget', () => {
  it('opens the API budget’s panel for a click on a budget draining’s banner, and marks it read', async () => {
    platform.listNotifications.mockResolvedValue({ items: [drainingEntry], unread: 1 })
    await renderApp()
    await screen.findByRole('button', { name: '1 unread notifications' })

    expect(screen.queryByRole('dialog', { name: 'API budget' })).not.toBeInTheDocument()

    send('inbox:reveal', {
      itemId: 0,
      url: '',
      runId: 0,
      runStarted: '',
      notificationId: 12,
      autopilot: autopilotTarget(),
    })

    expect(await screen.findByRole('dialog', { name: 'API budget' })).toBeInTheDocument()
    expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([12])
    expect(platform.showUpdateBanner).not.toHaveBeenCalled()
  })

  it('opens the API budget’s panel from the entry in the history', async () => {
    platform.listNotifications.mockResolvedValue({ items: [drainingEntry], unread: 1 })
    await renderApp()

    await userEvent.click(await screen.findByRole('button', { name: '1 unread notifications' }))
    // The entry's own button, not its Done.
    await userEvent.click(await screen.findByRole('button', { name: /^(?!Done: ).*used 1,800 GraphQL points/ }))

    await waitFor(() => {
      expect(screen.getByRole('dialog', { name: 'API budget' })).toBeInTheDocument()
    })
    expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([12])
  })
})
