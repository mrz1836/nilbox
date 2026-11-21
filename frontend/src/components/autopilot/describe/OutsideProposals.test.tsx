import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { DescribeCard } from '../../../platform'
import { at } from '../../../test/clock'
import { autopilotCatalog, describeCard } from '../../../test/fixtures'
import { listening, platform, send } from '../../../test/platform'
import { OutsideProposals } from './OutsideProposals'

vi.mock('../../../platform', () => import('../../../test/platform').then((fake) => fake.platform))

// outside is a card from a session of the user's own Claude Code, five minutes before the
// tests' noon.
function outside(overrides: Partial<DescribeCard> = {}): DescribeCard {
  return describeCard({
    ref: { source: 'outside', proposal: '20260615T115500Z-000000000003' },
    at: '2026-06-15T11:55:00Z',
    where: 'example-repo',
    turn: 0,
    summary: 'A routine from your terminal.',
    ...overrides,
  })
}

// show renders the cards from the user's own Claude Code.
function show() {
  return render(
    <OutsideProposals
      kit={{
        defaultMode: 'ask',
        catalog: autopilotCatalog(),
        onEditInBuilder: vi.fn(),
        onShowRoutine: vi.fn(),
        onSettingsSaved: vi.fn(),
        describeDelay: 0,
      }}
    />,
    { wrapper: at() },
  )
}

describe('OutsideProposals', () => {
  it('shows nothing while there are none, and the cards as they come', async () => {
    const { container } = show()

    await waitFor(() => {
      expect(listening('describeit:proposals')).toBe(1)
    })
    await waitFor(() => {
      expect(platform.describeItProposals).toHaveBeenCalledOnce()
    })
    expect(container).toBeEmptyDOMElement()

    platform.describeItProposals.mockResolvedValue([
      outside(),
      outside({ ref: { source: 'outside', proposal: '20260615T115000Z-000000000004' }, where: '' }),
    ])
    send('describeit:proposals')

    const region = within(await screen.findByRole('region', { name: 'From your Claude Code' }))

    expect(region.getAllByRole('article')).toHaveLength(2)
    expect(region.getByText('A session in example-repo,')).toBeInTheDocument()
    expect(region.getByText('A session,')).toBeInTheDocument()
    expect(region.getAllByText('5m ago')).toHaveLength(2)
  })

  it('discards a whole card, reading the cards again, and says why it couldn’t', async () => {
    const user = userEvent.setup()

    platform.describeItProposals.mockResolvedValue([outside()])
    show()

    const card = within(await screen.findByRole('article', { name: 'A routine from your terminal.' }))

    platform.discardDescribeItProposal.mockRejectedValueOnce(new Error('The card is gone.'))
    await user.click(card.getByRole('button', { name: 'Discard this card' }))
    expect(await card.findByText('The card is gone.')).toBeInTheDocument()
    expect(platform.discardDescribeItProposal).toHaveBeenCalledWith(outside().ref)

    platform.describeItProposals.mockResolvedValue([])
    await user.click(card.getByRole('button', { name: 'Discard this card' }))
    await waitFor(() => {
      expect(screen.queryByRole('region', { name: 'From your Claude Code' })).not.toBeInTheDocument()
    })
  })

  it('reads the cards again once a part of one is dealt with', async () => {
    const user = userEvent.setup()

    platform.describeItProposals.mockResolvedValue([outside()])
    show()

    const card = within(await screen.findByRole('article', { name: 'A routine from your terminal.' }))

    await user.click(
      within(card.getByRole('group', { name: 'Snooze passing bot PRs' })).getByRole('button', { name: 'Discard' }),
    )
    await waitFor(() => {
      expect(platform.describeItProposals).toHaveBeenCalledTimes(2)
    })
    expect(platform.settleDescribeItCard.mock.lastCall?.[0]).toMatchObject({ ref: outside().ref, kind: 'discarded' })
  })

  it('says quietly why the cards couldn’t be read', async () => {
    platform.describeItProposals.mockRejectedValue(new Error('The outside folder can’t be read.'))
    show()

    expect(
      await screen.findByText(
        'The proposals from your Claude Code couldn’t be read: The outside folder can’t be read.',
      ),
    ).toBeInTheDocument()
  })
})
