import { useId, useLayoutEffect, useRef, useState, type ReactNode } from 'react'

import { attempt } from '../../lib/call'
import { refText } from '../../lib/format'
import {
  changedSince,
  closeNote,
  issueHeldBack,
  keepsDraft,
  quoteInto,
  selectedIssueAction,
  standingTone,
} from '../../lib/issues'
import type { IssueDraft, IssueDrafts } from '../../lib/issueDrafts'
import { pictureLogins, PicturesContext, usePictures } from '../../lib/pictures'
import { placeIn, stepTo, useActionProgress, useSheetKeys, type SheetBack } from '../../lib/sheet'
import { toneDot, toneText } from '../../lib/signals'
import { formatAgo } from '../../lib/time'
import { useIssue } from '../../lib/useIssue'
import { useMutation } from '../../lib/useMutation'
import {
  actOnIssue,
  editIssue,
  type AgentSession,
  type InboxItem,
  type Issue,
  type IssueAction,
  type IssueOffer,
  type IssueResult,
  type IssueStanding,
  type ItemResolution,
  type Worktree,
} from '../../platform'
import { buttonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import type { LaunchOptions } from '../ItemRow'
import { RelativeTime } from '../RelativeTime'
import { ActionProgress } from '../review/ActionProgress'
import { AgentVerdict } from '../review/AgentVerdict'
import { DescriptionSkeleton, ReadingBanner, SummarySkeleton } from '../review/ReviewLoading'
import { Sheet } from '../Sheet'
import { WorkLine } from '../WorkLine'
import { Conversation } from './Conversation'
import { IssueCloseDialog } from './IssueCloseDialog'
import { IssueComposer } from './IssueComposer'
import { IssueHeader } from './IssueHeader'
import type { IssueEdit } from './IssuePickers'
import { IssueSummary } from './IssueSummary'

export interface IssueSheetProps {
  // id is the issue the sheet shows, and item its inbox card.
  id: number
  item: InboxItem | undefined
  // siblings are the cards of the list and tab it was opened from, in the order the list
  // shows them, for stepping through them; listTitle names that list.
  siblings: readonly InboxItem[]
  listTitle: string
  drafts: IssueDrafts
  launch: LaunchOptions | null
  resolution: ItemResolution | null
  sessions: readonly AgentSession[] | undefined
  // worktree is the issue's worktree, which the sheet's work line shows, if it has one.
  worktree?: Worktree | undefined
  onNavigate: (id: number) => void
  onClose: () => void
  // onDone hears an action's outcome, once the sheet closes for it.
  onDone: (result: IssueResult, issue: Issue) => void
  // onUnassign takes the user off the issue on GitHub, and resolves with why it couldn't,
  // or '' once it did, when the inbox closes the sheet; without it the sheet doesn't offer
  // it.
  onUnassign?: ((item: InboxItem) => Promise<string>) | undefined
  // onManageReplies opens the replies in the Prompts tab.
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

// Notice is what the sheet says of one issue after an action on it: a close refused for a
// comment since, or why taking the user off it failed. It says it only while that issue
// shows, never of the next one the sheet steps to.
interface Notice {
  id: number
  message: string
}

// StandingLine says where the issue stands, in its color, with the time it is about.
function StandingLine({ standing }: { standing: IssueStanding }) {
  const tone = standingTone(standing.state)

  return (
    <p className={`flex min-w-0 items-center gap-2 text-sm font-medium ${toneText[tone]}`}>
      <span aria-hidden="true" className={`size-2 shrink-0 rounded-full ${toneDot[tone]}`} />
      <span className="min-w-0">{standing.sentence}</span>
      {standing.at !== '' && (
        <span className="shrink-0 font-normal text-muted">
          · {standing.opened && 'opened '}
          <RelativeTime iso={standing.at} show={formatAgo} />
        </span>
      )}
    </p>
  )
}

// IssueSheet is the issue sheet: an issue's conversation, where it stands and the details
// beside it, the latest agent's verdict, and the comment to send. ← and → step through its
// list while the user isn't typing, ⌘↩ runs the button's action, and Escape closes it. It
// closes after an action, which the inbox's toast reports, except when someone commented
// since the user opened it, when it stays open, with the draft, and reads it again.
export function IssueSheet({
  id,
  item,
  siblings,
  listTitle,
  drafts,
  launch,
  resolution,
  sessions,
  worktree,
  onNavigate,
  onClose,
  onDone,
  onUnassign,
  onManageReplies,
  initialFocus,
  refocus,
  back = null,
  triage,
  toast,
}: IssueSheetProps) {
  const titleId = useId()
  const loaded = useIssue(id)
  const issue = loaded.issue
  const pictures = usePictures(issue === null ? [] : pictureLogins(issue))
  const draft = drafts.get(id)
  const act = useMutation(actOnIssue)
  const [confirming, setConfirming] = useState<IssueOffer | null>(null)
  const [notice, setNotice] = useState<Notice | null>(null)
  // actedOn is the issue the last action ran on, whose error the composer shows.
  const [actedOn, setActedOn] = useState(0)
  const [progress, setProgress] = useActionProgress('issue:progress', id)
  const textRef = useRef<HTMLTextAreaElement>(null)
  const noticeHere = notice?.id === id ? notice.message : ''
  // quoting is what a quote writes to: the issue shown and its drafts, as the sheet last
  // rendered. quote, made once, reads it, so the conversation's entries keep the same quote
  // while the user types, and render again only when their own entry changes.
  const quoting = useRef({ id, update: drafts.update })

  useLayoutEffect(() => {
    quoting.current = { id, update: drafts.update }
  })

  const [quote] = useState(() => (text: string) => {
    const { id: at, update } = quoting.current

    update(at, (d) => ({ ...d, body: quoteInto(d.body, text) }))
    textRef.current?.focus()
  })

  const shown = issue ?? item
  const target = shown === undefined ? '' : refText(shown)
  const reading = loaded.checking && (issue === null || issue.fetchedAt === '')
  const { prev, next, position } = placeIn(siblings, id, listTitle)

  const onDraft = (change: (d: IssueDraft) => IssueDraft) => {
    drafts.update(id, change)
  }

  const send = async (is: Issue, action: IssueAction) => {
    const offer = is.offers.find((o) => o.action === action)

    setProgress({ title: offer?.label ?? 'Working', target: refText(is), steps: [] })
    setActedOn(id)

    const result = await act.run({ itemId: id, action, seen: is.seen, body: draft.body })

    setProgress(null)

    if (!result.ok) {
      return
    }

    if (keepsDraft(result.data, draft.body)) {
      // A close someone made first sent nothing of the user's: their text stays, to copy.
      const kept = result.data.outcome === 'already_closed' ? '. Your text wasn’t posted, and is still here.' : ''

      setNotice({ id, message: `${result.data.message}${kept}` })
      loaded.reload()

      return
    }

    drafts.clear(id)
    onDone(result.data, is)
  }

  // edit changes the issue's labels or assignees, then reads it again once GitHub made any
  // of it, since what it made before a step that failed stands. It resolves with why it
  // couldn't, or '' once it did.
  const edit = async (change: IssueEdit): Promise<string> => {
    const result = await attempt(
      editIssue({
        itemId: id,
        addLabels: change.addLabels ?? [],
        removeLabels: change.removeLabels ?? [],
        addAssignees: change.addAssignees ?? [],
        removeAssignees: change.removeAssignees ?? [],
      }),
    )

    if (!result.ok) {
      return result.error
    }

    if (result.data.sent > 0) {
      loaded.reload()
    }

    return result.data.failed
  }

  const run = (action: IssueAction) => {
    const offer = issue?.offers.find((o) => o.action === action)

    if (issue === null || offer === undefined || !offer.enabled) {
      return
    }

    if (offer.confirm) {
      setConfirming(offer)

      return
    }

    void send(issue, action)
  }

  const unassign =
    onUnassign === undefined || item === undefined || issue?.assigned !== true
      ? undefined
      : () => {
          setProgress({ title: 'Unassign me', target, steps: ['Unassigning you on GitHub'] })
          setActedOn(id)

          void onUnassign(item).then((error) => {
            setProgress(null)

            if (error !== '') {
              setNotice({ id, message: error })
            }
          })
        }

  const selected = issue === null ? '' : selectedIssueAction(issue, draft.action)

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
      const offer = issue?.offers.find((o) => o.action === selected)

      if (offer?.enabled === true && issueHeldBack(offer, draft.body) === '') {
        run(selected)
      }
    },
    prev: toPrev,
    next: toNext,
  })

  const changed =
    issue !== null && !loaded.checking && (changedSince(issue, item) || (issue.stale && issue.fetchedAt !== ''))

  return (
    <Sheet labelledBy={titleId} onClose={close} initialFocus={initialFocus} refocus={refocus} toast={toast}>
      <div inert={working} className="flex min-h-0 flex-1 flex-col">
        <IssueHeader
          titleId={titleId}
          issue={issue}
          item={item}
          position={position}
          checking={loaded.checking}
          prev={toPrev}
          next={toNext}
          back={back}
          actions={triage}
          onRefresh={() => {
            loaded.reload()
          }}
          onClose={close}
        />
        <div className="space-y-2 px-5 empty:hidden">
          {loaded.error !== '' && <ErrorBanner>{loaded.error}</ErrorBanner>}
          {(noticeHere !== '' || changed) && (
            <div className="mt-2 flex items-center gap-3 rounded-md border border-warning/60 px-3 py-2 text-sm text-warning">
              <span className="flex-1">{noticeHere === '' ? 'This issue changed since it was read.' : noticeHere}</span>
              <button
                type="button"
                onClick={() => {
                  setNotice(null)
                  loaded.reload()
                }}
                className={buttonClass}
              >
                Reload
              </button>
            </div>
          )}
        </div>
        {/* One grid in every state, so the reading banner stays as the inbox's copy of the
            issue arrives. Before any copy, each column shows placeholders; with nothing read
            and nothing reading, only the error shows. */}
        {(issue !== null || loaded.checking || loaded.error === '') && (
          <PicturesContext value={pictures}>
            <div className="grid min-h-0 flex-1 grid-cols-[minmax(0,1fr)_22rem]">
              <main className="min-h-0 space-y-4 overflow-y-auto p-4">
                {reading && <ReadingBanner key={`reading-${String(id)}`} target={target} kind="issue" />}
                {issue === null ? (
                  <div className="mx-auto max-w-3xl rounded-md border border-border p-3">
                    <DescriptionSkeleton />
                  </div>
                ) : (
                  <Conversation
                    key={`conversation-${String(id)}`}
                    issue={issue}
                    lastSeenAt={loaded.lastSeenAt}
                    reading={reading}
                    onEarlier={() => {
                      void loaded.loadEarlier()
                    }}
                    loadingEarlier={loaded.loadingEarlier}
                    earlierError={loaded.earlierError}
                    onQuote={quote}
                  />
                )}
              </main>
              {issue === null ? (
                <aside className="min-h-0 border-l border-border p-4">
                  <span aria-hidden="true" className="skeleton mb-4 block h-3.5 w-36" />
                  <SummarySkeleton />
                </aside>
              ) : (
                /* The composer sits below the scrolling details, in no scrolling box of its
                 own, so its menus open over the conversation rather than being cut off at
                 the column's edge, and its button never scrolls out of view. The work line
                 sits in the strip above, outside the scroller, so its panel isn't cut off
                 either. */
                <aside className="flex min-h-0 flex-col border-l border-border">
                  <div className="shrink-0 space-y-1.5 border-b border-border px-4 py-3">
                    <StandingLine standing={issue.standing} />
                    <WorkLine worktree={worktree} sessions={sessions} agents={launch?.agents ?? []} />
                  </div>
                  <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
                    {reading ? <SummarySkeleton /> : <IssueSummary issue={issue} onEdit={edit} />}
                    <AgentVerdict
                      sessions={sessions}
                      agents={launch?.agents ?? []}
                      useLabel="Use as comment"
                      onUse={(text) => {
                        onDraft((d) => ({ ...d, body: text }))
                      }}
                    />
                  </div>
                  <div className="shrink-0 border-t border-border p-4">
                    <IssueComposer
                      issue={issue}
                      item={item}
                      draft={draft}
                      onDraft={onDraft}
                      selected={selected}
                      onRun={run}
                      busy={act.pending || working}
                      error={actedOn === id ? act.error : ''}
                      launch={launch}
                      resolution={resolution}
                      sessions={sessions}
                      onUnassign={unassign}
                      textRef={textRef}
                      onManageReplies={onManageReplies}
                    />
                  </div>
                </aside>
              )}
            </div>
          </PicturesContext>
        )}
      </div>
      {progress !== null && <ActionProgress title={progress.title} target={progress.target} steps={progress.steps} />}
      {confirming !== null && issue !== null && (
        <IssueCloseDialog
          issue={issue}
          offer={confirming}
          comment={closeNote(confirming, draft.body)}
          onCancel={() => {
            setConfirming(null)
          }}
          onConfirm={() => {
            setConfirming(null)
            void send(issue, confirming.action)
          }}
        />
      )}
    </Sheet>
  )
}
