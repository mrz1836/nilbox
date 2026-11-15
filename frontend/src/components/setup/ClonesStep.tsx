import { useEffect, useRef, useState, type CSSProperties } from 'react'

import { brand } from '../../generated/brand'
import { attempt } from '../../lib/call'
import { counted, formatCount } from '../../lib/format'
import { prefersReducedMotion } from '../../lib/motion'
import { rootNeedsAttention } from '../../lib/setup'
import { useMutation } from '../../lib/useMutation'
import type { Query } from '../../lib/useQuery'
import { useQuery } from '../../lib/useQuery'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  addCloneLocations,
  chooseFolder,
  createCloneRoot,
  previewCloneLocation,
  setCloneRoot,
  suggestCloneRoots,
  type CloneCandidate,
  type LocationPreview,
  type LocationState,
  type Settings,
} from '../../platform'
import { buttonClass, primaryButtonClass, quietButtonClass } from '../controls'
import { AlertIcon, ChevronRightIcon, CircleCheckIcon, FolderIcon, PlusIcon, SearchIcon } from '../icons'
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
  // asRoot offers each folder as the clone root too, since the one set is missing or holds
  // no clones; otherwise each is offered only beside it.
  asRoot: boolean
  busy: boolean
  onUse: (path: string) => void
  onAdd: (path: string) => void
}

// Suggestions lists the folders where clones were found, the most first: each can be added
// beside the clone root, or, when that has none, be the clone root.
function Suggestions({ candidates, asRoot, busy, onUse, onAdd }: SuggestionsProps) {
  return (
    <section aria-label="Folders with clones" className="flex flex-col gap-2">
      <p className="text-sm font-medium">
        {asRoot ? 'Found clones in these folders:' : 'Found clones in other folders too:'}
      </p>
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
            {asRoot && (
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
            )}
            <button
              type="button"
              disabled={busy}
              aria-label={`Add ${c.path} as a location`}
              title="Add as another repository location, beside the clone root"
              onClick={() => {
                onAdd(c.path)
              }}
              className={asRoot ? quietButtonClass : buttonClass}
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

// documentsFolder is where GitHub Desktop clones by default. It is in Documents, which macOS
// guards: reading anything there makes macOS ask the user first, even to find that it
// isn't there. So the step looks in it only when the user asks, never on its own.
const documentsFolder = '~/Documents/GitHub'

// DocumentsLook is what the look in documentsFolder found: the folders with clones to
// offer, or else a note that says why there are none.
interface DocumentsLook {
  found: CloneCandidate[]
  note: string
}

// documentsLookOf reads the preview of documentsFolder. It offers the folder itself when
// clones sit directly inside it, or else the folders inside it that hold some. With none,
// or a folder the locations hold already, it says so.
function documentsLookOf(preview: LocationPreview): DocumentsLook {
  if (preview.covered !== '') {
    return { found: [], note: preview.covered }
  }

  if (preview.clones > 0) {
    return { found: [{ path: preview.path, clones: preview.clones }], note: '' }
  }

  if (preview.inside.length > 0) {
    return { found: preview.inside, note: '' }
  }

  return { found: [], note: `No clones in ${preview.path}.` }
}

export interface ClonesStepProps {
  settings: Settings
  onSaved: (settings: Settings) => void
  // states is how the clone root and the other locations read now, which the setup shares
  // with Finish.
  states: Query<LocationState[]>
}

// ClonesStep finds the user's clones: the clone root as it reads now, and the other folders
// in their home that hold clones, whatever they are named. Each folder can be added beside
// the clone root, or, when the root is missing or holds no clones, used in its place. With
// none found and no clone root, it offers a fresh one made for them. It looks in GitHub
// Desktop's folder in Documents when the user asks, since macOS asks first. Any other
// folder can be chosen, and, folded away, the other repository locations show as Settings
// shows them.
export function ClonesStep({ settings: stored, onSaved, states }: ClonesStepProps) {
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  const root = states.data?.find((s) => s.primary)
  const missing = rootNeedsAttention(root)
  const empty = root?.state === 'ok' && root.clones === 0
  const suggest = missing || empty
  const candidates = useQuery(
    () => suggestCloneRoots(),
    [settings.cloneRoot, JSON.stringify(settings.cloneLocations)],
    { log: 'could not look for folders with clones' },
  )
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')
  // documents is what the look in GitHub Desktop's folder found, null until the user asks.
  const [documents, setDocuments] = useState<DocumentsLook | null>(null)
  const documentsLook = useMutation(() => previewCloneLocation(documentsFolder))

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

  const lookInDocuments = async () => {
    const result = await documentsLook.run()

    if (result.ok) {
      setDocuments(documentsLookOf(result.data))
    }
  }

  // held reports a folder that is the clone root or another location already.
  const held = (path: string) => path === settings.cloneRoot || settings.cloneLocations.some((l) => l.path === path)
  const found = [...(candidates.data ?? []), ...(documents?.found ?? []).filter((c) => !held(c.path))]
  const lookable = documents === null && !held(documentsFolder)

  return (
    <div className="flex flex-col gap-5">
      <RootCard path={settings.cloneRoot} root={root} />

      {root?.state === 'ok' && root.clones > 0 && (
        <p className="flex items-center gap-1.5 text-sm text-success">
          <CircleCheckIcon className="size-4" />
          Found them. New clones go here too.
        </p>
      )}

      {root !== undefined && found.length > 0 && (
        <Suggestions candidates={found} asRoot={suggest} busy={busy} onUse={chooseRoot} onAdd={addLocation} />
      )}

      {missing && candidates.data !== undefined && found.length === 0 && (
        <div className="flex flex-col items-start gap-3 rounded-lg border border-border bg-surface px-4 py-4">
          <p className="text-sm text-muted">
            No clones turned up in your home folder. Start fresh: new clones will go in {settings.cloneRoot}.
          </p>
          <p className="text-xs text-muted">
            Keep them in Documents, on the Desktop, or on another disk? Choose that folder below: {brand.name} doesn’t
            look there on its own.
          </p>
          <button type="button" disabled={busy} onClick={startFresh} className={primaryButtonClass}>
            Start fresh in {settings.cloneRoot}
            <ChevronRightIcon className="size-3.5" />
          </button>
        </div>
      )}

      <div className="flex flex-col gap-2">
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
          {lookable && (
            <button
              type="button"
              disabled={busy || documentsLook.pending}
              onClick={() => {
                void lookInDocuments()
              }}
              className={buttonClass}
            >
              <SearchIcon className="size-3.5 text-muted" />
              Look in {documentsFolder}
            </button>
          )}
          {(busy || states.pending || documentsLook.pending) && <Spinner />}
        </div>
        {lookable && (
          <p className="text-xs text-muted">
            GitHub Desktop clones into {documentsFolder}. macOS asks before {brand.name} looks in Documents.
          </p>
        )}
        {documents !== null && documents.note !== '' && (
          <p role="status" className="text-xs text-muted">
            {documents.note}
          </p>
        )}
        {documentsLook.error !== '' && (
          <p role="alert" className="text-xs text-danger">
            {documentsLook.error}
          </p>
        )}
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
