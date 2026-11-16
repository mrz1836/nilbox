import { useEffect, useEffectEvent, useId, useRef, useState } from 'react'

import {
  activityLines,
  elsewhereText,
  filters,
  inActivity,
  notListedText,
  pausedText,
  resultLabels,
  resultOrder,
  routineStatus,
  sameSecond,
  signedOutText,
  type ActivityFilter,
  type ActivityResult,
  type AutopilotRequest,
} from '../../lib/autopilot'
import { attempt } from '../../lib/call'
import { dryRunWhy, NEW_ROUTINE, newRoutineName, routineDraft, sameDraft, scratchDraft } from '../../lib/routineBuilder'
import type { AutopilotState } from '../../lib/useAutopilot'
import { useAutopilotActivity } from '../../lib/useAutopilotActivity'
import { useQuery } from '../../lib/useQuery'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  deleteRoutine,
  duplicateRoutine,
  getAutopilotCatalog,
  itemsByID,
  openExternal,
  routineWatching,
  saveRoutine,
  setAutopilotPaused,
  suggestForRoutines,
  type AgentInfo,
  type AutopilotDraft,
  type AutopilotItemRef,
  type AutopilotRoutine,
  type AutopilotSuggestions,
  type InboxItem,
  type Profile,
  type Prompt,
  type Settings,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { buttonClass, captionClass, fieldClass, primaryButtonClass, quietButtonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { AutopilotIcon, PauseIcon, PlayIcon, PlusIcon, PulseIcon, RoutineIcon, SlidersIcon } from '../icons'
import { defaultToastSeconds, Toast, type ToastMessage } from '../Toast'
import { ActivityList } from './ActivityList'
import type { AgentKit } from './builder/AgentStepBlock'
import { RoutineBuilder } from './builder/RoutineBuilder'
import { RoutineList, type RoutineActions } from './RoutineList'
import { RoutinePane } from './RoutinePane'
import { useRoutineChange } from './useRoutineChange'
import { useSheetHost, type RowsKit } from './useSheetHost'
import { WatchingList } from './WatchingList'

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
  // viewer is the account signed in, '' for none: "your account" in a search.
  viewer: string
  // prompts, agents, and profiles are the launch configuration the agent step picks from,
  // and onEditPrompts opens the Prompts page.
  prompts: readonly Prompt[]
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  onEditPrompts: () => void
  toastSeconds?: number
}

// Editing is the routine the builder holds: its key (a routine's ID, or NEW_ROUTINE), as it
// was opened or last saved, and as edited.
interface Editing {
  key: number
  baseline: AutopilotDraft
  draft: AutopilotDraft
}

// noSuggestions are the pickers' suggestions until they load.
const noSuggestions: AutopilotSuggestions = { authors: [], bots: [], owners: [], labels: [], checks: [] }

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

// editable reports a routine the builder may change: one of the user's whose spec reads,
// whether or not it still validates.
function editable(r: AutopilotRoutine): boolean {
  return !r.otherAccount && r.brokenReason !== 'newer' && r.brokenReason !== 'unreadable'
}

