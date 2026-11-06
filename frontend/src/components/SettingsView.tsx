import { useEffect, useId, useState, type ReactNode, type SubmitEvent } from 'react'

import { brand } from '../generated/brand'
import { counted, errorMessage } from '../lib/format'
import { kindPlural, promptApplies } from '../lib/launch'
import {
  commentThresholds,
  commitThresholds,
  reviewWakes,
  snoozeChoices,
  snoozeLength,
  snoozeShort,
} from '../lib/triage'
import { presetFor, sizeKey, sizeLabel, windowPresets } from '../lib/windowSize'
import {
  getAppInfo,
  openInBrowser,
  setCloneRoot,
  setDefaultPrompt,
  setDefaultProfile,
  setSnoozeDays,
  setSnoozeWake,
  setTerminal,
  setWindowSize,
  setWorktreeAutoCleanup,
  setWorktreeDefault,
  sweepWorktrees,
  type Account,
  type DefaultPrompts,
  type Info,
  type LaunchDefaults,
  type Profile,
  type Prompt,
  type Settings,
  type SnoozeRules,
} from '../platform'
import { Avatar } from './Avatar'
import { Segmented } from './controls'
import { ChevronLeftIcon, ExternalIcon } from './icons'
import { LaunchDefaultsSection } from './LaunchDefaultsSection'
import { Logo } from './Logo'
import { TrustSection } from './TrustSection'

// SettingsSection is one section of the settings page.
export type SettingsSection =
  'account' | 'snoozing' | 'window' | 'launching' | 'profiles' | 'repositories' | 'trust' | 'about'

type SectionId = SettingsSection

// sectionOrder is the sidebar's order; sections holds each one's heading and summary.
const sectionOrder: readonly SectionId[] = [
  'account',
  'snoozing',
  'window',
  'launching',
  'profiles',
  'repositories',
  'trust',
  'about',
]

const sections: Readonly<Record<SectionId, { label: string; description: string }>> = {
  account: { label: 'Account', description: 'The GitHub account your inbox comes from.' },
  snoozing: {
    label: 'Snoozing',
    description: 'How long a snooze lasts, and what new activity brings an item back early.',
  },
  window: { label: 'Window', description: 'The size the window opens at.' },
  launching: {
    label: 'Launching',
    description: 'How Open in Claude starts a coding session: the app defaults every launch starts from.',
  },
  profiles: {
    label: 'Profiles',
    description: 'The agent configurations a session can start with, and the folder each one runs from.',
  },
  repositories: {
    label: 'Repositories',
    description: 'Where your local clones live, and the launch defaults for orgs and repositories.',
  },
  trust: {
    label: 'Trust',
    description: 'Who launches trust with the profile’s permissions, and which bots count.',
  },
  about: { label: 'About', description: '' },
}

// permissionLabels name a profile's permission mode for people. A mode not listed here
// shows as stored.
const permissionLabels: Readonly<Record<string, string>> = {
  full: 'Full access',
  bypassPermissions: 'Full access',
  '': 'Asks first',
  default: 'Asks first',
  acceptEdits: 'Accepts edits',
  plan: 'Plans only',
}

const terminals: readonly { id: string; label: string }[] = [
  { id: 'iterm2', label: 'iTerm2' },
  { id: 'terminal', label: 'Terminal' },
]

const fieldClass = 'rounded-md border border-border bg-surface px-2.5 py-1.5 text-sm disabled:opacity-60'
const buttonClass = 'rounded-md border border-border bg-surface px-3 py-1.5 text-sm font-medium hover:bg-raised'

// A native checkbox drawn as a switch; role="switch" makes assistive tech announce on/off.
const switchClass =
  'relative h-5 w-9 shrink-0 appearance-none rounded-full bg-border transition-colors checked:bg-accent before:absolute before:top-0.5 before:left-0.5 before:size-4 before:rounded-full before:bg-white before:shadow before:transition-transform checked:before:translate-x-4'

