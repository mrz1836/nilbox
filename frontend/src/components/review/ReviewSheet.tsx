import { Activity, useId, useLayoutEffect, useRef, useState, type ReactNode } from 'react'

import { refText } from '../../lib/format'
import { PicturesContext, reviewPictureLogins, usePictures } from '../../lib/pictures'
import type { ReviewDrafts } from '../../lib/reviewDrafts'
import { placeIn, stepTo, useActionProgress, useSheetKeys, type SheetBack } from '../../lib/sheet'
import { commentCount, entryAnchor, quoteInto } from '../../lib/timeline'
import { useMutation } from '../../lib/useMutation'
import { heldRuns, useReview } from '../../lib/useReview'
import {
  actOnReview,
  openExternal,
  type AgentSession,
  type ConversationOrder,
  type InboxItem,
  type ItemResolution,
  type MergeMethod,
  type Review,
  type ReviewAction,
  type ReviewOffer,
  type ReviewResult,
  type ReviewTab,
  type Worktree,
} from '../../platform'
import { Description } from '../conversation/Description'
import { Timeline } from '../conversation/Timeline'
import { buttonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { CommentIcon, DocumentIcon } from '../icons'
import type { LaunchOptions } from '../ItemRow'
import { Sheet } from '../Sheet'
import { TabPanel, Tabs } from '../Tabs'
import { WorkLine } from '../WorkLine'
import { ActionProgress } from './ActionProgress'
import { AgentVerdict } from './AgentVerdict'
import { DiffView, fileAnchor, type Reveal } from './DiffView'
import { FileList } from './FileList'
import { HeldRuns } from './HeldRuns'
import { menuOnly } from './ActionButton'
import { ActionConfirmDialog } from './ActionConfirmDialog'
import { MergeConfirmDialog, merges } from './MergeConfirmDialog'
import { commentFirst, heldBack, ReviewComposer, sendsText, sentText, useReplyBody } from './ReviewComposer'
import { ReviewHeader } from './ReviewHeader'
import { DescriptionSkeleton, DiffSkeleton, FileListSkeleton, ReadingBanner, SummarySkeleton } from './ReviewLoading'
import { Readiness, ReviewSummary } from './ReviewSummary'

export interface ReviewSheetProps {
  // id is the pull request the sheet shows, and item its inbox card.
  id: number
  item: InboxItem | undefined
  // siblings are the pull requests of the list and tab it was opened from, in the order
  // the list shows them, for stepping through them; listTitle names that list.
  siblings: readonly InboxItem[]
  listTitle: string
  drafts: ReviewDrafts
  // tab is the tab of the larger column the sheet opens on, the one the user last chose,
  // and onTab hears each choice, which the window remembers.
  tab: ReviewTab
  onTab: (tab: ReviewTab) => void
  // order is the order the conversation is listed in, and onOrder hears the user choose
  // another, which the window remembers.
  order?: ConversationOrder
  onOrder?: ((order: ConversationOrder) => void) | undefined
  launch: LaunchOptions | null
  resolution: ItemResolution | null
  sessions: readonly AgentSession[] | undefined
  // worktree is the pull request's worktree, which the sheet's work line shows, if it has
  // one.
  worktree?: Worktree | undefined
  onNavigate: (id: number) => void
  onClose: () => void
  // onDone hears an action's outcome, once the sheet closes for it.
  onDone: (result: ReviewResult, review: Review) => void
  onFilesSettings?: (() => void) | undefined
  onManageReplies?: (() => void) | undefined
  // initialFocus selects the control focus starts on as the sheet opens (see Sheet), and
  // refocus moves it there again whenever it changes.
  initialFocus?: string | undefined
  refocus?: number | undefined
  // back returns to the sheet a link was followed from, or is null.
  back?: SheetBack | null
  // triage is the card's triage, in the header, and toast the inbox's toast, which shows in
  // the sheet while it is open, since the page behind it can't be reached.
  triage?: ReactNode
  toast?: ReactNode
}

// selectedAction is the action the sheet's button runs: the one the user chose, while the
// sheet still offers it enabled, else the sheet's primary; never one that runs only from
// the menu, such as a close.
export function selectedAction(review: Review, chosen: ReviewAction): ReviewAction {
  const offer = review.offers.find((o) => o.action === chosen)

  if (chosen !== '' && !menuOnly(chosen) && offer?.enabled === true) {
    return chosen
  }

  return menuOnly(review.primary) ? '' : review.primary
}

// keepsDraft reports an outcome that leaves the draft for another try: an action refused
// for a push, or one that changed nothing the draft was for.
function keepsDraft(result: ReviewResult): boolean {
  return result.outcome === 'pushed_since' || result.outcome === 'branch_updating' || result.outcome === 'asked_bot'
}

// Notice is what the sheet says of one pull request after an action on it: an action
// refused for a push since, or why one failed. It says it only while that pull request
// shows, never of the next one the sheet steps to.
interface Notice {
  id: number
  message: string
}

// Scrollers are the scrolling boxes of the two tabs, whose places the sheet keeps across a
// switch, and Places those places.
type Scrollers = Record<ReviewTab, HTMLElement | null>
type Places = Record<ReviewTab, number>

// ConversationSkeleton is the conversation's placeholder while the first read comes.
function ConversationSkeleton() {
  return (
    <div className="mx-auto max-w-3xl space-y-3">
      {[0, 1].map((i) => (
        <div key={i} className="rounded-md border border-border p-3">
          <DescriptionSkeleton />
        </div>
      ))}
    </div>
  )
}

// ReviewSheet is the review sheet: a pull request's description, files, and diffs (Files),
// its conversation (Comments), what it still needs to merge, the work on it (its worktree
// and agent session, in a line under that), the latest agent's verdict, and the review to
// send. The tab it opens on is the one the user last chose, and each keeps its place, and
// what was being written in it, while the other shows. ← and → step through its list while
// the user isn't typing or moving between the tabs, ⌘↩ runs the button's action, and
// Escape closes it. It closes after an action, which the inbox's toast reports, except when
// the pull request was pushed to since, when it stays open, with the draft, to reload, and
// after it approved the workflow runs GitHub held, when it stays open, with the draft, and
// reads again until their checks start (HeldRuns says so).
export function ReviewSheet({
  id,
  item,
  siblings,
  listTitle,
  drafts,
  tab: openOn,
  onTab,
  order = 'oldest',
  onOrder,
  launch,
  resolution,
  sessions,
  worktree,
  onNavigate,
  onClose,
  onDone,
  onFilesSettings,
  onManageReplies,
  initialFocus,
  refocus,
  back = null,
  triage,
  toast,
}: ReviewSheetProps) {
  const titleId = useId()
  const tabsId = useId()
  const loaded = useReview(id)
  const review = loaded.review
  const pictures = usePictures(review === null ? [] : reviewPictureLogins(review))
  const draft = drafts.get(id)
  // tab shows at once on a click, and the window remembers it (onTab) behind it.
  const [tab, setTab] = useState<ReviewTab>(openOn)
  const scrollers = useRef<Scrollers>({ files: null, comments: null })
  const places = useRef<Places>({ files: 0, comments: 0 })
  // goingTo is where a jump scrolls once the tab it is in shows: a file's block or an
  // entry's row, by ID.
  const goingTo = useRef('')
  const [reveal, setReveal] = useState<Reveal | null>(null)
  const textRef = useRef<HTMLTextAreaElement>(null)
  const act = useMutation(actOnReview)
  const [confirming, setConfirming] = useState<ReviewOffer | null>(null)
  const [pushed, setPushed] = useState<Notice | null>(null)
  const [approved, setApproved] = useState<Notice | null>(null)
  // actedOn is the pull request the last action ran on, and the action, whose error the
  // composer shows, or, for an approval of held runs, their bar.
  const [actedOn, setActedOn] = useState<{ id: number; action: ReviewAction }>({ id: 0, action: '' })
  const [progress, setProgress] = useActionProgress('review:progress', id)
  const pushedHere = pushed?.id === id ? pushed.message : ''
  const approvedHere = approved?.id === id ? approved.message : ''
  const actError = actedOn.id === id ? act.error : ''
  const approving = actedOn.action === 'approve_runs'

  // target names the pull request while it loads, and reading reports its first read from
  // GitHub under way: nothing the sheet would show has come from GitHub yet, so each
  // column shows placeholders shaped like what comes.
  const shown = review ?? item
  const target = shown === undefined ? '' : refText(shown)
  const reading = loaded.checking && (review === null || review.fetchedAt === '')

  const { prev, next, position } = placeIn(siblings, id, listTitle)

  const onDraft = (change: Parameters<ReviewDrafts['update']>[1]) => {
    drafts.update(id, change)
  }

  const send = async (r: Review, action: ReviewAction, method: MergeMethod, accept: boolean) => {
    const offer = r.offers.find((o) => o.action === action)
    // A review sends the text and line comments, and so do a merge with no review of its own
    // and a close, which post them as a comment first when there are any, as the offer says.
    const text = sendsText(offer)

    setProgress({
      title: offer?.label ?? 'Working',
      target: refText(r),
      steps: [],
      staysOpen: action === 'approve_runs',
    })
    setActedOn({ id, action })

    const result = await act.run({
      itemId: id,
      action,
      head: r.head,
      body: text ? sentText(action, draft, reply.body) : '',
      comments: text
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
      if (result.data.commented) {
        // The merge posted the text and line comments before it found the push, so the
        // draft keeps the chosen action and nothing that would post them twice. (A close
        // reads the head before its comment, so it never gets here having posted one.)
        drafts.update(id, (d) => ({ ...d, body: '', edited: true, replyId: 0, comments: [] }))
      }

      setPushed({ id, message: result.data.message })
      loaded.reload()

      return
    }

    if (result.data.outcome === 'runs_approved') {
      // The sheet stays open, with the draft, and reads again until the runs' checks start;
      // each read rewrites the inbox card too.
      setApproved({ id, message: result.data.message })
      loaded.followRuns(heldRuns(r))

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

  // quoting is what a quote writes to: the pull request shown, its drafts, and the review's
  // text, as the sheet last rendered. quote, made once, reads it, so the conversation's
  // entries keep the same quote while the user types, and render again only when their own
  // entry changes. A quote starts a comment, unless the user chose another action.
  const quoting = useRef({ id, update: drafts.update, body: reply.body })

  useLayoutEffect(() => {
    quoting.current = { id, update: drafts.update, body: reply.body }
  })

  const [quote] = useState(() => (text: string) => {
    const { id: at, update, body } = quoting.current

    update(at, (d) => ({
      ...d,
      body: quoteInto(d.edited ? d.body : body, text),
      edited: true,
      action: d.action === '' ? 'comment' : d.action,
    }))
    textRef.current?.focus()
  })

  // chooseTab shows the tab next, keeping the place of the one it leaves, and remembers it,
  // so the sheet opens on it from then on.
  const chooseTab = (next: ReviewTab) => {
    if (next === tab) {
      return
    }

    places.current[tab] = scrollers.current[tab]?.scrollTop ?? 0
    setTab(next)
    onTab(next)
  }

  // jumpTo shows the tab at and scrolls to the element with the ID anchor in it, once it
  // shows.
  const jumpTo = (at: ReviewTab, anchor: string) => {
    if (at === tab) {
      document.getElementById(anchor)?.scrollIntoView({ block: 'start' })

      return
    }

    goingTo.current = anchor
    chooseTab(at)
  }

  // The tab shown goes back to its place, or to where a jump to it goes, once its Activity
  // has shown it.
  useLayoutEffect(() => {
    const anchor = goingTo.current

    goingTo.current = ''

    const scroller = scrollers.current[tab]

    if (anchor === '') {
      if (scroller !== null) {
        scroller.scrollTop = places.current[tab]
      }
    } else {
      document.getElementById(anchor)?.scrollIntoView({ block: 'start' })
    }
  }, [tab])

  // jump shows the file a line comment is on in Files, opening it if it is folded. It is
  // made once, as quote is, for the conversation's entries, and reads the sheet's jumpTo as
  // last rendered.
  const jumping = useRef(jumpTo)

  useLayoutEffect(() => {
    jumping.current = jumpTo
  })

  const [jump] = useState(() => (path: string) => {
    setReveal((r) => ({ path, n: (r?.n ?? 0) + 1 }))
    jumping.current('files', fileAnchor(path))
  })

  // showReview shows a reviewer's review in Comments, or on GitHub while the conversation the
  // sheet holds hasn't it.
  const showReview = (url: string) => {
    const entry = url === '' ? undefined : review?.timeline.items.find((e) => e.url === url)

    if (entry === undefined) {
      openExternal(url)
    } else {
      jumpTo('comments', entryAnchor(entry.id))
    }
  }

  // While an action runs, the sheet waits for it: it neither closes nor moves on, so the
  // outcome is never lost.
  const working = progress !== null

  const close = () => {
    if (!working) {
      onClose()
    }
  }

  const toPrev = stepTo(prev, onNavigate)
  const toNext = stepTo(next, onNavigate)

  useSheetKeys(titleId, {
    working,
    run: () => {
      if (
        heldBack(
          review?.offers.find((o) => o.action === selected),
          reply.body,
          draft.comments.length,
        ) === ''
      ) {
        run(selected)
      }
    },
    prev: toPrev,
    next: toNext,
  })

  return (
    <Sheet labelledBy={titleId} onClose={close} initialFocus={initialFocus} refocus={refocus} toast={toast}>
      <div inert={working} className="flex min-h-0 flex-1 flex-col">
        <ReviewHeader
          titleId={titleId}
          review={review}
          item={item}
          position={position}
          checking={loaded.checking}
          onPrev={toPrev}
          onNext={toNext}
          back={back}
          actions={triage}
          onClose={close}
          // A notice that it was pushed to since stays until its own Reload is clicked.
          onRefresh={() => {
            loaded.reload()
          }}
        />
        <div className="space-y-2 px-5 empty:hidden">
          {loaded.error !== '' && <ErrorBanner>{loaded.error}</ErrorBanner>}
          {(pushedHere !== '' || (review?.stale === true && !loaded.checking && review.fetchedAt !== '')) && (
            <div className="mt-2 flex items-center gap-3 rounded-md border border-warning/60 px-3 py-2 text-sm text-warning">
              <span className="flex-1">
                {pushedHere === '' ? 'This pull request changed since it was read.' : pushedHere}
              </span>
              <button
                type="button"
                onClick={() => {
                  setPushed(null)
                  loaded.reload()
                }}
                className={buttonClass}
              >
                Reload
              </button>
            </div>
          )}
          {review !== null && (
            <HeldRuns
              review={review}
              notice={approvedHere}
              following={loaded.following}
              error={approving ? actError : ''}
              busy={act.pending}
              onApprove={() => {
                run('approve_runs')
              }}
            />
          )}
          {loaded.patchError !== '' && <ErrorBanner>{loaded.patchError}</ErrorBanner>}
        </div>
        {/* One grid in every state, so the reading banner stays as the inbox's copy of the
            pull request arrives and its seconds run on. Before any copy, each column shows
            placeholders; with nothing read and nothing reading, only the error shows. */}
        {(review !== null || loaded.checking || loaded.error === '') && (
          <PicturesContext value={pictures}>
            <div className="grid min-h-0 flex-1 grid-cols-[minmax(0,1fr)_22rem]">
              {/* The larger column holds two tabs, each kept mounted while the other shows, so
                  a line comment being written, the files folded, and each tab's place stay. */}
              <section aria-label="Pull request" className="flex min-h-0 flex-col">
                <Tabs
                  baseId={tabsId}
                  label="Pull request"
                  value={tab}
                  onChange={chooseTab}
                  tabs={[
                    {
                      value: 'files',
                      label: 'Files',
                      icon: <DocumentIcon className="size-3.5" />,
                      count: review === null ? '' : String(review.totalFiles),
                    },
                    {
                      value: 'comments',
                      label: 'Comments',
                      icon: <CommentIcon className="size-3.5" />,
                      count:
                        review === null || review.fetchedAt === ''
                          ? ''
                          : commentCount(review.timeline.items, review.timeline.before),
                    },
                  ]}
                  className="flex shrink-0 gap-1 border-b border-border px-4 pt-3"
                />
                {reading && (
                  <div className="shrink-0 px-4 pt-4">
                    <ReadingBanner key={id} target={target} />
                  </div>
                )}
                <TabPanel baseId={tabsId} value={tab} className="flex min-h-0 flex-1 flex-col">
                  <Activity mode={tab === 'files' ? 'visible' : 'hidden'}>
                    <div className="grid min-h-0 flex-1 grid-cols-[14rem_minmax(0,1fr)]">
                      <aside className="min-h-0 overflow-y-auto border-r border-border p-3">
                        {review === null || (reading && review.files.length === 0) ? (
                          <FileListSkeleton />
                        ) : (
                          <FileList review={review} viewed={draft.viewed} />
                        )}
                      </aside>
                      <main
                        ref={(el) => {
                          scrollers.current.files = el
                        }}
                        className="min-h-0 space-y-4 overflow-y-auto p-4"
                      >
                        {review === null ? (
                          <>
                            <div className="rounded-md border border-border p-3">
                              <DescriptionSkeleton />
                            </div>
                            <DiffSkeleton />
                          </>
                        ) : (
                          <>
                            <Description
                              author={review.author}
                              openedAt={review.openedAt}
                              editedAt={review.editedAt}
                              body={review.body}
                              bodyTrimmed={review.bodyTrimmed}
                              reactions={review.reactions}
                              url={review.url}
                              reading={reading}
                              read={review.fetchedAt !== ''}
                            />
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
                                reveal={reveal}
                              />
                            )}
                          </>
                        )}
                      </main>
                    </div>
                  </Activity>
                  <Activity mode={tab === 'comments' ? 'visible' : 'hidden'}>
                    <div
                      ref={(el) => {
                        scrollers.current.comments = el
                      }}
                      className="min-h-0 flex-1 overflow-y-auto p-4"
                    >
                      {review === null || reading ? (
                        <ConversationSkeleton />
                      ) : (
                        <Timeline
                          key={`conversation-${String(id)}`}
                          page={review.timeline}
                          repo={review.repo}
                          author={review.author}
                          lastSeenAt={loaded.lastSeenAt}
                          onEarlier={() => {
                            void loaded.loadEarlier()
                          }}
                          loadingEarlier={loaded.loadingEarlier}
                          earlierError={loaded.earlierError}
                          onQuote={quote}
                          onJump={jump}
                          order={order}
                          onOrder={onOrder}
                          empty={
                            <p className="text-sm text-muted">
                              {review.fetchedAt === '' ? 'Not read from GitHub yet.' : 'No comments yet.'}
                            </p>
                          }
                        />
                      )}
                    </div>
                  </Activity>
                </TabPanel>
              </section>
              {review === null ? (
                <aside className="min-h-0 border-l border-border p-4">
                  <span aria-hidden="true" className="skeleton mb-4 block h-3.5 w-36" />
                  <SummarySkeleton />
                </aside>
              ) : (
                /* The review form sits below the scrolling summary, in no scrolling box of its
                 own, so its menus open over the diff rather than being cut off at the column's
                 edge, and its button never scrolls out of view. The work line sits in the strip
                 above, outside the scroller, so its panel isn't cut off either. */
                <aside className="flex min-h-0 flex-col border-l border-border">
                  {/* How close it is to merging, and the work on it, stay in view while the
                    summary scrolls. */}
                  <div className="shrink-0 space-y-1.5 border-b border-border px-4 py-3">
                    <Readiness review={review} />
                    <WorkLine worktree={worktree} sessions={sessions} agents={launch?.agents ?? []} />
                  </div>
                  <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
                    {reading ? (
                      <SummarySkeleton />
                    ) : (
                      <ReviewSummary
                        review={review}
                        item={item}
                        onFilesSettings={onFilesSettings}
                        onShowReview={showReview}
                      />
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
                      error={approving ? '' : actError}
                      launch={launch}
                      resolution={resolution}
                      sessions={sessions}
                      onManageReplies={onManageReplies}
                      textRef={textRef}
                    />
                  </div>
                </aside>
              )}
            </div>
          </PicturesContext>
        )}
      </div>
      {progress !== null && (
        <ActionProgress
          title={progress.title}
          target={progress.target}
          steps={progress.steps}
          staysOpen={progress.staysOpen}
        />
      )}
      {/* A merge asks with its methods; any other action that asks first, with none. */}
      {confirming !== null &&
        review !== null &&
        (merges(confirming.action) ? (
          <MergeConfirmDialog
            review={review}
            offer={confirming}
            comment={commentFirst(confirming, reply.body, draft.comments.length)}
            onCancel={() => {
              setConfirming(null)
            }}
            onConfirm={(method) => {
              setConfirming(null)
              void send(review, confirming.action, method, confirming.warnings.length > 0)
            }}
          />
        ) : (
          <ActionConfirmDialog
            review={review}
            offer={confirming}
            comment={commentFirst(confirming, sentText(confirming.action, draft, reply.body), draft.comments.length)}
            onCancel={() => {
              setConfirming(null)
            }}
            onConfirm={() => {
              setConfirming(null)
              void send(review, confirming.action, review.defaultMethod, confirming.warnings.length > 0)
            }}
          />
        ))}
    </Sheet>
  )
}
