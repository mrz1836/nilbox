import { createContext, useEffect, useId, useState, type ReactNode } from 'react'

import { itemText } from '../../lib/autopilot'
import { attempt } from '../../lib/call'
import { askable, runnable } from '../../lib/routineBuilder'
import { formatAgo } from '../../lib/time'
import { useQuery } from '../../lib/useQuery'
import {
  explainRoutine,
  getAutopilotCatalog,
  runRoutineNow,
  type AutopilotCatalog,
  type AutopilotExplanation,
  type AutopilotRoutine,
  type InboxItem,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { fieldClass } from '../controls'
import { RelativeTime } from '../RelativeTime'
import { Toast, type ToastMessage } from '../Toast'
import { TraceList } from './Trace'

// AutopilotMenu is what an item's ⋯ menu offers of Autopilot: the routines it can run on
// the item now, and those it can ask why they didn't run, each read only as the menu opens;
// running one; and asking why. It never changes while the routines do, so a row renders
// again only as its menu opens.
export interface AutopilotMenu {
  // runnable lists the routines that are on, the user's own, that load and act on the
  // item's kind; askable those that may be off too.
  runnable: (item: InboxItem) => readonly AutopilotRoutine[]
  askable: (item: InboxItem) => readonly AutopilotRoutine[]
  onRun: (routine: AutopilotRoutine, item: InboxItem) => void
  onWhy: (item: InboxItem) => void
  // onMake opens the builder filled in from the item: Make a routine from this…
  onMake: (item: InboxItem) => void
}

// AutopilotMenuContext holds the item menus' Autopilot; without it a menu offers none.
export const AutopilotMenuContext = createContext<AutopilotMenu | null>(null)

// Held is what the menu reads as it opens: the routines and the catalog as last read.
interface Held {
  routines: readonly AutopilotRoutine[]
  catalog: AutopilotCatalog | undefined
  // make is the window's Make a routine from this…, as last rendered.
  make: (item: InboxItem) => void
}

// Anyway is a routine the item didn't match, which the user may run on it anyway: why it
// doesn't match, in the backend's words and its decision's.
interface Anyway {
  routine: AutopilotRoutine
  item: InboxItem
  message: string
  explanation: AutopilotExplanation
}

// Why is the item whose routines the user asks about, and the routine chosen.
interface Why {
  item: InboxItem
  routineId: number
}

// Shows are the window's own setters a menu's choices end in: a toast, the question Run
// anyway asks, and Why's dialog.
interface Shows {
  toast: (toast: ToastMessage) => void
  anyway: (anyway: Anyway) => void
  why: (why: Why) => void
}

// startedWords say what became of a routine run from the menu: the match's own words.
function startedWords(routine: AutopilotRoutine, words: string): Omit<ToastMessage, 'id'> {
  return { text: words === '' ? `Started “${routine.name}”.` : words, detail: routine.name }
}

// MenuSource is the item menus' Autopilot, made once for the window: it lists, as a menu
// opens, what the routines and the catalog last read hold, and ends each choice in one of
// the window's setters or a call to the backend, so it never changes while they do.
class MenuSource implements AutopilotMenu {
  private held: Held = { routines: [], catalog: undefined, make: () => undefined }
  private toasts = 0
  private readonly shows: Shows

  constructor(shows: Shows) {
    this.shows = shows
  }

  // hold keeps the routines and the catalog as last read, for the menus that open next.
  hold(next: Held) {
    this.held = next
  }

  // say shows a toast, numbered so the same words said again show again.
  say(message: Omit<ToastMessage, 'id'>) {
    this.toasts += 1
    this.shows.toast({ id: this.toasts, ...message })
  }

  // listed is what pick lists for the item's kind, none until the catalog is read.
  private listed(pick: typeof runnable, item: InboxItem): AutopilotRoutine[] {
    const { catalog, routines } = this.held

    return catalog === undefined ? [] : pick(catalog, routines, item.kind)
  }

  runnable = (item: InboxItem): AutopilotRoutine[] => this.listed(runnable, item)

  askable = (item: InboxItem): AutopilotRoutine[] => this.listed(askable, item)

  // onRun runs the routine on the item now, and says what became of it. On an item it
  // doesn't match, which its decision now says, it asks whether to run it anyway; any
  // other refusal is told as it is.
  onRun = (routine: AutopilotRoutine, item: InboxItem) => {
    void attempt(runRoutineNow(routine.id, item.id, false)).then(async (ran) => {
      if (ran.ok) {
        this.say(startedWords(routine, ran.data.words))

        return
      }

      const explained = await attempt(explainRoutine(routine.id, item.id))

      if (explained.ok && explained.data.match !== 'yes') {
        this.shows.anyway({ routine, item, message: ran.error, explanation: explained.data })
      } else {
        this.say({ text: `Couldn’t run “${routine.name}”`, detail: ran.error, tone: 'error' })
      }
    })
  }

  // onWhy asks about the item, with the first routine that acts on its kind chosen.
  onWhy = (item: InboxItem) => {
    this.shows.why({ item, routineId: this.askable(item)[0]?.id ?? 0 })
  }

  // onMake opens the Autopilot tab's builder filled in from the item.
  onMake = (item: InboxItem) => {
    this.held.make(item)
  }
}

// resultWords say what a routine decides about an item now.
const resultWords: Readonly<Record<string, string>> = {
  yes: 'It matches now.',
  no: 'It doesn’t match now.',
  unknown: 'It can’t tell yet: it waits to read more.',
}

// ExplanationView is what a routine decides about an item now: what of the routine itself
// holds it back (switched off, Pause, Dry run, this Mac), whether the item is in what it
// watches and why not, each condition with what it came to and its value, what its history
// says, and every record the ledger keeps of the two, newest first.
function ExplanationView({ explanation: e, name }: { explanation: AutopilotExplanation; name: string }) {
  return (
    <div className="flex flex-col gap-2">
      <p className="text-sm text-text">{resultWords[e.match] ?? e.match}</p>
      {e.routine.length > 0 && (
        <ul aria-label="What holds the routine back" className="flex flex-col gap-0.5">
          {e.routine.map((line) => (
            <li key={line} className="text-xs text-warning">
              {line}
            </li>
          ))}
        </ul>
      )}
      {!e.inWatch &&
        (e.watch.length === 0 ? (
          <p className="text-xs text-warning">The item isn’t in what it watches.</p>
        ) : (
          <ul aria-label="Why it isn’t in what it watches" className="flex flex-col gap-0.5">
            {e.watch.map((line) => (
              <li key={line} className="text-xs text-warning">
                {line}
              </li>
            ))}
          </ul>
        ))}
      {e.leftAlone && (
        <p className="text-xs text-warning">
          It leaves the item alone, since you took back what it did: Let it act again in Activity.
        </p>
      )}
      <TraceList label={`Why, for ${name}`} trace={e.trace} />
      {e.ledger.length > 0 && (
        <ul aria-label="What its history says" className="flex flex-col gap-0.5 border-t border-border pt-2">
          {e.ledger.map((line) => (
            <li key={line} className="text-xs text-muted">
              {line}
            </li>
          ))}
        </ul>
      )}
      {e.records.length > 0 && (
        <details className="border-t border-border pt-2">
          <summary className="cursor-pointer text-xs font-medium text-muted">
            {`Its ledger for this item: ${String(e.records.length)} ${e.records.length === 1 ? 'record' : 'records'}`}
          </summary>
          <ul aria-label="Its ledger for this item" className="mt-1 flex flex-col gap-0.5">
            {e.records.map((m) => (
              <li key={m.id} className="flex gap-2 text-xs text-muted">
                <span className="shrink-0 font-mono">
                  <RelativeTime iso={m.createdAt} show={formatAgo} />
                </span>
                <span className="min-w-0 flex-1">{`${m.dryRun ? 'Dry run · ' : ''}${m.words}`}</span>
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  )
}

interface WhyDialogProps {
  why: Why
  routines: readonly AutopilotRoutine[]
  onPick: (routineId: number) => void
  onClose: () => void
}

// WhyDialog answers Why didn't a routine run?: the routine picked, among those that act on
// the item's kind, and what it decides about the item now, with what its history says.
function WhyDialog({ why, routines, onPick, onClose }: WhyDialogProps) {
  const id = useId()
  const name = itemText(why.item)
  const explained = useQuery(() => explainRoutine(why.routineId, why.item.id), [why.routineId, why.item.id], {
    enabled: why.routineId > 0,
  })

  return (
    <ConfirmDialog
      title="Why didn’t a routine run?"
      confirmLabel="Done"
      tone="primary"
      cancelLabel={null}
      size="wide"
      onConfirm={onClose}
      onCancel={onClose}
    >
      <div className="flex flex-col gap-3">
        <p>
          What a routine decides about {name} now, from what is stored, and what its history says. Nothing is asked of
          GitHub.
        </p>
        {routines.length === 0 ? (
          <p>No routine of yours acts on {why.item.kind === 'pr' ? 'pull requests' : 'issues'}.</p>
        ) : (
          <>
            <div className="flex items-center gap-2">
              <label htmlFor={`${id}-routine`} className="text-xs font-medium text-muted">
                Routine
              </label>
              <select
                id={`${id}-routine`}
                value={why.routineId}
                onChange={(e) => {
                  onPick(Number(e.target.value))
                }}
                className={`${fieldClass} min-w-0 flex-1 py-1`}
              >
                {routines.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.enabled ? r.name : `${r.name} (off)`}
                  </option>
                ))}
              </select>
            </div>
            {explained.error !== '' ? (
              <p role="alert" className="text-xs text-danger">
                {explained.error}
              </p>
            ) : explained.data === undefined || explained.pending ? (
              <p className="text-xs">Reading what it decides…</p>
            ) : (
              <ExplanationView explanation={explained.data} name={name} />
            )}
          </>
        )}
      </div>
    </ConfirmDialog>
  )
}

// useRoutineMenu is the item menus' Autopilot, which the window provides around every list
// of items (AutopilotMenuContext), and what its choices show: Run a routine runs it at
// once and says what became of it, and on an item it doesn't match offers Run anyway,
// showing why it doesn't; Why didn't a routine run? opens what a routine decides about the
// item. The menu reads the routines, and the catalog that says what each acts on, as it
// opens, so it stays the same while they change.
export function useRoutineMenu(
  routines: readonly AutopilotRoutine[],
  toastSeconds: number,
  onMake: (item: InboxItem) => void,
): { menu: AutopilotMenu; node: ReactNode } {
  const catalog = useQuery(() => getAutopilotCatalog(), [], {
    enabled: routines.length > 0,
    log: 'could not read Autopilot’s catalog',
  })
  const [toast, setToast] = useState<ToastMessage | null>(null)
  const [anyway, setAnyway] = useState<Anyway | null>(null)
  const [running, setRunning] = useState(false)
  const [why, setWhy] = useState<Why | null>(null)
  const [menu] = useState(() => new MenuSource({ toast: setToast, anyway: setAnyway, why: setWhy }))

  useEffect(() => {
    menu.hold({ routines, catalog: catalog.data, make: onMake })
  }, [menu, routines, catalog.data, onMake])

  // runAnyway runs the routine the user chose to run on an item it doesn't match.
  const runAnyway = async (a: Anyway) => {
    setRunning(true)

    const ran = await attempt(runRoutineNow(a.routine.id, a.item.id, true))

    setRunning(false)
    setAnyway(null)
    menu.say(
      ran.ok
        ? startedWords(a.routine, ran.data.words)
        : { text: `Couldn’t run “${a.routine.name}”`, detail: ran.error, tone: 'error' },
    )
  }

  const node = (
    <>
      {anyway !== null && (
        <ConfirmDialog
          title={`Run “${anyway.routine.name}” anyway?`}
          confirmLabel="Run anyway"
          tone="primary"
          size="wide"
          busy={running}
          onConfirm={() => {
            void runAnyway(anyway)
          }}
          onCancel={() => {
            setAnyway(null)
          }}
        >
          <div className="flex flex-col gap-2">
            <p className="text-text">{anyway.message}</p>
            <TraceList label={`Why, for ${itemText(anyway.item)}`} trace={anyway.explanation.trace} />
            <p>
              Run anyway runs it once on {itemText(anyway.item)}, in its mode, whatever its conditions say. Every limit
              still holds it, as for any match.
            </p>
          </div>
        </ConfirmDialog>
      )}
      {why !== null && (
        <WhyDialog
          why={why}
          routines={catalog.data === undefined ? [] : askable(catalog.data, routines, why.item.kind)}
          onPick={(routineId) => {
            setWhy({ ...why, routineId })
          }}
          onClose={() => {
            setWhy(null)
          }}
        />
      )}
      <Toast
        toast={toast}
        seconds={toastSeconds}
        onClose={() => {
          setToast(null)
        }}
      />
    </>
  )

  return { menu, node }
}
