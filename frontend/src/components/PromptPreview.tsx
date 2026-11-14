import { useId, type Ref } from 'react'

import { formatCount } from '../lib/format'
import { copyLabel, useCopy } from '../lib/useCopy'
import type { InboxItem, PromptCheckResult } from '../platform'
import { quietButtonClass } from './controls'
import { AlertIcon, CheckIcon, CopyIcon } from './icons'

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
  // reply marks a review reply, which starts no session: it has no session switches.
  reply?: boolean
  // bodyRef is the filled-in text's scroll region, which the page focuses to scroll it
  // with the keyboard.
  bodyRef?: Ref<HTMLDivElement>
}

// itemLabel names an inbox item in the preview's item list.
function itemLabel(item: InboxItem): string {
  const name = item.repo.split('/').at(-1) ?? item.repo

  return `${name} #${String(item.number)} · ${item.title}`
}

// sentText is the text a launch sends: the filled-in prompt, then the outside contributor
// notice when there is one, joined as the backend's Render joins them.
function sentText(result: PromptCheckResult): string {
  return result.notice === '' ? result.preview : `${result.preview}\n${result.notice}\n`
}

// PromptPreview shows a prompt filled in for an inbox item (or a sample), exactly as a
// launch would send it, in a frame like the editor's: the item to fill it in for and the
// session facts the prompt can test above, the text at the editor's own size, and its
// length and a copy button below. A review reply shows as the review sheet would fill it
// in, with no session.
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
  reply = false,
  bodyRef,
}: PromptPreviewProps) {
  const id = useId()
  const noun = reply ? 'reply' : 'prompt'
  const { copy, onCopy } = useCopy(noun)
  const filled = result !== null && !hasErrors && result.previewError === '' ? result : null

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-border bg-background">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-border bg-surface/60 px-3.5 py-2">
        <div className="flex max-w-[28rem] min-w-0 flex-1 items-center gap-2">
          <label htmlFor={`${id}-item`} className="shrink-0 text-xs font-medium text-muted">
            Preview with
          </label>
          <select
            id={`${id}-item`}
            value={itemId}
            onChange={(e) => {
              onItemChange(Number(e.target.value))
            }}
            className="min-w-0 flex-1 truncate rounded-md border border-border bg-surface px-2 py-1 text-[0.8125rem]"
          >
            <option value={0}>A sample {forPRs ? 'pull request' : 'issue'}</option>
            {items.map((item) => (
              <option key={item.id} value={item.id}>
                {itemLabel(item)}
              </option>
            ))}
          </select>
        </div>
        {!reply && (
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
        )}
      </div>
      <div
        ref={bodyRef}
        role="region"
        aria-label={reply ? 'Filled-in reply' : 'Filled-in prompt'}
        // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- the text scrolls, and a scrolling region must take focus so the keyboard can scroll it (WCAG 2.1.1)
        tabIndex={0}
        className="min-h-0 flex-1 overflow-y-auto px-3.5 py-3 outline-none focus-visible:ring-2 focus-visible:ring-accent/60 focus-visible:ring-inset"
      >
        {result === null ? (
          <p className="text-sm text-muted">Filling in the {noun}…</p>
        ) : hasErrors ? (
          <p className="text-sm text-muted">Fix the problems in the {noun} to see it filled in.</p>
        ) : result.previewError !== '' ? (
          <p role="alert" className="flex items-start gap-2 text-sm text-danger">
            <AlertIcon className="mt-0.5 size-4 shrink-0" />
            {result.previewError}
          </p>
        ) : (
          <>
            {result.previewNote !== '' && <p className="mb-2 text-xs text-muted">{result.previewNote}</p>}
            <pre className="m-0 font-mono text-[0.8125rem] leading-6 break-words whitespace-pre-wrap text-text">
              {result.preview}
            </pre>
            {result.notice !== '' && (
              <div className="mt-3 rounded-md border border-warning/40 bg-warning/5 px-3 py-2">
                <p className="text-[0.6875rem] font-semibold tracking-wide text-warning uppercase">
                  Added for outside contributors
                </p>
                <p className="mt-1 font-mono text-[0.8125rem] leading-6 text-text">{result.notice}</p>
              </div>
            )}
          </>
        )}
      </div>
      {filled !== null && (
        <div className="flex items-center gap-2 border-t border-border px-3.5 py-1.5">
          <span className="mr-auto text-xs text-muted">
            {formatCount(filled.characters)} characters{filled.sample ? ' · sample data' : ''}
          </span>
          <button
            type="button"
            aria-label={`Copy the filled-in ${noun}`}
            onClick={() => {
              onCopy(sentText(filled))
            }}
            className={`${quietButtonClass} shrink-0`}
          >
            {copy === 'copied' ? <CheckIcon className="size-3.5 text-success" /> : <CopyIcon className="size-3.5" />}
            {copyLabel[copy]}
          </button>
          <span role="status" className="sr-only">
            {copy === 'copied' && 'Copied to the clipboard'}
            {copy === 'failed' && 'Could not copy. Select the text and copy it by hand.'}
          </span>
        </div>
      )}
    </div>
  )
}
