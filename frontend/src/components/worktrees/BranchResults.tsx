import { useState } from 'react'

import { branchSummary } from '../../lib/worktreeSummary'
import { copyText, type BranchResult } from '../../platform'
import { quietButtonClass } from '../controls'
import { ChevronRightIcon, CopyIcon } from '../icons'
import { shortCommit } from './BranchChoices'
import { ResultCard } from './ResultCard'

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
      <CopyIcon className="size-3.5" />
      {copied ? 'Copied' : 'Copy restore commands'}
    </button>
  )
}

// BranchResults reports the outcome of deleting merged branches: how many went, across how
// many clones, why each one kept stayed, and, folded away, each deleted branch with the
// commit it pointed at, which brings it back, with the commands to do so to copy.
export function BranchResults({ results, onDismiss }: { results: readonly BranchResult[]; onDismiss: () => void }) {
  const deleted = results.filter((r) => r.deleted)
  const kept = results.filter((r) => !r.deleted)

  return (
    <ResultCard summary={branchSummary(results)} onDismiss={onDismiss}>
      {kept.length > 0 && (
        <ul aria-label="Branches kept" className="mt-1 list-disc pl-5 text-xs text-muted">
          {kept.map((r) => (
            <li key={`${r.repoPath}\u0000${r.name}`}>
              {r.displayRepo}: <code className="font-mono text-text">{r.name}</code> kept: {r.reason}
            </li>
          ))}
        </ul>
      )}
      {deleted.length > 0 && (
        <>
          <details className="group/deleted mt-1 text-xs text-muted">
            <summary className="-ml-1 flex w-fit cursor-pointer list-none items-center gap-1 rounded-md py-0.5 pr-1.5 pl-1 select-none hover:bg-raised hover:text-text [&::-webkit-details-marker]:hidden">
              <ChevronRightIcon className="size-3.5 shrink-0 transition-transform group-open/deleted:rotate-90 motion-reduce:transition-none" />
              {deleted.length === 1 ? 'The branch deleted' : `The ${String(deleted.length)} branches deleted`}, with the
              commit each pointed at
            </summary>
            <ul aria-label="Branches deleted" className="mt-1 ml-1 max-h-60 list-disc overflow-y-auto pl-5">
              {deleted.map((r) => (
                <li key={`${r.repoPath}\u0000${r.name}`}>
                  {r.displayRepo}: <code className="font-mono text-text">{r.name}</code> at{' '}
                  <code className="font-mono text-text" title={r.head}>
                    {shortCommit(r.head)}
                  </code>
                </li>
              ))}
            </ul>
          </details>
          <p className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted">
            <CopyRestore results={results} />
            to bring any back: each runs{' '}
            <code className="font-mono text-text">git branch &lt;name&gt; &lt;commit&gt;</code> in its clone.
          </p>
        </>
      )}
    </ResultCard>
  )
}
