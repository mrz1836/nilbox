import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { costWords, tokenWords, weekCounts } from '../../lib/routineWeek'
import { routineStats } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { WeekStats } from './WeekStats'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// busy is a week with every count, a cost, and tokens.
function busy() {
  return routineStats({
    runs: 12,
    fixes: 3,
    merges: 1,
    reviews: 2,
    actions: 4,
    scripts: 1,
    costUsd: 1.2,
    costed: 2,
    estimated: 2,
    tokensIn: 820_000,
    tokensOut: 12_000,
    cacheRead: 380_000,
    counted: 2,
  })
}

describe("a routine's week", () => {
  it('counts what its runs did, then what they spent, the dollars as an estimate', async () => {
    platform.routineStats.mockResolvedValue(busy())
    render(<WeekStats routineID={3} version="a" />)

    expect(
      await screen.findByText('12 agent runs · 3 fixes · 1 merge · 2 reviews posted · 4 GitHub actions · 1 script run'),
    ).toBeInTheDocument()

    const spent = screen.getByText(/about \$1\.20, as 2 runs estimated it/)

    expect(spent).toHaveTextContent('1.2M tokens, 380K of them read from the cache')
    expect(spent).toHaveAttribute('title', expect.stringContaining('the dollars are an estimate'))
    expect(platform.routineStats).toHaveBeenCalledWith(3)
  })

  it('says when nothing ran, and reads again as the routine moves', async () => {
    const { rerender } = render(<WeekStats routineID={3} version="a" />)

    expect(await screen.findByText('Nothing ran in the last 7 days.')).toBeInTheDocument()

    platform.routineStats.mockResolvedValue(routineStats({ runs: 1 }))
    rerender(<WeekStats routineID={3} version="b" />)

    expect(await screen.findByText('1 agent run')).toBeInTheDocument()
    expect(platform.routineStats).toHaveBeenCalledTimes(2)
  })

  it('shows why it couldn’t be read', async () => {
    platform.routineStats.mockRejectedValue(new Error('The routine’s week couldn’t be read.'))
    render(<WeekStats routineID={3} version="a" />)

    expect(await screen.findByRole('alert')).toHaveTextContent('The routine’s week couldn’t be read.')
  })
})

describe("the week's words", () => {
  it('leaves out what is zero', () => {
    expect(weekCounts(routineStats())).toEqual([])
    expect(weekCounts(routineStats({ actions: 1, merges: 2 }))).toEqual(['2 merges', '1 GitHub action'])
  })

  it('says a cost only when a run reported one, always as an estimate, and how it was reported', () => {
    expect(costWords(routineStats())).toBe('')
    expect(costWords(routineStats({ costUsd: 0.5, costed: 1, estimated: 1 }))).toBe(
      'about $0.50, as its run estimated it',
    )
    expect(costWords(routineStats({ costUsd: 0.5, costed: 2 }))).toBe('about $0.50, as 2 runs reported it')
    expect(costWords(routineStats({ costUsd: 3, costed: 3, estimated: 2 }))).toBe(
      'about $3.00, as 3 runs reported it, 2 by estimate',
    )
  })

  it('counts tokens, and those from the cache', () => {
    expect(tokenWords(routineStats())).toBe('')
    expect(tokenWords(routineStats({ tokensIn: 900, tokensOut: 100 }))).toBe('1K tokens')
    expect(tokenWords(routineStats({ tokensOut: 1 }))).toBe('1 token')
  })
})
