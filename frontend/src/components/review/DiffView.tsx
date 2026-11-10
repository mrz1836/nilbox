import { useState, type MouseEvent } from 'react'

import {
  canComment,
  canRange,
  parsePatch,
  rowLine,
  rowSide,
  startsCollapsed,
  type DiffRow,
  type DiffSide,
} from '../../lib/diff'
import { nextCommentId, type PendingComment, type ReviewDraft } from '../../lib/reviewDrafts'
import { openExternal, type Review, type ReviewFile } from '../../platform'
import { quietButtonClass } from '../controls'
import { ChevronDownIcon, ChevronRightIcon, ExternalIcon } from '../icons'
import { LineComment } from './LineComment'

// largeDiffLines is how many changed lines make a file start collapsed.
export const largeDiffLines = 400

export interface DiffViewProps {
  review: Review
  draft: ReviewDraft
  onDraft: (change: (draft: ReviewDraft) => ReviewDraft) => void
  // loading are the paths whose diffs are being read, and onLoad reads more.
  loading: ReadonlySet<string>
  onLoad: (paths: string[]) => void
}

// fileAnchor is the ID of a file's block, which the file list scrolls to.
export function fileAnchor(path: string): string {
  return `diff-${path.replaceAll(/[^A-Za-z0-9_-]/g, '_')}`
}

// rowClass is each kind of row's tint, from the theme's success and danger tokens.
const rowClass: Readonly<Record<DiffRow['kind'], string>> = {
  add: 'bg-success/12',
  del: 'bg-danger/12',
  context: '',
  hunk: 'bg-accent/8 text-muted',
  note: 'text-muted italic',
}

// marker is each kind of row's first column.
const marker: Readonly<Record<DiffRow['kind'], string>> = { add: '+', del: '-', context: ' ', hunk: '', note: '' }

// Editing is the lines a comment is being written on, and the comment edited, if any.
interface Editing {
  side: DiffSide
  line: number
  startLine: number
  commentId: number
}

// whereText names a comment's lines, as in "line 12", "lines 10–12", or "old line 4" on the
// version before the change.
export function whereText(c: { line: number; startLine: number; side: DiffSide }): string {
  const old = c.side === 'LEFT' ? 'old ' : ''

  return c.startLine > 0 ? `${old}lines ${String(c.startLine)}–${String(c.line)}` : `${old}line ${String(c.line)}`
}

// newLines are the texts of the rows on the new version numbered from start to end.
function newLines(rows: readonly DiffRow[], start: number, end: number): string[] {
  return rows.filter((r) => r.newLine !== null && r.newLine >= start && r.newLine <= end).map((r) => r.text)
}

interface FileDiffProps {
  file: ReviewFile
  url: string
  viewed: boolean
  comments: readonly PendingComment[]
  loading: boolean
  onLoad: () => void
  onViewed: (viewed: boolean) => void
  onComment: (comment: PendingComment | null, edit: Editing, body: string) => void
  onDelete: (id: number) => void
}

