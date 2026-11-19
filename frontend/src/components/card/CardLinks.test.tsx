import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { CardLinksContext, linkIndex, type CardLinks } from '../../lib/cardLinks'
import type { InboxItem } from '../../platform'
import { closingPull, details, item, list, snapshot } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { LinkChip } from './CardLinks'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// The listed pull request #8 closes the listed issue #5 once it merges; #30, in another of
// the owner's repositories, closes it too, and the inbox doesn't hold it.
const pull = item({ id: 1, number: 8, title: 'Fix the flaky retry test' })
const issue = item({
  id: 2,
  kind: 'issue',
  number: 5,
  title: 'The retry test is flaky',
  details: details({ closingPulls: [closingPull()] }),
})

// inbox is the inbox around the cards: its lists, and a reveal that records the card shown.
function inbox(items: InboxItem[]): CardLinks {
  return { index: linkIndex(snapshot({ yours: list(items) })), reveal: vi.fn() }
}

// chip renders a card's link chip inside links, or with no inbox around it.
function chip(it: InboxItem, links: CardLinks | null, compact = false) {
  render(
    <CardLinksContext value={links}>
      <LinkChip item={it} compact={compact} />
    </CardLinksContext>,
  )
}

describe('LinkChip', () => {
  it('links an issue to the listed pull request fixing it, and shows its card', async () => {
    const links = inbox([pull, issue])
    chip(issue, links)

    const button = screen.getByRole('button', {
      name: 'Fix in progress: #8 Fix the flaky retry test. Show it in the inbox',
    })
    expect(button).toHaveTextContent(/^#8$/)
    expect(button).toHaveAttribute('title', 'Fix in progress: #8 Fix the flaky retry test\nShow it in the inbox')

    await userEvent.click(button)
    expect(links.reveal).toHaveBeenCalledExactlyOnceWith(1)
    expect(platform.openExternal).not.toHaveBeenCalled()
  })

  it('links a pull request to the listed issue it closes', async () => {
    const links = inbox([pull, issue])
    chip(pull, links)

    await userEvent.click(
      screen.getByRole('button', { name: 'Closes #5 The retry test is flaky once merged. Show it in the inbox' }),
    )
    expect(links.reveal).toHaveBeenCalledExactlyOnceWith(2)
  })

  it('opens a fix the inbox doesn’t hold on GitHub, names it from its repository, and counts the rest', async () => {
    const elsewhere = closingPull({
      repo: 'acme-org/other-repo',
      number: 30,
      url: 'https://github.com/acme-org/other-repo/pull/30',
      merged: true,
    })
    const links = inbox([issue])
    chip(item({ ...issue, details: details({ closingPulls: [elsewhere, closingPull()] }) }), links)

    const button = screen.getByRole('button', {
      name: 'Fix merged: other-repo#30 Fix the flaky retry test, and 1 more. Open it on GitHub',
    })
    expect(button).toHaveTextContent('other-repo#30+1')
    expect(button.getAttribute('title')?.split('\n')).toEqual([
      'Fix merged: other-repo#30 Fix the flaky retry test',
      'Fix in progress: #8 Fix the flaky retry test',
      'Open it on GitHub',
    ])

    await userEvent.click(button)
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/other-repo/pull/30')
    expect(links.reveal).not.toHaveBeenCalled()
  })

  it('says a fix from outside the project is one', () => {
    chip(item({ ...issue, details: details({ closingPulls: [closingPull({ trusted: false })] }) }), inbox([issue]))

    expect(
      screen.getByRole('button', {
        name: 'Fix in progress: #8 Fix the flaky retry test, from outside the project. Open it on GitHub',
      }),
    ).toBeInTheDocument()
  })

  it('outside the inbox, still links an issue to GitHub, and a pull request to nothing', async () => {
    chip(issue, null)
    await userEvent.click(screen.getByRole('button', { name: /^Fix in progress: #8 .*Open it on GitHub$/ }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/8')

    chip(pull, null)
    expect(screen.getAllByRole('button')).toHaveLength(1)
  })

  it('draws nothing for a card linked to nothing', () => {
    chip(item({ id: 9, number: 99 }), inbox([pull, issue]))
    chip(item({ id: 10, kind: 'issue', number: 98, details: null }), inbox([pull, issue]))

    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('draws a compact card’s link without the chip’s outline', () => {
    chip(issue, inbox([pull, issue]), true)

    expect(screen.getByRole('button')).not.toHaveClass('border')
  })
})
