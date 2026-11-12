import { startTransition, useActionState, useId, useState } from 'react'

import { attempt } from '../../lib/call'
import type { SettingsSave } from '../../lib/useSettingsSave'
import { useQuery } from '../../lib/useQuery'
import {
  addCloneLocations,
  checkCloneLocations,
  chooseFolder,
  previewCloneLocation,
  removeCloneLocation,
  type CloneLocation,
  type LocationPreview,
  type LocationState,
  type Settings,
} from '../../platform'
import { buttonClass, primaryButtonClass, quietButtonClass } from '../controls'
import { Note, SectionIntro } from './rows'
import { StringListEditor } from './StringListEditor'

// Refusal is why a change was refused, with the draft typed when it was, since typing
// again clears it.
interface Refusal {
  draft: string
  message: string
}

const noRefusal: Refusal = { draft: '', message: '' }

// LocationsState is what the card shows after its last step: what it offers in place of
// the folder asked about, why the draft was refused, what the last add did, and why a
// location's Check again failed.
interface LocationsState {
  offer: LocationPreview | null
  refusal: Refusal
  note: string
  rowError: { path: string; message: string } | null
}

const idle: LocationsState = { offer: null, refusal: noRefusal, note: '', rowError: null }

// Step is one thing the card does, one at a time: look at a folder before adding it, add
// one or several, remove one, check one again, or put the offer away.
type Step =
  | { kind: 'preview'; path: string; draft: string }
  | { kind: 'add'; locations: CloneLocation[]; replaced: readonly string[]; draft: string }
  | { kind: 'remove'; path: string }
  | { kind: 'check'; path: string }
  | { kind: 'cancel' }

// counted is "1 clone" or "3 clones".
function counted(n: number, one = 'clone', many = 'clones'): string {
  return `${String(n)} ${n === 1 ? one : many}`
}

// listed joins words as a sentence does: "a", "a and b", "a, b and c".
function listed(words: readonly string[]): string {
  return words.length < 2 ? (words[0] ?? '') : `${words.slice(0, -1).join(', ')} and ${words[words.length - 1] ?? ''}`
}

// Marker is what a location's row says of it now, whether that is a warning, and whether
// Check again may help.
interface Marker {
  text: string
  warn: boolean
  recheck: boolean
}

// marker words how a location reads now, or nothing while it is being read.
export function marker(state: LocationState | undefined): Marker {
  if (state === undefined) {
    return { text: '', warn: false, recheck: false }
  }

  const folder = `folder · ${counted(state.clones)}`

  switch (state.state) {
    case 'ok':
      return {
        text: state.kind === 'folder' ? folder : state.repo === '' ? 'clone' : `clone · ${state.repo}`,
        warn: false,
        recheck: false,
      }
    case 'nowAClone':
      return { text: `${folder} · now a clone itself`, warn: true, recheck: true }
    case 'missing':
      return { text: 'Not found', warn: true, recheck: true }
    case 'notFolder':
      return { text: 'Not a folder', warn: true, recheck: true }
    case 'unreadable':
      return { text: 'Can’t be read', warn: true, recheck: true }
    case 'notAClone':
      return { text: 'No longer a clone', warn: true, recheck: true }
    case 'notAnswering':
      return { text: 'Not answering', warn: true, recheck: true }
    case 'covered':
      return { text: `Already in ${state.coveredBy}`, warn: true, recheck: false }
  }
}

// offers reports a preview that suggests something other than adding its folder as it is:
// a folder with no clones directly inside it, or a clone whose folder holds other clones.
function offers(preview: LocationPreview): boolean {
  return preview.kind === 'folder' ? preview.clones === 0 : preview.parent.path !== ''
}

// addedNote says what adding a folder did beyond adding it: the clone locations it holds,
// which are no longer listed on their own.
function addedNote(path: string, replaced: readonly string[]): string {
  if (replaced.length === 0) {
    return ''
  }

  return `Added ${path}. It holds ${listed(replaced)}, so ${replaced.length === 1 ? 'that clone is' : 'those clones are'} no longer listed on ${replaced.length === 1 ? 'its' : 'their'} own.`
}

interface CloneLocationsProps {
  // settings are the settings as shown, with every change still being saved applied.
  settings: Settings
  // save stores a change as the section's other settings are stored.
  save: SettingsSave['save']
}