// FileDiff is one file's block: its header, which folds it and marks it viewed, and its
// diff, or why there is none.
function FileDiff({ file, url, viewed, comments, loading, onLoad, onViewed, onComment, onDelete }: FileDiffProps) {
  const [open, setOpen] = useState(!startsCollapsed(file) && !viewed)
  const [editing, setEditing] = useState<Editing | null>(null)
  const rows = file.patchState === '' ? parsePatch(file.patch) : []

  const clickLine = (e: MouseEvent, side: DiffSide, line: number) => {
    if (e.shiftKey && editing !== null && editing.commentId === 0 && editing.side === side) {
      const start = Math.min(editing.startLine || editing.line, line)
      const end = Math.max(editing.startLine || editing.line, line)

      if (start < end && canRange(rows, side, start, end)) {
        setEditing({ side, line: end, startLine: start, commentId: 0 })

        return
      }
    }

    setEditing({ side, line, startLine: 0, commentId: 0 })
  }

  const commentsAt = (side: DiffSide, line: number) => comments.filter((c) => c.side === side && c.line === line)

  return (
    <section
      id={fileAnchor(file.path)}
      aria-label={file.path}
      className="rounded-md border border-border [contain-intrinsic-size:auto_6rem] [content-visibility:auto]"
    >
      <header className="flex items-center gap-2 border-b border-border bg-raised/50 px-2 py-1.5 text-xs">
        <button
          type="button"
          aria-expanded={open}
          onClick={() => {
            setOpen(!open)
          }}
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
        >
          {open ? (
            <ChevronDownIcon className="size-3.5 shrink-0" />
          ) : (
            <ChevronRightIcon className="size-3.5 shrink-0" />
          )}
          <span className="truncate font-mono" title={file.path}>
            {file.previousPath === '' ? file.path : `${file.previousPath} → ${file.path}`}
          </span>
          <span className="shrink-0 text-muted">{file.status}</span>
        </button>
        <span className="shrink-0 font-mono text-success">+{file.additions}</span>
        <span className="shrink-0 font-mono text-danger">−{file.deletions}</span>
        <label className="flex shrink-0 items-center gap-1 text-muted">
          <input
            type="checkbox"
            className="checkbox"
            checked={viewed}
            onChange={(e) => {
              onViewed(e.target.checked)

              if (e.target.checked) {
                setOpen(false)
              }
            }}
          />
          Viewed
        </label>
      </header>
      {open && (
        <div className="overflow-x-auto font-mono text-[0.75rem] leading-5">
          <FileBody
            file={file}
            url={url}
            rows={rows}
            loading={loading}
            onLoad={onLoad}
            editing={editing}
            clickLine={clickLine}
            commentsAt={commentsAt}
            onEdit={(c) => {
              setEditing({ side: c.side, line: c.line, startLine: c.startLine, commentId: c.id })
            }}
            onDelete={onDelete}
            onSave={(body) => {
              if (editing !== null) {
                onComment(comments.find((c) => c.id === editing.commentId) ?? null, editing, body)
              }

              setEditing(null)
            }}
            onCancel={() => {
              setEditing(null)
            }}
          />
        </div>
      )}
    </section>
  )
}

interface FileBodyProps {
  file: ReviewFile
  url: string
  rows: readonly DiffRow[]
  loading: boolean
  onLoad: () => void
  editing: Editing | null
  clickLine: (e: MouseEvent, side: DiffSide, line: number) => void
  commentsAt: (side: DiffSide, line: number) => PendingComment[]
  onEdit: (c: PendingComment) => void
  onDelete: (id: number) => void
  onSave: (body: string) => void
  onCancel: () => void
}

// LineButton is a line number that opens a comment on its line.
function LineButton({
  side,
  line,
  onClick,
}: {
  side: DiffSide
  line: number
  onClick: (e: MouseEvent, side: DiffSide, line: number) => void
}) {
  return (
    <button
      type="button"
      aria-label={`Comment on ${whereText({ side, line, startLine: 0 })}`}
      title="Comment on this line (shift-click another line for a range)"
      onClick={(e) => {
        onClick(e, side, line)
      }}
      className="w-full text-right hover:text-accent"
    >
      {line}
    </button>
  )
}