export interface SettingsViewProps {
  // initialSection is the section the page opens at; it defaults to the account.
  initialSection?: SettingsSection
  // settings is null until it has loaded; the launch and clone sections wait for it.
  settings: Settings | null
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  // defaultPrompts is null until it has loaded.
  defaultPrompts: DefaultPrompts | null
  launchDefaults: readonly LaunchDefaults[]
  // account is null until the inbox has loaded.
  account: Account | null
  // onSaved receives the settings after a change was stored.
  onSaved: (settings: Settings) => void
  // onLaunchChanged reloads the prompts and launch defaults after a change; it resolves
  // once they are in.
  onLaunchChanged: () => Promise<void>
  // onClose leaves the settings page for the view it was opened from.
  onClose: () => void
}

// Input types that take no typing, so Escape in them is free to close the page.
const untypedInputs = new Set(['checkbox', 'radio', 'button', 'submit', 'reset', 'range', 'color', 'file'])

// isTyping reports whether a key event belongs to editing: a text field or any control in
// a form, where leaving the page would drop an unsaved draft, or an input method
// mid-composition.
function isTyping(e: KeyboardEvent): boolean {
  const target = e.target

  return (
    e.isComposing ||
    target instanceof HTMLTextAreaElement ||
    (target instanceof HTMLInputElement && !untypedInputs.has(target.type)) ||
    (target instanceof Element && target.closest('form') !== null)
  )
}

interface SettingRowProps {
  label: string
  // controlId is the id of the control the label names and the description describes.
  controlId: string
  description: ReactNode
  error?: string
  children: ReactNode
}

