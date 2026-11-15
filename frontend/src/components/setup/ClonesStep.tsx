import { useEffect, useRef, useState, type CSSProperties } from 'react'

import { attempt } from '../../lib/call'
import { counted, formatCount } from '../../lib/format'
import { prefersReducedMotion } from '../../lib/motion'
import { rootNeedsAttention } from '../../lib/setup'
import type { Query } from '../../lib/useQuery'
import { useQuery } from '../../lib/useQuery'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  addCloneLocations,
  chooseFolder,
  createCloneRoot,
  setCloneRoot,
  suggestCloneRoots,
  type CloneCandidate,
  type LocationState,
  type Settings,
} from '../../platform'
import { buttonClass, primaryButtonClass, quietButtonClass } from '../controls'
import { AlertIcon, ChevronRightIcon, CircleCheckIcon, FolderIcon, PlusIcon } from '../icons'
import { CloneLocations, rootMarker } from '../settings/CloneLocations'
import { Spinner } from '../Spinner'

// countUpMs is how long the clone count takes to count up to the number found.
const countUpMs = 700

// countUpAt is where the count stands t of the way through, easing out as it lands.
function countUpAt(n: number, t: number): number {
  return Math.round(n * (1 - (1 - Math.min(Math.max(t, 0), 1)) ** 3))
}

// useCountUp counts up from 0 to n, frame by frame, the first time n is more than 0; with
// reduced motion, or without animation frames, it shows n at once. A later change of n
// shows at once.
function useCountUp(n: number): number {
  // progress is how far the count has got, 0 to 1, or null when it isn't counting.
  const [progress, setProgress] = useState<number | null>(() => (animates() ? 0 : null))
  const done = useRef(false)

  useEffect(() => {
    if (done.current || n === 0 || !animates()) {
      return undefined
    }

    let frame = 0
    let start: number | null = null
    const tick = (now: number) => {
      start ??= now

      const t = (now - start) / countUpMs

      if (t >= 1) {
        done.current = true
        setProgress(null)

        return
      }

      setProgress(t)
      frame = requestAnimationFrame(tick)
    }

    frame = requestAnimationFrame(tick)

    return () => {
      cancelAnimationFrame(frame)
    }
  }, [n])

  return progress === null ? n : countUpAt(n, progress)
}

// animates reports whether the count may count up: the system allows motion, and there
// are animation frames to count on.
function animates(): boolean {
  return typeof requestAnimationFrame === 'function' && !prefersReducedMotion()
}

// RootCard is the clone root as it reads now: how many clones sit directly inside it,
// counting up as it first shows, or what is wrong with it.
function RootCard({ path, root }: { path: string; root: LocationState | undefined }) {
  const clones = root?.state === 'ok' ? root.clones : 0
  const shown = useCountUp(clones)
  const marker = rootMarker(root)
  const missing = rootNeedsAttention(root)

  return (
    <div
      className={`flex items-center gap-4 rounded-lg border px-4 py-4 ${missing ? 'border-warning/60 bg-warning/5' : 'border-border bg-surface'}`}
    >
      <FolderIcon className={`size-6 shrink-0 ${missing ? 'text-warning' : 'text-accent'}`} />
      <div className="min-w-0 flex-1">
        <p className="text-xs font-medium tracking-wide text-muted uppercase">Clone root</p>
        <p className="truncate font-mono text-sm">{path}</p>
      </div>
      {root === undefined ? (
        <Spinner />
      ) : root.state === 'ok' ? (
        <p className="shrink-0 text-right">
          <span className="block text-2xl font-semibold tabular-nums" data-testid="clone-count">
            {formatCount(shown)}
          </span>
          <span className="text-xs text-muted">{clones === 1 ? 'clone' : 'clones'} directly inside</span>
        </p>
      ) : (
        <p className="flex shrink-0 items-center gap-1.5 text-sm font-medium text-warning">
          <AlertIcon className="size-4" />
          {marker.text}
        </p>
      )}
    </div>
  )
}

interface SuggestionsProps {
  candidates: readonly CloneCandidate[]
  busy: boolean
  onUse: (path: string) => void
  onAdd: (path: string) => void
}

