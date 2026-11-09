import { useId, useState } from 'react'

import { brand } from '../generated/brand'
import { counted } from '../lib/format'
import { agentName, modelChoices, modelLabel, perAgentEntries } from '../lib/launch'
import { formatDateTime, formatFull } from '../lib/time'
import { useMutation } from '../lib/useMutation'
import { useQuery } from '../lib/useQuery'
import {
  importPrompts,
  listPromptBackups,
  previewPromptImport,
  type AgentInfo,
  type BundlePrompt,
  type ImportEntry,
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

// fileName is the last part of a path.
function fileName(path: string): string {
  return path.split(/[\\/]/).at(-1) ?? path
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
// it does, all chosen but those already in the library, so nothing changes until the user
// says so. Importing never changes a prompt the user wrote or edited.
export function ImportPromptsDialog({ start, agents, onImported, onCancel }: ImportPromptsDialogProps) {
  const id = useId()
  const [source, setSource] = useState<ImportSource | null>(
    start.kind === 'file' ? { kind: 'file', path: start.path } : null,
  )
  const [picked, setPicked] = useState<string | null>(null)
  // unchosen holds the entries, by index, the user turned off.
  const [unchosen, setUnchosen] = useState<ReadonlySet<number>>(new Set())

  const backups = useQuery(() => listPromptBackups(), [], { enabled: start.kind === 'backups' })
  const entries = useQuery(() => (source === null ? Promise.resolve([]) : previewPromptImport(source)), [source], {
    enabled: source !== null,
  })
  const importer = useMutation((chosen: BundlePrompt[]) => importPrompts(chosen))

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

  const shown = entries.data ?? []
  const chosen = shown.flatMap((e, i) => (e.outcome !== 'skip' && !unchosen.has(i) ? [e.prompt] : []))
  const verb = restoring ? 'Restore' : 'Import'
  const sourceName = start.kind === 'file' ? `“${fileName(start.path)}”` : 'the backup'

  return (
    <ConfirmDialog
      title={`${verb} prompts from ${sourceName}`}
      confirmLabel={importer.pending ? `${verb}ing…` : `${verb} ${counted(chosen.length, 'prompt', 'prompts')}`}
      tone="primary"
      size="wide"
      confirmDisabled={chosen.length === 0 || importer.pending || entries.pending}
      onCancel={onCancel}
      onConfirm={() => {
        void importer.run(chosen).then((result) => {
          if (result.ok) {
            onImported(result.data)
          }
        })
      }}
    >
      <EntryList
        agents={agents}
        entries={shown}
        pending={entries.pending}
        error={entries.error}
        unchosen={unchosen}
        onToggle={(i, on) => {
          setUnchosen((current) => {
            const next = new Set(current)

            if (on) {
              next.delete(i)
            } else {
              next.add(i)
            }

            return next
          })
        }}
      />
      {importer.error !== '' && (
        <p role="alert" className="mt-3 text-danger">
          {importer.error}
        </p>
      )}
    </ConfirmDialog>
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
                    className="row-span-3 mt-1 accent-accent"
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
                  className="mt-1 accent-accent"
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