// SettingRow lays out one setting: its name and help on the left, the control on the right.
function SettingRow({ label, controlId, description, error = '', children }: SettingRowProps) {
  return (
    <div className="flex items-center justify-between gap-8 px-4 py-3.5">
      <div className="min-w-0">
        <label htmlFor={controlId} className="text-sm font-medium">
          {label}
        </label>
        <p id={`${controlId}-help`} className="mt-0.5 text-xs text-muted">
          {description}
        </p>
        {error !== '' && (
          <p role="alert" className="mt-1 text-xs text-danger">
            {error}
          </p>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </div>
  )
}

// Group is a card of setting rows.
function Group({ children }: { children: ReactNode }) {
  return <div className="divide-y divide-border rounded-lg border border-border bg-surface">{children}</div>
}

function openProfile(url: string) {
  openInBrowser(url).catch((err: unknown) => {
    console.error('could not open the GitHub profile in the browser', err)
  })
}

function AccountSection({ account }: { account: Account | null }) {
  const signedIn = account !== null && account.login !== ''
  const hasName = signedIn && account.name.trim() !== ''

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-4 rounded-lg border border-border bg-surface p-4">
        <Avatar account={account} size="lg" />
        <div className="min-w-0 flex-1">
          {signedIn ? (
            <>
              <p className="truncate font-semibold">{hasName ? account.name : account.login}</p>
              <p className="truncate text-sm text-muted">@{account.login}</p>
            </>
          ) : (
            <>
              <p className="font-semibold">Not signed in yet</p>
              <p className="text-sm text-muted">Your account appears after the first refresh.</p>
            </>
          )}
        </div>
        {signedIn && (
          <button
            type="button"
            onClick={() => {
              openProfile(account.profileUrl)
            }}
            className={`${buttonClass} inline-flex items-center gap-1.5`}
          >
            View on GitHub
            <ExternalIcon className="size-3.5 text-muted" />
          </button>
        )}
      </div>
      <p className="text-xs leading-relaxed text-muted">
        {brand.name} signs in through the GitHub CLI: it borrows gh&apos;s token for each refresh and never stores it.
        To use another account, run <code className="font-mono">gh auth switch</code>, then refresh the inbox.
      </p>
    </div>
  )
}

interface SettingsSectionProps {
  settings: Settings
  onSaved: (settings: Settings) => void
}

// thresholdOption is one choice of a wake rule's count: 0 is never, 1 any.
function thresholdOption(n: number, any: string, many: (n: number) => string) {
  if (n === 0) {
    return { value: '0', label: 'Never', title: 'Never wakes a snooze' }
  }

  return n === 1
    ? { value: '1', label: 'Any', title: any }
    : { value: String(n), label: `${String(n)}+`, title: many(n) }
}

const commitOptions = commitThresholds.map((n) =>
  thresholdOption(n, 'Any push, a force-push included', (k) => `${String(k)} or more new commits`),
)

const commentOptions = commentThresholds.map((n) =>
  thresholdOption(n, 'Any new comment', (k) => `${String(k)} or more new comments`),
)

const reviewOptions: readonly { value: (typeof reviewWakes)[number]; label: string; title: string }[] = [
  { value: 'never', label: 'Never', title: 'Reviews never wake a snooze' },
  { value: 'decisions', label: 'Decisions', title: 'An approval or a request for changes' },
  { value: 'any', label: 'Any', title: 'Any review, including one that only comments' },
]

interface ChoiceRowProps {
  label: string
  description: ReactNode
  children: ReactNode
}

// ChoiceRow lays out one setting chosen with a segmented control: its name and help on
// the left, the choices on the right. The control carries its own accessible name.
function ChoiceRow({ label, description, children }: ChoiceRowProps) {
  return (
    <div className="flex items-center justify-between gap-8 px-4 py-3.5">
      <div className="min-w-0">
        <p className="text-sm font-medium">{label}</p>
        <p className="mt-0.5 text-xs text-muted">{description}</p>
      </div>
      {children}
    </div>
  )
}

// SnoozingSection holds how long the Snooze button sets an item aside and which new
// activity ends a snooze early, each saved as soon as it is chosen.
function SnoozingSection({ settings, onSaved }: SettingsSectionProps) {
  const [error, setError] = useState('')
  const wake = settings.snoozeWake

  const save = (call: Promise<void>, next: Settings) => {
    setError('')
    call
      .then(() => {
        onSaved(next)
      })
      .catch((err: unknown) => {
        setError(errorMessage(err))
      })
  }

  const chooseDays = (value: string) => {
    const snoozeDays = Number(value)

    save(setSnoozeDays(snoozeDays), { ...settings, snoozeDays })
  }

  const chooseWake = (change: Partial<SnoozeRules>) => {
    const snoozeWake = { ...wake, ...change }

    save(setSnoozeWake(snoozeWake), { ...settings, snoozeWake })
  }

  return (
    <div className="flex flex-col gap-8">
      <Group>
        <ChoiceRow
          label="Snooze for"
          description="How long a row’s Snooze button sets an item aside. Its arrow offers the other lengths for one snooze."
        >
          <Segmented
            label="Snooze for"
            value={String(settings.snoozeDays)}
            options={snoozeChoices.map((days) => ({
              value: String(days),
              label: snoozeShort(days),
              title: snoozeLength(days),
            }))}
            onChange={chooseDays}
          />
        </ChoiceRow>
      </Group>
      <div className="flex flex-col gap-3">
        <div>
          <h3 className="text-sm font-semibold">Wake early</h3>
          <p className="mt-0.5 text-xs text-muted">
            A snoozed item comes back before its time when new activity since you snoozed it reaches any of these. Its
            card then says what was new, such as <em>Back early: approved, 2 new commits</em>.
          </p>
        </div>
        <Group>
          <ChoiceRow
            label="New commits"
            description="Any counts a force-push too, except on a bot’s pull request, where a force-push is usually a rebase."
          >
            <Segmented
              label="Wake on new commits"
              value={String(wake.commits)}
              options={commitOptions}
              onChange={(v) => {
                chooseWake({ commits: Number(v) })
              }}
            />
          </ChoiceRow>
          <ChoiceRow
            label="Reviews"
            description="Decisions are approvals and requests for changes. Any adds reviews that only comment."
          >
            <Segmented
              label="Wake on reviews"
              value={wake.reviews}
              options={reviewOptions}
              onChange={(reviews) => {
                chooseWake({ reviews })
              }}
            />
          </ChoiceRow>
          <ChoiceRow
            label="New comments"
            description="Conversation and review comments, bots’ included: a higher count rides out their noise."
          >
            <Segmented
              label="Wake on new comments"
              value={String(wake.comments)}
              options={commentOptions}
              onChange={(v) => {
                chooseWake({ comments: Number(v) })
              }}
            />
          </ChoiceRow>
        </Group>
      </div>
      {error !== '' && (
        <p role="alert" className="-mt-5 text-xs text-danger">
          {error}
        </p>
      )}
      <p className="text-xs leading-relaxed text-muted">
        A snoozed item waits in its list&apos;s <strong className="font-medium">Snoozed</strong> tab, and an item back
        on time is marked <em>Back from snooze</em>. A dismissed item never comes back on its own: it stays in the{' '}
        <strong className="font-medium">Dismissed</strong> tab until you restore it. Both stay on this Mac, and GitHub
        never hears about them.
      </p>
    </div>
  )
}

// WindowSection holds the size the window opens at. Choosing a size stores it and resizes
// the open window to it at once.
function WindowSection({ settings, onSaved }: SettingsSectionProps) {
  const [error, setError] = useState('')
  const defaultKey = sizeKey(settings.defaultWindowSize)

  const choose = (key: string) => {
    const preset = presetFor(key)
    if (preset === undefined) {
      return
    }

    setError('')
    setWindowSize(preset.size)
      .then(() => {
        onSaved({ ...settings, windowSize: preset.size })
      })
      .catch((err: unknown) => {
        setError(errorMessage(err))
      })
  }

  return (
    <div className="flex flex-col gap-3">
      <Group>
        <div className="flex items-center justify-between gap-8 px-4 py-3.5">
          <div className="min-w-0">
            <p className="text-sm font-medium">Window size</p>
            <p className="mt-0.5 text-xs text-muted">
              Opens at {sizeLabel(settings.windowSize)}. Choosing a size resizes the window now, too.
            </p>
            {error !== '' && (
              <p role="alert" className="mt-1 text-xs text-danger">
                {error}
              </p>
            )}
          </div>
          <Segmented
            label="Window size"
            value={sizeKey(settings.windowSize)}
            options={windowPresets.map(({ label, size }) => ({
              value: sizeKey(size),
              label,
              title: sizeKey(size) === defaultKey ? `${sizeLabel(size)} (default)` : sizeLabel(size),
            }))}
            onChange={choose}
          />
        </div>
      </Group>
      <p className="text-xs leading-relaxed text-muted">
        Sizes are in points. A size larger than your screen opens as large as the screen allows, and you can still
        resize the window by dragging its edges.
      </p>
    </div>
  )
}

interface LaunchingSectionProps extends SettingsSectionProps {
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  defaultPrompts: DefaultPrompts | null
  onLaunchChanged: () => Promise<void>
}

function LaunchingSection({
  settings,
  profiles,
  prompts,
  defaultPrompts,
  onSaved,
  onLaunchChanged,
}: LaunchingSectionProps) {
  const id = useId()
  const [errors, setErrors] = useState({ terminal: '', profile: '', worktree: '', cleanup: '', pr: '', issue: '' })

  // save stores one change, then reports the new settings, or shows why it was refused.
  const save = (field: keyof typeof errors, call: Promise<void>, next: Settings) => {
    setErrors((was) => ({ ...was, [field]: '' }))

    call
      .then(() => {
        onSaved(next)
      })
      .catch((err: unknown) => {
        setErrors((was) => ({ ...was, [field]: errorMessage(err) }))
      })
  }

  // savePrompt makes a prompt the app's default for a kind of item.
  const savePrompt = (kind: 'pr' | 'issue', promptId: number) => {
    setErrors((was) => ({ ...was, [kind]: '' }))

    setDefaultPrompt(kind, promptId)
      .then(onLaunchChanged)
      .catch((err: unknown) => {
        setErrors((was) => ({ ...was, [kind]: errorMessage(err) }))
      })
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
          aria-describedby={`${id}-terminal-help`}
          value={settings.terminal}
          onChange={(e) => {
            const terminal = e.target.value
            save('terminal', setTerminal(terminal), { ...settings, terminal })
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
          aria-describedby={`${id}-profile-help`}
          value={settings.defaultProfile}
          disabled={profiles.length === 0}
          onChange={(e) => {
            const defaultProfile = e.target.value
            save('profile', setDefaultProfile(defaultProfile), { ...settings, defaultProfile })
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
            aria-describedby={`${id}-prompt-${kind}-help`}
            value={defaultPrompts === null ? 0 : defaultPrompts[kind]}
            disabled={defaultPrompts === null}
            onChange={(e) => {
              savePrompt(kind, Number(e.target.value))
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
      <SettingRow
        label="Start in a worktree"
        controlId={`${id}-worktree`}
        description="Presets each row's “Start in a worktree” box. You can still change it per launch."
        error={errors.worktree}
      >
        <input
          id={`${id}-worktree`}
          type="checkbox"
          role="switch"
          aria-describedby={`${id}-worktree-help`}
          checked={settings.worktreeDefault}
          onChange={(e) => {
            const worktreeDefault = e.target.checked
            save('worktree', setWorktreeDefault(worktreeDefault), { ...settings, worktreeDefault })
          }}
          className={switchClass}
        />
      </SettingRow>
      <SettingRow
        label="Clean up after merging"
        controlId={`${id}-cleanup`}
        description="Removes a session's worktree, with its own worktree-… branch, once its pull request merges or its issue closes, if it has no uncommitted changes, all its commits are on GitHub, and no session runs in it. Checked every few minutes; ignored files in it go with it."
        error={errors.cleanup}
      >
        <input
          id={`${id}-cleanup`}
          type="checkbox"
          role="switch"
          aria-describedby={`${id}-cleanup-help`}
          checked={settings.worktreeAutoCleanup}
          onChange={(e) => {
            const worktreeAutoCleanup = e.target.checked
            // Turned on, it sweeps at once rather than at the next check.
            const call = setWorktreeAutoCleanup(worktreeAutoCleanup).then(() => {
              if (worktreeAutoCleanup) {
                sweepWorktrees().catch((err: unknown) => {
                  console.error('could not sweep the worktrees', err)
                })
              }
            })
            save('cleanup', call, { ...settings, worktreeAutoCleanup })
          }}
          className={switchClass}
        />
      </SettingRow>
    </Group>
  )
}

// profileTraits lists what a profile sets for a session, in the order a row shows them:
// model, effort, plan mode, any extras, and its permissions.
function profileTraits(profile: Profile): string[] {
  const traits: string[] = []
  const addDirs = profile.addDirs?.length ?? 0
  const extraArgs = profile.extraArgs?.length ?? 0
  const env = Object.keys(profile.env ?? {}).length

  if (profile.model !== '') {
    traits.push(profile.model)
  }

  if (profile.effort !== '') {
    traits.push(`${profile.effort} effort`)
  }

  if (profile.planMode) {
    traits.push('Starts in plan mode')
  }

  if (addDirs > 0) {
    traits.push(counted(addDirs, 'extra directory', 'extra directories'))
  }

  if (extraArgs > 0) {
    traits.push(counted(extraArgs, 'extra argument', 'extra arguments'))
  }

  if (env > 0) {
    traits.push(counted(env, 'environment variable', 'environment variables'))
  }

  traits.push(permissionLabels[profile.permissionMode] ?? profile.permissionMode)

  return traits
}

const chipClass = 'rounded-full border border-border px-2 py-0.5 text-xs whitespace-nowrap text-muted'

// ProfilesSection lists every profile with the folder it runs from, what it sets for a
// session, and whether its folder has gone missing, then says how profiles are found.
function ProfilesSection({ profiles }: { profiles: readonly Profile[] }) {
  return (
    <div className="flex flex-col gap-3">
      {profiles.length === 0 ? (
        <Group>
          <p className="px-4 py-3.5 text-sm text-muted">No agent profiles were found in your home folder.</p>
        </Group>
      ) : (
        <ul aria-label="Profiles" className="divide-y divide-border rounded-lg border border-border bg-surface">
          {profiles.map((profile) => (
            <li key={profile.name} className="flex items-center justify-between gap-6 px-4 py-3">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{profile.name}</p>
                <p title={profile.folder} className="truncate font-mono text-xs text-muted">
                  {profile.folder}
                </p>
              </div>
              <ul aria-label={`${profile.name} settings`} className="flex shrink-0 flex-wrap justify-end gap-1.5">
                {profile.missing && (
                  <li className="rounded-full border border-warning px-2 py-0.5 text-xs whitespace-nowrap text-warning">
                    Folder missing
                  </li>
                )}
                {profileTraits(profile).map((trait) => (
                  <li key={trait} className={chipClass}>
                    {trait}
                  </li>
                ))}
              </ul>
            </li>
          ))}
        </ul>
      )}
      <p className="text-xs leading-relaxed text-muted">
        {brand.name} looks for profiles in your home folder each time it starts.{' '}
        <code className="font-mono">~/.claude</code> is the default profile, and each{' '}
        <code className="font-mono">~/.claude_&lt;name&gt;</code> or{' '}
        <code className="font-mono">~/.claude-&lt;name&gt;</code> folder the agent has run with becomes profile{' '}
        <code className="font-mono">&lt;name&gt;</code>. A folder counts only when it holds{' '}
        <code className="font-mono">.claude.json</code>, <code className="font-mono">settings.json</code>, or{' '}
        <code className="font-mono">projects/</code>, so folders other tools name the same way are left out.
      </p>
    </div>
  )
}

interface RepositoriesSectionProps extends SettingsSectionProps {
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  defaultPrompts: DefaultPrompts | null
  launchDefaults: readonly LaunchDefaults[]
  onLaunchChanged: () => Promise<void>
}

function RepositoriesSection({
  settings,
  onSaved,
  profiles,
  prompts,
  defaultPrompts,
  launchDefaults,
  onLaunchChanged,
}: RepositoriesSectionProps) {
  const id = useId()
  const [draft, setDraft] = useState(settings.cloneRoot)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)

  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault()
    setError('')
    setSaved(false)

    const cloneRoot = draft.trim()

    setCloneRoot(cloneRoot)
      .then(() => {
        setSaved(true)
        onSaved({ ...settings, cloneRoot })
      })
      .catch((err: unknown) => {
        setError(errorMessage(err))
      })
  }

  return (
    <div className="flex flex-col gap-8">
      <Group>
        <form onSubmit={submit}>
          <SettingRow
            label="Clone root"
            controlId={`${id}-clone-root`}
            description="The folder your local clones live in, such as ~/projects. Launches and the Worktrees view look one level inside it."
            error={error}
          >
            <input
              id={`${id}-clone-root`}
              type="text"
              aria-describedby={`${id}-clone-root-help`}
              value={draft}
              spellCheck={false}
              onChange={(e) => {
                setDraft(e.target.value)
                setSaved(false)
              }}
              className={`${fieldClass} w-56 font-mono text-[13px]`}
            />
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
      <LaunchDefaultsSection
        settings={settings}
        profiles={profiles}
        prompts={prompts}
        defaultPrompts={defaultPrompts}
        launchDefaults={launchDefaults}
        onChanged={onLaunchChanged}
      />
    </div>
  )
}

function openBugReport(url: string) {
  openInBrowser(url).catch((err: unknown) => {
    console.error('could not open the bug report in the browser', err)
  })
}

// aboutRows is what the About page lists: the release version and build number, the
// commit the build came from, and the system it runs on, which a bug report needs.
function aboutRows(info: Info | null, failure: string): [string, string][] {
  if (failure !== '') {
    return [['Version', `unknown (${failure})`]]
  }

  if (info === null) {
    return [['Version', '…']]
  }

  return [
    ['Version', `${info.release} (${info.build})`],
    ['Commit', info.version],
    ['System', `${info.system}, ${info.arch}`],
  ]
}

function AboutSection() {
  const [info, setInfo] = useState<Info | null>(null)
  const [failure, setFailure] = useState('')

  useEffect(() => {
    let active = true

    getAppInfo()
      .then((got) => {
        if (active) {
          setInfo(got)
        }
      })
      .catch((err: unknown) => {
        if (active) {
          setFailure(errorMessage(err))
        }
      })

    return () => {
      active = false
    }
  }, [])

  return (
    <div className="flex flex-col gap-5">
      <div>
        <p className="flex">
          <Logo className="text-2xl" />
        </p>
        <p className="mt-2.5 text-sm text-muted">{brand.tagline}</p>
      </div>
      <Group>
        {aboutRows(info, failure).map(([term, value]) => (
          <dl key={term} className="flex items-center justify-between gap-6 px-4 py-3 text-sm">
            <dt className="font-medium">{term}</dt>
            <dd className="text-right font-mono text-[13px] break-all text-muted">{value}</dd>
          </dl>
        ))}
      </Group>
      {info !== null && (
        <div className="flex items-center justify-between gap-6">
          <p className="text-sm text-muted">Found a bug? Open a report on GitHub with these details filled in.</p>
          <button
            type="button"
            onClick={() => {
              openBugReport(info.bugReportUrl)
            }}
            className={`${buttonClass} inline-flex shrink-0 items-center gap-1.5`}
          >
            Report a bug
            <ExternalIcon className="size-3.5 text-muted" />
          </button>
        </div>
      )}
      <p className="text-xs leading-relaxed text-muted">
        Your inbox, settings, and triage state stay on this Mac. {brand.name} talks only to GitHub.
      </p>
    </div>
  )
}

// SettingsView is the settings page: a sidebar of sections and the chosen section's
// settings, each saved as soon as it changes (the clone root on Save). Escape, or the
// back button, returns to the view it was opened from.
export function SettingsView({
  initialSection = 'account',
  settings,
  profiles,
  prompts,
  defaultPrompts,
  launchDefaults,
  account,
  onSaved,
  onLaunchChanged,
  onClose,
}: SettingsViewProps) {
  const headingId = useId()
  const [section, setSection] = useState<SectionId>(initialSection)
  const current = sections[section]

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !e.defaultPrevented && !isTyping(e)) {
        onClose()
      }
    }

    document.addEventListener('keydown', onKeyDown)

    return () => {
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [onClose])

  const loading = <p className="text-sm text-muted">Loading the settings…</p>

  return (
    <div className="flex h-full min-h-0">
      <nav aria-label="Settings sections" className="flex w-52 shrink-0 flex-col gap-0.5 border-r border-border p-3">
        <div className="flex pb-2">
          <button
            type="button"
            aria-label="Close settings"
            title="Close settings (Esc)"
            onClick={onClose}
            className="group flex items-center gap-1 rounded-md pr-2.5 text-sm font-semibold hover:bg-raised"
          >
            <span className="flex size-7 items-center justify-center text-muted group-hover:text-text">
              <ChevronLeftIcon className="size-4" />
            </span>
            Settings
          </button>
        </div>
        {sectionOrder.map((id) => (
          <button
            key={id}
            type="button"
            aria-current={id === section ? 'page' : undefined}
            onClick={() => {
              setSection(id)
            }}
            className="rounded-md px-2.5 py-1.5 text-left text-sm text-muted transition-colors hover:bg-raised hover:text-text aria-[current=page]:bg-raised aria-[current=page]:font-medium aria-[current=page]:text-text"
          >
            {sections[id].label}
          </button>
        ))}
      </nav>
      <section aria-labelledby={headingId} className="min-w-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-2xl px-8 py-7">
          <h2 id={headingId} className="text-lg font-semibold">
            {current.label}
          </h2>
          {current.description !== '' && <p className="mt-1 text-sm text-muted">{current.description}</p>}
          <div className="mt-6">
            {section === 'account' && <AccountSection account={account} />}
            {section === 'snoozing' &&
              (settings === null ? loading : <SnoozingSection settings={settings} onSaved={onSaved} />)}
            {section === 'window' &&
              (settings === null ? loading : <WindowSection settings={settings} onSaved={onSaved} />)}
            {section === 'launching' &&
              (settings === null ? (
                loading
              ) : (
                <LaunchingSection
                  settings={settings}
                  profiles={profiles}
                  prompts={prompts}
                  defaultPrompts={defaultPrompts}
                  onSaved={onSaved}
                  onLaunchChanged={onLaunchChanged}
                />
              ))}
            {section === 'profiles' && <ProfilesSection profiles={profiles} />}
            {section === 'repositories' &&
              (settings === null ? (
                loading
              ) : (
                <RepositoriesSection
                  settings={settings}
                  onSaved={onSaved}
                  profiles={profiles}
                  prompts={prompts}
                  defaultPrompts={defaultPrompts}
                  launchDefaults={launchDefaults}
                  onLaunchChanged={onLaunchChanged}
                />
              ))}
            {section === 'trust' &&
              (settings === null ? loading : <TrustSection settings={settings} onSaved={onSaved} />)}
            {section === 'about' && <AboutSection />}
          </div>
        </div>
      </section>
    </div>
  )
}
