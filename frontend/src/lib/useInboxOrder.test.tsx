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
    platform.getInboxOrder.mockResolvedValue({ statusSorted: ['assigned'], statusOrder: ['In progress'], kinds: {} })
    const { result } = renderHook(() => useInboxOrder())

    expect(result.current.order).toBeNull()

    await waitFor(() => {
      expect(result.current.order).toEqual({ statusSorted: ['assigned'], statusOrder: ['In progress'], kinds: {} })
    })
  })

  it('sorts a list by status and back, storing each change in order', async () => {
    const { result } = await loaded()

    act(() => {
      result.current.setStatusSort('assigned', true)
    })
    expect(result.current.order?.statusSorted).toEqual(['assigned'])

    act(() => {
      result.current.setStatusSort('assigned', false)
    })
    expect(result.current.order?.statusSorted).toEqual([])

    await waitFor(() => {
      expect(platform.setStatusSort.mock.calls).toEqual([
        ['assigned', true],
        ['assigned', false],
      ])
    })
  })

  it('shows only one kind in a list and every item again, storing each change in order', async () => {
    const { result } = await loaded()

    act(() => {
      result.current.setListKind('assigned', 'issue')
      result.current.setListKind('authored', 'pr')
    })
    expect(result.current.order?.kinds).toEqual({ assigned: 'issue', authored: 'pr' })

    act(() => {
      result.current.setListKind('assigned', 'all')
    })
    expect(result.current.order?.kinds).toEqual({ authored: 'pr' })

    await waitFor(() => {
      expect(platform.setListKind.mock.calls).toEqual([
        ['assigned', 'issue'],
        ['authored', 'pr'],
        ['assigned', 'all'],
      ])
    })
  })

  it('sorts a second list by status alongside the first', async () => {
    platform.getInboxOrder.mockResolvedValue({ statusSorted: ['assigned'], statusOrder: [], kinds: {} })
    const { result } = await loaded()

    act(() => {
      result.current.setStatusSort('reviewRequested', true)
      result.current.setStatusSort('assigned', true)
    })

    expect(result.current.order?.statusSorted).toEqual(['reviewRequested', 'assigned'])
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
      result.current.setStatusSort('assigned', true)
    })

    expect(result.current.order?.statusSorted).toEqual(['assigned'])
    await waitFor(() => {
      expect(logged).toHaveBeenCalledWith('could not store how the assigned list is sorted', expect.any(Error))
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
