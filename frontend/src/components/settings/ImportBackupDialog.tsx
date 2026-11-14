import { useId, useState } from 'react'

import { brand } from '../../generated/brand'
import { backupSections, changeText, planText, sectionLabel } from '../../lib/backup'
import { counted, fileName } from '../../lib/format'
import { formatDateTime } from '../../lib/time'
import { useMutation } from '../../lib/useMutation'
import { useQuery } from '../../lib/useQuery'
import {
  BackupMode,
  importBackup,
  previewBackup,
  type BackupImported,
  type BackupItem,
  type BackupPlan,
  type BackupPreview,
  type BackupRequest,
  type BackupSection,
  type BackupSectionPreview,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { chipClass, Segmented } from '../controls'
import { AlertIcon, ChevronRightIcon } from '../icons'

export interface ImportBackupDialogProps {
  // path is the backup the user chose.
  path: string
  // onImported is told what the import did, once every category is in.
  onImported: (result: BackupImported) => void
  onCancel: () => void
}

// modes are the ways an import treats what is here, Merge first, as the dialog starts.
const modes = [
  { value: BackupMode.ModeMerge, label: 'Merge' },
  { value: BackupMode.ModeReplace, label: 'Replace what’s here' },
]

// modeHelp says what each mode does.
const modeHelp: Readonly<Record<BackupMode, string>> = {
  [BackupMode.ModeMerge]: 'Adds what the backup holds and changes what it names. Nothing here is deleted.',
  [BackupMode.ModeReplace]:
    'Makes each category you choose exactly the backup’s. What’s here and not in the backup is deleted.',
}

// planOf is what importing a category would do under mode.
function planOf(preview: BackupSectionPreview, mode: BackupMode): BackupPlan {
  return mode === BackupMode.ModeReplace ? preview.replace : preview.merge
}

// importable reports whether a category can be imported: the backup holds it, and could
// read it.
function importable(preview: BackupSectionPreview | undefined): preview is BackupSectionPreview {
  return preview !== undefined && preview.present && preview.error === ''
}

// madeText says when, and by which version, the backup was made.
function madeText(preview: BackupPreview): string {
  const by = preview.app === '' ? brand.name : `${brand.name} ${preview.app}`

  return preview.exportedAt === '' ? `Made by ${by}.` : `Made by ${by} on ${formatDateTime(preview.exportedAt)}.`
}

// ImportBackupDialog brings a backup of the user's setup back: it shows each category the
// backup holds, what importing it would do under the chosen mode, with its notes and its
// items, and imports the categories left chosen, all or none. Merge, the default, deletes
// nothing; Replace asks again first, naming everything it would delete.
export function ImportBackupDialog({ path, onImported, onCancel }: ImportBackupDialogProps) {
  const id = useId()
  const preview = useQuery(() => previewBackup(path), [path])
  const [mode, setMode] = useState<BackupMode>(BackupMode.ModeMerge)
  // unchosen holds the categories the user turned off.
  const [unchosen, setUnchosen] = useState<ReadonlySet<BackupSection>>(new Set())
  // confirming is set once Replace was asked for, while the dialog asks again.
  const [confirming, setConfirming] = useState(false)
  const importer = useMutation((req: BackupRequest) => importBackup(req))

  const read = preview.data
  const sections = read?.sections ?? []
  const byName = new Map(sections.map((s) => [s.section, s]))
  const chosen = backupSections.flatMap(({ section }) =>
    importable(byName.get(section)) && !unchosen.has(section) ? [section] : [],
  )

  const run = () => {
    if (read === undefined) {
      return
    }

    void importer.run({ path, hash: read.hash, sections: chosen, mode }).then((result) => {
      if (result.ok) {
        onImported(result.data)
      }
    })
  }

  const failure = importer.error !== '' && (
    <p role="alert" className="mt-3 text-danger">
      {importer.error}
    </p>
  )

  // Each step is a dialog of its own, so the second opens with Cancel focused.
  if (confirming) {
    return (
      <ConfirmDialog
        key="replace"
        title="Replace what’s here?"
        confirmLabel={importer.pending ? 'Replacing…' : 'Replace'}
        confirmDisabled={importer.pending}
        size="wide"
        onCancel={() => {
          importer.reset()
          setConfirming(false)
        }}
        onConfirm={run}
      >
        <Deletions sections={chosen.flatMap((s) => byName.get(s) ?? [])} />
        {failure}
      </ConfirmDialog>
    )
  }

  return (
    <ConfirmDialog
      key="choose"
      title={`Restore from “${fileName(path)}”`}
      confirmLabel={importer.pending ? 'Importing…' : `Import ${counted(chosen.length, 'category', 'categories')}`}
      tone="primary"
      size="wide"
      confirmDisabled={chosen.length === 0 || importer.pending || preview.pending}
      onCancel={onCancel}
      onConfirm={() => {
        if (mode === BackupMode.ModeReplace) {
          importer.reset()
          setConfirming(true)
        } else {
          run()
        }
      }}
    >
      {preview.error !== '' && <p role="alert">Couldn’t read the backup: {preview.error}</p>}
      {preview.error === '' && read === undefined && <p>Reading the backup…</p>}
      {read !== undefined && (
        <>
          <p>{madeText(read)} Choose what to bring in. Your setup as it is now is saved first.</p>
          <div className="mt-3 flex flex-col gap-1.5">
            <Segmented
              label="How to import"
              value={mode}
              options={modes}
              onChange={setMode}
              size="sm"
              describedBy={`${id}-mode`}
            />
            <p id={`${id}-mode`} className="text-xs">
              {modeHelp[mode]}
            </p>
          </div>
          <ul aria-label="Categories" className="mt-3 flex max-h-96 flex-col gap-1 overflow-y-auto">
            {backupSections.map(({ section, label }) => (
              <li key={section}>
                <SectionRow
                  label={label}
                  preview={byName.get(section)}
                  mode={mode}
                  chosen={chosen.includes(section)}
                  onToggle={(on) => {
                    setUnchosen((current) => {
                      const next = new Set(current)

                      if (on) {
                        next.delete(section)
                      } else {
                        next.add(section)
                      }

                      return next
                    })
                  }}
                />
              </li>
            ))}
          </ul>
        </>
      )}
      {failure}
    </ConfirmDialog>
  )
}

interface SectionRowProps {
  label: string
  // preview is the category as the backup holds it, undefined when the preview names it
  // not at all.
  preview: BackupSectionPreview | undefined
  mode: BackupMode
  chosen: boolean
  onToggle: (on: boolean) => void
}

// SectionRow is one category of the backup: a box to bring it in, what importing it
// would do, its notes, and its items, folded.
function SectionRow({ label, preview, mode, chosen, onToggle }: SectionRowProps) {
  const id = useId()
  const open = importable(preview)
  const plan = open ? planOf(preview, mode) : undefined
  let summary = 'Not in this backup'

  if (preview !== undefined && preview.present && preview.error !== '') {
    summary = `Can’t be imported: ${preview.error}`
  } else if (plan !== undefined) {
    summary = planText(plan.count)
  }

  return (
    <div
      role="group"
      aria-labelledby={`${id}-name`}
      className={`rounded-md border border-border px-3 py-2 ${open ? '' : 'opacity-60'}`.trim()}
    >
      <label
        className={`grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-2.5 ${open ? 'cursor-pointer' : ''}`.trim()}
      >
        <input
          type="checkbox"
          checked={chosen}
          disabled={!open}
          onChange={(ev) => {
            onToggle(ev.target.checked)
          }}
          className="checkbox row-span-2 mt-0.5"
        />
        <span id={`${id}-name`} className="text-sm font-medium text-text">
          {label}
        </span>
        <span
          className={`col-start-2 text-xs ${preview?.error === '' || preview === undefined ? '' : 'text-danger'}`.trim()}
        >
          {summary}
        </span>
      </label>
      {plan !== undefined && (plan.notes.length > 0 || plan.items.length > 0) && (
        <div className="mt-1 flex flex-col gap-1 pl-6.5">
          {plan.notes.map((note) => (
            <span key={note} className="flex items-start gap-1 text-xs text-text">
              <AlertIcon className="mt-px size-3.5 shrink-0 text-warning" />
              {note}
            </span>
          ))}
          {plan.items.length > 0 && <Items label={label} items={plan.items} />}
        </div>
      )}
    </div>
  )
}

// Items is a category's items, folded until the user opens them, each with what importing
// does to it.
function Items({ label, items }: { label: string; items: readonly BackupItem[] }) {
  return (
    <details className="group/items">
      <summary className="-ml-1 flex w-fit cursor-pointer list-none items-center gap-1 rounded-md py-0.5 pr-1.5 pl-1 text-xs font-medium text-muted select-none hover:bg-raised hover:text-text [&::-webkit-details-marker]:hidden">
        <ChevronRightIcon className="size-3.5 shrink-0 transition-transform group-open/items:rotate-90 motion-reduce:transition-none" />
        {counted(items.length, 'item', 'items')}
      </summary>
      <ul aria-label={`${label} items`} className="mt-1 flex flex-col gap-0.5">
        {items.map((item, i) => (
          // An item's label may repeat across kinds, such as a setting and a target; the
          // list never reorders.
          <li key={i} className="flex flex-wrap items-baseline gap-x-2 text-xs">
            <span className="font-medium text-text">{item.label}</span>
            {item.detail !== '' && <span className="min-w-0 truncate">{item.detail}</span>}
            <span className={`${chipClass} ml-auto`}>{changeText[item.change]}</span>
          </li>
        ))}
      </ul>
    </details>
  )
}

// Deletions names, per category, everything Replace would delete, or says it deletes
// nothing, so the second confirmation shows exactly what goes.
function Deletions({ sections }: { sections: readonly BackupSectionPreview[] }) {
  const deleted = sections
    .map((s) => ({ section: s.section, items: s.replace.items.filter((item) => item.change === 'remove') }))
    .filter((s) => s.items.length > 0)

  if (deleted.length === 0) {
    return (
      <p>
        Each category you chose becomes exactly the backup’s. Nothing here is deleted, but what the backup holds takes
        the place of what’s here. Your setup as it is now is saved first.
      </p>
    )
  }

  return (
    <>
      <p>
        Each category you chose becomes exactly the backup’s, and what’s here but not in it is deleted. Your setup as it
        is now is saved first.
      </p>
      <ul aria-label="To delete" className="mt-3 flex max-h-72 flex-col gap-2 overflow-y-auto">
        {deleted.map(({ section, items }) => (
          <li key={section}>
            <p className="text-sm font-medium text-text">
              {sectionLabel(section)}: {counted(items.length, 'item', 'items')} deleted
            </p>
            <ul aria-label={`${sectionLabel(section)} to delete`} className="mt-0.5 flex flex-col text-xs">
              {items.map((item, i) => (
                // The list never reorders.
                <li key={i}>
                  {item.label}
                  {item.detail === '' ? '' : ` · ${item.detail}`}
                </li>
              ))}
            </ul>
          </li>
        ))}
      </ul>
    </>
  )
}
