import type { ReactNode } from 'react'

import type { SheetBack, SheetStep } from '../lib/sheet'
import { openExternal } from '../platform'
import { quietButtonClass } from './controls'
import { ChevronLeftIcon, ChevronRightIcon, ExternalIcon, RefreshIcon, XIcon } from './icons'
import { Spinner } from './Spinner'

export interface SheetHeaderProps {
  // titleId names the sheet: the heading that holds the title.
  titleId: string
  // icon marks what the sheet shows, as in its kind and state.
  icon: ReactNode
  title: string
  // url is the item's page on GitHub, or '' while nothing names it.
  url: string
  // meta says where the item is and who opened it, on the line below the title.
  meta: ReactNode
  // position says where the item sits in its list, as in "3 of 8 in Assigned", or ''.
  position: string
  // checking reports a read from GitHub under way.
  checking: boolean
  // prev and next step to the card before and after it, or are null at the ends.
  prev: SheetStep | null
  next: SheetStep | null
  // back returns to the sheet a link was followed from, or is null.
  back?: SheetBack | null
  // actions are buttons of the sheet's own before the steps, such as its triage.
  actions?: ReactNode
  // refreshTitle says what reading it from GitHub again reads.
  refreshTitle: string
  onRefresh: () => void
  onClose: () => void
}

// iconButtonClass is the header's square icon buttons.
const iconButtonClass = `${quietButtonClass} p-1`

// SheetHeader is the top of a sheet over the inbox: the item's title and what the sheet
// says of it, where it sits in its list, and its buttons: the sheet's own, the way back to
// the sheet a link was followed from, the steps through its list, which name the card
// each steps to, and buttons to read it from GitHub again, to open it on GitHub, and to
// close the sheet. While GitHub is read, it says so beside a spinner, the refresh button
// turns and waits, and a light glides along its bottom edge.
export function SheetHeader({
  titleId,
  icon,
  title,
  url,
  meta,
  position,
  checking,
  prev,
  next,
  back = null,
  actions,
  refreshTitle,
  onRefresh,
  onClose,
}: SheetHeaderProps) {
  return (
    <header className="relative flex items-start gap-3 border-b border-border px-5 py-3">
      {icon}
      <div className="min-w-0 flex-1">
        <h2 id={titleId} className="truncate text-base font-semibold" title={title}>
          {title}
        </h2>
        <p className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-2 text-xs text-muted">
          {meta}
          {position !== '' && <span>{position}</span>}
          {checking && (
            <span aria-live="polite" className="flex items-center gap-1">
              <Spinner className="size-3" />
              Checking GitHub…
            </span>
          )}
        </p>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        {actions}
        {back !== null && (
          <button
            type="button"
            aria-label={`Back to ${back.label}`}
            title={back.title === '' ? `Back to ${back.label}` : `Back to ${back.label}: ${back.title}`}
            onClick={back.onBack}
            className={`${quietButtonClass} flex items-center gap-0.5 px-1.5 py-1 text-xs`}
          >
            <ChevronLeftIcon className="size-3.5" />
            <span className="max-w-48 truncate font-mono">{back.label}</span>
          </button>
        )}
        <button
          type="button"
          aria-label="Previous"
          title={prev === null ? 'Previous (←)' : `Previous: ${prev.title} (←)`}
          data-step="prev"
          disabled={prev === null}
          onClick={() => prev?.onStep()}
          className={iconButtonClass}
        >
          <ChevronLeftIcon className="size-4" />
        </button>
        <button
          type="button"
          aria-label="Next"
          title={next === null ? 'Next (→)' : `Next: ${next.title} (→)`}
          data-step="next"
          disabled={next === null}
          onClick={() => next?.onStep()}
          className={iconButtonClass}
        >
          <ChevronRightIcon className="size-4" />
        </button>
        <button
          type="button"
          aria-label="Read it from GitHub again"
          title={checking ? 'Checking GitHub…' : refreshTitle}
          // It waits while a read runs, but stays focusable, so a keyboard user keeps
          // their place.
          aria-disabled={checking}
          onClick={() => {
            if (!checking) {
              onRefresh()
            }
          }}
          className={`${iconButtonClass} aria-disabled:cursor-default aria-disabled:opacity-60`}
        >
          <RefreshIcon className={`size-4 ${checking ? 'motion-safe:animate-spin' : ''}`} />
        </button>
        <button
          type="button"
          aria-label="Open on GitHub"
          title="Open on GitHub"
          disabled={url === ''}
          onClick={() => {
            openExternal(url)
          }}
          className={iconButtonClass}
        >
          <ExternalIcon className="size-4" />
        </button>
        <button type="button" aria-label="Close" title="Close (Esc)" onClick={onClose} className={iconButtonClass}>
          <XIcon className="size-4" />
        </button>
      </div>
      {/* While GitHub is read, a light glides along the header's bottom edge. */}
      {checking && <div aria-hidden="true" className="loading-bar absolute inset-x-0 -bottom-px" />}
    </header>
  )
}
