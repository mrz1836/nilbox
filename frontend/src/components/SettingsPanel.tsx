import { useId, useState, type SubmitEvent } from 'react'

import { errorMessage } from '../lib/format'
import { setCloneRoot, setTerminal, type Settings } from '../platform'

const terminals: readonly { id: string; label: string }[] = [
  { id: 'iterm2', label: 'iTerm2' },
  { id: 'terminal', label: 'Terminal' },
]

export interface SettingsPanelProps {
  // id identifies the panel for the header's toggle.
  id: string
  settings: Settings
  // onSaved receives the settings after a change was stored.
  onSaved: (settings: Settings) => void
}

// SettingsPanel edits the launch settings: the terminal launches open in, saved as soon
// as it is picked, and the clone root that clones and worktrees are found under.
export function SettingsPanel({ id, settings, onSaved }: SettingsPanelProps) {
  const fieldId = useId()
  const [cloneRoot, setCloneRootDraft] = useState(settings.cloneRoot)
  const [terminalError, setTerminalError] = useState('')
  const [cloneRootError, setCloneRootError] = useState('')
  const [saved, setSaved] = useState(false)

  const pickTerminal = (terminal: string) => {
    setTerminalError('')

    setTerminal(terminal)
      .then(() => {
        onSaved({ ...settings, terminal })
      })
      .catch((err: unknown) => {
        setTerminalError(errorMessage(err))
      })
  }

  const saveCloneRoot = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault()
    setCloneRootError('')
    setSaved(false)

    const root = cloneRoot.trim()

    setCloneRoot(root)
      .then(() => {
        setSaved(true)
        onSaved({ ...settings, cloneRoot: root })
      })
      .catch((err: unknown) => {
        setCloneRootError(errorMessage(err))
      })
  }

  return (
    <section
      id={id}
      aria-labelledby={`${fieldId}-title`}
      className="border-b border-border bg-surface px-4 py-3 text-sm"
    >
      <h2 id={`${fieldId}-title`} className="mb-2 text-xs font-semibold tracking-wide text-muted uppercase">
        Settings
      </h2>
      <div className="flex flex-wrap items-start gap-6">
        <div className="flex flex-col gap-1">
          <label htmlFor={`${fieldId}-terminal`} className="font-medium">
            Terminal
          </label>
          <select
            id={`${fieldId}-terminal`}
            value={settings.terminal}
            onChange={(e) => {
              pickTerminal(e.target.value)
            }}
            className="rounded-md border border-border bg-surface px-2 py-1"
          >
            {terminals.map((t) => (
              <option key={t.id} value={t.id}>
                {t.label}
              </option>
            ))}
          </select>
          {terminalError !== '' && (
            <p role="alert" className="text-xs text-danger">
              {terminalError}
            </p>
          )}
        </div>
        <form onSubmit={saveCloneRoot} className="flex flex-col gap-1">
          <label htmlFor={`${fieldId}-clone-root`} className="font-medium">
            Clone root
          </label>
          <div className="flex gap-2">
            <input
              id={`${fieldId}-clone-root`}
              type="text"
              value={cloneRoot}
              spellCheck={false}
              onChange={(e) => {
                setCloneRootDraft(e.target.value)
                setSaved(false)
              }}
              className="w-64 rounded-md border border-border bg-surface px-2 py-1"
            />
            <button type="submit" className="rounded-md border border-border px-3 py-1 hover:bg-raised">
              Save
            </button>
          </div>
          <p className="text-xs text-muted">The folder your local clones live in, such as ~/projects.</p>
          {cloneRootError !== '' && (
            <p role="alert" className="text-xs text-danger">
              {cloneRootError}
            </p>
          )}
          {saved && (
            <p role="status" className="text-xs text-success">
              Saved.
            </p>
          )}
        </form>
      </div>
    </section>
  )
}
