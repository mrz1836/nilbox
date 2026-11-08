import { useEffect, useEffectEvent } from 'react'

import { checkBudget, getBudget, on, type BudgetGauge } from '../platform'
import { attempt } from './call'
import { useQuery } from './useQuery'

// BudgetState is the GitHub API budgets as the window shows them.
export interface BudgetState {
  // gauge is null until the budgets load.
  gauge: BudgetGauge | null
  // check reads every budget from GitHub now, which costs no budget; the backend skips the
  // read while every budget was read moments ago. A failed read leaves the gauge as it was.
  check: () => void
}

// useBudget loads the API budgets and loads them again whenever the backend reports a
// change: every GitHub response the app gets says what is left, so the gauge follows each
// call the app makes, whichever part of it made the call.
export function useBudget(): BudgetState {
  const budget = useQuery(() => getBudget(), [], { log: 'could not read the API budget' })
  const reloadOnUpdate = useEffectEvent(() => {
    void budget.refetch()
  })

  useEffect(
    () =>
      on('budget:updated', () => {
        reloadOnUpdate()
      }),
    [],
  )

  const check = () => {
    void attempt(checkBudget()).then((result) => {
      if (result.ok) {
        budget.set(() => result.data)
      } else {
        console.error('could not read the API budget:', result.error)
      }
    })
  }

  return { gauge: budget.data ?? null, check }
}
