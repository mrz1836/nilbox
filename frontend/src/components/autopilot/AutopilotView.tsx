import { useEffect, useEffectEvent, useId, useRef, useState } from 'react'

import {
  activityLines,
  elsewhereText,
  pausedText,
  resultLabels,
  resultOrder,
  routineActivity,
  routineStatus,
  sameSecond,
  signedOutText,
  type ActivityFilter,
  type ActivityResult,
  type AutopilotRequest,
} from '../../lib/autopilot'
import { attempt } from '../../lib/call'
import type { AutopilotState } from '../../lib/useAutopilot'
import { useAutopilotActivity } from '../../lib/useAutopilotActivity'
import { useNowAs } from '../../lib/useNow'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  deleteRoutine,
  duplicateRoutine,
  setAutopilotPaused,
  type AutopilotItemRef,
  type AutopilotRoutine,
  type Settings,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { buttonClass, captionClass, fieldClass, quietButtonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { AutopilotIcon, PauseIcon, PlayIcon, PulseIcon, SlidersIcon } from '../icons'
import { defaultToastSeconds, Toast, type ToastMessage } from '../Toast'
import { ActivityList } from './ActivityList'
import { RoutineList, type RoutineActions } from './RoutineList'
import { RoutinePane } from './RoutinePane'
import { useRoutineChange } from './useRoutineChange'
import { WatchingList, type RowsKit } from './WatchingList'

export interface AutopilotViewProps {
  // autopilot is Autopilot as the window shares it: at a glance, the routines, the engine.
  autopilot: AutopilotState
  // settings are the stored settings, null until they load, whose Pause Autopilot the
  // header's button and the paused banner's Resume change; onSettingsSaved receives them
  // as stored after.
  settings: Settings | null
  onSettingsSaved: (settings: Settings) => void
  // request is what the window last asked the tab to show: Activity, a match in it, or a
  // routine.
  request: AutopilotRequest | null
  // kit is what the Watching list's rows share with the inbox's.
  kit: RowsKit
  // listed reports an item the inbox lists, whose card Open shows there with onShowItem.
  listed: (itemId: number) => boolean
  onShowItem: (itemId: number, url: string) => void
  // onSettings opens Settings → Autopilot.
  onSettings: () => void
  toastSeconds?: number
}

// noFilter shows every line.
const noFilter: ActivityFilter = { routine: 0, result: 'all' }

// Focus is a match a request asked Activity to show, waiting for the pages that hold it.
interface Focus {
  matchId: number
  createdAt: string
  seq: number
}

// introText explains Autopilot in two sentences, as the tab does while there is no routine.
const introText = [
  'Autopilot watches your pull requests and issues and acts when something happens: a check fails, a reviewer asks for changes, a bug comes in.',
  'Its routines can tell you, tidy your inbox, or start a coding agent, always within limits you set.',
]

// PauseButton is Pause Autopilot in the tab's header, which writes through the settings'
// own setter, as Settings → Autopilot and the menu bar do.
function PauseButton({ settings, onSaved }: { settings: Settings; onSaved: (settings: Settings) => void }) {
  const { settings: shown, save, error } = useSettingsSave(settings, onSaved)
  const paused = shown.autopilot.paused

  return (
    <>
      <button
        type="button"
        aria-pressed={paused}
        title={paused ? 'Autopilot is paused: nothing new starts' : 'Stop new starts until you resume'}
        onClick={() => {
          void save({ autopilot: { ...shown.autopilot, paused: !paused } }, () => setAutopilotPaused(!paused))
        }}
        className={`${buttonClass} aria-pressed:border-warning/60 aria-pressed:bg-warning/10`}
      >
        {paused ? <PlayIcon className="size-3.5 text-muted" /> : <PauseIcon className="size-3.5 text-muted" />}
        {paused ? 'Resume Autopilot' : 'Pause Autopilot'}
      </button>
      {error !== '' && (
        <span role="alert" className="text-xs text-danger">
          {error}
        </span>
      )}
    </>
  )
}

interface BannersProps {
  autopilot: AutopilotState
  settings: Settings | null
  onSettingsSaved: (settings: Settings) => void
}

// Banners say, across the top, what stops everything: Pause Autopilot (with Resume),
// another copy of the app running Autopilot, and no one signed in to GitHub.
function Banners({ autopilot, settings, onSettingsSaved }: BannersProps) {
  const engine = autopilot.engine
  const paused = settings?.autopilot.paused ?? autopilot.glance?.paused ?? false

  return (
    <>
      {paused && settings !== null && <PausedBanner settings={settings} onSaved={onSettingsSaved} />}
      {engine?.elsewhere === true && (
        <p
          role="status"
          className="flex items-center gap-2 rounded-md border border-border bg-surface px-3 py-2 text-sm"
        >
          <PulseIcon className="size-4 shrink-0 text-muted" />
          {elsewhereText}
        </p>
      )}
      {engine?.signedOut === true && (
        <p role="status" className="rounded-md border border-warning/60 bg-warning/10 px-3 py-2 text-sm">
          {signedOutText}
        </p>
      )}
    </>
  )
}

// PausedBanner says Autopilot is paused, with Resume.
function PausedBanner({ settings, onSaved }: { settings: Settings; onSaved: (settings: Settings) => void }) {
  const { save, error } = useSettingsSave(settings, onSaved)

  return (
    <div
      role="status"
      className="flex items-center gap-3 rounded-md border border-warning/60 bg-warning/10 px-3 py-2 text-sm"
    >
      <PauseIcon className="size-4 shrink-0 text-warning" />
      <span className="min-w-0 flex-1">{pausedText}</span>
      {error !== '' && <span className="text-xs text-danger">{error}</span>}
      <button
        type="button"
        onClick={() => {
          void save({ autopilot: { ...settings.autopilot, paused: false } }, () => setAutopilotPaused(false))
        }}
        className={buttonClass}
      >
        Resume
      </button>
    </div>
  )
}

// LoadMore reads Activity's earlier days as the list's end scrolls into view, and offers
// a button for it, which works wherever the list's end can't be seen coming.
function LoadMore({ more, loading, onMore }: { more: boolean; loading: boolean; onMore: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const onSeen = useEffectEvent(() => {
    if (more && !loading) {
      onMore()
    }
  })

  useEffect(() => {
    const el = ref.current

    if (el === null || typeof IntersectionObserver === 'undefined') {
      return
    }

    const seen = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) {
        onSeen()
      }
    })

    seen.observe(el)

    return () => {
      seen.disconnect()
    }
  }, [])

  return (
    <div ref={ref} className="flex justify-center py-3">
      {more && (
        <button type="button" disabled={loading} onClick={onMore} className={quietButtonClass}>
          {loading ? 'Reading earlier days…' : 'Show earlier days'}
        </button>
      )}
    </div>
  )
}