// AutopilotView is the Autopilot tab: the routines on the left, each with its switch and
// its status, under Activity and above + New routine; and on the right Activity (what the
// routines did, by day, newest first, with the banners for what stops everything and its
// filters), or the routine chosen (its mode, the builder, which is the routine itself edited
// in place, with Test below it, what it watches, and its own Activity), or a new routine's
// builder. Pause Autopilot sits in the header. While there is no routine, the tab explains
// Autopilot and offers Start from scratch. A routine's unsaved changes survive switching
// tabs, and leaving them for another routine or Activity asks first.
export default function AutopilotView({
  autopilot,
  settings,
  onSettingsSaved,
  request,
  kit,
  listed,
  onShowItem,
  onSettings,
  viewer,
  prompts,
  agents,
  profiles,
  onEditPrompts,
  toastSeconds = defaultToastSeconds,
}: AutopilotViewProps) {
  const id = useId()
  const activity = useAutopilotActivity()
  const [selected, setSelected] = useState(0)
  const [filter, setFilter] = useState<ActivityFilter>(noFilter)
  const [toast, setToast] = useState<ToastMessage | null>(null)
  const toasts = useRef(0)
  const [deleting, setDeleting] = useState<AutopilotRoutine | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)
  const [focus, setFocus] = useState<Focus | null>(null)
  const [highlight, setHighlight] = useState<{ matchId: number; seq: number } | null>(null)
  // watchHighlight is the row Activity's Open asks a routine's Watching list to show, once:
  // the list hands it back as it shows it.
  const [watchHighlight, setWatchHighlight] = useState<{ routineId: number; id: number; seq: number } | null>(null)
  // lone is an item Open shows in its sheet when no list here holds it.
  const [lone, setLone] = useState<InboxItem | null>(null)
  const loneHost = useSheetHost({
    kit,
    items: lone === null ? [] : [lone],
    listTitle: 'Autopilot',
    onClose: () => {
      setLone(null)
    },
  })
  const [handled, setHandled] = useState(0)
  const { routines, engine, reload } = autopilot
  // editing is the routine the builder holds, null on Activity; leaving is where the user
  // asked to go while it has unsaved changes, which asks first.
  const [editing, setEditing] = useState<Editing | null>(null)
  const [leaving, setLeaving] = useState<number | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  // justSaved is the routine a save made, shown until the list read again holds it, and
  // synced the routine as the builder last took it from the list.
  const [justSaved, setJustSaved] = useState<AutopilotRoutine | null>(null)
  const [synced, setSynced] = useState<AutopilotRoutine | null>(null)
  const catalog = useQuery(() => getAutopilotCatalog(), [], { log: 'could not read Autopilot’s catalog' })
  const suggestions = useQuery(() => suggestForRoutines(), [], { log: 'could not read what the items suggest' })
  const agentKit: AgentKit = {
    prompts,
    agents,
    profiles,
    defaultAgent: settings?.defaultAgent ?? 'claude',
    defaultProfiles: settings?.defaultProfiles ?? {},
    onEditPrompts,
  }

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

  // dirty reports a draft with changes that aren't saved.
  const dirty = editing !== null && !sameDraft(editing.draft, editing.baseline)

  // open shows next: Activity (0), a routine, or a new routine's builder, which starts
  // from scratch in the mode new routines start in. A routine's builder is filled in as it
  // shows, below.
  const open = (next: number) => {
    setSelected(next)
    setSaveError('')

    if (next === NEW_ROUTINE) {
      const d = scratchDraft(newRoutineName(routines), settings?.autopilot.defaultMode ?? 'ask')

      setEditing({ key: NEW_ROUTINE, baseline: d, draft: d })
    } else if (editing !== null && editing.key !== next) {
      setEditing(null)
    }
  }

  // go shows next, asking first when the draft has unsaved changes that next would leave.
  const go = (next: number) => {
    if (editing !== null && dirty && editing.key !== next) {
      setLeaving(next)
    } else {
      open(next)
    }
  }

  // A request from a notification, the menu bar, or a chip shows Activity, at the match
  // it names, or the routine it names, once the routines are read.
  if (request !== null && request.seq !== handled && (request.target.matchId > 0 || autopilot.loaded)) {
    setHandled(request.seq)

    const { target } = request

    if (target.matchId > 0) {
      go(0)
      setFilter(noFilter)
      setFocus({ matchId: target.matchId, createdAt: target.createdAt, seq: request.seq })
    } else if (target.routineId > 0 && !target.activity) {
      const routine = routines.find((r) => r.id === target.routineId)

      if (routine !== undefined && (target.createdAt === '' || sameSecond(routine.createdAt, target.createdAt))) {
        go(routine.id)
      } else {
        go(0)
        setToast(noteOf(request.seq, 'That routine is no longer here.'))
      }
    } else {
      go(0)
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

    if (focused === undefined || (focus.createdAt !== '' && !sameSecond(focused.createdAt, focus.createdAt))) {
      setToast(noteOf(focus.seq, 'That is no longer in Autopilot’s history.'))
    } else if (inActivity(focused)) {
      setHighlight({ matchId: focused.id, seq: focus.seq })
    } else {
      setToast(noteOf(focus.seq, notListedText(focused)))
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

  const working = (routineId: number) => autopilot.glance?.working.filter((w) => w.routineId === routineId).length ?? 0
  const problem = (routineId: number) => engine?.problems.find((p) => p.routineId === routineId)?.detail ?? ''
  const statusOf = (r: AutopilotRoutine) => routineStatus(r, working(r.id), problem(r.id))
  const chosen = routines.find((r) => r.id === selected) ?? (justSaved?.id === selected ? justSaved : undefined)
  const shownId = chosen === undefined ? 0 : chosen.id
  // The routine shown fills the builder, and each time the list reads it again, the
  // builder takes it as saved: its draft too, while it has no unsaved changes.
  if (chosen !== undefined && editable(chosen) && (chosen !== synced || editing?.key !== chosen.id)) {
    setSynced(chosen)

    if (editing === null || editing.key !== chosen.id || !dirty) {
      setEditing({ key: chosen.id, baseline: routineDraft(chosen), draft: routineDraft(chosen) })
    } else {
      setEditing({ ...editing, baseline: routineDraft(chosen) })
    }
  }

  // saveEditing saves the draft: a new routine, off, in the mode it was given, or the
  // routine shown, asking first, as its switch does, when it is on and what it watches,
  // fires on, or checks changed. The builder then holds the routine as saved, and edits
  // typed while it saved are kept.
  const saveEditing = () => {
    if (editing === null || saving || changes.busy !== 0) {
      return
    }

    const submitted = editing.draft
    const draft: AutopilotDraft = { ...submitted, name: submitted.name.trim() }
    const made = editing.key === NEW_ROUTINE
    const done = (saved: AutopilotRoutine) => {
      setJustSaved(saved)
      setSynced(saved)
      setSelected(saved.id)
      setEditing((e) =>
        e === null
          ? e
          : {
              key: saved.id,
              baseline: routineDraft(saved),
              draft: sameDraft(e.draft, submitted) ? routineDraft(saved) : { ...e.draft, id: saved.id },
            },
      )
      say(
        made ? `Made “${saved.name}”, switched off. Switch it on once you’re happy with it.` : `Saved “${saved.name}”.`,
      )
    }

    setSaveError('')

    if (!made) {
      if (chosen !== undefined) {
        changes.request(chosen, { kind: 'save', draft }, done)
      }

      return
    }

    setSaving(true)
    void attempt(saveRoutine(draft)).then((result) => {
      setSaving(false)

      if (result.ok) {
        reload()
        done(result.data)
      } else {
        setSaveError(result.error)
      }
    })
  }

  // discardEditing puts the draft back as it was saved, or leaves a new one.
  const discardEditing = () => {
    if (editing === null) {
      return
    }

    if (editing.key === NEW_ROUTINE) {
      setEditing(null)
      setSelected(0)
    } else {
      setEditing({ ...editing, draft: editing.baseline })
    }

    setSaveError('')
  }

  // builderFor is the builder of the routine the draft is of, once the catalog is read.
  const builderFor = (r: AutopilotRoutine | null) => {
    const key = r === null ? NEW_ROUTINE : r.id

    if (editing === null || editing.key !== key || catalog.data === undefined) {
      return undefined
    }

    return (
      <RoutineBuilder
        draft={editing.draft}
        baseline={editing.baseline}
        isNew={r === null}
        sentence={r?.sentence ?? ''}
        mode={r?.mode ?? editing.draft.mode ?? 'ask'}
        heldKinds={r?.dryRunOnly ?? []}
        catalog={catalog.data}
        suggestions={suggestions.data ?? noSuggestions}
        viewer={viewer}
        agentKit={agentKit}
        saving={r === null ? saving : changes.busy === r.id}
        saveError={saveError}
        onChange={(draft) => {
          setEditing({ ...editing, draft })
          setSaveError('')
        }}
        onSave={saveEditing}
        onDiscard={discardEditing}
      />
    )
  }

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
          go(result.data.id)
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

        // Its draft goes with it.
        if (editing?.key === gone.id) {
          setEditing(null)
        }

        reload()
        say(`Deleted “${gone.name}”. Its history stays, under its name.`)
      } else {
        say(result.error, 'error')
      }
    })
  }

  // openItem shows a line's item, whatever became of its routine: its card in the inbox
  // while it is listed there; else its row in a routine that still watches it, the line's
  // own first; else its sheet, while the item is stored; else its page on GitHub. Each
  // follows the click alone.
  const openItem = async (item: AutopilotItemRef, routineId: number) => {
    if (item.id > 0 && listed(item.id)) {
      onShowItem(item.id, item.url)

      return
    }

    if (item.id > 0) {
      const order = [routineId, ...routines.map((r) => r.id).filter((rid) => rid !== routineId)].filter((rid) =>
        routines.some((r) => r.id === rid),
      )

      for (const rid of order) {
        const watched = await attempt(routineWatching(rid))

        if (watched.ok && watched.data.includes(item.id)) {
          toasts.current += 1
          go(rid)
          setWatchHighlight({ routineId: rid, id: item.id, seq: toasts.current })

          return
        }
      }

      const stored = await attempt(itemsByID([item.id]))
      const found = stored.ok ? stored.data[0] : undefined

      if (found !== undefined) {
        setLone(found)
        loneHost.open(found)

        return
      }
    }

    if (item.url !== '') {
      openExternal(item.url)
    }
  }
  const onOpen = (item: AutopilotItemRef, routineId: number) => {
    void openItem(item, routineId)
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

  const making = selected === NEW_ROUTINE && editing?.key === NEW_ROUTINE

  if (empty && !making && chosen === undefined) {
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
          <button
            type="button"
            onClick={() => {
              open(NEW_ROUTINE)
            }}
            className={`${primaryButtonClass} mt-2`}
          >
            <PlusIcon className="size-4" />
            Start from scratch
          </button>
        </div>
      </div>
    )
  }

  // shown is what the right side shows: a new routine's builder, a routine, or Activity.
  const shown = making ? 'new' : chosen === undefined ? 'activity' : 'routine'

  return (
    <div className="flex h-full min-h-0">
      <nav
        aria-label="Autopilot"
        className="flex w-64 shrink-0 flex-col gap-1 overflow-y-auto border-r border-border p-3"
      >
        <p className={`${captionClass} px-2 pb-1`}>Autopilot</p>
        <button
          type="button"
          aria-current={shown === 'activity' ? 'page' : undefined}
          onClick={() => {
            go(0)
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
            selected={shown === 'routine' ? shownId : 0}
            onSelect={go}
            status={statusOf}
            busy={changes.busy}
            actions={actions}
          />
        )}
        <button
          type="button"
          aria-current={making ? 'page' : undefined}
          onClick={() => {
            go(NEW_ROUTINE)
          }}
          className="mt-1 flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-muted transition-colors hover:bg-raised hover:text-text aria-[current=page]:bg-raised aria-[current=page]:text-text"
        >
          <PlusIcon className="size-3.5" />
          New routine
        </button>
        <div className="mt-auto border-t border-border pt-2">
          <button type="button" onClick={onSettings} className={`${quietButtonClass} w-full`}>
            <SlidersIcon className="size-3.5" />
            Autopilot settings
          </button>
        </div>
      </nav>
      <section
        aria-labelledby={shown === 'routine' ? undefined : `${id}-title`}
        aria-label={shown === 'routine' ? chosen?.name : undefined}
        className="min-w-0 flex-1 overflow-y-auto"
      >
        <div className="mx-auto flex max-w-4xl flex-col gap-4 px-6 py-5">
          <div className="flex flex-wrap items-center gap-3">
            {shown === 'activity' && (
              <h2 id={`${id}-title`} className="text-lg font-semibold">
                Activity
              </h2>
            )}
            {shown === 'new' && (
              <h2 id={`${id}-title`} className="flex items-center gap-3 text-lg font-semibold">
                <RoutineIcon className="size-5 shrink-0 text-muted" />
                New routine
              </h2>
            )}
            <div className="ml-auto flex items-center gap-2">
              {settings !== null && <PauseButton settings={settings} onSaved={onSettingsSaved} />}
            </div>
          </div>
          <Banners autopilot={autopilot} settings={settings} onSettingsSaved={onSettingsSaved} />
          {activity.error !== '' && <ErrorBanner>{activity.error}</ErrorBanner>}
          {shown === 'new' ? (
            (builderFor(null) ?? (
              <p role="status" className="text-sm text-muted">
                Reading what a routine can do…
              </p>
            ))
          ) : chosen === undefined ? (
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
                {filters(filter) && (
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
                  onOpen={onOpen}
                  onToast={toastText}
                  empty={
                    <p className="text-sm text-muted">
                      {!filters(filter)
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
              builder={editable(chosen) ? builderFor(chosen) : undefined}
              heldText={catalog.data === undefined ? undefined : dryRunWhy(catalog.data, chosen.dryRunOnly)}
              watching={
                <WatchingList
                  key={chosen.id}
                  routineId={chosen.id}
                  kit={kit}
                  highlight={watchHighlight?.routineId === chosen.id ? watchHighlight : null}
                  onHighlighted={() => {
                    setWatchHighlight(null)
                  }}
                />
              }
              activity={
                <>
                  <ActivityList
                    lines={routineLines}
                    routineColumn={false}
                    onOpen={onOpen}
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
      {loneHost.node}
      {leaving !== null && editing !== null && (
        <ConfirmDialog
          title="Discard your changes?"
          confirmLabel="Discard changes"
          cancelLabel="Keep editing"
          onConfirm={() => {
            const next = leaving

            setLeaving(null)
            setEditing(null)
            open(next)
          }}
          onCancel={() => {
            setLeaving(null)
          }}
        >
          {editing.key === NEW_ROUTINE
            ? `“${editing.draft.name.trim() === '' ? 'The new routine' : editing.draft.name}” hasn’t been created yet.`
            : `“${editing.baseline.name}” has changes that aren’t saved.`}
        </ConfirmDialog>
      )}
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
