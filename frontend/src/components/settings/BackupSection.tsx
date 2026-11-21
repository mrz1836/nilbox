import { useId, useState } from 'react'

import { brand } from '../../generated/brand'
import { backupFiles, backupSections, doneText, neverBacked, sectionLabel } from '../../lib/backup'
import { attempt, type Result } from '../../lib/call'
import { pickFileToOpen, pickFileToSave } from '../../lib/dialogs'
import { fileDate, fileName } from '../../lib/format'
import type { Theme } from '../../lib/theme'
import { useMutation } from '../../lib/useMutation'
import {
  exportBackup,
  getSettings,
  type BackupExported,
  type BackupImported,
  type BackupSection,
  type Settings,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { buttonClass } from '../controls'
import { ExportIcon, ImportIcon } from '../icons'
import { ErrorText, Note, SectionIntro } from './rows'
import { ImportBackupDialog } from './ImportBackupDialog'

export interface BackupSectionProps {
  // onSaved receives every setting as stored once an import brought them in.
  onSaved: (settings: Settings) => void
  // onLaunchChanged reloads the prompts and launch defaults once an import brought them in.
  onLaunchChanged: () => Promise<void>
  // theme is the theme the window shows, and onThemeChange switches it, as an import that
  // brings another one in does.
  theme: Theme
  onThemeChange: (theme: Theme) => Promise<Result<void>>
}

// exportedText says what an export wrote, and where, as in "Backed up to “x.json”:
// Settings (24), Prompts and replies (12)."
function exportedText(result: BackupExported): string {
  const held = result.sections.map((s) => `${sectionLabel(s.section)} (${String(s.items)})`).join(', ')

  return `Backed up to “${fileName(result.path)}”: ${held}.`
}

// importedText says what an import did, as in "Imported Settings (1 changed), Your
// scripts (2 added)."
function importedText(result: BackupImported): string {
  return `Imported ${result.sections.map((s) => `${sectionLabel(s.section)} (${doneText(s.count)})`).join(', ')}.`
}

// Outcome is what the last export or import did, as the section shows it.
type Outcome =
  { tone: 'success'; text: string; notes: readonly string[]; savedTo: string } | { tone: 'error'; text: string }

// BackupSection backs the user's whole setup up to a file, and brings one back: Export
// asks which categories to include, then where to save them; Import asks for a backup,
// then shows what importing it would do before anything changes. After an import the
// window reads the settings, the prompts, and the launch defaults again.
export function BackupSection({ onSaved, onLaunchChanged, theme, onThemeChange }: BackupSectionProps) {
  const id = useId()
  // choosing is set while the export dialog asks which categories to include.
  const [choosing, setChoosing] = useState(false)
  const [left, setLeft] = useState<ReadonlySet<BackupSection>>(new Set())
  // opened is the backup the import dialog shows, or ''.
  const [opened, setOpened] = useState('')
  const [exported, setExported] = useState<Outcome | null>(null)
  const [imported, setImported] = useState<Outcome | null>(null)
  const exporter = useMutation((path: string, sections: BackupSection[]) => exportBackup(path, sections))

  const included = backupSections.flatMap(({ section }) => (left.has(section) ? [] : [section]))

  // save asks where to save the categories chosen, then writes the backup there.
  const save = async () => {
    setChoosing(false)

    const picked = await pickFileToSave({
      title: 'Back up your setup',
      button: 'Save',
      filters: backupFiles,
      filename: `${brand.name} backup ${fileDate(new Date())}.json`,
    })

    if (!picked.ok) {
      setExported({ tone: 'error', text: picked.error })

      return
    }

    if (picked.data === '') {
      return
    }

    const result = await exporter.run(picked.data, included)

    setExported(
      result.ok
        ? { tone: 'success', text: exportedText(result.data), notes: [], savedTo: '' }
        : { tone: 'error', text: result.error },
    )
  }

  // open asks for a backup, then shows what importing it would do.
  const open = async () => {
    setImported(null)

    const picked = await pickFileToOpen({ title: 'Restore from a backup', button: 'Open', filters: backupFiles })

    if (!picked.ok) {
      setImported({ tone: 'error', text: picked.error })
    } else if (picked.data !== '') {
      setOpened(picked.data)
    }
  }

  // done shows what an import did, once the window has read what it brought in again.
  const done = async (result: BackupImported) => {
    setOpened('')

    const reread = await attempt(getSettings())

    if (reread.ok) {
      onSaved(reread.data)

      if (reread.data.theme !== theme) {
        void onThemeChange(reread.data.theme)
      }
    }

    await onLaunchChanged()

    setImported({
      tone: 'success',
      text: importedText(result),
      notes: result.sections.flatMap((s) => s.notes),
      savedTo: result.savedTo,
    })
  }

  return (
    <div className="flex flex-col gap-8">
      <div className="flex flex-col gap-3">
        <SectionIntro
          title="Back up your setup"
          action={
            <button
              type="button"
              disabled={exporter.pending}
              onClick={() => {
                setExported(null)
                setLeft(new Set())
                setChoosing(true)
              }}
              className={buttonClass}
            >
              <ExportIcon className="size-4" />
              {exporter.pending ? 'Backing up…' : 'Export…'}
            </button>
          }
        >
          Saves your settings, repositories, prompts and replies, launch defaults, your own scripts, and your Autopilot
          routines to one file, readable only by you, to keep or to bring to another Mac.
        </SectionIntro>
        <OutcomeLine outcome={exported} />
      </div>

      <div className="flex flex-col gap-3">
        <SectionIntro
          title="Restore from a backup"
          action={
            <button
              type="button"
              onClick={() => {
                void open()
              }}
              className={buttonClass}
            >
              <ImportIcon className="size-4" />
              Import…
            </button>
          }
        >
          Shows what a backup holds and what bringing it in would change, before anything does. Merge adds and changes;
          Replace makes each category exactly the backup’s. Your setup as it is now is saved first.
        </SectionIntro>
        <OutcomeLine outcome={imported} />
      </div>

      <div className="flex flex-col gap-3">
        <SectionIntro id={`${id}-never`} title="What a backup never holds" />
        <ul aria-labelledby={`${id}-never`} className="flex list-disc flex-col gap-1 pl-5 text-xs text-muted">
          {neverBacked.map((line) => (
            <li key={line}>{line}</li>
          ))}
        </ul>
      </div>

      {choosing && (
        <ConfirmDialog
          title="Back up your setup"
          confirmLabel="Choose where to save…"
          tone="primary"
          confirmDisabled={included.length === 0}
          onCancel={() => {
            setChoosing(false)
          }}
          onConfirm={() => {
            void save()
          }}
        >
          <fieldset>
            <legend>Choose what to include. Each category stays out of the file unless it is ticked.</legend>
            <ul className="mt-3 flex flex-col gap-1">
              {backupSections.map(({ section, label, about }) => (
                <li key={section}>
                  <label className="grid cursor-pointer grid-cols-[auto_minmax(0,1fr)] items-start gap-x-2.5 gap-y-0.5 rounded-md border border-border px-3 py-2">
                    <input
                      type="checkbox"
                      checked={!left.has(section)}
                      onChange={(ev) => {
                        const on = ev.target.checked

                        setLeft((current) => {
                          const next = new Set(current)

                          if (on) {
                            next.delete(section)
                          } else {
                            next.add(section)
                          }

                          return next
                        })
                      }}
                      className="checkbox row-span-2 mt-0.5"
                    />
                    <span className="text-sm font-medium text-text">{label}</span>
                    <span className="col-start-2 text-xs">{about}</span>
                  </label>
                </li>
              ))}
            </ul>
          </fieldset>
        </ConfirmDialog>
      )}

      {opened !== '' && (
        <ImportBackupDialog
          path={opened}
          onCancel={() => {
            setOpened('')
          }}
          onImported={(result) => {
            void done(result)
          }}
        />
      )}
    </div>
  )
}

// OutcomeLine shows what the last export or import did: a status line, with the notes it
// left and where the setup as it was is saved, or why it failed.
function OutcomeLine({ outcome }: { outcome: Outcome | null }) {
  if (outcome === null) {
    return null
  }

  if (outcome.tone === 'error') {
    return <ErrorText error={outcome.text} />
  }

  return (
    <div role="status" className="flex flex-col gap-1">
      <p className="text-xs text-success">{outcome.text}</p>
      {outcome.savedTo !== '' && (
        <Note>
          Your setup as it was before is saved as <span className="font-mono text-text">{outcome.savedTo}</span>.
        </Note>
      )}
      {outcome.notes.length > 0 && (
        <ul className="flex flex-col gap-0.5 text-xs text-muted">
          {outcome.notes.map((note) => (
            <li key={note}>{note}</li>
          ))}
        </ul>
      )}
    </div>
  )
}
