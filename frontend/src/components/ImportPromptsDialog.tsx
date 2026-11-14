import { useId, useState } from 'react'

import { brand } from '../generated/brand'
import { counted, fileName } from '../lib/format'
import { agentName, modelChoices, modelLabel, perAgentEntries } from '../lib/launch'
import { replySlotPlaces } from '../lib/replies'
import { formatDateTime, formatFull } from '../lib/time'
import { useMutation } from '../lib/useMutation'
import { useQuery } from '../lib/useQuery'
import {
  importPrompts,
  listPromptBackups,
  previewPromptImport,
  type AgentInfo,
  type BundleDefaults,
  type BundlePrompt,
  type DefaultOffer,
  type DefaultSlot,
  type ImportEntry,
  type ImportPreview,
  type ImportResult,
  type ImportSource,
  type PromptBackup,
  type PromptBackupKind,
} from '../platform'
import { ConfirmDialog } from './ConfirmDialog'
import { chipClass } from './controls'
import { AlertIcon } from './icons'

// ImportStart is where an import begins: a prompt file the user chose, whose prompts it
// shows at once, or the backups the app keeps, one of which the user picks first.
export type ImportStart = { kind: 'file'; path: string } | { kind: 'backups' }

export interface ImportPromptsDialogProps {
  start: ImportStart
  // agents are the coding agents, which name each prompt's per-agent settings.
  agents: readonly AgentInfo[]
  // onImported is told what the import did, once the library holds it.
  onImported: (result: ImportResult) => void
  onCancel: () => void
}

// backupKinds names each kind of backup and says how the app came to keep it.
const backupKinds: Record<PromptBackupKind, { title: string; about: string }> = {
  library: {
    title: 'Saved copy of your prompts',
    about: `${brand.name} saves it after every change to your prompts.`,
  },
  'set-aside': {
    title: 'Data set aside',
    about: `${brand.name} couldn’t open it, so it started fresh and kept it.`,
  },
  update: {
    title: 'Backup before an update',
    about: `${brand.name} took it before an update changed its data.`,
  },
}

// appliesText says what a prompt is for.
const appliesText: Record<BundlePrompt['appliesTo'], string> = {
  pr: 'For pull requests',
  issue: 'For issues',
  any: 'For pull requests and issues',
}

// settingsText lists a prompt's launch settings that are not inherited, as in "For pull
// requests · Claude: Opus · high effort · plan mode".
function settingsText(p: BundlePrompt, agents: readonly AgentInfo[]): string {
  const parts = [appliesText[p.appliesTo]]

  for (const [id, model] of perAgentEntries(p.models, agents)) {
    if (model !== '') {
      parts.push(`${agentName(agents, id)}: ${modelLabel(model, modelChoices(agents, id))}`)
    }
  }

  if (p.effort !== '') {
    parts.push(`${p.effort} effort`)
  }

  if (p.planMode !== null) {
    parts.push(p.planMode ? 'plan mode' : 'no plan mode')
  }

  if (p.worktree !== null) {
    parts.push(p.worktree ? 'in a worktree' : 'not in a worktree')
  }

  return parts.join(' · ')
}

// outcomeText says what importing an entry does.
function outcomeText(e: ImportEntry): string {
  switch (e.outcome) {
    case 'add':
      return 'New'
    case 'rename':
      return `New · saved as “${e.nickname}”`
    case 'replace':
      return `Updates the starter “${e.replaces}”`
    default:
      return e.nickname === e.prompt.nickname ? 'Already in your library' : `Already in your library as “${e.nickname}”`
  }
}

// slotText names what a default is the default for, as in "the default prompt for pull
// requests" or "the default reply for approvals".
function slotText(slot: DefaultSlot): string {
  switch (slot) {
    case 'pr':
      return 'the default prompt for pull requests'
    case 'issue':
      return 'the default prompt for issues'
    default:
      return `the default reply for ${replySlotPlaces[slot]}`
  }
}

// offerText says what taking a default offer does, as in "Make “LGTM” the default reply
// for approvals (now “Looks good”)".
function offerText(offer: DefaultOffer): string {
  const now = offer.current === '' ? 'now none' : `now “${offer.current}”`

  return `Make “${offer.nickname}” ${slotText(offer.slot)} (${now})`
}

