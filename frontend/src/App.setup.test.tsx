import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

import type { Setup } from './platform'
import { appInfo, item, list, snapshot } from './test/fixtures'
import { emit, platform, renderApp } from './test/platform'

vi.mock('./platform', () => import('./test/platform').then((fake) => fake.platform))

// The setup and Settings load apart from the first paint; they are imported once before
// the tests, so no test pays their cold import inside a findBy's wait.
beforeAll(async () => {
  await Promise.all([import('./components/setup/SetupView'), import('./components/SettingsView')])
})

afterEach(() => {
  window.history.replaceState(null, '', '/')
})

// pendingSetup is the setup as a fresh database stores it.
const pendingSetup: Setup = { done: false, step: 'welcome' }

// column is the window's column beneath the setup: its toolbar's parent.
function column() {
  return screen.getByRole('banner').parentElement
}

// setup is the setup's dialog.
async function setupShown() {
  return within(await screen.findByRole('dialog', { name: /Welcome|Connect GitHub/ }))
}

describe('App with the first-run setup', () => {
  it('shows the setup over an inert window, and the inbox once it ends', async () => {
    const filled = snapshot({ lastRefreshedAt: '2026-06-15T12:00:00Z', reviewRequested: list([item()]) })
    platform.getSetup.mockResolvedValue(pendingSetup)
    platform.getInboxSnapshot.mockResolvedValue(snapshot())
    platform.refreshInbox.mockResolvedValue(filled)
    await renderApp()

    const setup = await setupShown()

    // The sign-in reads ready, so the setup fills the inbox while the user goes on.
    await waitFor(() => {
      expect(platform.refreshInbox).toHaveBeenCalledOnce()
    })
    expect(column()).toHaveAttribute('inert')
    expect(column()).toHaveClass('isolate')

    await userEvent.click(setup.getAllByRole('button', { name: 'Skip setup' })[0] as HTMLElement)
    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'Skip the setup?' })).getByRole('button', { name: 'Skip setup' }),
    )

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: /Welcome/ })).not.toBeInTheDocument()
    })
    expect(platform.finishSetup).toHaveBeenCalledOnce()
    expect(column()).not.toHaveAttribute('inert')
    expect(await screen.findByRole('region', { name: 'Review requested' })).toBeInTheDocument()
  })

  it('covers the window from the first frame the URL asks for the setup', async () => {
    window.history.replaceState(null, '', '/?setup=1&textSize=default')
    platform.getSetup.mockReturnValue(new Promise(() => undefined))
    await renderApp()

    // The stored state never answers: the URL alone shows the setup, never the inbox.
    expect(await screen.findByRole('dialog', { name: /Welcome/ })).toBeInTheDocument()
    expect(column()).toHaveAttribute('inert')
  })

  it('keeps the window’s shortcuts and reveals from reaching beneath the setup', async () => {
    platform.getSetup.mockResolvedValue(pendingSetup)
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item({ id: 9 })]) }))
    await renderApp()
    await setupShown()

    await userEvent.keyboard('{Meta>},{/Meta}')
    expect(screen.queryByRole('navigation', { name: 'Settings sections' })).not.toBeInTheDocument()

    act(() => {
      emit('inbox:reveal', {
        itemId: 9,
        url: 'https://github.com/acme-org/example-repo/pull/7',
        runId: 0,
        runStarted: '',
        notificationId: 0,
      })
    })
    expect(platform.openExternal).not.toHaveBeenCalled()
  })

  it('runs the setup again from Settings → About', async () => {
    platform.getAppInfo.mockResolvedValue(appInfo())
    await renderApp()

    await userEvent.click(await screen.findByRole('button', { name: 'Account: octo-user' }))
    await userEvent.click(screen.getByRole('menuitem', { name: /Settings/ }))
    await userEvent.click(await screen.findByRole('button', { name: 'About' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Run setup again' }))

    expect(platform.restartSetup).toHaveBeenCalledOnce()
    expect(await screen.findByRole('heading', { name: 'Welcome back' })).toBeInTheDocument()
  })

  it('marks a window on a sandbox’s data', async () => {
    platform.getAppInfo.mockResolvedValue(appInfo({ sandbox: true }))
    await renderApp()

    expect(await screen.findByText('Test data')).toBeInTheDocument()
  })
})
