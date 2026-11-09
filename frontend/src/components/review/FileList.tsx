import { levelDot } from '../../lib/sensitive'
import type { Review } from '../../platform'
import { fileAnchor } from './DiffView'

export interface FileListProps {
  review: Review
  // viewed are the paths the user marked as viewed.
  viewed: readonly string[]
}

// FileList lists the files a pull request changes: each path, its additions and
// deletions, a dot for a sensitive category, and a tick once viewed. A file scrolls its
// diff into view.
export function FileList({ review, viewed }: FileListProps) {
  return (
    <nav aria-label="Files" className="text-xs">
      <p className="mb-1 font-medium text-muted">
        {review.totalFiles === 1 ? '1 file' : `${String(review.totalFiles)} files`}
      </p>
      <ul className="space-y-0.5">
        {review.files.map((f) => (
          <li key={f.path}>
            <button
              type="button"
              title={f.category === '' ? f.path : `${f.path} · ${f.category}`}
              onClick={() => {
                document.getElementById(fileAnchor(f.path))?.scrollIntoView({ block: 'start' })
              }}
              className="flex w-full min-w-0 items-center gap-1.5 rounded px-1 py-0.5 text-left hover:bg-raised"
            >
              <span
                aria-hidden="true"
                className={`size-1.5 shrink-0 rounded-full ${f.level === '' ? '' : levelDot[f.level]}`}
              />
              <span className={`min-w-0 flex-1 truncate font-mono ${viewed.includes(f.path) ? 'text-muted' : ''}`}>
                {f.path.split('/').at(-1)}
              </span>
              <span className="shrink-0 font-mono text-success">+{f.additions}</span>
              <span className="shrink-0 font-mono text-danger">−{f.deletions}</span>
              {viewed.includes(f.path) && (
                <span aria-label="viewed" className="shrink-0 text-success">
                  ✓
                </span>
              )}
            </button>
          </li>
        ))}
      </ul>
    </nav>
  )
}
