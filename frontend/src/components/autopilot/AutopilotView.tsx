import { Activity, useEffect, useEffectEvent, useId, useRef, useState } from 'react'

import {
  activityLines,
  describeAttach,
  describeExamples,
  describeSeesText,
  editable,
  elsewhereText,
  filters,
  inActivity,
  notListedText,
  notReadyBanner,
  notReadyText,
  pausedText,
  pendingText,
  readyOf,
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
import { formatCount } from '../../lib/format'
import { agentName } from '../../lib/launch'
import { dryRunWhy, NEW_ROUTINE, newRoutineName, routineDraft, sameDraft, scratchDraft } from '../../lib/routineBuilder'
import { useNeedsYou, useReadiness, type SharedAutopilot } from '../../lib/useAutopilot'
import { useAutopilotActivity } from '../../lib/useAutopilotActivity'
import { useMutation } from '../../lib/useMutation'
import { useOn } from '../../lib/useOn'
import { useQuery } from '../../lib/useQuery'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  declineGraduate,
  deleteRoutine,
  describeItStatus,
  duplicateRoutine,
  getAutopilotCatalog,
  itemsByID,
  listTemplates,
  openExternal,
  routineFromItem,
  routineWatching,
  saveRoutine,
  setAutopilotPaused,
  suggestForRoutines,
  type AgentInfo,
  type AutopilotClause,
  type AutopilotDraft,
  type AutopilotItemRef,
  type AutopilotMatch,
  type AutopilotRoutine,
  type AutopilotSuggestions,
  type AutopilotTemplate,
  type DescribeStatus,
  type Profile,
  type Prompt,
  type Settings,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { buttonClass, captionClass, fieldClass, primaryButtonClass, quietButtonClass, Select } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import {
  AlertIcon,
  AutopilotIcon,
  ExportIcon,
  ImportIcon,
  PauseIcon,
  PlayIcon,
  PlusIcon,
  PulseIcon,
  RoutineIcon,
  SlidersIcon,
  SparkIcon,
  StarIcon,
} from '../icons'
import { defaultToastSeconds, Toast, type ToastMessage } from '../Toast'
import { ActivityList } from './ActivityList'
import { NeedsYouSection } from './NeedsYouSection'
import type { AgentKit } from './builder/AgentStepBlock'
import { RoutineBuilder } from './builder/RoutineBuilder'
import type { Pinned } from './builder/TestPanel'
import { DescribeView, type DescribeRequest } from './describe/DescribeView'
import { QuickSetup } from './QuickSetup'
import { RoutineList, type RoutineActions } from './RoutineList'
import { RoutinePane } from './RoutinePane'
import { useRoutineTransfer } from './RoutineTransfer'
import { TemplateGallery } from './TemplateGallery'
import { useRoutineChange } from './useRoutineChange'
import { useSheetHost, type RowsKit } from './useSheetHost'
import { WatchingList } from './WatchingList'
import { RunView } from './RunView'
import { WorkingList } from './WorkingList'

export interface AutopilotViewProps {
  // autopilot is Autopilot as the window shares it: at a glance, the routines, the engine.
  autopilot: SharedAutopilot
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
  // onShowInbox shows the inbox, whose items' ⋯ menus make a routine from an item.
  onShowInbox?: () => void
  toastSeconds?: number
}

// Editing is the routine the builder holds: its key (a routine's ID, or NEW_ROUTINE), as it
// was opened or last saved, and as edited; for a new routine made from an item, the
// conditions the item suggests and the item itself, which Test shows at the top.
interface Editing {
  key: number
  baseline: AutopilotDraft
  draft: AutopilotDraft
  suggested?: AutopilotClause[]
  pinned?: Pinned
}

// TEMPLATES is the gallery's place in the tab, beside Activity (0), a routine, and a new
// routine (NEW_ROUTINE); DESCRIBE is Describe it's.
const TEMPLATES = -2
const DESCRIBE = -3

// DescribeWith is what opens Describe it besides showing it: a message it sends at once,
// or what it attaches to the next one (a DescribeRequest, before the tab numbers it).
type DescribeWith = Omit<DescribeRequest, 'seq'>

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
  autopilot: SharedAutopilot
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

// NotReadyBanner says how many of the user's routines this Mac can't run, with the way to
// what is missing, in Settings → Autopilot.
function NotReadyBanner({ count, onSettings }: { count: number; onSettings: () => void }) {
  return (
    <div
      role="status"
      className="flex items-center gap-3 rounded-md border border-danger/45 bg-danger/10 px-3 py-2 text-sm"
    >
      <AlertIcon className="size-4 shrink-0 text-danger" />
      <span className="min-w-0 flex-1">{notReadyBanner(count)}</span>
      <button type="button" onClick={onSettings} className={buttonClass}>
        See what’s missing
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

// CountBadge is the count beside a place in the nav, the words it stands for left to
// assistive tech.
function CountBadge({ count, words }: { count: number; words: string }) {
  return (
    <>
      {' '}
      <span
        aria-hidden="true"
        className="rounded-full bg-accent/14 px-1.5 text-caption leading-[1.125rem] font-medium text-text tabular-nums"
      >
        {formatCount(count)}
      </span>
      <span className="sr-only">({words})</span>
    </>
  )
}

interface AskBoxProps {
  // status is whether Describe it can run a turn now, undefined until it is read, and
  // error why it couldn't be read; checking reports it read again, as Check again asks.
  status: DescribeStatus | undefined
  error: string
  checking: boolean
  onCheck: () => void
  // onSend opens Describe it with the message, which it sends at once; onOpen opens it
  // without one.
  onSend: (text: string) => void
  onOpen: () => void
}

// AskBox is the empty tab's way into Describe it: the user tells Autopilot what they want,
// an example a click away filling in the box, and sending (⌘↩) opens Describe it, which
// sends it at once. While Describe it can't run a turn, it is held back with why and what
// to do, and Check again reads that again once the user has done it. Open Describe it goes
// there without a message, to a conversation under way or to the proposals from the user's
// own Claude Code, whose count it shows.
function AskBox({ status, error, checking, onCheck, onSend, onOpen }: AskBoxProps) {
  const id = useId()
  const box = useRef<HTMLTextAreaElement>(null)
  const [text, setText] = useState('')
  const held = error !== '' || status?.ready === false
  // A status that couldn't be read says only why; one read says why it can't run, and
  // what to do.
  const problem = error === '' ? (status?.problem ?? '') : error
  const fix = error === '' ? (status?.fix ?? '') : ''
  const pending = status?.pending ?? 0

  const send = () => {
    const message = text.trim()

    if (message !== '' && !held) {
      onSend(message)
    }
  }

  return (
    <form
      aria-labelledby={`${id}-title`}
      onSubmit={(e) => {
        e.preventDefault()
        send()
      }}
      className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4"
    >
      <div className="flex items-center gap-2">
        <SparkIcon className="size-4 shrink-0 text-accent" />
        <label id={`${id}-title`} htmlFor={`${id}-text`} className="min-w-0 flex-1 text-sm font-semibold">
          Tell Autopilot what you want
        </label>
        <button type="button" onClick={onOpen} className={`${quietButtonClass} shrink-0`}>
          Open Describe it
          {pending > 0 && <CountBadge count={pending} words={pendingText(pending)} />}
        </button>
      </div>
      <textarea
        ref={box}
        id={`${id}-text`}
        rows={3}
        value={text}
        disabled={held}
        placeholder="What should Autopilot do, and when?"
        aria-describedby={held ? `${id}-held` : `${id}-sees`}
        onChange={(e) => {
          setText(e.target.value)
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && e.metaKey) {
            e.preventDefault()
            send()
          }
        }}
        className={`${fieldClass} w-full resize-y`}
      />
      <div className="flex flex-wrap items-start gap-3">
        <ul aria-label="Examples" className="flex min-w-0 flex-1 flex-wrap gap-1.5">
          {describeExamples.map((example) => (
            <li key={example}>
              <button
                type="button"
                disabled={held}
                onClick={() => {
                  setText(example)
                  box.current?.focus()
                }}
                className="rounded-full border border-border px-2.5 py-0.5 text-left text-xs text-muted transition-colors hover:bg-raised hover:text-text disabled:cursor-default disabled:opacity-50 disabled:hover:bg-transparent"
              >
                {example}
              </button>
            </li>
          ))}
        </ul>
        <button
          type="submit"
          disabled={held || text.trim() === ''}
          title="Opens Describe it, where Claude Code answers (⌘↩)"
          className={`${primaryButtonClass} shrink-0`}
        >
          Send
        </button>
      </div>
      {held ? (
        <div className="flex items-start gap-2 rounded-md border border-warning/60 bg-warning/10 px-3 py-2 text-xs">
          <AlertIcon className="mt-0.5 size-3.5 shrink-0 text-warning" />
          <p id={`${id}-held`} role="status" className="min-w-0 flex-1">
            <span className="text-text">{problem}</span>
            {fix !== '' && (
              <>
                {' '}
                <span className="text-muted">{fix}</span>
              </>
            )}
          </p>
          <button type="button" disabled={checking} onClick={onCheck} className={`${buttonClass} shrink-0`}>
            {checking ? 'Checking…' : 'Check again'}
          </button>
        </div>
      ) : (
        <p id={`${id}-sees`} className="text-xs text-muted">
          {describeSeesText}
        </p>
      )}
    </form>
  )
}

// AutopilotView is the Autopilot tab: the routines on the left, each with its switch and
// its status, under Activity and above Describe it and + New routine; and on the right
// Activity (what the routines did, by day, newest first, with the banners for what stops
// everything and its filters), or the routine chosen (its mode, the builder, which is the
// routine itself edited in place, with Test below it, what it watches, and its own
// Activity), or a new routine's builder, or Describe it, the conversation with Claude
// Code. Pause Autopilot sits in the header. While there is no routine, the tab explains
// Autopilot, asks what the user wants, and offers Start from scratch. A routine's unsaved
// changes survive switching tabs, and leaving them for another routine, Activity, or
// Describe it asks first.
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
  onShowInbox,
  toastSeconds = defaultToastSeconds,
}: AutopilotViewProps) {
  const id = useId()
  const activity = useAutopilotActivity()
  const needs = useNeedsYou()
  const ready = useReadiness()
  const [selected, setSelected] = useState(0)
  const [filter, setFilter] = useState<ActivityFilter>(noFilter)
  const [toast, setToast] = useState<ToastMessage | null>(null)
  const toasts = useRef(0)
  const [deleting, setDeleting] = useState<AutopilotRoutine | null>(null)
  const remover = useMutation((routineId: number) => deleteRoutine(routineId))
  const [focus, setFocus] = useState<Focus | null>(null)
  const [highlight, setHighlight] = useState<{ matchId: number; seq: number } | null>(null)
  // runShown is the agent's run open in the run view beside Activity, 0 for none.
  const [runShown, setRunShown] = useState(0)
  // needsHighlight is the card of Needs you a request asked to show, for a match still
  // open there.
  const [needsHighlight, setNeedsHighlight] = useState<{ matchId: number; seq: number } | null>(null)
  // watchHighlight is the row Activity's Open asks a routine's Watching list to show, once:
  // the list hands it back as it shows it, or once it knows it can't, and it goes as soon
  // as another routine or Activity is chosen.
  const [watchHighlight, setWatchHighlight] = useState<{ routineId: number; id: number; seq: number } | null>(null)
  // loneId is the item Open shows in its sheet when no list here holds it, read again
  // whenever the stored inbox changes, as its own triage in the sheet does, so the sheet
  // offers what fits the item as it is now.
  const [loneId, setLoneId] = useState(0)
  const loneRead = useQuery(() => itemsByID([loneId]), [loneId], { enabled: loneId > 0 })
  const lone = loneId > 0 ? loneRead.data?.find((i) => i.id === loneId) : undefined
  const loneHost = useSheetHost({
    kit,
    items: lone === undefined ? [] : [lone],
    listTitle: 'Autopilot',
    onClose: () => {
      setLoneId(0)
    },
  })
  // The stored inbox changed, the sheet's own triage included, which the backend says
  // once it is stored: the item is read again once, as it is now.
  useOn('inbox:updated', () => {
    if (loneId > 0) {
      void loneRead.refetch()
    }
  })
  const [handled, setHandled] = useState(0)
  const { routines, engine, reload } = autopilot
  // editing is the routine the builder holds, null on Activity; leaving is where the user
  // asked to go while it has unsaved changes, which asks first, and leavingWith what
  // Describe it opens with when that is where.
  const [editing, setEditing] = useState<Editing | null>(null)
  const [leaving, setLeaving] = useState<number | null>(null)
  const [leavingWith, setLeavingWith] = useState<DescribeWith | undefined>(undefined)
  // describeRequest is what Describe it was last opened with, numbered by describeSeq: kept
  // while it shows, and dropped as soon as it doesn't, so a view made again never acts on
  // it twice. describeVisited keeps the view, hidden, once it has shown, so a message being
  // written survives a look elsewhere in the tab.
  const [describeRequest, setDescribeRequest] = useState<DescribeRequest | undefined>(undefined)
  const [describeSeq, setDescribeSeq] = useState(0)
  const [describeVisited, setDescribeVisited] = useState(false)
  // describeStatus says whether Describe it can run a turn, which the empty tab's box
  // needs, and how many proposals from the user's own Claude Code wait, Describe it's count:
  // read as the tab shows, and again as those proposals change.
  const describeStatus = useQuery(() => describeItStatus(), [])

  useOn('describeit:proposals', () => {
    void describeStatus.refetch()
  })
  const pending = describeStatus.data?.pending ?? 0
  // saver saves a new routine, and saving reports it under way. saveError is why the last
  // save failed, shown in the builder's footer: a new routine's from saver, a change to the
  // routine shown from changeError, which its change sets. clearSaveError clears both,
  // whatever its closure saw: some callers run it once an await is over, after a save
  // that failed meanwhile.
  const saver = useMutation((draft: AutopilotDraft) => saveRoutine(draft))
  const saving = saver.pending
  const [changeError, setChangeError] = useState('')
  const saveError = changeError !== '' ? changeError : saver.error
  const clearSaveError = () => {
    setChangeError('')
    saver.reset()
  }
  // justSaved is the routine a save made, shown until the list read again holds it, and
  // synced the routine as the builder last took it from the list.
  const [justSaved, setJustSaved] = useState<AutopilotRoutine | null>(null)
  const [synced, setSynced] = useState<AutopilotRoutine | null>(null)
  const catalog = useQuery(() => getAutopilotCatalog(), [], { log: 'could not read Autopilot’s catalog' })
  const suggestions = useQuery(() => suggestForRoutines(), [], { log: 'could not read what the items suggest' })
  // templates are the gallery as this Mac sees it, and quick the template whose quick setup
  // shows, by its key, as the gallery last read it; fromItemHint says how to start from an
  // item, once asked.
  const templates = useQuery(() => listTemplates(), [], { log: 'could not read the templates' })
  const [quickKey, setQuickKey] = useState('')
  const quick = quickKey === '' ? null : (templates.data?.find((t) => t.key === quickKey) ?? null)
  const [fromItemHint, setFromItemHint] = useState(false)
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

  // transfer is Export and Import, whose dialogs show over the tab.
  const transfer = useRoutineTransfer(say, () => {
    reload()
    void templates.refetch()
  })

  // noteOf is a toast a request's answer raises as the tab renders, numbered by the
  // request, below every toast the tab raises from a click.
  const noteOf = (seq: number, text: string): ToastMessage => ({ id: -seq, text, tone: 'warning' })

  // dirty reports a draft with changes that aren't saved.
  const dirty = editing !== null && !sameDraft(editing.draft, editing.baseline)

  // open shows next: Activity (0), a routine, or a new routine's builder, which starts
  // from scratch in the mode new routines start in, or keeps the new routine being made.
  // A routine's builder is filled in as it shows, below.
  const open = (next: number) => {
    setSelected(next)
    setQuickKey('')

    if (next === NEW_ROUTINE && editing?.key === NEW_ROUTINE) {
      return
    }

    clearSaveError()

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

  // newFrom opens a new routine's builder over draft, as a template's Customize or an item's
  // Make a routine from this… fills it in: leaving it asks first, as any new routine's
  // changes do.
  const newFrom = (draft: AutopilotDraft, extra: Pick<Editing, 'suggested' | 'pinned'> = {}) => {
    const scratch = scratchDraft(newRoutineName(routines), settings?.autopilot.defaultMode ?? 'ask')

    setSelected(NEW_ROUTINE)
    setQuickKey('')
    clearSaveError()
    setEditing({ key: NEW_ROUTINE, baseline: scratch, draft, ...extra })
  }

  // showTemplate opens a template's quick setup.
  const showTemplate = (t: AutopilotTemplate) => {
    if (editing !== null && dirty) {
      setLeaving(TEMPLATES)

      return
    }

    open(TEMPLATES)
    setQuickKey(t.key)
  }

  // showDescribe shows Describe it, and hands it ask, a message to send or what to attach,
  // numbered as a request of its own, when there is one.
  const showDescribe = (ask?: DescribeWith) => {
    open(DESCRIBE)

    if (ask !== undefined) {
      setDescribeSeq(describeSeq + 1)
      setDescribeRequest({ ...ask, seq: describeSeq + 1 })
    }
  }

  // describe opens Describe it with ask, asking first, as leaving for anywhere else does,
  // while the builder has unsaved changes; ask waits for the answer.
  const describe = (ask?: DescribeWith) => {
    if (editing !== null && dirty) {
      setLeaving(DESCRIBE)
      setLeavingWith(ask)

      return
    }

    showDescribe(ask)
  }

  // Make a routine from this… reads what the item suggests, once per request, and opens the
  // builder with it; while the builder holds unsaved changes, it says so instead.
  const makeFrom = useEffectEvent((itemId: number) => {
    if (editing !== null && dirty) {
      say('Save or discard the routine you’re editing first, then make one from the item.', 'error')

      return
    }

    void attempt(routineFromItem(itemId)).then((made) => {
      if (made.ok) {
        newFrom(made.data.draft, {
          suggested: made.data.suggestions,
          pinned: { item: made.data.item, match: made.data.match, trace: made.data.trace },
        })
      } else {
        say(made.error, 'error')
      }
    })
  })

  // made is the last request made from an item, kept while the tab hides, whose effects
  // run again as it shows.
  const made = useRef(0)

  useEffect(() => {
    if (request !== null && (request.fromItem ?? 0) > 0 && made.current !== request.seq) {
      made.current = request.seq
      makeFrom(request.fromItem ?? 0)
    }
  }, [request])

  // A request for a template's quick setup shows it once the gallery is read.
  if (request?.template !== undefined && request.seq !== handled && templates.data !== undefined) {
    setHandled(request.seq)

    const t = templates.data.find((x) => x.key === request.template)

    open(TEMPLATES)

    if (t !== undefined) {
      setQuickKey(t.key)
    }
  }

  // A request for Describe it, as an item's Describe a routine for this… makes, opens it
  // with what it attaches, asking first while the builder has unsaved changes.
  if (request?.describe !== undefined && request.seq !== handled) {
    setHandled(request.seq)
    describe({ attach: describeAttach(request.describe) })
  }

  // A request from a notification, the menu bar, or a chip shows Activity, at the match
  // it names, or the routine it names, once the routines are read; one that names an
  // agent's run opens its run view beside Activity.
  if (
    request !== null &&
    request.seq !== handled &&
    request.template === undefined &&
    request.describe === undefined &&
    (request.fromItem ?? 0) === 0 &&
    (request.target.matchId > 0 || request.target.runId > 0 || autopilot.loaded)
  ) {
    setHandled(request.seq)

    const { target } = request

    if (target.runId > 0) {
      // An agent's run, as Agent finished, a session's Watch, or the menu bar's agent
      // names: its run view opens beside Activity.
      go(0)
      setFilter(noFilter)
      setRunShown(target.runId)
    } else if (target.matchId > 0) {
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

  // A match asked for shows once the pages that hold it are read: the pages as they are
  // now, not as the tab held them before it hid (a match recorded meanwhile isn't in
  // them), then earlier ones until one does; one no longer in the history, or made at
  // another time (Clear history hands an ID out again), says so.
  //
  // A match still open, waiting for the user, this Mac, or a limit, shows as its card in
  // Needs you instead, once that is read.
  const waitingOne =
    focus === null
      ? undefined
      : needs.matches.find(
          (m) => m.id === focus.matchId && (focus.createdAt === '' || sameSecond(m.createdAt, focus.createdAt)),
        )

  if (focus !== null && waitingOne !== undefined) {
    setFocus(null)
    setNeedsHighlight({ matchId: waitingOne.id, seq: focus.seq })
  }

  const loadedMatches = activity.days.flatMap((d) => d.matches)
  const focused = focus === null ? undefined : loadedMatches.find((m) => m.id === focus.matchId)
  const oldest = loadedMatches.reduce((low, m) => Math.min(low, m.id), Number.MAX_SAFE_INTEGER)
  const current = activity.loaded && activity.current && needs.settled
  const readOn = focus !== null && current && focused === undefined && activity.more && oldest > focus.matchId

  if (focus !== null && waitingOne === undefined && current && !readOn) {
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
  const statusOf = (r: AutopilotRoutine) =>
    routineStatus(r, working(r.id), problem(r.id), notReadyText(readyOf(ready.readiness, r.id)))
  // agentFor names the agent a match's routine runs: its agent step's, or the default one.
  const agentFor = (m: AutopilotMatch) => {
    const step = routines.find((r) => r.id === m.routineId)?.spec.steps.find((st) => st.agent != null)?.agent
    const chosen = step?.agent ?? ''

    if (chosen === '' || chosen === 'inherit') {
      return agentName(agents, settings?.defaultAgent ?? 'claude')
    }

    return agents.find((a) => a.id === chosen)?.name ?? chosen
  }
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
  // fires on, checks, or fires once per changed. The builder then holds the routine as
  // saved, and edits typed while it saved are kept. Either one's failure shows in the
  // builder's footer. Nothing is saved again while a save, a change, or the question it
  // asked is under way.
  const saveEditing = () => {
    if (editing === null || saving || changes.busy !== 0 || changes.asking) {
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

    clearSaveError()

    if (!made) {
      if (chosen !== undefined) {
        changes.request(chosen, { kind: 'save', draft }, done, setChangeError)
      }

      return
    }

    void saver.run(draft).then((result) => {
      if (result.ok) {
        reload()
        done(result.data)
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

    clearSaveError()
  }

  // builderFor is the builder of the routine the draft is of, once the catalog is read.
  const builderFor = (r: AutopilotRoutine | null) => {
    const key = r === null ? NEW_ROUTINE : r.id

    if (editing === null || editing.key !== key || catalog.data === undefined) {
      return undefined
    }

    return (
      <RoutineBuilder
        // Each routine's builder is its own: one's test, sentence, and errors never show
        // under another.
        key={key}
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
          clearSaveError()
        }}
        onSave={saveEditing}
        onDiscard={discardEditing}
        {...(r === null && editing.suggested !== undefined ? { suggested: editing.suggested } : {})}
        pinned={r === null ? editing.pinned : undefined}
      />
    )
  }

  // An ask for a Watching row is for the routine Open chose; choosing another lets it go,
  // and so does staying where the tab was, once the question about unsaved changes that
  // going there asked is answered Keep editing. While that question is open, it waits.
  if (watchHighlight !== null && watchHighlight.routineId !== shownId && leaving !== watchHighlight.routineId) {
    setWatchHighlight(null)
  }

  const actions: RoutineActions = {
    onSwitch: (r, on) => {
      changes.request(r, { kind: 'enable', on })
    },
    onDryRun: (r, on) => {
      changes.request(r, { kind: 'mode', mode: on ? 'dryRun' : 'ask' })
    },
    onDescribe: (r) => {
      describe({ attach: describeAttach({ routineId: r.id }) })
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
    onExport: (r) => {
      transfer.exportRoutines([r])
    },
    onDelete: (r) => {
      setDeleting(r)
    },
  }

  // notNow answers Not now to putting a routine on Autopilot: it offers again after five
  // more clean approvals, and its mode stays as it is.
  const notNow = (r: AutopilotRoutine) => {
    void attempt(declineGraduate(r.id)).then((result) => {
      if (result.ok) {
        reload()
      } else {
        say(result.error, 'error')
      }
    })
  }

  // madeFromTemplate is a routine quick setup just saved: the list reads it again, and the
  // tab shows it.
  const madeFromTemplate = (saved: AutopilotRoutine) => {
    reload()
    setQuickKey('')
    setJustSaved(saved)
    setSelected(saved.id)
    say(`Made “${saved.name}”, switched off. Switch it on once you’re happy with it.`)
  }

  // quickSetup is the template's quick setup, once the catalog is read.
  const quickSetup = (t: AutopilotTemplate) =>
    catalog.data === undefined ? (
      <p role="status" className="text-sm text-muted">
        Reading what a routine can do…
      </p>
    ) : (
      <QuickSetup
        key={t.key}
        template={t}
        routines={routines}
        defaultMode={settings?.autopilot.defaultMode ?? 'ask'}
        catalog={catalog.data}
        suggestions={suggestions.data ?? noSuggestions}
        viewer={viewer}
        onSaved={madeFromTemplate}
        onCustomize={(draft) => {
          newFrom(draft)
        }}
        onCancel={() => {
          setQuickKey('')
        }}
        onPromptsChanged={() => {
          void templates.refetch()
        }}
      />
    )

  // gallery is every template, the suggested first; picking one opens its quick setup.
  const gallery = (limit?: number) =>
    templates.data === undefined ? (
      <p role="status" className="text-sm text-muted">
        {templates.error === '' ? 'Reading the templates…' : templates.error}
      </p>
    ) : (
      <TemplateGallery templates={templates.data} onPick={showTemplate} {...(limit === undefined ? {} : { limit })} />
    )

  // fromItem says how to start from an item: its ⋯ menu, in the inbox or a Watching list.
  const fromItem = fromItemHint && (
    <div
      role="status"
      className="flex flex-wrap items-center gap-3 rounded-md border border-border bg-surface px-3 py-2 text-sm"
    >
      <span className="min-w-0 flex-1">
        Open the ⋯ menu on any item, in the inbox or a routine’s Watching list, and choose Autopilot ▸ Make a routine
        from this…
      </span>
      {onShowInbox !== undefined && (
        <button type="button" onClick={onShowInbox} className={buttonClass}>
          Show the inbox
        </button>
      )}
    </div>
  )

  const removeChosen = () => {
    if (deleting === null) {
      return
    }

    const gone = deleting

    void remover.run(gone.id).then((result) => {
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

      // Every routine is asked at once, from the local database alone; the first in order
      // that watches it wins, and one that couldn't be read is passed over.
      const lists = await Promise.all(order.map((rid) => attempt(routineWatching(rid))))
      const rid = order[lists.findIndex((watched) => watched.ok && watched.data.includes(item.id))]

      if (rid !== undefined) {
        toasts.current += 1
        go(rid)
        setWatchHighlight({ routineId: rid, id: item.id, seq: toasts.current })

        return
      }

      const stored = await attempt(itemsByID([item.id]))
      const found = stored.ok ? stored.data[0] : undefined

      if (found !== undefined) {
        setLoneId(found.id)
        loneRead.set(() => [found])
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
  const describing = selected === DESCRIBE

  // What Describe it was opened with goes as soon as it doesn't show, and its view, once
  // shown, is kept while the tab shows anything else.
  if (!describing && describeRequest !== undefined) {
    setDescribeRequest(undefined)
  }

  if (describing && !describeVisited) {
    setDescribeVisited(true)
  }

  // The empty tab explains Autopilot in two sentences, asks what the user wants, which opens
  // Describe it, then offers the templates that fit what this Mac knows (the first few),
  // Start from scratch, Start from an item, Import, and every template.
  if (empty && !making && !describing && chosen === undefined && selected !== TEMPLATES) {
    return (
      // The column keeps to the top (items-start) so it is as tall as what it holds: stretched
      // to the box's height, its content overflowed it, and neither the box's padding nor the
      // column's own came after the last row, which sat flush against the window's edge.
      <div className="flex h-full items-start justify-center overflow-y-auto p-8">
        <div className="flex w-full max-w-3xl flex-col gap-6 pb-12">
          {/* The intro keeps to a reading width, its lines balanced, so a sentence never
              leaves a word or two alone on its last line. */}
          <div className="flex flex-col items-center gap-3 text-center">
            <AutopilotIcon className="size-8 text-accent" />
            <h2 className="text-lg font-semibold">Autopilot</h2>
            {introText.map((sentence) => (
              <p key={sentence} className="max-w-2xl text-sm leading-relaxed text-balance text-muted">
                {sentence}
              </p>
            ))}
          </div>
          <AskBox
            status={describeStatus.data}
            error={describeStatus.error}
            checking={describeStatus.pending}
            onCheck={() => {
              void describeStatus.refetch()
            }}
            onSend={(text) => {
              describe({ text })
            }}
            onOpen={() => {
              describe()
            }}
          />
          {gallery(4)}
          <div className="flex flex-wrap items-center justify-center gap-2">
            <button
              type="button"
              onClick={() => {
                open(NEW_ROUTINE)
              }}
              className={primaryButtonClass}
            >
              <PlusIcon className="size-4" />
              Start from scratch
            </button>
            <button
              type="button"
              onClick={() => {
                setFromItemHint(true)
              }}
              className={buttonClass}
            >
              Start from an item
            </button>
            <button
              type="button"
              onClick={() => {
                open(TEMPLATES)
              }}
              className={buttonClass}
            >
              <StarIcon className="size-3.5 text-muted" />
              Every template
            </button>
            <button type="button" onClick={transfer.importRoutines} className={quietButtonClass}>
              <ImportIcon className="size-3.5" />
              Import routines…
            </button>
          </div>
          {fromItem}
        </div>
        {transfer.node}
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

  // shown is what the right side shows: a new routine's builder, the templates, Describe
  // it, a routine, or Activity; watching reports the run view open beside Activity.
  const shown = making
    ? 'new'
    : selected === TEMPLATES
      ? 'templates'
      : describing
        ? 'describe'
        : chosen === undefined
          ? 'activity'
          : 'routine'
  const watching = runShown > 0 && shown === 'activity'
  // named reports a right side named by its own heading: not a routine's, which its name
  // labels, nor Describe it's, whose view is a region of its own.
  const named = shown !== 'routine' && shown !== 'describe'

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
          aria-current={describing ? 'page' : undefined}
          title={
            pending > 0 ? pendingText(pending) : 'Tell Claude Code what you want, and review the routines it proposes'
          }
          onClick={() => {
            describe()
          }}
          className="mt-1 flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-muted transition-colors hover:bg-raised hover:text-text aria-[current=page]:bg-raised aria-[current=page]:text-text"
        >
          <SparkIcon className="size-3.5 text-accent" />
          <span className="min-w-0 flex-1 truncate">Describe it</span>
          {pending > 0 && <CountBadge count={pending} words={pendingText(pending)} />}
        </button>
        <button
          type="button"
          aria-current={making ? 'page' : undefined}
          onClick={() => {
            go(NEW_ROUTINE)
          }}
          className="flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-muted transition-colors hover:bg-raised hover:text-text aria-[current=page]:bg-raised aria-[current=page]:text-text"
        >
          <PlusIcon className="size-3.5" />
          New routine
        </button>
        <button
          type="button"
          aria-current={shown === 'templates' ? 'page' : undefined}
          onClick={() => {
            go(TEMPLATES)
          }}
          className="flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-muted transition-colors hover:bg-raised hover:text-text aria-[current=page]:bg-raised aria-[current=page]:text-text"
        >
          <StarIcon className="size-3.5" />
          Templates
        </button>
        <div className="mt-auto flex flex-col gap-1 border-t border-border pt-2">
          <button type="button" onClick={transfer.importRoutines} className={`${quietButtonClass} w-full`}>
            <ImportIcon className="size-3.5" />
            Import routines…
          </button>
          <button
            type="button"
            disabled={routines.length === 0}
            onClick={() => {
              transfer.exportRoutines(routines)
            }}
            className={`${quietButtonClass} w-full`}
          >
            <ExportIcon className="size-3.5" />
            Export every routine…
          </button>
          <button type="button" onClick={onSettings} className={`${quietButtonClass} w-full`}>
            <SlidersIcon className="size-3.5" />
            Autopilot settings
          </button>
        </div>
      </nav>
      <section
        aria-labelledby={named ? `${id}-title` : undefined}
        aria-label={shown === 'routine' ? chosen?.name : undefined}
        className={`min-w-0 overflow-y-auto ${watching ? 'w-[30rem] shrink-0 border-r border-border' : 'flex-1'}`}
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
            {shown === 'templates' && quick === null && (
              <h2 id={`${id}-title`} className="flex items-center gap-3 text-lg font-semibold">
                <StarIcon className="size-5 shrink-0 text-muted" />
                Templates
              </h2>
            )}
            {shown === 'templates' && quick !== null && (
              <span id={`${id}-title`} className="sr-only">
                {`Set up ${quick.name}`}
              </span>
            )}
            <div className="ml-auto flex items-center gap-2">
              {settings !== null && <PauseButton settings={settings} onSaved={onSettingsSaved} />}
            </div>
          </div>
          <Banners autopilot={autopilot} settings={settings} onSettingsSaved={onSettingsSaved} />
          {shown === 'activity' && (ready.readiness?.notReady ?? 0) > 0 && (
            <NotReadyBanner count={ready.readiness?.notReady ?? 0} onSettings={onSettings} />
          )}
          {activity.error !== '' && <ErrorBanner>{activity.error}</ErrorBanner>}
          {/* Describe it's view stays once it has shown, hidden while the tab shows anything
              else, so what is being written in it survives a look at a routine. */}
          {describeVisited && (
            <Activity mode={describing ? 'visible' : 'hidden'}>
              <DescribeView
                {...(describeRequest === undefined ? {} : { request: describeRequest })}
                settings={settings}
                catalog={catalog.data}
                onEditInBuilder={(draft) => {
                  newFrom(draft)
                }}
                onShowRoutine={(routineId) => {
                  if (!routines.some((r) => r.id === routineId)) {
                    reload()
                  }

                  go(routineId)
                }}
                onSettingsSaved={onSettingsSaved}
              />
            </Activity>
          )}
          {shown === 'describe' ? null : shown === 'new' ? (
            (builderFor(null) ?? (
              <p role="status" className="text-sm text-muted">
                Reading what a routine can do…
              </p>
            ))
          ) : shown === 'templates' ? (
            quick === null ? (
              <>
                <p className="text-sm text-muted">
                  Each is a routine made for you to start from: set up its few blanks, try it on your items, and save
                  it. Customize opens the whole builder with it filled in.
                </p>
                {gallery()}
                <div className="flex flex-wrap items-center gap-2">
                  <button
                    type="button"
                    onClick={() => {
                      setFromItemHint(true)
                    }}
                    className={buttonClass}
                  >
                    Start from an item
                  </button>
                </div>
                {fromItem}
              </>
            ) : (
              quickSetup(quick)
            )
          ) : chosen === undefined ? (
            <>
              <WorkingList onOpen={onOpen} onWatch={setRunShown} watching={runShown} onToast={toastText} />
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
                <Select<ActivityResult>
                  id={`${id}-result`}
                  className={`${fieldClass} py-1`}
                  value={filter.result}
                  options={resultOrder.map((r) => ({ value: r, label: resultLabels[r] }))}
                  onChange={(result) => {
                    setFilter({ ...filter, result })
                  }}
                />
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
              <NeedsYouSection
                matches={needs.matches}
                routine={filter.routine}
                error={needs.error}
                readiness={ready}
                highlight={needsHighlight}
                agentFor={agentFor}
                onOpen={onOpen}
                onToast={toastText}
                onChanged={() => {
                  needs.reload()
                  reload()
                }}
              />
              {!activity.loaded ? (
                <p role="status" className="text-sm text-muted">
                  Reading Activity…
                </p>
              ) : (
                <ActivityList
                  lines={lines}
                  highlight={highlight}
                  onOpen={onOpen}
                  onShowRun={setRunShown}
                  onToast={toastText}
                  onChanged={activity.reload}
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
              key={chosen.id}
              routine={chosen}
              status={statusOf(chosen)}
              busy={changes.busy === chosen.id}
              actions={actions}
              onMode={(mode) => {
                changes.request(chosen, { kind: 'mode', mode })
              }}
              onGraduate={() => {
                changes.request(chosen, { kind: 'mode', mode: 'auto' })
              }}
              onNotNow={() => {
                notNow(chosen)
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
                    onChanged={activity.reload}
                    empty={<p className="text-sm text-muted">Nothing yet.</p>}
                  />
                  {loadMore}
                </>
              }
            />
          )}
        </div>
      </section>
      {watching && (
        <div className="min-w-0 flex-1">
          <RunView
            key={runShown}
            runId={runShown}
            terminal={settings?.terminal ?? null}
            agents={agents}
            onOpenItem={onOpen}
            onToast={toastText}
            onClose={() => {
              setRunShown(0)
            }}
          />
        </div>
      )}
      {changes.dialog}
      {transfer.node}
      {loneHost.node}
      {leaving !== null && editing !== null && (
        <ConfirmDialog
          title="Discard your changes?"
          confirmLabel="Discard changes"
          cancelLabel="Keep editing"
          onConfirm={() => {
            const next = leaving

            setLeaving(null)
            setLeavingWith(undefined)
            setEditing(null)

            if (next === DESCRIBE) {
              showDescribe(leavingWith)
            } else {
              open(next)
            }
          }}
          onCancel={() => {
            setLeaving(null)
            setLeavingWith(undefined)
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
          busy={remover.pending}
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
