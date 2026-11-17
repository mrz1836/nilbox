import type { ReactNode, Ref } from 'react'

export interface DividerProps {
  // label is what a screen reader reads for it, and children what it shows.
  label: string
  children: ReactNode
  // dividerRef reaches its element, as for a scroll, and id names it, for a jump to it.
  dividerRef?: Ref<HTMLLIElement> | undefined
  id?: string | undefined
}

// Divider marks a place in a list, its words between two accent lines: what is new in the
// conversation since the user's last visit, or the commits since their review.
export function Divider({ label, children, dividerRef, id }: DividerProps) {
  return (
    <li
      ref={dividerRef}
      id={id}
      role="separator"
      aria-label={label}
      className="flex scroll-mt-4 items-center gap-2 text-xs font-medium text-accent"
    >
      <span aria-hidden="true" className="h-px flex-1 bg-accent/60" />
      {children}
      <span aria-hidden="true" className="h-px flex-1 bg-accent/60" />
    </li>
  )
}
