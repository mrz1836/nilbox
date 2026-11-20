import type { Bulk } from '../../lib/useWorktrees'
import { bulkSummary } from '../../lib/worktreeSummary'
import { ResultCard } from './ResultCard'

// BulkResults reports the outcome of a bulk action on the result card's line: how many
// worktrees it took and how many branches went with them; under it, set in under the
// headline, each worktree it left, or whose branch it kept, with the reason, which outlast
// the notes on the rows, gone with the listing after the action. Past a few lines the notes
// scroll, so a long list never pushes the clones off the page.
export function BulkResults({ bulk, onDismiss }: { bulk: Bulk; onDismiss: () => void }) {
  const notes = bulk.results.filter((r) => r.reason !== '')

  return (
    <ResultCard summary={bulkSummary(bulk.results, bulk.noun, bulk.verb)} onDismiss={onDismiss}>
      {notes.length > 0 && (
        <ul
          aria-label="Notes"
          className="flex max-h-28 flex-col gap-0.5 overflow-y-auto pt-0.5 pb-1 pl-6 text-xs text-muted"
        >
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
