import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  DescriptionSkeleton,
  DiffLinesSkeleton,
  DiffSkeleton,
  FileListSkeleton,
  ReadingBanner,
  SummarySkeleton,
} from './ReviewLoading'

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('ReadingBanner', () => {
  it('names the pull request and counts the seconds once the read runs a few, unannounced', () => {
    render(<ReadingBanner target="acme-org/api#212" />)

    expect(screen.getByRole('region', { name: 'Reading from GitHub' })).toHaveTextContent(
      'Reading acme-org/api#212 from GitHub…',
    )

    act(() => {
      vi.advanceTimersByTime(2000)
    })
    expect(screen.queryByText('2s')).not.toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(2000)
    })
    expect(screen.getByText('4s')).toHaveAttribute('aria-hidden', 'true')
  })

  it('reads as the pull request when nothing names it yet', () => {
    render(<ReadingBanner target="" />)

    expect(screen.getByRole('region', { name: 'Reading from GitHub' })).toHaveTextContent(
      'Reading the pull request from GitHub…',
    )
  })
})

describe('the placeholders', () => {
  it('are all hidden from assistive technology', () => {
    const { container } = render(
      <>
        <FileListSkeleton />
        <DescriptionSkeleton />
        <DiffSkeleton />
        <SummarySkeleton />
      </>,
    )

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