// CloneLocations is the card of the other repository locations: each with what it is now,
// a button that removes it, and Check again for one that reads otherwise than it did;
// and a field, with Choose…, that adds another. A folder is looked at before it is added,
// and the card suggests a better choice where it finds one: the folders inside a folder
// with no clones of its own, or the folder of a clone that holds other clones.
export function CloneLocations({ settings, save }: CloneLocationsProps) {
  const id = useId()
  const [draft, setDraft] = useState('')
  const locations = settings.cloneLocations
  const states = useQuery(() => checkCloneLocations(), [settings.cloneRoot, JSON.stringify(locations)], {
    log: 'check the repository locations',
  })

  // addAll adds locations, all or none, and clears the draft once they are stored.
  async function addAll(added: CloneLocation[], replaced: readonly string[], typed: string): Promise<LocationsState> {
    const kept = locations.filter((l) => !replaced.includes(l.path))
    const result = await save({ cloneLocations: [...kept, ...added] }, () =>
      addCloneLocations(added.map((l) => l.path)),
    )

    if (!result.ok) {
      return { ...idle, refusal: { draft: typed, message: result.error } }
    }

    setDraft('')

    return { ...idle, note: added.length === 1 ? addedNote(added[0]?.path ?? '', replaced) : '' }
  }

  const [shown, run, busy] = useActionState(async (last: LocationsState, step: Step): Promise<LocationsState> => {
    switch (step.kind) {
      case 'cancel':
        return idle
      case 'preview': {
        const preview = await attempt(previewCloneLocation(step.path))

        if (!preview.ok) {
          return { ...idle, refusal: { draft: step.draft, message: preview.error } }
        }

        const found = preview.data

        if (found.covered !== '') {
          return { ...idle, refusal: { draft: step.draft, message: found.covered } }
        }

        if (offers(found)) {
          return { ...idle, offer: found }
        }

        return addAll([{ path: found.path, kind: found.kind }], found.replaces, step.draft)
      }
      case 'add':
        return addAll(step.locations, step.replaced, step.draft)
      case 'remove': {
        const result = await save({ cloneLocations: locations.filter((l) => l.path !== step.path) }, () =>
          removeCloneLocation(step.path),
        )

        return result.ok ? idle : { ...last, rowError: { path: step.path, message: result.error } }
      }
      case 'check': {
        const result = await save({}, () => addCloneLocations([step.path]))

        if (result.ok) {
          await states.refetch()

          return idle
        }

        return { ...last, rowError: { path: step.path, message: result.error } }
      }
    }
  }, idle)

  const step = (next: Step) => {
    startTransition(() => {
      run(next)
    })
  }

  const choose = async () => {
    const chosen = await attempt(chooseFolder({ title: 'Choose a repository location', button: 'Add' }))

    if (chosen.ok && chosen.data !== '') {
      setDraft(chosen.data)
      step({ kind: 'preview', path: chosen.data, draft: chosen.data })
    }
  }

  const refusal = busy ? noRefusal : shown.refusal
  const error = refusal.draft === draft ? refusal.message : ''
  const stateOf = (path: string) => states.data?.find((s) => !s.primary && s.path === path)

  return (
    <section aria-labelledby={`${id}-heading`} className="flex flex-col gap-3">
      <SectionIntro id={`${id}-heading`} title="Other repository locations">
        Folders with more of your clones, or single clones, anywhere else. Launches, the Worktrees view, and the Scripts
        pages look one level inside each folder, as they do in the clone root.
      </SectionIntro>
      <StringListEditor
        label="Other repository locations"
        items={locations.map((l) => l.path)}
        empty="None yet: only the clone root is looked in."
        removeLabel={(path) => `Remove ${path}`}
        onRemove={(path) => {
          step({ kind: 'remove', path })
        }}
        extra={(path) => (
          <LocationMarker
            marker={marker(stateOf(path))}
            error={shown.rowError?.path === path ? shown.rowError.message : ''}
            busy={busy}
            onCheck={() => {
              step({ kind: 'check', path })
            }}
          />
        )}
        inputLabel="Folder to add"
        placeholder="A folder, such as ~/work, or a single clone"
        submitLabel="Add"
        draft={draft}
        onDraftChange={(next) => {
          setDraft(next)
        }}
        onAdd={() => {
          step({ kind: 'preview', path: draft.trim(), draft })
        }}
        error={error}
        busy={busy}
        actions={
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              void choose()
            }}
            className={buttonClass}
          >
            Choose…
          </button>
        }
        below={
          <>
            {shown.offer !== null && (
              <Offer
                preview={shown.offer}
                busy={busy}
                onAdd={(added, replaced) => {
                  step({ kind: 'add', locations: added, replaced, draft })
                }}
                onCancel={() => {
                  step({ kind: 'cancel' })
                }}
              />
            )}
            {shown.note !== '' && (
              <p role="status" className="px-4 pb-3 text-xs text-muted">
                {shown.note}
              </p>
            )}
          </>
        }
      />
      <Note>
        Removing a location leaves its clones and their worktrees as they are, but they aren&apos;t listed or cleaned up
        until you add it again. New clones always go in the clone root.
      </Note>
    </section>
  )
}

