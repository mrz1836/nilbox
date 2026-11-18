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
  it('chooses the cards’ density, storing each choice', async () => {
    platform.setCardDensity.mockImplementation((cardDensity) => Promise.resolve(settings({ cardDensity })))
    const { onSaved } = renderSection()

    const density = screen.getByRole('radiogroup', { name: 'Density' })
    expect(within(density).getByRole('radio', { name: 'Comfortable' })).toBeChecked()
    expect(
      screen.getByText(
        'Comfortable shows two lines per card, with the state track and the agent beside your next step. Compact fits about three times as many, one line each. Both lists follow it.',
      ),
    ).toBeInTheDocument()

    await userEvent.click(within(density).getByRole('radio', { name: 'Compact' }))

    expect(platform.setCardDensity).toHaveBeenCalledExactlyOnceWith('compact')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ cardDensity: 'compact' }))
    })
  })

  it('says why the density could not change, and shows it as stored', async () => {
    platform.setCardDensity.mockRejectedValue(new Error('the database is locked'))
    renderSection(settings({ cardDensity: 'compact' }))

    const density = screen.getByRole('radiogroup', { name: 'Density' })
    await userEvent.click(within(density).getByRole('radio', { name: 'Comfortable' }))

    await waitFor(() => {
      expect(density).toHaveAccessibleDescription(/the database is locked/)
    })
    // The change shows at once and goes back as its save ends, which can be a render
    // after the refusal shows.
    await waitFor(() => {
      expect(within(density).getByRole('radio', { name: 'Compact' })).toBeChecked()
    })
  })

  it('turns Author pictures off and on, storing each change', async () => {
    platform.setCardAvatars.mockImplementation((cardAvatars) => Promise.resolve(settings({ cardAvatars })))
    const { onSaved } = renderSection()

    const pictures = screen.getByRole('switch', { name: 'Author pictures' })
    expect(pictures).toBeChecked()
    expect(pictures).toHaveAccessibleDescription(
      'Each person’s GitHub picture in a circle, each bot’s in a rounded square, with initials or the bot’s mark until it arrives. Downloaded once a week each, without your token.',
    )

    await userEvent.click(pictures)

    expect(platform.setCardAvatars).toHaveBeenCalledExactlyOnceWith(false)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ cardAvatars: false }))
    })
  })

  it('says why Author pictures could not change, and shows it as stored', async () => {
    platform.setCardAvatars.mockRejectedValue(new Error('the database is locked'))
    renderSection(settings({ cardAvatars: false }))

    const pictures = screen.getByRole('switch', { name: 'Author pictures' })
    await userEvent.click(pictures)

    await waitFor(() => {
      expect(pictures).toHaveAccessibleDescription(/the database is locked/)
    })
    // The change shows at once and goes back as its save ends, which can be a render
    // after the refusal shows.
    await waitFor(() => {
      expect(pictures).not.toBeChecked()
    })
  })

  it('turns Unread dots off and on, storing each change', async () => {
    platform.setCardUnread.mockImplementation((cardUnread) => Promise.resolve(settings({ cardUnread })))
    const { onSaved } = renderSection()

    const dots = screen.getByRole('switch', { name: 'Unread dots' })
    expect(dots).toBeChecked()
    expect(dots).toHaveAccessibleDescription('A blue dot on a card with activity since you last opened it.')

    await userEvent.click(dots)

    expect(platform.setCardUnread).toHaveBeenCalledExactlyOnceWith(false)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ cardUnread: false }))
    })
  })

  it('says why Unread dots could not change, and shows it as stored', async () => {
    platform.setCardUnread.mockRejectedValue(new Error('the database is locked'))
    renderSection(settings({ cardUnread: false }))

    const dots = screen.getByRole('switch', { name: 'Unread dots' })
    await userEvent.click(dots)

    await waitFor(() => {
      expect(dots).toHaveAccessibleDescription(/the database is locked/)
    })
    // The change shows at once and goes back as its save ends, which can be a render
    // after the refusal shows.
    await waitFor(() => {
      expect(dots).not.toBeChecked()
    })
  })

  it('shows the card guide on asking, and offers it only where the window can open it', async () => {
    const onShowCardGuide = vi.fn()
    const { unmount } = render(
      <CardsSection settings={settings()} onSaved={vi.fn()} onShowCardGuide={onShowCardGuide} />,
    )

    expect(screen.getByText('How to read a card')).toBeInTheDocument()
    expect(
      screen.getByText('A sample card with its parts numbered, and a key to its colors and symbols.'),
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Show the guide' }))

    expect(onShowCardGuide).toHaveBeenCalledOnce()

    unmount()
    renderSection()

    expect(screen.queryByRole('button', { name: 'Show the guide' })).not.toBeInTheDocument()
  })

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
