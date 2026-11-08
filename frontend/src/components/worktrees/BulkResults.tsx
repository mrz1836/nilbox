import type { Bulk } from '../../lib/useWorktrees'
import { plural } from './shared'

// BulkResults reports the outcome of a bulk action, listing every worktree that was not
// removed, or whose branch was kept, with the reason.
export function BulkResults({ bulk }: { bulk: Bulk }) {
  const { results, noun, verb } = bulk

  if (results.length === 0) {
    return (
      <p role="status" className="text-sm text-muted">
        There were no {noun}s to remove.
      </p>
    )
  }

  const removed = results.filter((r) => r.removed).length
  const notes = results.filter((r) => r.reason !== '')

  return (
    <div role="status" className="flex flex-col gap-1 text-sm">
      <p className={removed === results.length ? 'text-success' : 'text-warning'}>
        {verb} {String(removed)} of {plural(results.length, noun)}.
      </p>
      {notes.length > 0 && (
        <ul className="list-disc pl-5 text-xs text-muted">
          {notes.map((r) => (
            <li key={r.path}>
              {r.displayPath}: {r.reason}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