// chosenDefaults are the defaults an import sends for the offers taken: each slot named by
// the nickname its entry's prompt is sent with.
function chosenDefaults(offers: readonly DefaultOffer[], entries: readonly ImportEntry[]): BundleDefaults {
  const out: BundleDefaults = { pr: '', issue: '', replies: {} }

  for (const offer of offers) {
    const nickname = entries[offer.entry]?.prompt.nickname ?? ''

    if (offer.slot === 'pr' || offer.slot === 'issue') {
      out[offer.slot] = nickname
    } else {
      out.replies = { ...out.replies, [offer.slot]: nickname }
    }
  }

  return out
}

// importLabel words the confirm button: what the import brings, as in "Import 2 prompts
// and 1 default", or only the prompts when it takes no default.
function importLabel(verb: string, prompts: number, defaults: number): string {
  const what = counted(prompts, 'prompt', 'prompts')

  if (defaults === 0) {
    return `${verb} ${what}`
  }

  const made = counted(defaults, 'default', 'defaults')

  return prompts === 0 ? `${verb} ${made}` : `${verb} ${what} and ${made}`
}

// noPreview is what a dialog without a source yet shows.
const noPreview: ImportPreview = { entries: [], defaults: [] }

// backupDetail says what a backup holds, compared with the library.
function backupDetail(b: PromptBackup): string {
  if (b.error !== '') {
    return `Couldn’t be read: ${b.error}`
  }

  if (b.prompts === 0) {
    return 'Holds no prompts.'
  }

  const held = counted(b.prompts, 'prompt', 'prompts')

  return b.new === 0 ? `${held}, all already in your library` : `${held}, ${String(b.new)} not in your library`
}

// usable reports whether a backup can be picked: it was read, and holds prompts.
function usable(b: PromptBackup): boolean {
  return b.error === '' && b.prompts > 0
}

// ImportPromptsDialog brings prompts into the library: from a prompt file the user chose,
// or from one of the copies the app keeps (its saved copy, data it set aside, and backups
// taken before updates). It first shows every prompt the source holds and what importing
// it does, all chosen but those already in the library, and below them each of the
// source's defaults that importing would change here, all taken, so nothing changes until
// the user says so. A default goes with its prompt: one whose prompt is left out is held
// back. Importing never changes a prompt the user wrote or edited.
export function ImportPromptsDialog({ start, agents, onImported, onCancel }: ImportPromptsDialogProps) {
  const id = useId()
  const [source, setSource] = useState<ImportSource | null>(
    start.kind === 'file' ? { kind: 'file', path: start.path } : null,
  )
  const [picked, setPicked] = useState<string | null>(null)
  // unchosen holds the entries, by index, the user turned off, and declined the default
  // offers, by index.
  const [unchosen, setUnchosen] = useState<ReadonlySet<number>>(new Set())
  const [declined, setDeclined] = useState<ReadonlySet<number>>(new Set())

  const backups = useQuery(() => listPromptBackups(), [], { enabled: start.kind === 'backups' })
  const preview = useQuery(
    () => (source === null ? Promise.resolve(noPreview) : previewPromptImport(source)),
    [source],
    { enabled: source !== null },
  )
  const importer = useMutation((chosen: BundlePrompt[], defaults: BundleDefaults) => importPrompts(chosen, defaults))

  const restoring = start.kind === 'backups'

  if (source === null) {
    const list = backups.data ?? []
    const choice = picked ?? (list.find((b) => usable(b) && b.new > 0) ?? list.find(usable))?.path ?? null

    return (
      <ConfirmDialog
        title="Restore prompts from a backup"
        confirmLabel="Show prompts"
        tone="primary"
        size="wide"
        confirmDisabled={choice === null}
        onCancel={onCancel}
        onConfirm={() => {
          if (choice !== null) {
            setSource({ kind: 'backup', path: choice })
          }
        }}
      >
        <BackupList
          id={id}
          backups={list}
          pending={backups.pending}
          error={backups.error}
          choice={choice}
          onPick={setPicked}
        />
      </ConfirmDialog>
    )
  }

  const shown = preview.data?.entries ?? []
  const offers = preview.data?.defaults ?? []
  // brought reports whether an entry's prompt is chosen; held, whether the library holds
  // it already, which a default it offers needs no import for.
  const brought = (i: number) => shown[i] !== undefined && shown[i].outcome !== 'skip' && !unchosen.has(i)
  const held = (i: number) => shown[i]?.outcome === 'skip'
  const offerable = (offer: DefaultOffer) => brought(offer.entry) || held(offer.entry)
  const taken = offers.filter((offer, i) => offerable(offer) && !declined.has(i))
  const chosen = shown.filter((_, i) => brought(i))
  // A default names its prompt among those sent, so a prompt the library holds already is
  // sent with it, which the import skips again.
  const sent = shown.flatMap((e, i) => (brought(i) || taken.some((o) => o.entry === i) ? [e.prompt] : []))
  const verb = restoring ? 'Restore' : 'Import'
  const sourceName = start.kind === 'file' ? `“${fileName(start.path)}”` : 'the backup'

  return (
    <ConfirmDialog
      title={`${verb} prompts from ${sourceName}`}
      confirmLabel={importer.pending ? `${verb}ing…` : importLabel(verb, chosen.length, taken.length)}
      tone="primary"
      size="wide"
      confirmDisabled={chosen.length + taken.length === 0 || importer.pending || preview.pending}
      onCancel={onCancel}
      onConfirm={() => {
        void importer.run(sent, chosenDefaults(taken, shown)).then((result) => {
          if (result.ok) {
            onImported(result.data)
          }
        })
      }}
    >
      <EntryList
        agents={agents}
        entries={shown}
        pending={preview.pending}
        error={preview.error}
        unchosen={unchosen}
        onToggle={(i, on) => {
          setUnchosen((current) => toggled(current, i, on))
        }}
      />
      {offers.length > 0 && (
        <DefaultList
          offers={offers}
          offerable={offerable}
          declined={declined}
          onToggle={(i, on) => {
            setDeclined((current) => toggled(current, i, on))
          }}
        />
      )}
      {importer.error !== '' && (
        <p role="alert" className="mt-3 text-danger">
          {importer.error}
        </p>
      )}
    </ConfirmDialog>
  )
}