// AutopilotView is the Autopilot tab: the routines on the left, each with its switch and
// its status, under Activity; and on the right Activity (what the routines did, by day,
// newest first, with the banners for what stops everything and its filters) or the routine
// chosen (its mode, its sentence, what it watches, and its own Activity). Pause Autopilot
// sits in the header. While there is no routine, the tab explains Autopilot.
export default function AutopilotView({
  autopilot,
  settings,
  onSettingsSaved,
  request,
  kit,
  listed,
  onShowItem,
  onSettings,
  toastSeconds = defaultToastSeconds,
}: AutopilotViewProps) {
  const id = useId()
  // today is the clock's day, as its midnight, which the routines' lines count today's
  // matches by; a tick within the day changes nothing.
  const today = useNowAs((now) => new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime())
  const activity = useAutopilotActivity()
  const [selected, setSelected] = useState(0)
  const [filter, setFilter] = useState<ActivityFilter>(noFilter)
  const [toast, setToast] = useState<ToastMessage | null>(null)
  const toasts = useRef(0)
  const [deleting, setDeleting] = useState<AutopilotRoutine | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)
  const [focus, setFocus] = useState<Focus | null>(null)
  const [highlight, setHighlight] = useState<{ matchId: number; seq: number } | null>(null)
  const [watchHighlight, setWatchHighlight] = useState<{ id: number; url: string; seq: number } | null>(null)
  const [handled, setHandled] = useState(0)
  const { routines, engine, reload } = autopilot

  const say = (text: string, tone?: ToastMessage['tone']) => {
    toasts.current += 1
    setToast({ id: toasts.current, text, ...(tone === undefined ? {} : { tone }) })
  }

  const changes = useRoutineChange(
    () => {
      reload()
    },
    (text) => {
      say(text, 'error')
    },
  )

  // noteOf is a toast a request's answer raises as the tab renders, numbered by the
  // request, below every toast the tab raises from a click.
  const noteOf = (seq: number, text: string): ToastMessage => ({ id: -seq, text, tone: 'warning' })

  // A request from a notification, the menu bar, or a chip shows Activity, at the match
  // it names, or the routine it names, once the routines are read.
  if (request !== null && request.seq !== handled && (request.target.matchId > 0 || autopilot.loaded)) {
    setHandled(request.seq)

    const { target } = request

    if (target.matchId > 0) {
      setSelected(0)
      setFilter(noFilter)
      setFocus({ matchId: target.matchId, createdAt: target.createdAt, seq: request.seq })
    } else if (target.routineId > 0 && !target.activity) {
      const routine = routines.find((r) => r.id === target.routineId)

      if (routine !== undefined && (target.createdAt === '' || sameSecond(routine.createdAt, target.createdAt))) {
        setSelected(routine.id)
      } else {
        setSelected(0)
        setToast(noteOf(request.seq, 'That routine is no longer here.'))
      }
    } else {
      setSelected(0)
    }
  }

  // A match asked for shows once the pages that hold it are read: earlier ones are read
  // until one does, and one no longer in the history, or made at another time (Clear
  // history hands an ID out again), says so.
  const loadedMatches = activity.days.flatMap((d) => d.matches)
  const focused = focus === null ? undefined : loadedMatches.find((m) => m.id === focus.matchId)
  const oldest = loadedMatches.reduce((low, m) => Math.min(low, m.id), Number.MAX_SAFE_INTEGER)
  const readOn = focus !== null && activity.loaded && focused === undefined && activity.more && oldest > focus.matchId

  if (focus !== null && activity.loaded && !readOn) {
    setFocus(null)

    if (focused !== undefined && (focus.createdAt === '' || sameSecond(focused.createdAt, focus.createdAt))) {
      setHighlight({ matchId: focused.id, seq: focus.seq })
    } else {
      setToast(noteOf(focus.seq, 'That is no longer in Autopilot’s history.'))
    }
  }

  const readEarlier = useEffectEvent(() => {
    if (!activity.loadingMore) {
      void activity.loadMore()
    }
  })

  useEffect(() => {
    if (readOn) {
      readEarlier()
    }
  }, [readOn, loadedMatches.length])

  const perRoutine = routineActivity(activity.days, new Date(today))
  const working = (routineId: number) => autopilot.glance?.working.filter((w) => w.routineId === routineId).length ?? 0
  const problem = (routineId: number) => engine?.problems.find((p) => p.routineId === routineId)?.detail ?? ''
  const statusOf = (r: AutopilotRoutine) => routineStatus(r, perRoutine.get(r.id), working(r.id), problem(r.id))
  const chosen = routines.find((r) => r.id === selected)
  const shownId = chosen === undefined ? 0 : chosen.id

  const actions: RoutineActions = {
    onSwitch: (r, on) => {
      changes.request(r, { kind: 'enable', on })
    },
    onDryRun: (r, on) => {
      changes.request(r, { kind: 'mode', mode: on ? 'dryRun' : 'ask' })
    },
    onDuplicate: (r) => {
      void attempt(duplicateRoutine(r.id)).then((result) => {
        if (result.ok) {
          reload()
          setSelected(result.data.id)
          say(`Made “${result.data.name}”, switched off.`)
        } else {
          say(result.error, 'error')
        }
      })
    },
    onDelete: (r) => {
      setDeleting(r)
    },
  }

  const removeChosen = () => {
    if (deleting === null) {
      return
    }

    const gone = deleting

    setDeleteBusy(true)
    void attempt(deleteRoutine(gone.id)).then((result) => {
      setDeleteBusy(false)
      setDeleting(null)

      if (result.ok) {
        if (selected === gone.id) {
          setSelected(0)
        }

        reload()
        say(`Deleted “${gone.name}”. Its history stays, under its name.`)
      } else {
        say(result.error, 'error')
      }
    })
  }

  // openItem shows a line's item: its card in the inbox while it is listed there, else its
  // row in the routine's Watching list.
  const openItem = (item: AutopilotItemRef, routineId: number) => {
    if (item.id > 0 && listed(item.id)) {
      onShowItem(item.id, item.url)

      return
    }

    toasts.current += 1
    setSelected(routineId)
    setWatchHighlight({ id: item.id, url: item.url, seq: toasts.current })
  }

  const lines = activityLines(activity.days, activity.leftAlone, filter)
  const routineLines =
    chosen === undefined ? [] : activityLines(activity.days, activity.leftAlone, { routine: chosen.id, result: 'all' })
  const empty =
    autopilot.loaded &&
    routines.length === 0 &&
    activity.loaded &&
    activity.days.length === 0 &&
    activity.leftAlone.length === 0
  const loadMore = (
    <LoadMore
      more={activity.more}
      loading={activity.loadingMore}
      onMore={() => {
        void activity.loadMore()
      }}
    />
  )
  const toastText = (text: string, tone?: 'error') => {
    say(text, tone)
  }

  if (empty) {
    return (
      <div className="flex h-full items-center justify-center p-8">
        <div className="flex max-w-lg flex-col items-center gap-3 text-center">
          <AutopilotIcon className="size-8 text-accent" />
          <h2 className="text-lg font-semibold">Autopilot</h2>
          {introText.map((sentence) => (
            <p key={sentence} className="text-sm text-muted">
              {sentence}
            </p>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-0">
      <nav
        aria-label="Autopilot"
        className="flex w-64 shrink-0 flex-col gap-1 overflow-y-auto border-r border-border p-3"
      >
        <p className={`${captionClass} px-2 pb-1`}>Autopilot</p>
        <button
          type="button"
          aria-current={selected === 0 || chosen === undefined ? 'page' : undefined}
          onClick={() => {
            setSelected(0)
          }}
          className="flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm font-medium text-muted transition-colors hover:bg-raised hover:text-text aria-[current=page]:bg-raised aria-[current=page]:text-text"
        >
          <AutopilotIcon className="size-3.5 text-accent" />
          Activity
        </button>
        <p className={`${captionClass} mt-3 border-t border-border px-2 pt-3 pb-1`}>Routines</p>
        {autopilot.error !== '' && <ErrorBanner>{autopilot.error}</ErrorBanner>}
        {routines.length === 0 ? (
          <p className="px-2 text-xs text-muted">No routines yet.</p>
        ) : (
          <RoutineList
            routines={routines}
            selected={shownId}
            onSelect={setSelected}
            status={statusOf}
            busy={changes.busy}
            actions={actions}
          />
        )}
        <div className="mt-auto border-t border-border pt-2">
          <button type="button" onClick={onSettings} className={`${quietButtonClass} w-full`}>
            <SlidersIcon className="size-3.5" />
            Autopilot settings
          </button>
        </div>
      </nav>
      <section
        aria-labelledby={chosen === undefined ? `${id}-title` : undefined}
        aria-label={chosen === undefined ? undefined : chosen.name}
        className="min-w-0 flex-1 overflow-y-auto"
      >
        <div className="mx-auto flex max-w-4xl flex-col gap-4 px-6 py-5">
          <div className="flex flex-wrap items-center gap-3">
            {chosen === undefined && (
              <h2 id={`${id}-title`} className="text-lg font-semibold">
                Activity
              </h2>
            )}
            <div className="ml-auto flex items-center gap-2">
              {settings !== null && <PauseButton settings={settings} onSaved={onSettingsSaved} />}
            </div>
          </div>
          <Banners autopilot={autopilot} settings={settings} onSettingsSaved={onSettingsSaved} />
          {activity.error !== '' && <ErrorBanner>{activity.error}</ErrorBanner>}
          {chosen === undefined ? (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <label htmlFor={`${id}-routine`} className="sr-only">
                  Routine
                </label>
                <select
                  id={`${id}-routine`}
                  value={filter.routine}
                  onChange={(e) => {
                    setFilter({ ...filter, routine: Number(e.target.value) })
                  }}
                  className={`${fieldClass} py-1`}
                >
                  <option value={0}>Every routine</option>
                  {routines.map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.name}
                    </option>
                  ))}
                </select>
                <label htmlFor={`${id}-result`} className="sr-only">
                  Result
                </label>
                <select
                  id={`${id}-result`}
                  value={filter.result}
                  onChange={(e) => {
                    setFilter({ ...filter, result: e.target.value as ActivityResult })
                  }}
                  className={`${fieldClass} py-1`}
                >
                  {resultOrder.map((result) => (
                    <option key={result} value={result}>
                      {resultLabels[result]}
                    </option>
                  ))}
                </select>
                {(filter.routine !== 0 || filter.result !== 'all') && (
                  <button
                    type="button"
                    onClick={() => {
                      setFilter(noFilter)
                    }}
                    className={quietButtonClass}
                  >
                    Clear the filter
                  </button>
                )}
              </div>
              {!activity.loaded ? (
                <p role="status" className="text-sm text-muted">
                  Reading Activity…
                </p>
              ) : (
                <ActivityList
                  lines={lines}
                  highlight={highlight}
                  onOpen={openItem}
                  onToast={toastText}
                  empty={
                    <p className="text-sm text-muted">
                      {filter === noFilter
                        ? 'Nothing yet. What your routines do shows here, newest first.'
                        : 'Nothing here matches the filter.'}
                    </p>
                  }
                />
              )}
              {loadMore}
            </>
          ) : (
            <RoutinePane
              routine={chosen}
              status={statusOf(chosen)}
              busy={changes.busy === chosen.id}
              actions={actions}
              onMode={(mode) => {
                changes.request(chosen, { kind: 'mode', mode })
              }}
              watching={<WatchingList routineId={chosen.id} kit={kit} highlight={watchHighlight} />}
              activity={
                <>
                  <ActivityList
                    lines={routineLines}
                    routineColumn={false}
                    onOpen={openItem}
                    onToast={toastText}
                    empty={<p className="text-sm text-muted">Nothing yet.</p>}
                  />
                  {loadMore}
                </>
              }
            />
          )}
        </div>
      </section>
      {changes.dialog}
      {deleting !== null && (
        <ConfirmDialog
          title={`Delete “${deleting.name}”?`}
          confirmLabel="Delete"
          busy={deleteBusy}
          onConfirm={removeChosen}
          onCancel={() => {
            setDeleting(null)
          }}
        >
          It stops, and leaves the list. What it did stays in Activity, under its name. Snoozes, pins, and dismissals it
          made stay as they are.
        </ConfirmDialog>
      )}
      <Toast
        toast={toast}
        seconds={toastSeconds}
        onClose={() => {
          setToast(null)
        }}
      />
    </div>
  )
}
