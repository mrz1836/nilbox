import { useEffect, useEffectEvent, useId, useState } from 'react'

import type { Result } from '../lib/call'
import type { Theme } from '../lib/theme'
import type { Account, DefaultPrompts, LaunchDefaults, Profile, Prompt, Settings } from '../platform'
import { ChevronLeftIcon } from './icons'
import { AboutSection } from './settings/AboutSection'
import { AccountSection } from './settings/AccountSection'
import { AppearanceSection } from './settings/AppearanceSection'
import { LaunchingSection } from './settings/LaunchingSection'
import { NotificationsSection } from './settings/NotificationsSection'
import { ProfilesSection } from './settings/ProfilesSection'
import { RepositoriesSection } from './settings/RepositoriesSection'
import { SensitiveFilesSection } from './settings/SensitiveFilesSection'
import { SnoozingSection } from './settings/SnoozingSection'
import { TrustSection } from './settings/TrustSection'

// SettingsSection is one section of the settings page.
export type SettingsSection =
  | 'account'
  | 'appearance'
  | 'notifications'
  | 'snoozing'
  | 'launching'
  | 'profiles'
  | 'repositories'
  | 'trust'
  | 'files'
  | 'about'

type SectionId = SettingsSection

// sectionOrder is the sidebar's order; sections holds each one's heading and summary.
const sectionOrder: readonly SectionId[] = [
  'account',
  'appearance',
  'notifications',
  'snoozing',
  'launching',
  'profiles',
  'repositories',
  'trust',
  'files',
  'about',
]

const sections: Readonly<Record<SectionId, { label: string; description: string }>> = {
  account: { label: 'Account', description: 'The GitHub account your inbox comes from.' },
  appearance: { label: 'Appearance', description: 'The theme, and the size the window opens at.' },
  notifications: {
    label: 'Notifications',
    description: 'The menu bar icon, and the banners that tell you when something new needs you.',
  },
  snoozing: {
    label: 'Snoozing',
    description: 'How long a snooze lasts, and what new activity brings an item back early.',
  },
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
  files: {
    label: 'Sensitive files',
    description:
      'The changed files to point out on pull requests from outside the repository, before you review them or open a session on them.',
  },
  about: { label: 'About', description: '' },
}

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
  // theme is the chosen theme, and onThemeChange switches to another and stores it,
  // resolving with why it could not be stored, if it could not.
  theme: Theme
  onThemeChange: (theme: Theme) => Promise<Result<void>>
  // onSaved receives every setting as stored after a change, as the setter returned it.
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
  theme,
  onThemeChange,
  onSaved,
  onLaunchChanged,
  onClose,
}: SettingsViewProps) {
  const headingId = useId()
  const [section, setSection] = useState<SectionId>(initialSection)
  const current = sections[section]

  const closeOnEscape = useEffectEvent((e: KeyboardEvent) => {
    if (e.key === 'Escape' && !e.defaultPrevented && !isTyping(e)) {
      onClose()
    }
  })

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      closeOnEscape(e)
    }

    document.addEventListener('keydown', onKeyDown)

    return () => {
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [])

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
            {section === 'appearance' && (
              <AppearanceSection settings={settings} onSaved={onSaved} theme={theme} onThemeChange={onThemeChange} />
            )}
            {section === 'notifications' &&
              (settings === null ? loading : <NotificationsSection settings={settings} onSaved={onSaved} />)}
            {section === 'snoozing' &&
              (settings === null ? loading : <SnoozingSection settings={settings} onSaved={onSaved} />)}
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
            {section === 'files' &&
              (settings === null ? loading : <SensitiveFilesSection settings={settings} onSaved={onSaved} />)}
            {section === 'about' && <AboutSection />}
          </div>
        </div>
      </section>
    </div>
  )
}
