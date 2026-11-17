import { inboxLists, listTitles } from '../lib/triage'
import { GitHubReading } from './GitHubReading'
import { InboxIcon } from './icons'

// coming are the parts the inbox's first refresh brings.
const coming = ['Review requests', 'Your pull requests', 'Assignments', 'Checks and reviews'] as const

// rowWidths are the widths of a placeholder card's title and its line below, varied so
// the cards read as a list.
const rowWidths = [
  { title: 'w-2/3', meta: 'w-1/3' },
  { title: 'w-1/2', meta: 'w-2/5' },
  { title: 'w-3/5', meta: 'w-1/4' },
] as const

// sweep staggers a placeholder's highlight by its place, so the sweep travels down them.
function sweep(i: number): { animationDelay: string } {
  return { animationDelay: `${String(i * 90)}ms` }
}

// SectionShell stands in for one of the inbox's lists until the first refresh lands: its
// title and a few rows shaped like cards.
function SectionShell({ title, at }: { title: string; at: number }) {
  return (
    <div className="rounded-lg border border-border bg-surface">
      <div className="border-b border-border px-4 py-2.5 text-sm font-semibold text-muted">{title}</div>
      <ul>
        {rowWidths.map((row, i) => (
          <li key={row.title} className="flex items-start gap-3 border-b border-border px-4 py-3 last:border-b-0">
            <span className="skeleton mt-0.5 size-4 shrink-0 rounded-full" style={sweep(at + i)} />
            <span className="flex min-w-0 flex-1 flex-col gap-2">
              <span className={`skeleton block h-3 ${row.title}`} style={sweep(at + i)} />
              <span className={`skeleton block h-2.5 ${row.meta}`} style={sweep(at + i)} />
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}

// InboxFilling stands in for the inbox while its first refresh fills it: a banner saying
// what is read from GitHub, and the two lists' shells, which the real lists replace once
// the refresh lands. The toolbar's status says how far it has got.
export function InboxFilling() {
  return (
    <>
      <GitHubReading
        icon={<InboxIcon className="size-4 text-accent" />}
        heading="Filling your inbox from GitHub…"
        coming={coming}
      />
      <div aria-hidden="true" data-testid="inbox-skeleton" className="flex flex-col gap-4">
        {inboxLists.map((list, i) => (
          <SectionShell key={list} title={listTitles[list]} at={i * rowWidths.length} />
        ))}
      </div>
    </>
  )
}
