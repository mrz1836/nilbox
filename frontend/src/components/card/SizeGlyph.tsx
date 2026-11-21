import { counted, formatCount } from '../../lib/format'
import { sizeOf, sizeText, type Size } from '../../lib/signals'
import type { InboxItem } from '../../platform'
import { CommentIcon } from '../icons'

// grades are the sizes in order, each lighting one more of the bar's cells.
const grades: readonly Size[] = ['XS', 'S', 'M', 'L', 'XL']

// Cell is one of the bar's cells: lit in the success color for lines added, the danger
// color for lines removed, or not lit.
type Cell = 'added' | 'removed' | 'off'

const cellClass: Readonly<Record<Cell, string>> = {
  added: 'bg-success',
  removed: 'bg-danger',
  off: 'bg-border',
}

// sizeCells are the bar's five cells for a change of additions and deletions: its grade
// lights one (XS) to five (XL) of them, split between lines added and lines removed in
// proportion, with at least one for lines added when there are any.
function sizeCells(additions: number, deletions: number): { grade: Size; cell: Cell }[] {
  const lines = additions + deletions
  const lit = grades.indexOf(sizeOf(lines)) + 1
  const share = lines === 0 ? 0 : Math.round((lit * additions) / lines)
  const added = additions > 0 ? Math.max(share, 1) : 0
  const removed = lines === 0 ? 0 : lit - added

  return grades.map((grade, i) => ({ grade, cell: i < added ? 'added' : i < added + removed ? 'removed' : 'off' }))
}

// SizeGlyph is how big a pull request is, under its track: its comments, when it has any,
// its grade in a small box, and a five-cell bar of lines added against lines removed.
// While the details load it is a placeholder bar.
export function SizeGlyph({ item }: { item: InboxItem }) {
  const d = item.details

  if (d === null) {
    return <span aria-hidden="true" className="skeleton block h-2 w-12" />
  }

  const { additions, deletions, changedFiles, commits, comments } = d

  return (
    <div className="flex items-center gap-1.75 text-caption text-muted tabular-nums">
      {comments > 0 && (
        <span className="inline-flex items-center gap-0.75">
          <CommentIcon className="size-3" />
          <span aria-hidden="true">{formatCount(comments)}</span>
          <span className="sr-only">{counted(comments, 'comment', 'comments')}</span>
        </span>
      )}
      <span aria-hidden="true" className="rounded border border-border px-1 text-badge leading-3.5 font-bold text-text">
        {sizeOf(additions + deletions)}
      </span>
      <span aria-hidden="true" data-size-bar className="inline-flex gap-0.5">
        {sizeCells(additions, deletions).map(({ grade, cell }) => (
          <span key={grade} data-cell={cell} className={`block h-2 w-1.25 rounded-[1.5px] ${cellClass[cell]}`} />
        ))}
      </span>
      <span className="sr-only">{sizeText(additions, deletions, changedFiles, commits)}</span>
    </div>
  )
}

// gradeClass is a compact card's size box.
export const gradeClass =
  'block truncate rounded border border-border px-0.5 text-center text-badge leading-3.75 font-bold text-muted'

// SizeGrade is a compact card's size: a pull request's grade in a small box, with the
// size's sentence for a screen reader, or a dash while the details load.
export function SizeGrade({ item }: { item: InboxItem }) {
  const d = item.details

  if (d === null) {
    return (
      <span aria-hidden="true" className={gradeClass}>
        –
      </span>
    )
  }

  const { additions, deletions, changedFiles, commits } = d

  return (
    <span className={gradeClass}>
      <span aria-hidden="true">{sizeOf(additions + deletions)}</span>
      <span className="sr-only">{sizeText(additions, deletions, changedFiles, commits)}</span>
    </span>
  )
}
