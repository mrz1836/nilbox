import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { GitHubTag } from './GitHubTag'

describe('GitHubTag', () => {
  it('counts the unread notifications, with why GitHub notified you about the item in its tooltip', () => {
    render(<GitHubTag tag={{ unread: 2, reasons: ['review_requested', 'mention'] }} />)

    expect(screen.getByText('2 unread on GitHub')).toHaveAttribute(
      'title',
      '2 unread on GitHub · GitHub notified you: Review requested, Mentioned',
    )
  })

  it('says one in the singular, and a reason it does not know as GitHub wrote it', () => {
    render(<GitHubTag tag={{ unread: 1, reasons: ['member_feature_requested'] }} />)

    expect(screen.getByText('1 unread on GitHub')).toHaveAttribute(
      'title',
      '1 unread on GitHub · GitHub notified you: member_feature_requested',
    )
  })

  it('shows nothing while none are unread', () => {
    const { container } = render(<GitHubTag tag={{ unread: 0, reasons: ['mention'] }} />)

    expect(container).toBeEmptyDOMElement()
  })
})
