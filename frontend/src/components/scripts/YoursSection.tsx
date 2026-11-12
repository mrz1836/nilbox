import { useState } from 'react'

import { otherPlace } from '../../lib/repoScripts'
import { aboutScript, placeName, placeOf } from '../../lib/userScripts'
import type { ScriptAsk } from '../../lib/useScriptRun'
import {
  deleteUserScript,
  duplicateUserScript,
  moveUserScript,
  type RepoScripts,
  type RunsIn,
  type ScriptEntry,
  type ScriptSection,
  type UserScript,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { AlertIcon } from '../icons'
import { GroupHeading } from './GroupHeading'
import { ScriptRow } from './ScriptRow'
import { commandClass } from './TrustDialog'

// Yours is one of the user's own scripts as the page lists it, with the script as saved.
type Yours = ScriptEntry & { mine: UserScript }

export interface YoursSectionProps {
  page: RepoScripts
  section: ScriptSection
  headingId: string
  // open shows the scripts under the heading, and onToggle opens or folds them.
  open: boolean
  onToggle: () => void
  // pending holds the Run buttons while a run is being asked for; onRun asks for one.
  pending: boolean
  onRun: (ask: ScriptAsk) => void
  // setting is where runs go, as set, and terminalName the terminal they go to there.
  setting: RunsIn
  terminalName: string
  // onOpenRelease opens the Release page, or is null while it isn't on.
  onOpenRelease: (() => void) | null
  // onEdit opens the editor on a script; onChange makes a change to the scripts, after
  // which the page reads them again.
  onEdit: (script: UserScript) => void
  onChange: (change: () => PromiseLike<unknown>) => void
}

// YoursSection is the user's own scripts that show in the clone, first on the page, in
// their order: each with the command it runs, where it shows, and Run, in the place it
// says or else where runs go, and a menu that runs it in the other place, edits it,
// duplicates it, moves it up or down among those shown here, or deletes it, asking first.
// With none here, it says how to save one, or that theirs show elsewhere.
export function YoursSection({
  page,
  section,
  headingId,
  open,
  onToggle,
  pending,
  onRun,
  setting,
  terminalName,
  onOpenRelease,
  onEdit,
  onChange,
}: YoursSectionProps) {
  const [deleting, setDeleting] = useState<UserScript | null>(null)
  const entries = section.entries.filter((e): e is Yours => e.mine !== null)

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-1">
      <GroupHeading
        id={headingId}
        title={section.title}
        count={entries.length}
        open={open}
        disabled={false}
        onToggle={onToggle}
      />
      {open && section.error !== '' && (
        <p className="flex items-start gap-1.5 px-1 text-xs text-warning">
          <AlertIcon className="mt-px size-3.5 shrink-0" />
          {section.error}
        </p>
      )}
      {open && entries.length > 0 && (
        <ul aria-label={section.title} className="flex flex-col">
          {entries.map((entry, i) => {
            const { mine } = entry
            const other = otherPlace(placeOf(mine, setting))

            return (
              <ScriptRow
                key={entry.ref}
                entry={entry}
                args={[]}
                pending={pending}
                command
                about={aboutScript(mine, terminalName)}
                other={other}
                otherName={placeName(other, terminalName)}
                onRun={onRun}
                onOpenRelease={onOpenRelease}
                more={[
                  {
                    label: 'Edit…',
                    onSelect: () => {
                      onEdit(mine)
                    },
                  },
                  {
                    label: 'Duplicate',
                    onSelect: () => {
                      onChange(() => duplicateUserScript(mine.id))
                    },
                  },
                  {
                    label: 'Move up',
                    disabled: i === 0,
                    onSelect: () => {
                      onChange(() => moveUserScript(mine.id, 'up', page.dir))
                    },
                  },
                  {
                    label: 'Move down',
                    disabled: i === entries.length - 1,
                    onSelect: () => {
                      onChange(() => moveUserScript(mine.id, 'down', page.dir))
                    },
                  },
                  {
                    label: 'Delete…',
                    danger: true,
                    divided: true,
                    onSelect: () => {
                      setDeleting(mine)
                    },
                  },
                ]}
              />
            )
          })}
        </ul>
      )}
      {open && entries.length === 0 && section.error === '' && (
        <p className="px-1 text-sm text-muted">
          {section.found
            ? `None of your scripts show in ${page.name}.`
            : 'Save a command you run often with New script, and it shows here.'}
        </p>
      )}
      {deleting !== null && (
        <ConfirmDialog
          title={`Delete “${deleting.name}”?`}
          confirmLabel="Delete"
          onConfirm={() => {
            const { id } = deleting

            setDeleting(null)
            onChange(() => deleteUserScript(id))
          }}
          onCancel={() => {
            setDeleting(null)
          }}
        >
          It no longer shows in any repository. Its runs stay in Runs.
        </ConfirmDialog>
      )}
    </section>
  )
}

export interface AskFirstProps {
  // ask is the run asked for, and script the user's script it runs.
  ask: ScriptAsk
  script: UserScript
  // dir is the clone it runs in, with ~; place where it goes, by name.
  dir: string
  place: string
  // onRun runs it; onCancel runs nothing.
  onRun: () => void
  onCancel: () => void
}

// AskFirst is the plain confirmation one of the user's own scripts set to ask before
// running asks: the command, and where it runs. It is the user's own choice, which the
// window keeps, not a question of trust.
export function AskFirst({ ask, script, dir, place, onRun, onCancel }: AskFirstProps) {
  return (
    <ConfirmDialog
      title={`Run “${script.name}”?`}
      confirmLabel={ask.anyway ? 'Run it anyway' : 'Run'}
      tone="primary"
      size="wide"
      onConfirm={onRun}
      onCancel={onCancel}
    >
      <div className="flex flex-col gap-2">
        <pre className={commandClass}>{ask.entry.command}</pre>
        <p>
          It runs in <span className="font-mono break-all text-text">{dir}</span>, in {place}.
        </p>
      </div>
    </ConfirmDialog>
  )
}
