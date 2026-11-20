import { checkBudget, getBudget, type BudgetGauge } from '../platform'
import { logFailure } from './call'
import { useOn } from './useOn'
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

  useOn('budget:updated', () => {
    void budget.refetch()
  })

  const check = () => {
    logFailure(checkBudget(), 'could not read the API budget:', (data) => {
      budget.set(() => data)
    })
  }

  return { gauge: budget.data ?? null, check }
}
