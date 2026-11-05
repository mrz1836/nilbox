import { useEffect, useId } from 'react'

import { levelText, summarize, summaryText } from '../lib/budget'
import { usePopover } from '../lib/usePopover'
import type { RateGauge as Gauge } from '../platform'
import { GaugeIcon } from './GaugeIcon'
import { RateGauge } from './RateGauge'

export interface BudgetMenuProps {
  // gauge is null until the inbox has loaded.
  gauge: Gauge | null
  now: Date
}

// BudgetMenu is the toolbar's API budget gauge. The dial's color and needle show the
// budget with the least left at a glance (green, yellow, orange, red), and a click opens
// the details.
export function BudgetMenu({ gauge, now }: BudgetMenuProps) {
  const id = useId()
  const { open, toggle, triggerRef, panelRef } = usePopover<HTMLButtonElement, HTMLDivElement>()
  const summary = summarize(gauge, now)
  const label = summaryText(summary)

  // Focus moves into the panel, so Escape and Tab behave from there.
  useEffect(() => {
    if (open) {
      panelRef.current?.focus()
    }
  }, [open, panelRef])

  return (
    <div className="relative">
      <button
        ref={triggerRef}
        type="button"
        aria-label={label}
        title={label}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={toggle}
        className={`flex size-8 items-center justify-center rounded-md transition-colors hover:bg-raised aria-expanded:bg-raised ${levelText[summary.level]}`}
      >
        <GaugeIcon percent={summary.percent} known={summary.level !== 'unknown'} />
      </button>
      {open && (
        <div
          ref={panelRef}
          id={id}
          role="dialog"
          aria-labelledby={`${id}-title`}
          tabIndex={-1}
          className="absolute top-full right-0 z-40 mt-2 w-80 origin-top-right rounded-lg border border-border bg-surface shadow-xl shadow-black/20 outline-none motion-safe:animate-pop-in"
        >
          <RateGauge gauge={gauge} now={now} titleId={`${id}-title`} />
        </div>
      )}
    </div>
  )
}
