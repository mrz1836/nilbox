import { counted } from '../../lib/format'
import type { BranchResult } from '../../platform'
import { shortCommit } from './BranchChoices'

// BranchResults reports the outcome of deleting merged branches: how many went, the
// commit each pointed at, which brings it back, and why any was kept.
export function BranchResults({ results }: { results: readonly BranchResult[] }) {
  const deleted = results.filter((r) => r.deleted)

  return (
    <div role="status" className="flex flex-col gap-1 text-sm">
      <p className={deleted.length === results.length ? 'text-success' : 'text-warning'}>
        Deleted {String(deleted.length)} of {counted(results.length, 'branch', 'branches')}.
      </p>
      <ul className="list-disc pl-5 text-xs text-muted">
        {results.map((r) => (
          <li key={`${r.repoPath}\u0000${r.name}`}>
            {r.displayRepo}: <code className="font-mono text-text">{r.name}</code>{' '}
            {r.deleted ? (
              <>
                deleted, at <code className="font-mono text-text">{shortCommit(r.head)}</code>
              </>
            ) : (
              <>kept: {r.reason}</>
            )}
          </li>
        ))}
      </ul>
      {deleted.length > 0 && (
        <p className="text-xs text-muted">
          To bring a branch back, run{' '}
          <code className="font-mono text-text">git branch &lt;name&gt; &lt;commit&gt;</code> in its clone.
        </p>
      )}
    </div>
  )
}
