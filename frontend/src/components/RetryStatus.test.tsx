import { act, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { FIXED_NOW, fromNow, renderAt } from '../test/clock'
import { RetryStatus } from './RetryStatus'

const error = 'Refresh the inbox: read the item details: the GitHub API returned HTTP 500.'

describe('RetryStatus', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'], now: FIXED_NOW })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('counts down to the next try, each second', () => {
    renderAt(<RetryStatus at={fromNow(12_000).toISOString()} failures={1} error={error} />)

    const pill = screen.getByTitle(/HTTP 500/)
    expect(pill).toHaveTextContent('Couldn’t refresh · retrying in 12s')

    act(() => {
      vi.advanceTimersByTime(5000)
    })
    expect(pill).toHaveTextContent('retrying in 7s')

    act(() => {
      vi.advanceTimersByTime(7000)
    })
    expect(pill).toHaveTextContent('Couldn’t refresh · retrying now…')
  })

  it('counts a longer wait in minutes and seconds', () => {
    renderAt(<RetryStatus at={fromNow(90_000).toISOString()} failures={3} error={error} />)

    expect(screen.getByTitle(/HTTP 500/)).toHaveTextContent('retrying in 1m 30s')
  })

  it('holds the error and the tries in its tooltip', () => {
    const { rerender } = renderAt(<RetryStatus at={fromNow(15_000).toISOString()} failures={1} error={error} />)

    expect(screen.getByTitle(/HTTP 500/)).toHaveAttribute('title', `${error}\nTried once; trying again on its own.`)

    rerender(<RetryStatus at={fromNow(60_000).toISOString()} failures={3} error={error} />)
    expect(screen.getByTitle(/HTTP 500/)).toHaveAttribute('title', `${error}\nTried 3 times; trying again on its own.`)
  })

  it('tells a screen reader once that the inbox will try again, never the countdown', () => {
    renderAt(<RetryStatus at={fromNow(12_000).toISOString()} failures={1} error={error} />)

    expect(screen.getByRole('status')).toHaveTextContent(
      'The last refresh failed. The inbox will try again on its own.',
    )
    expect(screen.getByRole('status')).not.toHaveTextContent('12s')
  })
})
