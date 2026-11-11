import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { DiffLinesSkeleton, ReadingBanner, SheetSkeleton } from './ReviewLoading'

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('ReadingBanner', () => {
  it('names the pull request and counts the seconds once the read runs a few, unannounced', () => {
    render(<ReadingBanner target="acme-org/api#212" />)

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent('Reading acme-org/api#212 from GitHub…')

    act(() => {
      vi.advanceTimersByTime(2000)
    })
    expect(screen.queryByText('2s')).not.toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(2000)
    })
    expect(screen.getByText('4s')).toHaveAttribute('aria-hidden', 'true')
  })
})

describe('SheetSkeleton', () => {
  it('shows the banner over placeholders that assistive technology skips', () => {
    const { container } = render(<SheetSkeleton target="acme-org/api#212" />)

    expect(screen.getByRole('status')).toHaveTextContent('Reading acme-org/api#212 from GitHub…')
    // Every placeholder sits inside something hidden from assistive technology.
    const placeholders = [...container.querySelectorAll('.skeleton')]
    expect(placeholders.length).toBeGreaterThan(10)
    expect(placeholders.every((p) => p.closest('[aria-hidden="true"]') !== null)).toBe(true)
  })
})

describe('DiffLinesSkeleton', () => {
  it('shows as many lines as asked, at most a hunk', () => {
    const { container, rerender } = render(<DiffLinesSkeleton lines={3} />)

    expect(container.querySelectorAll('[aria-hidden="true"] > div')).toHaveLength(3)

    rerender(<DiffLinesSkeleton lines={50} />)
    expect(container.querySelectorAll('[aria-hidden="true"] > div')).toHaveLength(8)
  })
})
