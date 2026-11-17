import { useEffect, useRef, type ReactNode } from 'react'

import { counted } from '../../lib/format'
import { itemText, type ActivityLine } from '../../lib/autopilot'
import { agentRunOf } from '../../lib/runView'
import { formatFull } from '../../lib/time'
import { useMutation } from '../../lib/useMutation'
import {
  letRoutineActAgain,
  tryRoutineAgain,
  undoAutopilot,
  type AutopilotItemRef,
  type AutopilotMatch,
} from '../../platform'
import { quietButtonClass } from '../controls'
import { DayGroups } from '../notifications/DayGroups'
import { ArchiveIcon, AutopilotIcon, BellIcon, ClockIcon, PinIcon, ResetIcon, RingIcon, XIcon } from '../icons'

// clockTime is a line's time of day: "11:30 AM".
function clockTime(iso: string): string {
  const ms = Date.parse(iso)

  return Number.isNaN(ms) ? '' : new Date(ms).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

// LineIcon marks what a line says: the step a grouped line stands for, a Dry run's record,
// a notice, what gave up or failed, and what a routine leaves alone.
function LineIcon({ line }: { line: ActivityLine }) {
  const cls = 'size-3.5 shrink-0'

  switch (line.kind) {
    case 'group':
      switch (line.group.action) {
        case 'snooze':
          return <ClockIcon className={`${cls} text-accent`} />
        case 'dismiss':
          return <ArchiveIcon className={`${cls} text-accent`} />
        case 'pin':
          return <PinIcon filled className={`${cls} text-accent`} />
        default:
          return <ResetIcon className={`${cls} text-accent`} />
      }
    case 'leftAlone':
      return <ResetIcon className={`${cls} text-warning`} />
    case 'match':
      if (line.match.dryRun) {
        return <RingIcon className={`${cls} text-muted`} />
      }

      if (line.match.state === 'skipped' || line.match.reason === 'failed') {
        return <XIcon className={`${cls} text-danger`} />
      }

      return line.match.steps.some((s) => s.kind === 'notify' || s.kind === 'tellMe') ? (
        <BellIcon className={`${cls} text-accent`} />
      ) : (
        <AutopilotIcon className={`${cls} text-accent`} />
      )
  }
}

// matchHeadline is what a match's line says first: what it did, in a word or two.
function matchHeadline(m: AutopilotMatch): string {
  if (m.dryRun) {
    return 'Would run'
  }

  if (m.state === 'skipped') {
    return m.reason === 'tries' ? 'Gave up' : 'Skipped'
  }

  if (m.state === 'dismissed') {
    return 'You skipped it'
  }

  if (m.reason === 'failed') {
    return 'A step failed'
  }

  return m.steps.some((s) => s.kind === 'notify' || s.kind === 'tellMe') ? 'Told you' : 'Ran'
}

// groupItems names the items a grouped line stands for, the first few by name.
function groupItems(items: readonly AutopilotItemRef[]): string {
  const named = items.slice(0, 3).map(itemText).join(', ')

  return items.length > 3 ? `${named}, and ${String(items.length - 3)} more` : named
}

export interface ActivityListProps {
  lines: readonly ActivityLine[]
  // routineColumn shows each line's routine, as Activity across every routine does; a
  // routine's own Activity leaves it out.
  routineColumn?: boolean
  // highlight is the match a notification or a chip asked to show, whose line is marked
  // and scrolled into view; seq numbers the asks, so asking again scrolls again.
  highlight?: { matchId: number; seq: number } | null
  // onOpen shows an item: its card in the inbox when it is listed, else its row in the
  // routine's Watching list.
  onOpen: (item: AutopilotItemRef, routineId: number) => void
  // onShowRun opens a match's agent's run in the background in its run view; without it, a
  // line offers none.
  onShowRun?: ((runId: number) => void) | undefined
  // onToast says what an action did, or why it couldn't.
  onToast: (text: string, tone?: 'error') => void
  // onChanged hears a line's action (Undo, Try again, Let it act again) go through, which
  // may change a line further down than Activity reads again on its own.
  onChanged?: () => void
  // empty is what the list says while it shows no line.
  empty: ReactNode
}

// LineProps is one line, with what the list shares.
interface LineProps extends Pick<ActivityListProps, 'onOpen' | 'onShowRun' | 'onToast' | 'onChanged'> {
  line: ActivityLine
  highlighted: boolean
  routineColumn: boolean
}

// ActivityLineRow is one line of Activity: its mark, what happened (and to which items),
// the routine, its actions (Undo, Try again, Let it act again, Show the run, Open), and its
// time.
function ActivityLineRow({ line, highlighted, routineColumn, onOpen, onShowRun, onToast, onChanged }: LineProps) {
  const act = useMutation((run: () => Promise<string>) => run())
  const time = clockTime(line.at)
  const run = (what: () => Promise<string>) => {
    void act.run(what).then((result) => {
      if (result.ok) {
        onChanged?.()

        if (result.data !== '') {
          onToast(result.data)
        }
      } else {
        onToast(result.error, 'error')
      }
    })
  }

  let headline: string
  let what: ReactNode
  let routine: string
  let actions: ReactNode = null

  switch (line.kind) {
    case 'group': {
      const g = line.group
      const taken = g.undone + g.reversed

      headline = g.words
      routine = g.routine
      what = <span className="truncate">{groupItems(g.items)}</span>
      actions =
        g.undo.length > 0 ? (
          <button
            type="button"
            disabled={act.pending}
            title={`Put back ${counted(g.undo.length, 'item', 'items')} as they were`}
            onClick={() => {
              run(() => undoAutopilot(g.undo).then((u) => u.words))
            }}
            className={quietButtonClass}
          >
            Undo
          </button>
        ) : (
          taken > 0 && (
            <span className="text-xs text-muted">{taken === g.count ? 'Taken back' : 'Partly taken back'}</span>
          )
        )
      break
    }
    case 'leftAlone': {
      const l = line.left

      headline = 'Left alone'
      routine = l.routine
      what = (
        <span className="truncate" title={l.item.title}>
          {itemText(l.item)} · {l.why}
        </span>
      )
      actions = (
        <>
          <button
            type="button"
            disabled={act.pending}
            onClick={() => {
              run(() => letRoutineActAgain(l.routineId, l.item.id).then(() => `${l.routine} can act on it again.`))
            }}
            className={quietButtonClass}
          >
            Let it act again
          </button>
          <OpenButton item={l.item} routineId={l.routineId} onOpen={onOpen} />
        </>
      )
      break
    }
    case 'match': {
      const m = line.match

      headline = matchHeadline(m)
      routine = m.routine
      what = (
        <span className="truncate" title={m.item.title}>
          {itemText(m.item)}
          {m.words !== '' && ` · ${m.words}`}
        </span>
      )
      actions = (
        <>
          {m.state === 'skipped' && m.reason === 'tries' && m.item.id > 0 && (
            <button
              type="button"
              disabled={act.pending}
              onClick={() => {
                run(() =>
                  tryRoutineAgain(m.routineId, m.item.id).then((opened) =>
                    opened.length > 0 ? `${m.routine} will try again.` : 'Its tries are reset for the next time.',
                  ),
                )
              }}
              className={quietButtonClass}
            >
              Try again
            </button>
          )}
          <ShowRunButton match={m} onShowRun={onShowRun} />
          <OpenButton item={m.item} routineId={m.routineId} onOpen={onOpen} />
        </>
      )
      break
    }
  }

  return (
    <li
      data-line={line.key}
      data-highlighted={highlighted || undefined}
      className="group flex min-w-0 items-center gap-3 rounded-md px-3 py-1.5 text-sm transition-colors hover:bg-raised/40 data-highlighted:bg-accent/10"
    >
      <LineIcon line={line} />
      <span className="w-28 shrink-0 truncate font-medium text-text" title={headline}>
        {headline}
      </span>
      <span className="flex min-w-0 flex-1 items-center gap-1 text-xs text-muted">{what}</span>
      {routineColumn && (
        <span className="hidden max-w-56 shrink-0 truncate text-xs text-muted lg:inline" title={routine}>
          {routine}
        </span>
      )}
      <span className="flex shrink-0 items-center gap-0.5">{actions}</span>
      <time
        dateTime={line.at}
        title={formatFull(line.at)}
        className="w-16 shrink-0 text-right text-xs text-muted tabular-nums"
      >
        {time}
      </time>
    </li>
  )
}

// ShowRunButton opens a match's newest agent's run in the background in its run view, when
// it has one.
function ShowRunButton({ match, onShowRun }: { match: AutopilotMatch; onShowRun: ActivityListProps['onShowRun'] }) {
  const run = agentRunOf(match)

  if (run === undefined || onShowRun === undefined) {
    return null
  }

  return (
    <button
      type="button"
      onClick={() => {
        onShowRun(run.id)
      }}
      className={quietButtonClass}
    >
      Show the run
    </button>
  )
}

// OpenButton shows a line's item, unless it is gone.
function OpenButton({
  item,
  routineId,
  onOpen,
}: {
  item: AutopilotItemRef
  routineId: number
  onOpen: ActivityListProps['onOpen']
}) {
  if (item.id <= 0 && item.url === '') {
    return null
  }

  return (
    <button
      type="button"
      aria-label={`Open ${itemText(item)}`}
      title={item.title}
      onClick={() => {
        onOpen(item, routineId)
      }}
      className={quietButtonClass}
    >
      Open
    </button>
  )
}

// ActivityList lists Activity's lines by day, newest first, under each day's heading: what
// the routines did, each with its actions. The line a notification asked to show is marked
// and scrolled into view.
export function ActivityList({
  lines,
  routineColumn = true,
  highlight = null,
  onOpen,
  onShowRun,
  onToast,
  onChanged,
  empty,
}: ActivityListProps) {
  const listRef = useRef<HTMLDivElement>(null)
  const shownKey =
    highlight === null
      ? ''
      : (lines.find(
          (l) =>
            (l.kind === 'match' && l.match.id === highlight.matchId) ||
            (l.kind === 'group' && l.group.matches.includes(highlight.matchId)),
        )?.key ?? '')

  useEffect(() => {
    if (shownKey === '' || highlight === null) {
      return
    }

    const shown = [...(listRef.current?.querySelectorAll('[data-line]') ?? [])].find(
      (el) => el.getAttribute('data-line') === shownKey,
    )

    shown?.scrollIntoView({ block: 'center' })
  }, [shownKey, highlight])

  if (lines.length === 0) {
    return <>{empty}</>
  }

  return (
    <div ref={listRef}>
      <DayGroups
        items={lines}
        when={(line) => line.at}
        render={(line) => (
          <ActivityLineRow
            key={line.key}
            line={line}
            highlighted={line.key === shownKey}
            routineColumn={routineColumn}
            onOpen={onOpen}
            onShowRun={onShowRun}
            onToast={onToast}
            {...(onChanged === undefined ? {} : { onChanged })}
          />
        )}
      />
    </div>
  )
}
