import { useId } from 'react'

import { levelText, summarize, summaryText } from '../lib/budget'
import { useNow } from '../lib/useNow'
import { usePopover } from '../lib/usePopover'
import type { RateGauge as Gauge } from '../platform'
import { GaugeIcon } from './GaugeIcon'
import { PopoverPanel } from './PopoverPanel'
import { RateGauge } from './RateGauge'

export interface BudgetMenuProps {
  // gauge is null until the inbox has loaded.
  gauge: Gauge | null
}

// BudgetMenu is the toolbar's API budget gauge. The dial's color and needle show the
// budget with the least left at a glance (green, yellow, orange, red), and a click opens
// the details. It reads the clock itself, since a budget refills as its reset passes.
export function BudgetMenu({ gauge }: BudgetMenuProps) {
  const id = useId()
  const now = useNow()
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ focus: 'panel' })
  const { open, toggle, triggerRef } = popover
  const summary = summarize(gauge, now)
  const label = summaryText(summary)

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
      <PopoverPanel
        state={popover}
        align="right"
        id={id}
        role="dialog"
        aria-labelledby={`${id}-title`}
        tabIndex={-1}
        className="z-40 w-80 shadow-black/20 outline-none"
      >
        <RateGauge gauge={gauge} now={now} titleId={`${id}-title`} />
      </PopoverPanel>
    </div>
  )
}