// FileBody is a file's diff, with its comments, or why it has none.
function FileBody({
  file,
  url,
  rows,
  loading,
  onLoad,
  editing,
  clickLine,
  commentsAt,
  onEdit,
  onDelete,
  onSave,
  onCancel,
}: FileBodyProps) {
  const onGitHub = (
    <button
      type="button"
      onClick={() => {
        openExternal(`${url}/files`)
      }}
      className={quietButtonClass}
    >
      <ExternalIcon className="size-3" /> Open on GitHub
    </button>
  )

  switch (file.patchState) {
    case 'omitted':
      return (
        <p className="flex items-center gap-2 px-3 py-2 font-sans text-xs text-muted">
          This diff is large.
          <button type="button" disabled={loading} onClick={onLoad} className={quietButtonClass}>
            {loading ? 'Loading…' : 'Show the diff'}
          </button>
        </p>
      )
    case 'no_content':
      return (
        <p className="flex items-center gap-2 px-3 py-2 font-sans text-xs text-muted">
          GitHub shows no diff for this file (binary or too large). {onGitHub}
        </p>
      )
    case 'cache_cap':
      return (
        <p className="flex items-center gap-2 px-3 py-2 font-sans text-xs text-muted">
          This pull request&apos;s diffs are too large to keep here. {onGitHub}
        </p>
      )
    default:
      return (
        <div role="table" aria-label={`Diff of ${file.path}`} className="min-w-max">
          {rows.map((row, i) => {
            const side = rowSide(row)
            const line = rowLine(row)
            const commentable = side !== null && line !== null && canComment(rows, side, line)
            const editingHere = editing !== null && side === editing.side && line === editing.line

            return (
              <div key={i} role="row">
                <div className={`flex ${rowClass[row.kind]}`}>
                  <span role="cell" className="w-12 shrink-0 pr-2 text-right text-muted select-none">
                    {commentable && side === 'LEFT' ? (
                      <LineButton side={side} line={line} onClick={clickLine} />
                    ) : (
                      (row.oldLine ?? '')
                    )}
                  </span>
                  <span role="cell" className="w-12 shrink-0 pr-2 text-right text-muted select-none">
                    {commentable && side === 'RIGHT' ? (
                      <LineButton side={side} line={line} onClick={clickLine} />
                    ) : (
                      (row.newLine ?? '')
                    )}
                  </span>
                  <span role="cell" className="pr-4 whitespace-pre">
                    {marker[row.kind]}
                    {row.text}
                  </span>
                </div>
                {side !== null &&
                  line !== null &&
                  commentsAt(side, line).map((c) =>
                    editing?.commentId === c.id ? null : (
                      <div
                        key={c.id}
                        className="my-1 ml-24 rounded-md border border-border bg-surface p-2 font-sans text-xs"
                      >
                        <p className="text-muted">Comment on {whereText(c)}</p>
                        <p className="mt-1 whitespace-pre-wrap">{c.body}</p>
                        <div className="mt-1 flex gap-2">
                          <button
                            type="button"
                            onClick={() => {
                              onEdit(c)
                            }}
                            className={quietButtonClass}
                          >
                            Edit
                          </button>
                          <button
                            type="button"
                            onClick={() => {
                              onDelete(c.id)
                            }}
                            className={quietButtonClass}
                          >
                            Delete
                          </button>
                        </div>
                      </div>
                    ),
                  )}
                {editingHere && (
                  <LineComment
                    where={whereText(editing)}
                    side={editing.side}
                    lines={newLines(rows, editing.startLine || editing.line, editing.line)}
                    initial={commentsAt(editing.side, editing.line).find((c) => c.id === editing.commentId)?.body ?? ''}
                    onAdd={onSave}
                    onCancel={onCancel}
                  />
                )}
              </div>
            )
          })}
        </div>
      )
  }
}

// DiffView is the pull request's diff, file by file, in GitHub's order: unified, added
// and removed lines tinted, lock and generated files and long diffs folded to start with.
// A line number on a line GitHub takes comments on opens a comment there; shift-click
// another for a range. Comments wait in the draft and go with the review.
export function DiffView({ review, draft, onDraft, loading, onLoad }: DiffViewProps) {
  if (review.files.length === 0) {
    return <p className="text-sm text-muted">{review.stale ? 'Reading the files from GitHub…' : 'No files changed.'}</p>
  }

  return (
    <div className="space-y-3">
      {!review.filesComplete && (
        <p className="text-xs text-warning">
          GitHub lists only the first {review.files.length} of {review.totalFiles} files.
        </p>
      )}
      {review.files.map((file) => (
        <FileDiff
          key={`${review.head}:${file.path}`}
          file={file}
          url={review.url}
          viewed={draft.viewed.includes(file.path)}
          comments={draft.comments.filter((c) => c.path === file.path)}
          loading={loading.has(file.path)}
          onLoad={() => {
            onLoad([file.path])
          }}
          onViewed={(viewed) => {
            onDraft((d) => ({
              ...d,
              viewed: viewed ? [...d.viewed, file.path] : d.viewed.filter((p) => p !== file.path),
            }))
          }}
          onComment={(was, edit, body) => {
            onDraft((d) =>
              was === null
                ? {
                    ...d,
                    comments: [
                      ...d.comments,
                      {
                        id: nextCommentId(d),
                        path: file.path,
                        side: edit.side,
                        line: edit.line,
                        startLine: edit.startLine,
                        body,
                      },
                    ],
                  }
                : { ...d, comments: d.comments.map((c) => (c.id === was.id ? { ...c, body } : c)) },
            )
          }}
          onDelete={(id) => {
            onDraft((d) => ({ ...d, comments: d.comments.filter((c) => c.id !== id) }))
          }}
        />
      ))}
    </div>
  )
}
