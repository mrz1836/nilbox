import type { ReactNode } from 'react'

// The small parts both tabs of the Notifications page share: the toolbar under the tabs,
// a note where a list would be, and what a filter that matches nothing says.

// Toolbar is the row under the page's tabs: the list's Show and Type on the left, the
// tab's actions on the right.
export function Toolbar({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">{children}</div>
}

export interface PageNoteProps {
  title: string
  children?: ReactNode
}

// PageNote stands where a list would be: why it is empty, and what to do.
export function PageNote({ title, children }: PageNoteProps) {
  return (
    <div className="px-4 py-10 text-center">
      <p className="text-sm font-medium">{title}</p>
      {children !== undefined && <div className="mt-1 text-xs text-muted">{children}</div>}
    </div>
  )
}

// NoMatches says the toolbar's filter matches nothing in the list, with a way to clear it,
// as the inbox does.
export function NoMatches({ onClear }: { onClear: () => void }) {
  return (
    <div className="mx-2 mt-3 flex items-start gap-4 rounded-lg border border-border bg-surface px-4 py-3">
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium">No notifications match</p>
        <p className="mt-0.5 text-xs text-muted">
          The filter looks at what this list shows: titles, repositories, numbers, and what happened.
        </p>
      </div>
      <button
        type="button"
        onClick={onClear}
        className="shrink-0 rounded-md border border-border bg-surface px-3 py-1 text-sm font-medium hover:bg-raised"
      >
        Clear the filter
      </button>
    </div>
  )
}
