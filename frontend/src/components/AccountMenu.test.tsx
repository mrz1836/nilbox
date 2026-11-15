import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Account, ServiceStatus } from '../platform'
import { account, claudeOutage, serviceIncident, serviceProvider, serviceStatus, signedOut } from '../test/fixtures'
import { platform } from '../test/platform'
import { AccountMenu } from './AccountMenu'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

function renderMenu(who: Account | null = account(), status: ServiceStatus | null = null) {
  const onOpenSettings = vi.fn()
  const onChooseWatch = vi.fn()

  render(
    <>
      <AccountMenu account={who} serviceStatus={status} onOpenSettings={onOpenSettings} onChooseWatch={onChooseWatch} />
      <button type="button">Elsewhere</button>
    </>,
  )

  return { onOpenSettings, onChooseWatch, trigger: screen.getByRole('button', { name: /^Account/ }) }
}

// openServiceStatus opens the menu and its Service status submenu, and returns the submenu.
async function openServiceStatus(trigger: HTMLElement): Promise<HTMLElement> {
  await userEvent.click(trigger)
  await userEvent.click(screen.getByRole('menuitem', { name: /^Service status/ }))

  return screen.getByRole('menu', { name: 'Service status' })
}

describe('AccountMenu', () => {
  it("shows the account's picture on the button", () => {
    const { trigger } = renderMenu()

    expect(trigger).toHaveAccessibleName('Account: octo-user')
    expect(trigger).toHaveAttribute('title', 'octo-user')
    expect(trigger).toHaveAttribute('aria-haspopup', 'menu')
    expect(trigger.querySelector('img')).not.toBeNull()
  })

  it('opens a menu with the account, Settings, the GitHub profile, Service status, a bug report, and feedback', async () => {
    const { trigger } = renderMenu()

    await userEvent.click(trigger)

    expect(trigger).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('Octo User')).toBeInTheDocument()
    expect(screen.getByText('@octo-user')).toBeInTheDocument()

    const menu = screen.getByRole('menu', { name: 'Account' })
    expect(trigger).toHaveAttribute('aria-controls', menu.id)
    expect(
      within(menu)
        .getAllByRole('menuitem')
        .map((item) => item.textContent),
    ).toEqual(['Settings⌘,', 'View GitHub profile', 'Service status', 'Report a bug', 'Send feedback'])
    expect(within(menu).getByRole('menuitem', { name: /Settings/ })).toHaveFocus()
  })

  it('opens the GitHub profile from the account card and closes the menu', async () => {
    const { trigger } = renderMenu()

    await userEvent.click(trigger)
    await userEvent.click(screen.getByText('Octo User'))

    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/octo-user')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

  it('opens a bug report made now in the browser', async () => {
    const { trigger } = renderMenu()

    await userEvent.click(trigger)
    await userEvent.click(screen.getByRole('menuitem', { name: 'Report a bug' }))

    expect(platform.bugReportURL).toHaveBeenCalledOnce()
    await waitFor(() => {
      expect(platform.openExternal).toHaveBeenCalledWith(
        'https://github.com/acme-org/example-repo/issues/new?template=bug_report.yml',
      )
    })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('opens feedback in the browser', async () => {
    const { trigger } = renderMenu()

    await userEvent.click(trigger)
    await userEvent.click(screen.getByRole('menuitem', { name: 'Send feedback' }))

    expect(platform.feedbackURL).toHaveBeenCalledOnce()
    await waitFor(() => {
      expect(platform.openExternal).toHaveBeenCalledWith(
        'https://github.com/acme-org/example-repo/issues/new?template=BLANK_ISSUE',
      )
    })
  })

  it('opens nothing when the link cannot be made', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.feedbackURL.mockRejectedValue(new Error('the app is closing'))
    const { trigger } = renderMenu()

    await userEvent.click(trigger)
    await userEvent.click(screen.getByRole('menuitem', { name: 'Send feedback' }))

    await waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not make the feedback link', expect.any(Error))
    })
    expect(platform.openExternal).not.toHaveBeenCalled()
  })

  it('opens Settings and puts focus back on the button', async () => {
    const { trigger, onOpenSettings } = renderMenu()

    await userEvent.click(trigger)
    await userEvent.click(screen.getByRole('menuitem', { name: /Settings/ }))

    expect(onOpenSettings).toHaveBeenCalledOnce()
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

  it('opens the GitHub profile in the browser', async () => {
    const { trigger } = renderMenu()

    await userEvent.click(trigger)
    await userEvent.click(screen.getByRole('menuitem', { name: 'View GitHub profile' }))

    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/octo-user')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('lists each provider watched with its state, each opening its status page', async () => {
    const { trigger } = renderMenu(account(), serviceIncident(claudeOutage()))

    const submenu = await openServiceStatus(trigger)

    expect(
      within(submenu)
        .getAllByRole('menuitem')
        .map((item) => item.textContent),
    ).toEqual(['GitHub, Operational', 'Claude, Partial outage', 'OpenAI, Operational', 'Choose what to watch…'])
    expect(within(submenu).getByRole('menuitem', { name: /^Claude/ })).toHaveAccessibleName(
      /^Claude,\s*Partial outage$/,
    )
    expect(within(within(submenu).getByRole('menuitem', { name: /^Claude/ })).getByText('Partial outage')).toHaveClass(
      'text-danger',
    )

    await userEvent.click(within(submenu).getByRole('menuitem', { name: /^Claude/ }))

    expect(platform.openExternal).toHaveBeenCalledWith('https://status.claude.com/')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('leaves out a provider not watched, and opens what to watch', async () => {
    const { trigger, onChooseWatch, onOpenSettings } = renderMenu(
      account(),
      serviceStatus({}, serviceProvider({ id: 'openai', name: 'OpenAI', state: 'off' })),
    )

    const submenu = await openServiceStatus(trigger)

    expect(
      within(submenu)
        .getAllByRole('menuitem')
        .map((item) => item.textContent),
    ).toEqual(['GitHub, Operational', 'Claude, Operational', 'Choose what to watch…'])

    await userEvent.click(within(submenu).getByRole('menuitem', { name: 'Choose what to watch…' }))

    expect(onChooseWatch).toHaveBeenCalledOnce()
    expect(onOpenSettings).not.toHaveBeenCalled()
  })

  it('offers only what to watch before the first reading', async () => {
    const { trigger } = renderMenu()

    const submenu = await openServiceStatus(trigger)

    expect(
      within(submenu)
        .getAllByRole('menuitem')
        .map((item) => item.textContent),
    ).toEqual(['Choose what to watch…'])
  })

  it('names the worst problem beside Service status, an outage in red', async () => {
    const { trigger } = renderMenu(account(), serviceIncident())

    await userEvent.click(trigger)

    const item = screen.getByRole('menuitem', { name: /^Service status/ })
    expect(item).toHaveAccessibleName(/^Service status,\s*Partial outage$/)
    expect(within(item).getByText('Partial outage')).toHaveClass('text-danger')
  })

  it('names maintenance beside Service status, in amber', async () => {
    const { trigger } = renderMenu(account(), serviceIncident(serviceProvider({ state: 'maintenance' })))

    await userEvent.click(trigger)

    const item = screen.getByRole('menuitem', { name: /^Service status/ })
    expect(item).toHaveAccessibleName(/^Service status,\s*Maintenance$/)
    expect(within(item).getByText('Maintenance')).toHaveClass('text-warning')
  })

  it('names nothing beside Service status while everything watched works', async () => {
    const { trigger } = renderMenu(account(), serviceStatus())

    await userEvent.click(trigger)

    expect(screen.getByRole('menuitem', { name: /^Service status/ })).toHaveAccessibleName('Service status')
  })

  it('moves between items with the arrow keys, Home, and End, wrapping at the ends', async () => {
    const { trigger } = renderMenu()
    await userEvent.click(trigger)

    const settings = screen.getByRole('menuitem', { name: /Settings/ })
    const profile = screen.getByRole('menuitem', { name: 'View GitHub profile' })
    const status = screen.getByRole('menuitem', { name: 'Service status' })
    const bug = screen.getByRole('menuitem', { name: 'Report a bug' })
    const feedback = screen.getByRole('menuitem', { name: 'Send feedback' })

    await userEvent.keyboard('{ArrowDown}')
    expect(profile).toHaveFocus()
    await userEvent.keyboard('{ArrowDown}')
    expect(status).toHaveFocus()
    await userEvent.keyboard('{ArrowDown}')
    expect(bug).toHaveFocus()
    await userEvent.keyboard('{ArrowDown}')
    expect(feedback).toHaveFocus()
    await userEvent.keyboard('{ArrowDown}')
    expect(settings).toHaveFocus()
    await userEvent.keyboard('{ArrowUp}')
    expect(feedback).toHaveFocus()
    await userEvent.keyboard('{Home}')
    expect(settings).toHaveFocus()
    await userEvent.keyboard('{End}')
    expect(feedback).toHaveFocus()

    // Other keys are left alone.
    await userEvent.keyboard('x')
    expect(feedback).toHaveFocus()
  })

  it('opens from the keyboard at the first or the last item', async () => {
    const { trigger } = renderMenu()
    trigger.focus()

    await userEvent.keyboard('{ArrowDown}')
    expect(screen.getByRole('menuitem', { name: /Settings/ })).toHaveFocus()

    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()

    await userEvent.keyboard('{ArrowUp}')
    expect(screen.getByRole('menuitem', { name: 'Send feedback' })).toHaveFocus()

    // Enter on an item activates it.
    await userEvent.keyboard('{Home}{Enter}')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('closes on a click outside', async () => {
    const { trigger } = renderMenu(signedOut)

    // The card before the first refresh opens nothing, so a click on it keeps the menu.
    await userEvent.click(trigger)
    await userEvent.click(screen.getByText('Not signed in yet'))
    expect(screen.getByRole('menu')).toBeInTheDocument()
    expect(platform.openExternal).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Elsewhere' }))
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('names an account without a display name by its login', async () => {
    const { trigger } = renderMenu(account({ name: '' }))

    await userEvent.click(trigger)

    expect(screen.getByText('octo-user')).toBeInTheDocument()
    expect(screen.getByText('Signed in to GitHub')).toBeInTheDocument()
  })

  it('offers Settings, Service status, a bug report, and feedback before the first refresh', async () => {
    const { trigger } = renderMenu(signedOut)

    expect(trigger).toHaveAccessibleName('Account')
    await userEvent.click(trigger)

    expect(screen.getByText('Not signed in yet')).toBeInTheDocument()
    expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual([
      'Settings⌘,',
      'Service status',
      'Report a bug',
      'Send feedback',
    ])
  })

  it('works before the inbox has loaded', async () => {
    const { trigger } = renderMenu(null)

    await userEvent.click(trigger)

    expect(screen.getByText('Not signed in yet')).toBeInTheDocument()
  })
})
