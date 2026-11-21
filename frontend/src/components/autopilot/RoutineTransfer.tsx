import { useId, useState, type ReactNode } from 'react'

import { brand } from '../../generated/brand'
import { attempt } from '../../lib/call'
import { pickFileToOpen, pickFileToSave } from '../../lib/dialogs'
import { counted } from '../../lib/format'
import {
  exportRoutines,
  importRoutines,
  previewRoutineImport,
  type AutopilotImportPreview,
  type AutopilotMissingRef,
  type AutopilotRefChoice,
  type AutopilotRoutine,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { captionClass, fieldClass } from '../controls'

// routineFiles is the type of file routines are exported to and imported from.
const routineFiles = [{ name: `${brand.name} routines`, pattern: '*.json' }]

// refKey names a missing reference among a preview's.
function refKey(r: Pick<AutopilotMissingRef, 'kind' | 'agent' | 'name' | 'starter'>): string {
  return `${r.kind}:${r.agent}:${r.starter}:${r.name}`
}

// refWords say what a routine names that this Mac lacks.
export function refWords(r: AutopilotMissingRef): string {
  switch (r.kind) {
    case 'prompt':
      return r.reply ? `The reply “${r.name}”` : `The prompt “${r.name}”`
    case 'agent':
      return `The agent ${r.agent}, which this Mac doesn’t have, so it inherits the one a click would use`
    case 'profile':
      return r.agent === 'inherit' ? `The profile “${r.name}”` : `${r.agent}’s profile “${r.name}”`
  }
}

// inheritWords say what leaving a reference unmapped does.
function inheritWords(r: AutopilotMissingRef): string {
  if (r.kind === 'prompt') {
    return r.reply ? 'Leave it, and pick one later' : 'Inherit: the item’s own default prompt'
  }

  return 'Inherit: the profile a click would use'
}

// choicesOf is what a preview's missing references map to, each to inherit to begin with.
export function choicesOf(p: AutopilotImportPreview, chosen: Readonly<Record<string, string>>): AutopilotRefChoice[] {
  return p.missing
    .filter((r) => !r.inherits)
    .map((r) => ({ kind: r.kind, agent: r.agent, name: r.name, starter: r.starter, to: chosen[refKey(r)] ?? '' }))
}

// importedWords say what an import brought in.
export function importedWords(routines: readonly string[], prompts: readonly string[]): string {
  const made = `Imported ${counted(routines.length, 'routine', 'routines')}, switched off.`

  if (prompts.length === 0) {
    return made
  }

  return `${made} Added ${counted(prompts.length, 'prompt', 'prompts')}: ${prompts.join(', ')}.`
}

interface PreviewProps {
  preview: AutopilotImportPreview
  chosen: Readonly<Record<string, string>>
  onChoose: (key: string, to: string) => void
}

// ImportPreview says what importing the file would do: each routine with the name it takes
// and its sentence, the prompts it brings, and a choice for each thing its routines name
// that this Mac lacks.
function ImportPreview({ preview: p, chosen, onChoose }: PreviewProps) {
  const id = useId()

  return (
    <div className="flex flex-col gap-3">
      <section aria-labelledby={`${id}-routines`} className="flex flex-col gap-1">
        <h3 id={`${id}-routines`} className={captionClass}>
          Routines, each switched off
        </h3>
        <ul className="flex flex-col gap-1.5">
          {p.routines.map((r) => (
            <li key={r.arrivesAs} className="flex flex-col text-sm">
              <span className="font-medium text-text">
                {r.arrivesAs}
                {r.arrivesAs !== r.name && <span className="font-normal text-muted">{` (from “${r.name}”)`}</span>}
              </span>
              <span className={`text-xs ${r.broken === '' ? 'text-muted' : 'text-warning'}`}>
                {r.broken === '' ? r.sentence : r.broken}
              </span>
            </li>
          ))}
        </ul>
      </section>
      {p.prompts.length > 0 && (
        <section aria-labelledby={`${id}-prompts`} className="flex flex-col gap-1">
          <h3 id={`${id}-prompts`} className={captionClass}>
            Prompts it brings
          </h3>
          <ul className="flex flex-col gap-0.5">
            {p.prompts.map((a) => (
              <li key={a.nickname} className="text-xs text-muted">
                <span className="text-text">{a.nickname}</span>
                {a.arrives === 'new'
                  ? ': added to your prompts'
                  : a.arrives === 'same'
                    ? ': yours is the same'
                    : ': yours differs, and yours is kept'}
              </li>
            ))}
          </ul>
        </section>
      )}
      {p.missing.length > 0 && (
        <section aria-labelledby={`${id}-missing`} className="flex flex-col gap-2">
          <h3 id={`${id}-missing`} className={captionClass}>
            What this Mac doesn’t have
          </h3>
          {p.missing.map((r, i) => (
            <div key={refKey(r)} className="flex flex-col gap-1">
              <label htmlFor={`${id}-${String(i)}`} className="text-xs text-text">
                {`${refWords(r)}, used by ${r.routines.join(', ')}`}
              </label>
              {!r.inherits && (
                <select
                  id={`${id}-${String(i)}`}
                  value={chosen[refKey(r)] ?? ''}
                  onChange={(e) => {
                    onChoose(refKey(r), e.target.value)
                  }}
                  className={`${fieldClass} py-1`}
                >
                  <option value="">{inheritWords(r)}</option>
                  {r.choices.map((c) => (
                    <option key={c} value={c}>
                      {c}
                    </option>
                  ))}
                </select>
              )}
            </div>
          ))}
        </section>
      )}
    </div>
  )
}

interface Importing {
  path: string
  preview: AutopilotImportPreview
}

// useRoutineTransfer is Export and Import for the Autopilot tab: Export asks where to save
// the routines (all of them, or one) and whether to include the text of the prompts they run;
// Import asks for a file, previews it in a dialog with a choice for each thing its routines
// name that this Mac lacks, and brings the routines in switched off. Each says what it did
// through say, and its dialogs are node.
export function useRoutineTransfer(
  say: (text: string, tone?: 'error') => void,
  onImported: () => void,
): {
  exportRoutines: (routines: readonly AutopilotRoutine[]) => void
  importRoutines: () => void
  node: ReactNode
} {
  const [exporting, setExporting] = useState<readonly AutopilotRoutine[] | null>(null)
  const [withPrompts, setWithPrompts] = useState(true)
  const [importing, setImporting] = useState<Importing | null>(null)
  const [chosen, setChosen] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const promptsId = useId()

  const startImport = async () => {
    const picked = await pickFileToOpen({ title: 'Import routines', button: 'Read', filters: routineFiles })

    if (!picked.ok) {
      say(picked.error, 'error')

      return
    }

    const path = picked.data

    if (path === '') {
      return
    }

    const read = await attempt(previewRoutineImport(path))

    if (read.ok) {
      setChosen({})
      setImporting({ path, preview: read.data })
    } else {
      say(read.error, 'error')
    }
  }

  const finishImport = async (i: Importing) => {
    setBusy(true)

    const done = await attempt(
      importRoutines({ path: i.path, sha256: i.preview.sha256, map: choicesOf(i.preview, chosen) }),
    )

    setBusy(false)
    setImporting(null)

    if (done.ok) {
      onImported()
      say(importedWords(done.data.routines, done.data.prompts))
    } else {
      say(done.error, 'error')
    }
  }

  const finishExport = async (routines: readonly AutopilotRoutine[]) => {
    const one = routines.length === 1 ? routines[0] : undefined
    const filename = `${one === undefined ? `${brand.slug}-routines` : one.name.replace(/[^\w-]+/g, '-').toLowerCase()}.json`
    const picked = await pickFileToSave({ title: 'Export routines', button: 'Export', filters: routineFiles, filename })

    setExporting(null)

    if (!picked.ok) {
      say(picked.error, 'error')

      return
    }

    const path = picked.data

    if (path === '') {
      return
    }

    const done = await attempt(
      exportRoutines(
        path,
        routines.map((r) => r.id),
        withPrompts,
      ),
    )

    if (done.ok) {
      const prompts = done.data.prompts > 0 ? `, with ${counted(done.data.prompts, 'prompt', 'prompts')}` : ''

      say(`Exported ${counted(done.data.routines, 'routine', 'routines')}${prompts}.`)
    } else {
      say(done.error, 'error')
    }
  }

  const node = (
    <>
      {exporting !== null && (
        <ConfirmDialog
          title={exporting.length === 1 ? `Export “${exporting[0]?.name ?? ''}”?` : 'Export your routines?'}
          confirmLabel="Choose where…"
          tone="primary"
          onConfirm={() => {
            void finishExport(exporting)
          }}
          onCancel={() => {
            setExporting(null)
          }}
        >
          <div className="flex flex-col gap-2">
            <p>
              The file holds the routines’ definitions, with your org and repository names, so it’s yours to share or
              not. It never holds their history.
            </p>
            <label htmlFor={promptsId} className="flex items-center gap-2 text-sm text-text">
              <input
                id={promptsId}
                type="checkbox"
                checked={withPrompts}
                onChange={(e) => {
                  setWithPrompts(e.target.checked)
                }}
                className="checkbox"
              />
              Include the text of the prompts they run
            </label>
          </div>
        </ConfirmDialog>
      )}
      {importing !== null && (
        <ConfirmDialog
          title="Import routines?"
          confirmLabel={`Import ${counted(importing.preview.routines.length, 'routine', 'routines')}`}
          tone="primary"
          size="wide"
          busy={busy}
          confirmDisabled={importing.preview.routines.length === 0}
          onConfirm={() => {
            void finishImport(importing)
          }}
          onCancel={() => {
            setImporting(null)
          }}
        >
          <ImportPreview
            preview={importing.preview}
            chosen={chosen}
            onChoose={(key, to) => {
              setChosen({ ...chosen, [key]: to })
            }}
          />
        </ConfirmDialog>
      )}
    </>
  )

  return {
    exportRoutines: (routines) => {
      setExporting(routines)
    },
    importRoutines: () => {
      void startImport()
    },
    node,
  }
}
