import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Settings } from '../../platform'
import { settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { CardsSection } from './CardsSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

function renderSection(current: Settings = settings()) {
  const onSaved = vi.fn()

  render(<CardsSection settings={current} onSaved={onSaved} />)

  return { onSaved }
}

function hiddenList() {
  return within(screen.getByRole('list', { name: 'Hidden labels' }))
}

beforeEach(() => {
  // Each change comes back with the settings as stored.
  platform.setHiddenLabels.mockImplementation((hiddenLabels) =>
    Promise.resolve(settings({ hiddenLabels: hiddenLabels ?? [] })),
  )
})

describe('CardsSection', () => {
  it('lists the hidden labels, marking the defaults, and says how a pattern reads', () => {
    renderSection(settings({ hiddenLabels: ['size/*', 'wontfix'] }))

    expect(
      hiddenList()
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['size/*default', 'wontfix'])
    expect(screen.getByRole('button', { name: 'Restore the defaults' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Label to hide' })).toHaveAccessibleDescription(
      /matches anything, as in size\/\*.*Hidden labels still match the filter\./,
    )
  })

  it('offers no restore while the hidden labels are the defaults, in any case', () => {
    renderSection(settings({ hiddenLabels: ['Size/*'] }))

    expect(screen.queryByRole('button', { name: 'Restore the defaults' })).not.toBeInTheDocument()
    expect(hiddenList().getByRole('listitem')).toHaveTextContent('Size/*default')
  })

  it('hides another label', async () => {
    const { onSaved } = renderSection()

    await userEvent.type(screen.getByRole('textbox', { name: 'Label to hide' }), ' priority/* ')
    await userEvent.click(screen.getByRole('button', { name: 'Hide label' }))

    expect(platform.setHiddenLabels).toHaveBeenCalledWith(['size/*', 'priority/*'])
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ hiddenLabels: ['size/*', 'priority/*'] }))
    })
    expect(screen.getByRole('textbox', { name: 'Label to hide' })).toHaveValue('')
  })

  it('says when a label is already hidden, ignoring case, or the backend refuses it', async () => {
    platform.setHiddenLabels.mockRejectedValue(new Error('a hidden label pattern is too long: keep each to 100'))
    renderSection()

    const input = screen.getByRole('textbox', { name: 'Label to hide' })

    await userEvent.type(input, 'SIZE/*')
    await userEvent.click(screen.getByRole('button', { name: 'Hide label' }))

    expect(screen.getByRole('alert')).toHaveTextContent('SIZE/* is already hidden.')
    expect(platform.setHiddenLabels).not.toHaveBeenCalled()

    await userEvent.clear(input)
    await userEvent.type(input, 'x'.repeat(101))
    await userEvent.click(screen.getByRole('button', { name: 'Hide label' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('too long')

    // Typing again clears the refusal.
    await userEvent.type(input, '!')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows a label again', async () => {
    const { onSaved } = renderSection(settings({ hiddenLabels: ['size/*', 'wontfix'] }))

    await userEvent.click(hiddenList().getByRole('button', { name: 'Show wontfix again' }))

    expect(platform.setHiddenLabels).toHaveBeenCalledWith(['size/*'])
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings())
    })
  })

  it('restores the defaults', async () => {
    renderSection(settings({ hiddenLabels: [] }))

    expect(screen.getByText('Every label shows on the cards.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Restore the defaults' }))

    expect(platform.setHiddenLabels).toHaveBeenCalledWith(['size/*'])
  })
})
