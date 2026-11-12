import { startTransition, useActionState, useId, useState, type SubmitEvent } from 'react'

import { attempt } from '../../lib/call'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  chooseFolder,
  setCloneRoot,
  type DefaultPrompts,
  type LaunchDefaults,
  type AgentInfo,
  type Profile,
  type Prompt,
} from '../../platform'
import { buttonClass, fieldClass } from '../controls'
import { CloneLocations } from './CloneLocations'
import { LaunchDefaultsSection } from './LaunchDefaultsSection'
import { Group, rowDescribedBy, SettingRow } from './rows'
import type { SectionProps } from './types'

interface RepositoriesSectionProps extends SectionProps {
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  // defaultPrompts is null until it has loaded.
  defaultPrompts: DefaultPrompts | null
  launchDefaults: readonly LaunchDefaults[]
  // onLaunchChanged reloads the prompts and launch defaults after a change; it resolves
  // once they are in.
  onLaunchChanged: () => Promise<void>
}

// CloneRootSave is how the last Save of the clone root went: the draft it saved, or why
// it was refused.
interface CloneRootSave {
  saved: string | null
  error: string
}

// RepositoriesSection holds where the local clones live: the clone root, which new clones
// go in, saved with its Save button or as soon as Choose… picks a folder, and the other
// repository locations; and the launch defaults for orgs and repositories.
export function RepositoriesSection({
  settings: stored,
  onSaved,
  agents,
  profiles,
  prompts,
  defaultPrompts,
  launchDefaults,
  onLaunchChanged,
}: RepositoriesSectionProps) {
  const id = useId()
  const { settings, save } = useSettingsSave(stored, onSaved)
  const [draft, setDraft] = useState(settings.cloneRoot)
  const [outcome, saveCloneRoot, saving] = useActionState(
    async (_: CloneRootSave, typed: string): Promise<CloneRootSave> => {
      const cloneRoot = typed.trim()
      const result = await save({ cloneRoot }, () => setCloneRoot(cloneRoot))

      return result.ok ? { saved: typed, error: '' } : { saved: null, error: result.error }
    },
    { saved: null, error: '' },
  )
  // Both wait while a save runs; Saved shows until the draft changes.
  const error = saving ? '' : outcome.error
  const saved = !saving && outcome.saved === draft

  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault()
    startTransition(() => {
      saveCloneRoot(draft)
    })
  }

  // choose picks the clone root in the system's folder dialog and saves it at once.
  const choose = async () => {
    const chosen = await attempt(chooseFolder({ title: 'Choose the clone root', button: 'Use' }))

    if (chosen.ok && chosen.data !== '') {
      setDraft(chosen.data)
      startTransition(() => {
        saveCloneRoot(chosen.data)
      })
    }
  }

  return (
    <div className="flex flex-col gap-8">
      <Group>
        <form onSubmit={submit}>
          <SettingRow
            label="Clone root"
            controlId={`${id}-clone-root`}
            description="The folder new clones go in, such as ~/projects. Launches, the Worktrees view, and the Scripts pages look one level inside it, and in your other repository locations below."
            error={error}
          >
            <input
              id={`${id}-clone-root`}
              type="text"
              aria-describedby={rowDescribedBy(`${id}-clone-root`, error)}
              value={draft}
              spellCheck={false}
              onChange={(e) => {
                setDraft(e.target.value)
              }}
              className={`${fieldClass} w-56 font-mono text-[0.8125rem]`}
            />
            <button
              type="button"
              disabled={saving}
              onClick={() => {
                void choose()
              }}
              className={buttonClass}
            >
              Choose…
            </button>
            <button type="submit" className={buttonClass}>
              Save
            </button>
          </SettingRow>
        </form>
        {saved && (
          <p role="status" className="px-4 py-2 text-xs text-success">
            Saved.
          </p>
        )}
      </Group>
      <CloneLocations settings={settings} save={save} />
      <LaunchDefaultsSection
        settings={settings}
        agents={agents}
        profiles={profiles}
        prompts={prompts}
        defaultPrompts={defaultPrompts}
        launchDefaults={launchDefaults}
        onChanged={onLaunchChanged}
      />
    </div>
  )
}
