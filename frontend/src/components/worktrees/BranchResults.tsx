import { useState } from 'react'

import { counted } from '../../lib/format'
import { copyText, type BranchResult } from '../../platform'
import { quietButtonClass } from '../controls'
import { shortCommit } from './BranchChoices'

// shellQuoted quotes text for a POSIX shell: single quotes, each one inside closed,
// escaped, and reopened.
function shellQuoted(text: string): string {
  return `'${text.replaceAll("'", `'\\''`)}'`
}

// restoreCommands are the git commands that bring the deleted branches back, one a line:
// each branch made again at the commit it pointed at, in its clone.
function restoreCommands(results: readonly BranchResult[]): string {
  return results
    .filter((r) => r.deleted)
    .map((r) => `git -C ${shellQuoted(r.repoPath)} branch ${shellQuoted(r.name)} ${r.head}`)
    .join('\n')
}

// CopyRestore copies the commands that bring the deleted branches back.
function CopyRestore({ results }: { results: readonly BranchResult[] }) {
  const [copied, setCopied] = useState(false)

  return (
    <button
      type="button"
      onClick={() => {
        copyText(restoreCommands(results)).then(
          () => {
            setCopied(true)
          },
          (err: unknown) => {
            console.error('could not copy the restore commands', err)
          },
        )
      }}
      className={quietButtonClass}
    >
      {copied ? 'Copied' : 'Copy restore commands'}
    </button>
  )
}

// BranchResults reports the outcome of deleting merged branches: how many went, why each
// one kept stayed, and, folded away, each deleted branch with the commit it pointed at,
// which brings it back, with the commands to do so to copy.
export function BranchResults({ results }: { results: readonly BranchResult[] }) {
  const deleted = results.filter((r) => r.deleted)
  const kept = results.filter((r) => !r.deleted)

  return (
    <div role="status" className="flex flex-col gap-1 text-sm">
      <p className={kept.length === 0 ? 'text-success' : 'text-warning'}>
        Deleted {String(deleted.length)} of {counted(results.length, 'branch', 'branches')}.
      </p>
      {kept.length > 0 && (
        <ul aria-label="Branches kept" className="list-disc pl-5 text-xs text-muted">
          {kept.map((r) => (
            <li key={`${r.repoPath}\u0000${r.name}`}>
              {r.displayRepo}: <code className="font-mono text-text">{r.name}</code> kept: {r.reason}
            </li>
          ))}
        </ul>
      )}
      {deleted.length > 0 && (
        <details className="text-xs text-muted">
          <summary className="cursor-pointer select-none hover:text-text">
            {deleted.length === 1 ? 'The branch deleted' : `The ${String(deleted.length)} branches deleted`}, and the
            commits that bring them back
          </summary>
          <ul aria-label="Branches deleted" className="mt-1 list-disc pl-5">
            {deleted.map((r) => (
              <li key={`${r.repoPath}\u0000${r.name}`}>
                {r.displayRepo}: <code className="font-mono text-text">{r.name}</code> at{' '}
                <code className="font-mono text-text" title={r.head}>
                  {shortCommit(r.head)}
                </code>
              </li>
            ))}
          </ul>
          <p className="mt-1 flex flex-wrap items-center gap-2">
            To bring a branch back, run{' '}
            <code className="font-mono text-text">git branch &lt;name&gt; &lt;commit&gt;</code> in its clone.
            <CopyRestore results={results} />
          </p>
        </details>
      )}
    </div>
  )
}