interface LocationMarkerProps {
  marker: Marker
  error: string
  busy: boolean
  onCheck: () => void
}

// LocationMarker is what a location's row says of it, with Check again for one that reads
// otherwise than it did, and why the last check failed.
function LocationMarker({ marker: m, error, busy, onCheck }: LocationMarkerProps) {
  return (
    <span className="flex min-w-0 items-center gap-2 text-xs">
      {m.text !== '' && <span className={m.warn ? 'text-warning' : 'text-muted'}>{m.text}</span>}
      {m.recheck && (
        <button type="button" disabled={busy} onClick={onCheck} className={quietButtonClass}>
          Check again
        </button>
      )}
      {error !== '' && (
        <span role="alert" className="text-danger">
          {error}
        </span>
      )}
    </span>
  )
}

interface OfferProps {
  preview: LocationPreview
  busy: boolean
  // onAdd adds the locations, dropping the clone locations replaced holds.
  onAdd: (locations: CloneLocation[], replaced: readonly string[]) => void
  onCancel: () => void
}

// Offer is what the card suggests in place of the folder asked about: the folders inside
// a folder with no clones of its own, or the folder of a clone that holds other clones.
function Offer({ preview, busy, onAdd, onCancel }: OfferProps) {
  const asked: CloneLocation = { path: preview.path, kind: preview.kind }
  let text: string
  let better: { label: string; locations: CloneLocation[] } | null = null
  let anyway: string

  if (preview.kind === 'clone') {
    text = `${preview.path} is a clone. Its folder, ${preview.parent.path}, holds ${counted(preview.parent.clones, 'other clone', 'other clones')}.`
    better = { label: `Add ${preview.parent.path}`, locations: [{ path: preview.parent.path, kind: 'folder' }] }
    anyway = 'Add only this clone'
  } else if (preview.inside.length > 0) {
    const inside = listed(preview.inside.map((s) => `${s.path} (${String(s.clones)})`))
    const more = preview.moreInside ? ', among others,' : ''

    text = `${preview.path} has no clones directly inside; ${inside}${more} do.`
    better = {
      label: preview.inside.length === 1 ? `Add ${preview.inside[0]?.path ?? ''}` : 'Add those',
      locations: preview.inside.map((s) => ({ path: s.path, kind: 'folder' })),
    }
    anyway = `Add ${preview.path} anyway`
  } else {
    text = `${preview.path} has no clones directly inside, nor in the folders inside it.`
    anyway = `Add ${preview.path} anyway`
  }

  if (preview.partial) {
    text += ' Only its first folders were looked in.'
  }

  return (
    <div role="status" className="flex flex-col items-start gap-2 px-4 pb-3 text-xs">
      <p className="text-text">{text}</p>
      <div className="flex flex-wrap items-center gap-2">
        {better !== null && (
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              onAdd(better.locations, [])
            }}
            className={primaryButtonClass}
          >
            {better.label}
          </button>
        )}
        <button
          type="button"
          disabled={busy}
          onClick={() => {
            onAdd([asked], preview.replaces)
          }}
          className={buttonClass}
        >
          {anyway}
        </button>
        <button type="button" disabled={busy} onClick={onCancel} className={quietButtonClass}>
          Cancel
        </button>
      </div>
    </div>
  )
}
