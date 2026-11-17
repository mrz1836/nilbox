import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { settings } from '../test/fixtures'
import { platform } from '../test/platform'
import { useInboxOrder } from './useInboxOrder'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// loaded renders the hook and waits for the stored order.
async function loaded() {
  const view = renderHook(() => useInboxOrder())

  await waitFor(() => {
    expect(view.result.current.order).not.toBeNull()
  })

  return view
}

describe('useInboxOrder', () => {
  it('is null until the stored order loads, then holds it', async () => {
    platform.getInboxOrder.mockResolvedValue({ statusSorted: ['yours'], statusOrder: ['In progress'], kinds: {} })
    const { result } = renderHook(() => useInboxOrder())

    expect(result.current.order).toBeNull()

    await waitFor(() => {
      expect(result.current.order).toEqual({ statusSorted: ['yours'], statusOrder: ['In progress'], kinds: {} })
    })
  })

  it('sorts a list by status and back, storing each change in order', async () => {
    const { result } = await loaded()

    act(() => {
      result.current.setStatusSort('yours', true)
    })
    expect(result.current.order?.statusSorted).toEqual(['yours'])

    act(() => {
      result.current.setStatusSort('yours', false)
    })
    expect(result.current.order?.statusSorted).toEqual([])

    await waitFor(() => {
      expect(platform.setStatusSort.mock.calls).toEqual([
        ['yours', true],
        ['yours', false],
      ])
    })
  })

  it('shows only one kind in a list and every item again, storing each change in order', async () => {
    const { result } = await loaded()

    act(() => {
      result.current.setListKind('yours', 'issue')
      result.current.setListKind('reviewRequested', 'pr')
    })
    expect(result.current.order?.kinds).toEqual({ yours: 'issue', reviewRequested: 'pr' })

    act(() => {
      result.current.setListKind('yours', 'all')
    })
    expect(result.current.order?.kinds).toEqual({ reviewRequested: 'pr' })

    await waitFor(() => {
      expect(platform.setListKind.mock.calls).toEqual([
        ['yours', 'issue'],
        ['reviewRequested', 'pr'],
        ['yours', 'all'],
      ])
    })
  })

  it('sorts a second list by status alongside the first', async () => {
    platform.getInboxOrder.mockResolvedValue({ statusSorted: ['yours'], statusOrder: [], kinds: {} })
    const { result } = await loaded()

    act(() => {
      result.current.setStatusSort('reviewRequested', true)
      result.current.setStatusSort('yours', true)
    })

    expect(result.current.order?.statusSorted).toEqual(['reviewRequested', 'yours'])
  })

  it('shows a new status order at once and stores it', async () => {
    const { result } = await loaded()

    act(() => {
      result.current.setStatusOrder(['Ready', 'In progress'])
    })

    expect(result.current.order?.statusOrder).toEqual(['Ready', 'In progress'])
    await waitFor(() => {
      expect(platform.setStatusOrder).toHaveBeenCalledWith(['Ready', 'In progress'])
    })
  })

  it('keeps a change it could not store for this session, and logs why', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.setStatusSort.mockRejectedValue(new Error('locked'))
    const { result } = await loaded()

    act(() => {
      result.current.setStatusSort('yours', true)
    })

    expect(result.current.order?.statusSorted).toEqual(['yours'])
    await waitFor(() => {
      expect(logged).toHaveBeenCalledWith('could not store how the yours list is sorted', expect.any(Error))
    })
  })

  it('orders every list by newest activity when the stored order cannot be read', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.getInboxOrder.mockRejectedValue(new Error('locked'))
    const { result } = await loaded()

    expect(result.current.order).toEqual({ statusSorted: [], statusOrder: [], kinds: {} })
    expect(settings().inboxOrder).toEqual(result.current.order)

    act(() => {
      result.current.setStatusOrder(['Ready'])
    })
    expect(result.current.order).toEqual({ statusSorted: [], statusOrder: ['Ready'], kinds: {} })
  })
})
