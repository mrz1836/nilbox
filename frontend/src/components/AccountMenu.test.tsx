import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Account } from '../platform'
import { account, signedOut } from '../test/fixtures'
import { platform } from '../test/platform'
import { AccountMenu } from './AccountMenu'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

function renderMenu(who: Account | null = account()) {
  const onOpenSettings = vi.fn()

  render(
    <>
      <AccountMenu account={who} onOpenSettings={onOpenSettings} />
      <button type="button">Elsewhere</button>
    </>,
  )

  return { onOpenSettings, trigger: screen.getByRole('button', { name: /^Account/ }) }
}

describe('AccountMenu', () => {
  it("shows the account's picture on the button", () => {
    const { trigger } = renderMenu()

    expect(trigger).toHaveAccessibleName('Account: octo-user')
    expect(trigger).toHaveAttribute('title', 'octo-user')
    expect(trigger).toHaveAttribute('aria-haspopup', 'menu')
    expect(trigger.querySelector('img')).not.toBeNull()
  })

  it('opens a menu with the account, Settings, the GitHub profile, and GitHub’s status', async () => {
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
    ).toEqual(['Settings⌘,', 'View GitHub profile', 'GitHub status'])
    expect(within(menu).getByRole('menuitem', { name: /Settings/ })).toHaveFocus()
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

  it('opens GitHub’s status page in the browser', async () => {
    const { trigger } = renderMenu()

    await userEvent.click(trigger)
    await userEvent.click(screen.getByRole('menuitem', { name: 'GitHub status' }))

    expect(platform.openExternal).toHaveBeenCalledWith('https://www.githubstatus.com/')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('moves between items with the arrow keys, Home, and End, wrapping at the ends', async () => {
    const { trigger } = renderMenu()
    await userEvent.click(trigger)

    const settings = screen.getByRole('menuitem', { name: /Settings/ })
    const profile = screen.getByRole('menuitem', { name: 'View GitHub profile' })
    const status = screen.getByRole('menuitem', { name: 'GitHub status' })

    await userEvent.keyboard('{ArrowDown}')
    expect(profile).toHaveFocus()
    await userEvent.keyboard('{ArrowDown}')
    expect(status).toHaveFocus()
    await userEvent.keyboard('{ArrowDown}')
    expect(settings).toHaveFocus()
    await userEvent.keyboard('{ArrowUp}')
    expect(status).toHaveFocus()
    await userEvent.keyboard('{Home}')
    expect(settings).toHaveFocus()
    await userEvent.keyboard('{End}')
    expect(status).toHaveFocus()

    // Other keys are left alone.
    await userEvent.keyboard('x')
    expect(status).toHaveFocus()
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
    expect(screen.getByRole('menuitem', { name: 'GitHub status' })).toHaveFocus()

    // Enter on an item activates it.
    await userEvent.keyboard('{Home}{Enter}')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('closes on a click outside', async () => {
    const { trigger } = renderMenu()

    await userEvent.click(trigger)
    await userEvent.click(screen.getByText('Octo User'))
    expect(screen.getByRole('menu')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Elsewhere' }))
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('names an account without a display name by its login', async () => {
    const { trigger } = renderMenu(account({ name: '' }))

    await userEvent.click(trigger)

    expect(screen.getByText('octo-user')).toBeInTheDocument()
    expect(screen.getByText('Signed in to GitHub')).toBeInTheDocument()
  })

  it('offers Settings and GitHub’s status before the first refresh', async () => {
    const { trigger } = renderMenu(signedOut)

    expect(trigger).toHaveAccessibleName('Account')
    await userEvent.click(trigger)

    expect(screen.getByText('Not signed in yet')).toBeInTheDocument()
    expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual(['Settings⌘,', 'GitHub status'])
  })

  it('works before the inbox has loaded', async () => {
    const { trigger } = renderMenu(null)

    await userEvent.click(trigger)

    expect(screen.getByText('Not signed in yet')).toBeInTheDocument()
  })
})
