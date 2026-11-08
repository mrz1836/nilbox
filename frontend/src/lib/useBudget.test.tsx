import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { budgetGauge, reading } from '../test/fixtures'
import { emit, platform } from '../test/platform'
import { useBudget } from './useBudget'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('useBudget', () => {
  it('loads the budgets, and loads them again whenever the backend says they changed', async () => {
    platform.getBudget.mockResolvedValue(budgetGauge({ graphql: reading({ used: 20 }) }))
    const { result } = renderHook(() => useBudget())

    expect(result.current.gauge).toBeNull()
    await waitFor(() => {
      expect(result.current.gauge?.graphql.used).toBe(20)
    })

    platform.getBudget.mockResolvedValue(budgetGauge({ graphql: reading({ used: 25 }) }))
    act(() => {
      emit('budget:updated')
    })

    await waitFor(() => {
      expect(result.current.gauge?.graphql.used).toBe(25)
    })
  })

  it('checks every budget with GitHub and shows the answer', async () => {
    platform.checkBudget.mockResolvedValue(budgetGauge({ core: reading({ used: 40 }) }))
    const { result } = renderHook(() => useBudget())

    await waitFor(() => {
      expect(result.current.gauge).not.toBeNull()
    })

    act(() => {
      result.current.check()
    })

    await waitFor(() => {
      expect(result.current.gauge?.core.used).toBe(40)
    })
  })

  it('keeps the gauge as it was when a check fails, and logs why', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.getBudget.mockResolvedValue(budgetGauge({ graphql: reading({ used: 20 }) }))
    platform.checkBudget.mockRejectedValue(new Error('offline'))
    const { result } = renderHook(() => useBudget())

    await waitFor(() => {
      expect(result.current.gauge?.graphql.used).toBe(20)
    })

    act(() => {
      result.current.check()
    })

    await waitFor(() => {
      expect(logged).toHaveBeenCalledWith('could not read the API budget:', 'offline')
    })
    expect(result.current.gauge?.graphql.used).toBe(20)
  })
})
