import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { platform } from '../test/platform'
import { SheetHeader, type SheetHeaderProps } from './SheetHeader'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

function header(overrides: Partial<SheetHeaderProps> = {}) {
  const props: SheetHeaderProps = {
    titleId: 't',
    icon: <span>icon</span>,
    title: 'Parser drops the trailing newline',
    url: 'https://github.com/acme-org/example-repo/issues/12',
    meta: <span>acme-org/example-repo#12 · octo-user</span>,
    position: '2 of 5 in Assigned',
    checking: false,
    prev: { title: 'Add the widget cache', onStep: vi.fn() },
    next: { title: 'Crash on start', onStep: vi.fn() },
    refreshTitle: 'Read it from GitHub again: the conversation',
    onRefresh: vi.fn(),
    onClose: vi.fn(),
    ...overrides,
  }

  return { props, ...render(<SheetHeader {...props} />) }
}

describe('SheetHeader', () => {
  it('names the item, where it is, and the cards its steps go to', async () => {
    const { props } = header()

    expect(screen.getByRole('heading', { name: 'Parser drops the trailing newline' })).toBeInTheDocument()
    expect(screen.getByText('acme-org/example-repo#12 · octo-user')).toBeInTheDocument()
    expect(screen.getByText('2 of 5 in Assigned')).toBeInTheDocument()

    const previous = screen.getByRole('button', { name: 'Previous' })
    const next = screen.getByRole('button', { name: 'Next' })
    expect(previous).toHaveAttribute('title', 'Previous: Add the widget cache (←)')
    expect(previous).toHaveAttribute('data-step', 'prev')
    expect(next).toHaveAttribute('title', 'Next: Crash on start (→)')
    expect(next).toHaveAttribute('data-step', 'next')

    await userEvent.click(previous)
    expect(props.prev?.onStep).toHaveBeenCalledOnce()
    await userEvent.click(next)
    expect(props.next?.onStep).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Read it from GitHub again' }))
    expect(props.onRefresh).toHaveBeenCalledOnce()
    await userEvent.click(screen.getByRole('button', { name: 'Open on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/issues/12')
    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(props.onClose).toHaveBeenCalledOnce()
  })

  it('stops at the ends of its list, and opens nothing without a page', () => {
    header({ prev: null, next: null, url: '', position: '' })

    expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Previous' })).toHaveAttribute('title', 'Previous (←)')
    expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Next' })).toHaveAttribute('title', 'Next (→)')
    expect(screen.getByRole('button', { name: 'Open on GitHub' })).toBeDisabled()
    expect(screen.queryByText(/in Assigned/)).not.toBeInTheDocument()
  })

  it('waits while GitHub is read, says so, and draws the light along its edge', async () => {
    const { props, container } = header({ checking: true })

    const refresh = screen.getByRole('button', { name: 'Read it from GitHub again' })
    expect(refresh).toHaveAttribute('aria-disabled', 'true')
    expect(refresh).toHaveAttribute('title', 'Checking GitHub…')
    expect(screen.getByText('Checking GitHub…')).toBeInTheDocument()
    expect(container.querySelector('.loading-bar')).toBeInTheDocument()

    await userEvent.click(refresh)
    expect(props.onRefresh).not.toHaveBeenCalled()
  })

  it('says what a refresh reads while it can run', () => {
    header()

    expect(screen.getByRole('button', { name: 'Read it from GitHub again' })).toHaveAttribute(
      'title',
      'Read it from GitHub again: the conversation',
    )
  })

  it('goes back to the sheet a link was followed from, after the sheet’s own buttons', async () => {
    const onBack = vi.fn()
    header({
      actions: <button type="button">Snooze</button>,
      back: { label: 'acme-org/example-repo#7', title: 'Add the widget cache', onBack },
    })

    const back = screen.getByRole('button', { name: 'Back to acme-org/example-repo#7' })
    expect(back).toHaveAttribute('title', 'Back to acme-org/example-repo#7: Add the widget cache')
    expect(back).toHaveTextContent('acme-org/example-repo#7')

    const buttons = screen.getAllByRole('button').map((b) => b.getAttribute('aria-label') ?? b.textContent)
    expect(buttons.slice(0, 3)).toEqual(['Snooze', 'Back to acme-org/example-repo#7', 'Previous'])

    await userEvent.click(back)
    expect(onBack).toHaveBeenCalledOnce()
  })
})
