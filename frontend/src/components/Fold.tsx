import { useState, type ReactNode } from 'react'

import { ChevronRightIcon } from './icons'

export interface FoldProps {
  // title names what folds away, and count how many it holds, beside it.
  title: string
  count: number
  // auto says whether it starts open, as its size decides, until the user opens or folds it.
  auto: boolean
  children: ReactNode
}

// Fold is a list folded away under its title and count, behind a chevron. It is open as
// auto says until the user opens or folds it, and then as they chose, which it keeps while it
// shows: as the sheet steps to another item, say. Setting it open fires the toggle too, so
// only a toggle that leaves it other than it was set is the user's.
export function Fold({ title, count, auto, children }: FoldProps) {
  const [chosen, setChosen] = useState<boolean | null>(null)
  const open = chosen ?? auto

  return (
    <details
      open={open}
      onToggle={(e) => {
        if (e.currentTarget.open !== open) {
          setChosen(e.currentTarget.open)
        }
      }}
      className="group/fold"
    >
      <summary className="flex w-fit cursor-default list-none items-center gap-1 text-xs font-medium text-muted select-none hover:text-text [&::-webkit-details-marker]:hidden">
        <ChevronRightIcon className="size-3 shrink-0 transition-transform group-open/fold:rotate-90" />
        <span>{title}</span>
        <span className="rounded-full bg-raised px-1.5 text-caption leading-[1.125rem] tabular-nums">
          {String(count)}
        </span>
      </summary>
      <div className="mt-1.5">{children}</div>
    </details>
  )
}
