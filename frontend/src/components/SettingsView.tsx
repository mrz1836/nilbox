import { useEffect, useId, useState, type ReactNode, type SubmitEvent } from 'react'

import { brand } from '../generated/brand'
import { errorMessage } from '../lib/format'
import {
  getAppInfo,
  openInBrowser,
  setCloneRoot,
  setDefaultProfile,
  setTerminal,
  setWorktreeDefault,
  type Account,
  type Profile,
  type Settings,
} from '../platform'
import { Avatar } from './Avatar'
import { ChevronLeftIcon, ExternalIcon } from './icons'

type SectionId = 'account' | 'launching' | 'repositories' | 'about'

// sectionOrder is the sidebar's order; sections holds each one's heading and summary.
const sectionOrder: readonly SectionId[] = ['account', 'launching', 'repositories', 'about']

const sections: Readonly<Record<SectionId, { label: string; description: string }>> = {
  account: { label: 'Account', description: 'The GitHub account your inbox comes from.' },
  launching: { label: 'Launching', description: 'How Open in Claude starts a coding session.' },
  repositories: { label: 'Repositories', description: 'Where your local clones live.' },
  about: { label: 'About', description: '' },
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
  // settings is null until it has loaded; the launch and clone sections wait for it.
  settings: Settings | null
  profiles: Profile[]
  // account is null until the inbox has loaded.
  account: Account | null
  // onSaved receives the settings after a change was stored.
  onSaved: (settings: Settings) => void
  // onClose leaves the settings page for the view it was opened from.
  onClose: () => void
}

// Input types that take no typing, so Escape in them is free to close the page.
const untypedInputs = new Set(['checkbox', 'radio', 'button', 'submit', 'reset', 'range', 'color', 'file'])

// isTyping reports whether a key event belongs to text entry: a text field, where leaving
// the page would drop an unsaved draft, or an input method mid-composition.
function isTyping(e: KeyboardEvent): boolean {
  const target = e.target

  return (
    e.isComposing ||
    target instanceof HTMLTextAreaElement ||
    (target instanceof HTMLInputElement && !untypedInputs.has(target.type))
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

function LaunchingSection({ settings, profiles, onSaved }: SettingsSectionProps & { profiles: Profile[] }) {
  const id = useId()
  const [errors, setErrors] = useState({ terminal: '', profile: '', worktree: '' })

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
    </Group>
  )
}

function RepositoriesSection({ settings, onSaved }: SettingsSectionProps) {
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
  )
}

function AboutSection() {
  const [version, setVersion] = useState('')

  useEffect(() => {
    let active = true

    getAppInfo()
      .then((info) => {
        if (active) {
          setVersion(info.version)
        }
      })
      .catch((err: unknown) => {
        if (active) {
          setVersion(`unknown (${errorMessage(err)})`)
        }
      })

    return () => {
      active = false
    }
  }, [])

  return (
    <div className="flex flex-col gap-5">
      <div>
        <p className="text-base font-semibold">{brand.name}</p>
        <p className="text-sm text-muted">{brand.tagline}</p>
      </div>
      <Group>
        <dl className="flex items-center justify-between px-4 py-3 text-sm">
          <dt className="font-medium">Version</dt>
          <dd className="font-mono text-[13px] text-muted">{version === '' ? '…' : version}</dd>
        </dl>
      </Group>
      <p className="text-xs leading-relaxed text-muted">
        Your inbox, settings, and triage state stay on this Mac. {brand.name} talks only to GitHub.
      </p>
    </div>
  )
}

// SettingsView is the settings page: a sidebar of sections and the chosen section's
// settings, each saved as soon as it changes (the clone root on Save). Escape, or the
// back button, returns to the view it was opened from.
export function SettingsView({ settings, profiles, account, onSaved, onClose }: SettingsViewProps) {
  const headingId = useId()
  const [section, setSection] = useState<SectionId>('account')
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
        <div className="flex items-center gap-1 pb-2">
          <button
            type="button"
            aria-label="Close settings"
            title="Close settings (Esc)"
            onClick={onClose}
            className="flex size-7 items-center justify-center rounded-md text-muted hover:bg-raised hover:text-text"
          >
            <ChevronLeftIcon className="size-4" />
          </button>
          <p className="text-sm font-semibold">Settings</p>
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
            {section === 'launching' &&
              (settings === null ? (
                loading
              ) : (
                <LaunchingSection settings={settings} profiles={profiles} onSaved={onSaved} />
              ))}
            {section === 'repositories' &&
              (settings === null ? loading : <RepositoriesSection settings={settings} onSaved={onSaved} />)}
            {section === 'about' && <AboutSection />}
          </div>
        </div>
      </section>
    </div>
  )
}
