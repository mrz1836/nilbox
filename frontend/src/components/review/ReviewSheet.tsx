import { useEffect, useEffectEvent, useId, useState } from 'react'

import { isTyping } from '../../lib/keys'
import type { ReviewDrafts } from '../../lib/reviewDrafts'
import { useMutation } from '../../lib/useMutation'
import { isTopmost } from '../../lib/useModalDialog'
import { useReview } from '../../lib/useReview'
import {
  actOnReview,
  on,
  type AgentSession,
  type InboxItem,
  type ItemResolution,
  type MergeMethod,
  type Review,
  type ReviewAction,
  type ReviewOffer,
  type ReviewResult,
} from '../../platform'
import { buttonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import type { LaunchOptions } from '../ItemRow'
import { Markdown } from '../Markdown'
import { Sheet } from '../Sheet'
import { ActionProgress } from './ActionProgress'
import { AgentVerdict } from './AgentVerdict'
import { DiffView } from './DiffView'
import { FileList } from './FileList'
import { MergeConfirmDialog } from './MergeConfirmDialog'
import { needsText, ReviewComposer, useReplyBody } from './ReviewComposer'
import { ReviewHeader } from './ReviewHeader'
import { DescriptionSkeleton, DiffSkeleton, FileListSkeleton, ReadingBanner, SummarySkeleton } from './ReviewLoading'
import { Readiness, ReviewSummary } from './ReviewSummary'

export interface ReviewSheetProps {
  // id is the pull request the sheet shows, and item its inbox card.
  id: number
  item: InboxItem | undefined
  // siblings are the pull requests of the list and tab it was opened from, in order, for
  // stepping through them; listTitle names that list.
  siblings: readonly InboxItem[]
  listTitle: string
  drafts: ReviewDrafts
  launch: LaunchOptions | null
  resolution: ItemResolution | null
  sessions: readonly AgentSession[] | undefined
  onNavigate: (id: number) => void
  onClose: () => void
  // onDone hears an action's outcome, once the sheet closes for it.
  onDone: (result: ReviewResult, review: Review) => void
  onFilesSettings?: (() => void) | undefined
  onManageReplies?: (() => void) | undefined
}

// selectedAction is the action the sheet's button runs: the one the user chose, while the
// sheet still offers it enabled, else the sheet's primary.
export function selectedAction(review: Review, chosen: ReviewAction): ReviewAction {
  const offer = review.offers.find((o) => o.action === chosen)

  return chosen !== '' && offer?.enabled === true ? chosen : review.primary
}

// keepsDraft reports an outcome that leaves the draft for another try: an action refused
// for a push, or one that changed nothing the draft was for.
function keepsDraft(result: ReviewResult): boolean {
  return result.outcome === 'pushed_since' || result.outcome === 'branch_updating' || result.outcome === 'asked_bot'
}

// Progress is an action under way: what it is, and the steps the backend reported so far.
interface Progress {
  title: string
  steps: readonly string[]
}

// withStep adds a step the backend reported to progress, once: a step repeated, as the
// wait for GitHub to work out a merge can be, shows as one.
export function withStep(progress: Progress | null, step: string): Progress | null {
  if (progress === null || progress.steps.at(-1) === step) {
    return progress
  }

  return { ...progress, steps: [...progress.steps, step] }
}

// ReviewSheet is the review sheet: a pull request's files and diffs, its description, what
// it still needs to merge, the latest agent's verdict, and the review to send. ← and → step
// through its list while the user isn't typing, ⌘↩ runs the button's action, and Escape
// closes it. It closes after an action, which the inbox's toast reports, except when the
// pull request was pushed to since, when it stays open, with the draft, to reload.
export function ReviewSheet({
  id,
  item,
  siblings,
  listTitle,
  drafts,
  launch,
  resolution,
  sessions,
  onNavigate,
  onClose,
  onDone,
  onFilesSettings,
  onManageReplies,
}: ReviewSheetProps) {
  const titleId = useId()
  const loaded = useReview(id)
  const review = loaded.review
  const draft = drafts.get(id)
  const act = useMutation(actOnReview)
  const [confirming, setConfirming] = useState<ReviewOffer | null>(null)
  const [pushed, setPushed] = useState('')
  const [progress, setProgress] = useState<Progress | null>(null)

  // target names the pull request while it loads, and reading reports its first read from
  // GitHub under way: nothing the sheet would show has come from GitHub yet, so each
  // column shows placeholders shaped like what comes.
  const shown = review ?? item
  const target = shown === undefined ? '' : `${shown.repo}#${String(shown.number)}`
  const reading = loaded.checking && (review === null || review.fetchedAt === '')

  const at = siblings.findIndex((s) => s.id === id)
  const prev = at > 0 ? siblings[at - 1] : undefined
  const next = at >= 0 ? siblings[at + 1] : undefined
  const position = at < 0 ? '' : `${String(at + 1)} of ${String(siblings.length)} in ${listTitle}`

  const onDraft = (change: Parameters<ReviewDrafts['update']>[1]) => {
    drafts.update(id, change)
  }

  const send = async (r: Review, action: ReviewAction, method: MergeMethod, accept: boolean) => {
    const reviews = !['merge_now', 'auto_merge', 'merge_bypass', 'update_branch', 'rebase_bot'].includes(action)
    const label = r.offers.find((o) => o.action === action)?.label ?? 'Working'

    setProgress({ title: `${label} ${r.repo}#${String(r.number)}`, steps: [] })

    const result = await act.run({
      itemId: id,
      action,
      head: r.head,
      body: reviews ? reply.body : '',
      comments: reviews
        ? draft.comments.map((c) => ({
            path: c.path,
            line: c.line,
            side: c.side,
            startLine: c.startLine,
            startSide: c.startLine > 0 ? c.side : '',
            body: c.body,
          }))
        : [],
      method,
      accept,
    })

    setProgress(null)

    if (!result.ok) {
      return
    }

    if (result.data.outcome === 'pushed_since') {
      setPushed(result.data.message)
      loaded.reload()

      return
    }

    if (!keepsDraft(result.data)) {
      drafts.clear(id)
    }

    onDone(result.data, r)
  }

  const run = (action: ReviewAction) => {
    if (review === null) {
      return
    }

    const offer = review.offers.find((o) => o.action === action)

    if (offer === undefined || !offer.enabled) {
      return
    }

    if (offer.confirm) {
      setConfirming(offer)

      return
    }

    void send(review, action, review.defaultMethod, false)
  }

  const selected = review === null ? '' : selectedAction(review, draft.action)
  const reply = useReplyBody(id, review?.isBot ?? item?.authorIsBot ?? false, draft, selected)

  // The backend reports each step of an action on this pull request while it runs.
  const heard = useEffectEvent((itemId: number, step: string) => {
    if (itemId === id) {
      setProgress((p) => withStep(p, step))
    }
  })

  useEffect(
    () =>
      on('review:progress', (data) => {
        heard(data.itemId, data.step)
      }),
    [],
  )

  // While an action runs, the sheet waits for it: it neither closes nor moves on, so the
  // outcome is never lost.
  const working = progress !== null

  const close = () => {
    if (!working) {
      onClose()
    }
  }

  const onKey = useEffectEvent((e: KeyboardEvent) => {
    const dialog = document.querySelector(`dialog[aria-labelledby="${titleId}"]`)

    if (!(dialog instanceof HTMLDialogElement) || !isTopmost(dialog) || working) {
      return
    }

    if (e.key === 'Enter' && e.metaKey) {
      e.preventDefault()

      if (needsText(selected, reply.body, draft.comments.length) === '') {
        run(selected)
      }

      return
    }

    if (isTyping(e) || e.metaKey || e.altKey || e.ctrlKey) {
      return
    }

    if (e.key === 'ArrowLeft' && prev !== undefined) {
      e.preventDefault()
      onNavigate(prev.id)
    } else if (e.key === 'ArrowRight' && next !== undefined) {
      e.preventDefault()
      onNavigate(next.id)
    }
  })

  useEffect(() => {
    const listener = (e: KeyboardEvent) => {
      onKey(e)
    }

    document.addEventListener('keydown', listener)

    return () => {
      document.removeEventListener('keydown', listener)
    }
  }, [])

  return (
    <Sheet labelledBy={titleId} onClose={close}>
      <div inert={working} className="flex min-h-0 flex-1 flex-col">
        <ReviewHeader
          titleId={titleId}
          review={review}
          item={item}
          position={position}
          checking={loaded.checking}
          onPrev={
            prev === undefined
              ? null
              : () => {
                  onNavigate(prev.id)
                }
          }
          onNext={
            next === undefined
              ? null
              : () => {
                  onNavigate(next.id)
                }
          }
          onClose={close}
        />
        <div className="space-y-2 px-5 empty:hidden">
          {loaded.error !== '' && <ErrorBanner>{loaded.error}</ErrorBanner>}
          {(pushed !== '' || (review?.stale === true && !loaded.checking && review.fetchedAt !== '')) && (
            <div className="mt-2 flex items-center gap-3 rounded-md border border-warning/60 px-3 py-2 text-sm text-warning">
              <span className="flex-1">{pushed === '' ? 'This pull request changed since it was read.' : pushed}</span>
              <button
                type="button"
                onClick={() => {
                  setPushed('')
                  loaded.reload()
                }}
                className={buttonClass}
              >
                Reload
              </button>
            </div>
          )}
          {loaded.patchError !== '' && <ErrorBanner>{loaded.patchError}</ErrorBanner>}
        </div>
        {/* One grid in every state, so the reading banner stays as the inbox's copy of the
            pull request arrives and its seconds run on. Before any copy, each column shows
            placeholders; with nothing read and nothing reading, only the error shows. */}
        {(review !== null || loaded.checking || loaded.error === '') && (
          <div className="grid min-h-0 flex-1 grid-cols-[14rem_minmax(0,1fr)_22rem]">
            <aside className="min-h-0 overflow-y-auto border-r border-border p-3">
              {review === null || (reading && review.files.length === 0) ? (
                <FileListSkeleton />
              ) : (
                <FileList review={review} viewed={draft.viewed} />
              )}
            </aside>
            <main className="min-h-0 space-y-4 overflow-y-auto p-4">
              {reading && <ReadingBanner key={id} target={target} />}
              {review === null ? (
                <>
                  <div className="rounded-md border border-border p-3">
                    <DescriptionSkeleton />
                  </div>
                  <DiffSkeleton />
                </>
              ) : (
                <>
                  <section aria-label="Description" className="rounded-md border border-border p-3">
                    {review.body.trim() !== '' ? (
                      <Markdown text={review.body} />
                    ) : reading ? (
                      <DescriptionSkeleton />
                    ) : (
                      <p className="text-sm text-muted">
                        {review.fetchedAt === '' ? 'Not read from GitHub yet.' : 'No description.'}
                      </p>
                    )}
                  </section>
                  {reading && review.files.length === 0 ? (
                    <DiffSkeleton />
                  ) : (
                    <DiffView
                      review={review}
                      draft={draft}
                      onDraft={onDraft}
                      loading={loaded.loading}
                      onLoad={(paths) => {
                        void loaded.loadPatches(paths)
                      }}
                    />
                  )}
                </>
              )}
            </main>
            {review === null ? (
              <aside className="min-h-0 border-l border-border p-4">
                <span aria-hidden="true" className="skeleton mb-4 block h-3.5 w-36" />
                <SummarySkeleton />
              </aside>
            ) : (
              /* The review form sits below the scrolling summary, in no scrolling box of its
                 own, so its menus open over the diff rather than being cut off at the column's
                 edge, and its button never scrolls out of view. */
              <aside className="flex min-h-0 flex-col border-l border-border">
                {/* How close it is to merging stays in view while the summary scrolls. */}
                <div className="shrink-0 border-b border-border px-4 py-3">
                  <Readiness review={review} />
                </div>
                <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
                  {reading ? (
                    <SummarySkeleton />
                  ) : (
                    <ReviewSummary review={review} item={item} onFilesSettings={onFilesSettings} />
                  )}
                  <AgentVerdict
                    sessions={sessions}
                    agents={launch?.agents ?? []}
                    onUse={(text) => {
                      onDraft((d) => ({
                        ...d,
                        body: text,
                        edited: true,
                        action: d.action === '' ? 'comment' : d.action,
                      }))
                    }}
                  />
                </div>
                <div className="shrink-0 border-t border-border p-4">
                  <ReviewComposer
                    review={review}
                    item={item}
                    draft={draft}
                    onDraft={onDraft}
                    reply={reply}
                    selected={selected}
                    onRun={run}
                    busy={act.pending}
                    error={act.error}
                    launch={launch}
                    resolution={resolution}
                    sessions={sessions}
                    onManageReplies={onManageReplies}
                  />
                </div>
              </aside>
            )}
          </div>
        )}
      </div>
      {progress !== null && <ActionProgress title={progress.title} steps={progress.steps} />}
      {confirming !== null && review !== null && (
        <MergeConfirmDialog
          review={review}
          offer={confirming}
          onCancel={() => {
            setConfirming(null)
          }}
          onConfirm={(method) => {
            setConfirming(null)
            void send(review, confirming.action, method, confirming.warnings.length > 0)
          }}
        />
      )}
    </Sheet>
  )
}
