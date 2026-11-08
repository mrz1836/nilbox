import { useId, useState } from 'react'

import { attempt } from '../../lib/call'
import { kindPlural, promptApplies } from '../../lib/launch'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  setDefaultProfile,
  setDefaultPrompt,
  setTerminal,
  setWorktreeAutoCleanup,
  setWorktreeDefault,
  sweepWorktrees,
  type DefaultPrompts,
  type Profile,
  type Prompt,
  type Settings,
  type TerminalID,
} from '../../platform'
import { fieldClass } from '../controls'
import { Group, rowDescribedBy, SettingRow, SwitchRow } from './rows'
import type { SectionProps } from './types'

const terminals: readonly { id: TerminalID; label: string }[] = [
  { id: 'iterm2', label: 'iTerm2' },
  { id: 'terminal', label: 'Terminal' },
]

interface LaunchingSectionProps extends SectionProps {
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  // defaultPrompts is null until it has loaded.
  defaultPrompts: DefaultPrompts | null
  // onLaunchChanged reloads the prompts and launch defaults after a change; it resolves
  // once they are in.
  onLaunchChanged: () => Promise<void>
}

// LaunchingSection holds the app defaults every launch starts from: the terminal, the
// profile, the prompt for each kind of item, and the worktree a session runs in. Each is
// saved as soon as it changes, with why a change was refused shown under its row.
export function LaunchingSection({
  settings: stored,
  profiles,
  prompts,
  defaultPrompts,
  onSaved,
  onLaunchChanged,
}: LaunchingSectionProps) {
  const id = useId()
  const { settings, save } = useSettingsSave(stored, onSaved)
  const [errors, setErrors] = useState({ terminal: '', profile: '', worktree: '', cleanup: '', pr: '', issue: '' })

  // shows sets the message under one field: why its change was refused, or ''.
  const shows = (field: keyof typeof errors, message: string) => {
    setErrors((was) => ({ ...was, [field]: message }))
  }

  // store saves one change, showing why it was refused under its field.
  const store = async (field: keyof typeof errors, change: Partial<Settings>, call: () => PromiseLike<Settings>) => {
    shows(field, '')

    const result = await save(change, call)

    if (!result.ok) {
      shows(field, result.error)
    }
  }

  // savePrompt makes a prompt the app's default for a kind of item, then loads the
  // prompts again, since which one is the default shows in the library.
  const savePrompt = async (kind: 'pr' | 'issue', promptId: number) => {
    shows(kind, '')

    const result = await attempt(setDefaultPrompt(kind, promptId))

    if (result.ok) {
      await onLaunchChanged()
    } else {
      shows(kind, result.error)
    }
  }

  return (
    <Group>
      <SettingRow
        label="Terminal"
        controlId={`${id}-terminal`}
        description="The app new sessions open in."
        error={errors.terminal}
      >
        <select
          id={`${id}-terminal`}
          aria-describedby={rowDescribedBy(`${id}-terminal`, errors.terminal)}
          value={settings.terminal}
          onChange={(e) => {
            const terminal = terminals.find((t) => t.id === e.target.value)?.id

            if (terminal !== undefined) {
              void store('terminal', { terminal }, () => setTerminal(terminal))
            }
          }}
          className={fieldClass}
        >
          {terminals.map((t) => (
            <option key={t.id} value={t.id}>
              {t.label}
            </option>
          ))}
        </select>
      </SettingRow>
      <SettingRow
        label="Default profile"
        controlId={`${id}-profile`}
        description={
          profiles.length === 0
            ? 'No agent profiles were found in your home directory.'
            : "Preselected in each row's profile menu."
        }
        error={errors.profile}
      >
        <select
          id={`${id}-profile`}
          aria-describedby={rowDescribedBy(`${id}-profile`, errors.profile)}
          value={settings.defaultProfile}
          disabled={profiles.length === 0}
          onChange={(e) => {
            const defaultProfile = e.target.value

            void store('profile', { defaultProfile }, () => setDefaultProfile(defaultProfile))
          }}
          className={fieldClass}
        >
          {profiles.map((p) => (
            <option key={p.name} value={p.name}>
              {p.name}
            </option>
          ))}
        </select>
      </SettingRow>
      {(['pr', 'issue'] as const).map((kind) => (
        <SettingRow
          key={kind}
          label={kind === 'pr' ? 'Prompt for pull requests' : 'Prompt for issues'}
          controlId={`${id}-prompt-${kind}`}
          description={`What a launch on ${kindPlural(kind)} asks, unless an org or repository chooses another. Edit prompts on the Prompts tab.`}
          error={errors[kind]}
        >
          <select
            id={`${id}-prompt-${kind}`}
            aria-describedby={rowDescribedBy(`${id}-prompt-${kind}`, errors[kind])}
            value={defaultPrompts === null ? 0 : defaultPrompts[kind]}
            disabled={defaultPrompts === null}
            onChange={(e) => {
              void savePrompt(kind, Number(e.target.value))
            }}
            className={fieldClass}
          >
            {prompts
              .filter((p) => promptApplies(p, kind))
              .map((p) => (
                <option key={p.id} value={p.id}>
                  {p.nickname}
                </option>
              ))}
          </select>
        </SettingRow>
      ))}
      <SwitchRow
        label="Start in a worktree"
        controlId={`${id}-worktree`}
        description="Presets each row's “Start in a worktree” box. You can still change it per launch."
        error={errors.worktree}
        checked={settings.worktreeDefault}
        onChange={(worktreeDefault) => {
          void store('worktree', { worktreeDefault }, () => setWorktreeDefault(worktreeDefault))
        }}
      />
      <SwitchRow
        label="Clean up after merging"
        controlId={`${id}-cleanup`}
        description="Removes a session's worktree, with its own worktree-… branch, once its pull request merges or its issue closes, if it has no uncommitted changes, all its commits are on GitHub, and no session runs in it. Checked every few minutes; ignored files in it go with it."
        error={errors.cleanup}
        checked={settings.worktreeAutoCleanup}
        onChange={(worktreeAutoCleanup) => {
          // Turned on, it sweeps at once rather than at the next check.
          const call = () =>
            setWorktreeAutoCleanup(worktreeAutoCleanup).then((saved) => {
              if (worktreeAutoCleanup) {
                sweepWorktrees().catch((err: unknown) => {
                  console.error('could not sweep the worktrees', err)
                })
              }

              return saved
            })

          void store('cleanup', { worktreeAutoCleanup }, call)
        }}
      />
    </Group>
  )
}