// toggled returns set with i taken out when on, or put in when not.
function toggled(set: ReadonlySet<number>, i: number, on: boolean): ReadonlySet<number> {
  const next = new Set(set)

  if (on) {
    next.delete(i)
  } else {
    next.add(i)
  }

  return next
}

interface DefaultListProps {
  offers: readonly DefaultOffer[]
  // offerable reports whether an offer's prompt is brought in or already held.
  offerable: (offer: DefaultOffer) => boolean
  declined: ReadonlySet<number>
  onToggle: (index: number, on: boolean) => void
}

// DefaultList offers each of the source's defaults that importing would change, each with
// a box to leave it as it is; one whose prompt is left out is held back with it.
function DefaultList({ offers, offerable, declined, onToggle }: DefaultListProps) {
  const id = useId()

  return (
    <div role="group" aria-labelledby={`${id}-title`} className="mt-4">
      <h3 id={`${id}-title`} className="text-sm font-semibold text-text">
        Defaults
      </h3>
      <p className="mt-0.5 text-xs">The file’s defaults that differ from yours. Each goes with its prompt.</p>
      <ul className="mt-2 flex flex-col gap-1">
        {offers.map((offer, i) => {
          const open = offerable(offer)

          return (
            <li key={offer.slot}>
              <label
                title={open ? undefined : 'Its prompt isn’t chosen.'}
                className={`flex items-start gap-2.5 rounded-md border border-border px-3 py-2 text-sm text-text ${open ? 'cursor-pointer' : 'opacity-60'}`}
              >
                <input
                  type="checkbox"
                  checked={open && !declined.has(i)}
                  disabled={!open}
                  onChange={(ev) => {
                    onToggle(i, ev.target.checked)
                  }}
                  className="checkbox mt-0.5"
                />
                {offerText(offer)}
              </label>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

interface BackupListProps {
  id: string
  backups: readonly PromptBackup[]
  pending: boolean
  error: string
  choice: string | null
  onPick: (path: string) => void
}

// BackupList offers the copies of the library the app keeps, newest first, saying what
// each holds; a copy that cannot be read, or holds nothing, cannot be picked.
function BackupList({ id, backups, pending, error, choice, onPick }: BackupListProps) {
  if (error !== '') {
    return <p role="alert">Couldn’t list the backups: {error}</p>
  }

  if (pending && backups.length === 0) {
    return <p>Looking for backups…</p>
  }

  if (backups.length === 0) {
    return <p>{brand.name} hasn’t kept any backups of your prompts yet.</p>
  }

  return (
    <>
      <p>Choose a backup to see the prompts in it. Nothing changes until you restore them.</p>
      <fieldset className="mt-3">
        <legend className="sr-only">Backups</legend>
        <ul className="flex max-h-72 flex-col gap-1 overflow-y-auto">
          {backups.map((b) => {
            const kind = backupKinds[b.kind]

            return (
              <li key={b.path}>
                <label
                  title={`${formatFull(b.at)}\n${b.path}`}
                  className="grid cursor-pointer grid-cols-[auto_minmax(0,1fr)] items-start gap-x-2.5 rounded-md border border-border px-3 py-2 has-checked:border-accent/60 has-checked:bg-accent/8 has-disabled:cursor-default has-disabled:opacity-60"
                >
                  <input
                    type="radio"
                    name={`${id}-backup`}
                    value={b.path}
                    checked={choice === b.path}
                    disabled={!usable(b)}
                    onChange={() => {
                      onPick(b.path)
                    }}
                    className="radio row-span-3 mt-0.5"
                  />
                  <span className="text-sm font-medium text-text">
                    {kind.title} · {formatDateTime(b.at)}
                  </span>
                  <span className="col-start-2 text-xs">{kind.about}</span>
                  <span className={`col-start-2 text-xs ${b.error === '' ? 'text-text' : 'text-danger'}`}>
                    {backupDetail(b)}
                  </span>
                </label>
              </li>
            )
          })}
        </ul>
      </fieldset>
    </>
  )
}

interface EntryListProps {
  agents: readonly AgentInfo[]
  entries: readonly ImportEntry[]
  pending: boolean
  error: string
  unchosen: ReadonlySet<number>
  onToggle: (index: number, on: boolean) => void
}

// EntryList shows each prompt a source holds, what importing it does, its launch
// settings, and anything changed to import it, each with a box to leave it out. A prompt
// the library already holds is shown, unchecked, for the record.
function EntryList({ agents, entries, pending, error, unchosen, onToggle }: EntryListProps) {
  if (error !== '') {
    return <p role="alert">Couldn’t read the prompts: {error}</p>
  }

  if (pending && entries.length === 0) {
    return <p>Reading the prompts…</p>
  }

  if (entries.length === 0) {
    return <p>There are no prompts in it.</p>
  }

  const held = entries.filter((e) => e.outcome === 'skip').length

  return (
    <>
      <p>
        {held === entries.length
          ? 'Everything in it is already in your library.'
          : 'Choose the prompts to bring in. Prompts you wrote or edited are never changed.'}
      </p>
      <ul aria-label="Prompts" className="mt-3 flex max-h-80 flex-col gap-1 overflow-y-auto">
        {entries.map((e, i) => {
          const skip = e.outcome === 'skip'
          const flags = perAgentEntries(e.prompt.extraArgs, agents)
            .filter(([, args]) => (args ?? []).length > 0)
            .map(([id, args]) => `${agentName(agents, id)}: ${(args ?? []).join(' ')}`)
            .join(' · ')

          return (
            // The prompts in a file have no IDs; the list never reorders.
            <li key={i}>
              <label
                className={`flex items-start gap-2.5 rounded-md border border-border px-3 py-2 ${skip ? 'opacity-60' : 'cursor-pointer'}`}
              >
                <input
                  type="checkbox"
                  checked={!skip && !unchosen.has(i)}
                  disabled={skip}
                  onChange={(ev) => {
                    onToggle(i, ev.target.checked)
                  }}
                  className="checkbox mt-0.5"
                />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                    <span className="text-sm font-medium text-text">{e.prompt.nickname}</span>
                    <span className="min-w-0 truncate text-xs">{e.prompt.name}</span>
                    <span className={`${chipClass} ml-auto`}>{outcomeText(e)}</span>
                  </span>
                  <span className="text-xs">{settingsText(e.prompt, agents)}</span>
                  {flags !== '' && (
                    <span className="text-xs">
                      Adds flags: <code className="font-mono text-text">{flags}</code>
                    </span>
                  )}
                  {e.notes.map((note) => (
                    <span key={note} className="flex items-start gap-1 text-xs text-text">
                      <AlertIcon className="mt-px size-3.5 shrink-0 text-warning" />
                      {note}
                    </span>
                  ))}
                </span>
              </label>
            </li>
          )
        })}
      </ul>
    </>
  )
}
