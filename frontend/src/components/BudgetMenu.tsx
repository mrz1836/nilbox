import { useEffect, useEffectEvent, useId } from 'react'

import { drainingLabels, drainingText, levelText, summarize, summaryText } from '../lib/budget'
import { useNow } from '../lib/useNow'
import { usePopover } from '../lib/usePopover'
import type { BudgetGauge } from '../platform'
import { toolbarButtonShape } from './controls'
import { GaugeIcon } from './GaugeIcon'
import { PopoverPanel, ToolbarPopoverButton } from './PopoverPanel'
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
  // openRequest opens the panel each time it changes to a number above 0, as a click on a
  // budget's draining notification asks.
  openRequest?: number | undefined
}

// BudgetMenu is the toolbar's API budget gauge. The dial's color and needle show the
// budget with the least left at a glance (green, yellow, orange, red), and a click opens
// the details. It reads the clock itself, since a budget refills as its reset passes. The
// gauge follows every GitHub response the app gets; the open panel also reads the
// budgets from GitHub, for what everything else signed in as the user spends. While
// something else drains a budget fast, a small dot sits on the dial, whatever its color,
// and the label says which budget.
export function BudgetMenu({ gauge, onCheck, openRequest = 0 }: BudgetMenuProps) {
  const id = useId()
  const now = useNow()
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ focus: 'panel' })
  const { open, show } = popover
  const check = useEffectEvent(() => {
    onCheck?.()
  })
  const showPanel = useEffectEvent(() => {
    show()
  })

  useEffect(() => {
    if (openRequest > 0) {
      showPanel()
    }
  }, [openRequest])

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
  const draining = drainingText(drainingLabels(gauge, now))
  const label = draining === '' ? summaryText(summary) : `${summaryText(summary)} · ${draining}`

  return (
    <div className="relative">
      <ToolbarPopoverButton
        popover={popover}
        id={id}
        label={label}
        className={`${toolbarButtonShape} ${levelText[summary.level]}`}
      >
        <GaugeIcon percent={summary.percent} known={summary.level !== 'unknown'} />
        {draining !== '' && (
          <span
            data-testid="budget-draining"
            aria-hidden="true"
            className="absolute top-1 right-1 size-2 rounded-full bg-warning ring-2 ring-surface"
          />
        )}
      </ToolbarPopoverButton>
      <PopoverPanel
        state={popover}
        align="right"
        id={id}
        role="dialog"
        aria-labelledby={`${id}-title`}
        className="layer-popover w-80 outline-none"
      >
        <RateGauge gauge={gauge} now={now} titleId={`${id}-title`} />
      </PopoverPanel>
    </div>
  )
}