// Suggestions lists the folders where clones were found, the most first: each can be the
// clone root, or be added beside it.
function Suggestions({ candidates, busy, onUse, onAdd }: SuggestionsProps) {
  return (
    <section aria-label="Folders with clones" className="flex flex-col gap-2">
      <p className="text-sm font-medium">Found clones in these folders:</p>
      <ul className="divide-y divide-border rounded-lg border border-border bg-surface">
        {candidates.map((c, i) => (
          <li
            key={c.path}
            className="nil-line flex items-center gap-3 px-4 py-3"
            style={{ '--i': String(i) } as CSSProperties}
          >
            <FolderIcon className="size-4 shrink-0 text-muted" />
            <p className="min-w-0 flex-1 truncate">
              <span className="font-mono text-sm">{c.path}</span>
              <span className="text-sm text-muted"> · {counted(c.clones, 'clone', 'clones')}</span>
            </p>
            <button
              type="button"
              disabled={busy}
              onClick={() => {
                onUse(c.path)
              }}
              className={i === 0 ? primaryButtonClass : buttonClass}
            >
              Use as clone root
            </button>
            <button
              type="button"
              disabled={busy}
              aria-label={`Add ${c.path} as a location`}
              title="Add as another repository location, beside the clone root"
              onClick={() => {
                onAdd(c.path)
              }}
              className={quietButtonClass}
            >
              <PlusIcon className="size-3.5" />
              Add as a location
            </button>
          </li>
        ))}
      </ul>
    </section>
  )
}

export interface ClonesStepProps {
  settings: Settings
  onSaved: (settings: Settings) => void
  // states is how the clone root and the other locations read now, which the setup shares
  // with Finish.
  states: Query<LocationState[]>
}

// ClonesStep finds the user's clones: the clone root as it reads now; when it is missing
// or holds no clones, the folders where clones commonly live that hold some, each to use as
// the clone root or add beside it, or, with none, a fresh clone root made for them; any
// other folder chosen; and, folded away, the other repository locations, as Settings shows
// them.
export function ClonesStep({ settings: stored, onSaved, states }: ClonesStepProps) {
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  const root = states.data?.find((s) => s.primary)
  const missing = rootNeedsAttention(root)
  const empty = root?.state === 'ok' && root.clones === 0
  const suggest = missing || empty
  const candidates = useQuery(
    () => suggestCloneRoots(),
    [settings.cloneRoot, JSON.stringify(settings.cloneLocations)],
    { enabled: suggest, log: 'could not look for folders with clones' },
  )
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')

  // run stores one change, then reads the locations again.
  const run = async (change: Partial<Settings>, call: () => PromiseLike<Settings>, done: string) => {
    setBusy(true)
    setNote('')

    const result = await save(change, call)

    setBusy(false)

    if (result.ok) {
      setNote(done)
      void states.refetch()
    }
  }

  const chooseRoot = (path: string) => {
    void run({ cloneRoot: path }, () => setCloneRoot(path), `${path} is your clone root.`)
  }

  const addLocation = (path: string) => {
    void run(
      { cloneLocations: [...settings.cloneLocations, { path, kind: 'folder' }] },
      () => addCloneLocations([path]),
      `Added ${path} beside the clone root.`,
    )
  }

  const startFresh = () => {
    void run({}, () => createCloneRoot(settings.cloneRoot), `Made ${settings.cloneRoot} for your clones.`)
  }

  const choose = async () => {
    const chosen = await attempt(chooseFolder({ title: 'Choose the clone root', button: 'Use' }))

    if (chosen.ok && chosen.data !== '') {
      chooseRoot(chosen.data)
    }
  }

  const found = candidates.data ?? []

  return (
    <div className="flex flex-col gap-5">
      <RootCard path={settings.cloneRoot} root={root} />

      {root?.state === 'ok' && root.clones > 0 && (
        <p className="flex items-center gap-1.5 text-sm text-success">
          <CircleCheckIcon className="size-4" />
          Found them. New clones go here too.
        </p>
      )}

      {suggest && candidates.data !== undefined && found.length > 0 && (
        <Suggestions candidates={found} busy={busy} onUse={chooseRoot} onAdd={addLocation} />
      )}

      {missing && candidates.data !== undefined && found.length === 0 && (
        <div className="flex flex-col items-start gap-3 rounded-lg border border-border bg-surface px-4 py-4">
          <p className="text-sm text-muted">
            No clones turned up in the usual places. Start fresh: new clones will go in {settings.cloneRoot}.
          </p>
          <button type="button" disabled={busy} onClick={startFresh} className={primaryButtonClass}>
            Start fresh in {settings.cloneRoot}
            <ChevronRightIcon className="size-3.5" />
          </button>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          disabled={busy}
          onClick={() => {
            void choose()
          }}
          className={buttonClass}
        >
          <FolderIcon className="size-3.5 text-muted" />
          Choose another folder…
        </button>
        {(busy || states.pending) && <Spinner />}
      </div>

      {note !== '' && (
        <p role="status" className="text-xs text-success">
          {note}
        </p>
      )}
      {error !== '' && (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}

      <details className="group rounded-lg border border-border bg-surface/60 px-4 py-3">
        <summary className="cursor-pointer text-sm font-medium select-none">Clones somewhere else too?</summary>
        <div className="mt-3">
          <CloneLocations settings={settings} save={save} states={states} />
        </div>
      </details>
    </div>
  )
}
