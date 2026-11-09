import { useId } from 'react'

import { formatCount } from '../lib/format'
import type { InboxItem, PromptCheckResult } from '../platform'
import { AlertIcon } from './icons'

export interface PromptPreviewProps {
  // items are the inbox items the prompt applies to, offered to preview with.
  items: readonly InboxItem[]
  // itemId is the chosen item, or 0 for a sample.
  itemId: number
  onItemChange: (id: number) => void
  inWorktree: boolean
  onInWorktreeChange: (on: boolean) => void
  atPrHead: boolean
  onAtPrHeadChange: (on: boolean) => void
  // forPRs reports whether the preview item is a pull request, which can start at its head.
  forPRs: boolean
  // result is the latest check, or null before the first one.
  result: PromptCheckResult | null
  // hasErrors reports whether the text has errors, so there is nothing to preview.
  hasErrors: boolean
}

// itemLabel names an inbox item in the preview's item list.
function itemLabel(item: InboxItem): string {
  const name = item.repo.split('/').at(-1) ?? item.repo

  return `${name} #${String(item.number)} · ${item.title}`
}

// PromptPreview shows a prompt filled in for an inbox item (or a sample), exactly as a
// launch would send it, with switches for the session facts the prompt can test.
export function PromptPreview({
  items,
  itemId,
  onItemChange,
  inWorktree,
  onInWorktreeChange,
  atPrHead,
  onAtPrHeadChange,
  forPRs,
  result,
  hasErrors,
}: PromptPreviewProps) {
  const id = useId()

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-col gap-2 px-4 pt-3 pb-3">
        <label htmlFor={`${id}-item`} className="text-xs font-medium text-muted">
          Preview with
        </label>
        <select
          id={`${id}-item`}
          value={itemId}
          onChange={(e) => {
            onItemChange(Number(e.target.value))
          }}
          className="w-full truncate rounded-md border border-border bg-surface px-2 py-1.5 text-[13px]"
        >
          <option value={0}>A sample {forPRs ? 'pull request' : 'issue'}</option>
          {items.map((item) => (
            <option key={item.id} value={item.id}>
              {itemLabel(item)}
            </option>
          ))}
        </select>
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs">
          <label className="flex cursor-pointer items-center gap-1.5 text-muted">
            <input
              type="checkbox"
              checked={inWorktree}
              onChange={(e) => {
                onInWorktreeChange(e.target.checked)
              }}
              className="checkbox"
            />
            In a worktree
          </label>
          {forPRs && (
            <label className={`flex items-center gap-1.5 text-muted ${inWorktree ? 'cursor-pointer' : 'opacity-50'}`}>
              <input
                type="checkbox"
                checked={inWorktree && atPrHead}
                disabled={!inWorktree}
                onChange={(e) => {
                  onAtPrHeadChange(e.target.checked)
                }}
                className="checkbox"
              />
              At the PR&apos;s head
            </label>
          )}
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto border-t border-border px-4 py-3">
        {result === null ? (
          <p className="text-sm text-muted">Filling in the prompt…</p>
        ) : hasErrors ? (
          <p className="text-sm text-muted">Fix the problems in the prompt to see it filled in.</p>
        ) : result.previewError !== '' ? (
          <p role="alert" className="flex items-start gap-2 text-sm text-danger">
            <AlertIcon className="mt-0.5 size-4 shrink-0" />
            {result.previewError}
          </p>
        ) : (
          <>
            {result.previewNote !== '' && <p className="mb-2 text-xs text-muted">{result.previewNote}</p>}
            <pre className="font-mono text-[12px] leading-5 break-words whitespace-pre-wrap text-text">
              {result.preview}
            </pre>
            {result.notice !== '' && (
              <div className="mt-3 rounded-md border border-warning/40 bg-warning/5 px-3 py-2">
                <p className="text-[11px] font-semibold tracking-wide text-warning uppercase">
                  Added for outside contributors
                </p>
                <p className="mt-1 font-mono text-[12px] leading-5 text-text">{result.notice}</p>
              </div>
            )}
          </>
        )}
      </div>
      {result !== null && !hasErrors && result.previewError === '' && (
        <p className="border-t border-border px-4 py-2 text-xs text-muted">
          {formatCount(result.characters)} characters{result.sample ? ' · sample data' : ''}
        </p>
      )}
    </div>
  )
}
