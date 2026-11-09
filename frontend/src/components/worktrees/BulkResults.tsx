import type { Bulk } from '../../lib/useWorktrees'
import { bulkSummary } from '../../lib/worktreeSummary'
import { ResultCard } from './ResultCard'

// BulkResults reports the outcome of a bulk action: how many worktrees it took and how
// many branches went with them, listing every worktree left, or whose branch was kept,
// with the reason.
export function BulkResults({ bulk, onDismiss }: { bulk: Bulk; onDismiss: () => void }) {
  const notes = bulk.results.filter((r) => r.reason !== '')

  return (
    <ResultCard summary={bulkSummary(bulk.results, bulk.noun, bulk.verb)} onDismiss={onDismiss}>
      {notes.length > 0 && (
        <ul aria-label="Notes" className="mt-1 list-disc pl-5 text-xs text-muted">
          {notes.map((r) => (
            <li key={r.path}>
              {r.displayPath}: {r.reason}
            </li>
          ))}
        </ul>
      )}
    </ResultCard>
  )
}
