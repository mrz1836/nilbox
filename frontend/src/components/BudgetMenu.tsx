import { useEffect, useEffectEvent, useId } from 'react'

import { levelText, summarize, summaryText } from '../lib/budget'
import { useNow } from '../lib/useNow'
import { usePopover } from '../lib/usePopover'
import type { BudgetGauge } from '../platform'
import { GaugeIcon } from './GaugeIcon'
import { PopoverPanel } from './PopoverPanel'
import { RateGauge } from './RateGauge'

// checkEveryMs is how often an open panel reads the budgets from GitHub again, so what gh
// and the agents spend shows while the user watches. The read costs no budget.
export const checkEveryMs = 30_000

export interface BudgetMenuProps {
  // gauge is null until the budgets have loaded.
  gauge: BudgetGauge | null
  // onCheck reads every budget from GitHub now: as the panel opens, and every
  // checkEveryMs while it stays open.
  onCheck?: (() => void) | undefined
}

// BudgetMenu is the toolbar's API budget gauge. The dial's color and needle show the
// budget with the least left at a glance (green, yellow, orange, red), and a click opens
// the details. It reads the clock itself, since a budget refills as its reset passes. The
// gauge follows every GitHub response the app gets; the open panel also reads the
// budgets from GitHub, for what everything else signed in as the user spends.
export function BudgetMenu({ gauge, onCheck }: BudgetMenuProps) {
  const id = useId()
  const now = useNow()
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ focus: 'panel' })
  const { open, toggle, triggerRef } = popover
  const check = useEffectEvent(() => {
    onCheck?.()
  })

  useEffect(() => {
    if (!open) {
      return undefined
    }

    check()

    const timer = setInterval(() => {
      check()
    }, checkEveryMs)

    return () => {
      clearInterval(timer)
    }
  }, [open])
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
