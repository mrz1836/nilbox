import { useEffect, useEffectEvent, useId, useRef, useState } from 'react'

import type { Result } from '../lib/call'
import type { GitHubThreadsState } from '../lib/useGitHubThreads'
import type { UpdateControl } from '../lib/useUpdate'
import { counted, formatCount } from '../lib/format'
import { isTyping } from '../lib/keys'
import { useScrollEdges } from '../lib/useScrollEdges'
import {
  hiddenOf,
  matchSettings,
  sectionGroups,
  sectionInfo,
  shownSection,
  type SettingsSection,
} from '../lib/settingsIndex'
import type { Theme } from '../lib/theme'
import type {
  Account,
  AgentInfo,
  DefaultPrompts,
  LaunchDefaults,
  Profile,
  ProjectAccess,
  Prompt,
  Settings,
} from '../platform'
import { BetaBadge } from './BetaBadge'
import { ChevronLeftIcon } from './icons'
import { ScrollFades } from './ScrollFades'
import { AboutSection } from './settings/AboutSection'
import { AccountSection } from './settings/AccountSection'
import { AppearanceSection } from './settings/AppearanceSection'
import { AutopilotSection } from './settings/AutopilotSection'
import { BackupSection } from './settings/BackupSection'
import { CardsSection } from './settings/CardsSection'
import { GitHubNotificationsSection } from './settings/GitHubNotificationsSection'
import { IntegrationsSection } from './settings/IntegrationsSection'
import { LabsSection } from './settings/LabsSection'
import { LaunchingSection } from './settings/LaunchingSection'
import { SettingMatch } from './settings/match'
import { NotificationsSection } from './settings/NotificationsSection'
import { ProfilesSection } from './settings/ProfilesSection'
import { RepositoriesSection } from './settings/RepositoriesSection'
import { ReviewingSection } from './settings/ReviewingSection'
import { SensitiveFilesSection } from './settings/SensitiveFilesSection'
import { ServiceStatusSection } from './settings/ServiceStatusSection'
import { SnoozingSection } from './settings/SnoozingSection'
import { TrustSection } from './settings/TrustSection'
import { RefreshingSection } from './settings/RefreshingSection'
import { WorktreesSection } from './settings/WorktreesSection'

export type { SettingsSection }

type SectionId = SettingsSection

export interface SettingsViewProps {
  // initialSection is the section the page opens at; it defaults to the account.
  initialSection?: SettingsSection
  // opened counts the openings of a page that stays mounted between them: each new
  // count turns to initialSection, as a page opened afresh would.
  opened?: number
  // settings is null until it has loaded; the launch and clone sections wait for it.
  settings: Settings | null
  // agents are the coding agents, each with whether it is installed.
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  // defaultPrompts is null until it has loaded.
  defaultPrompts: DefaultPrompts | null
  launchDefaults: readonly LaunchDefaults[]
  // account is null until the inbox has loaded.
  account: Account | null
  // projectAccess is whether the last refresh could read the GitHub projects issues are
  // in: '' before the first refresh, or while the inbox loads.
  projectAccess?: ProjectAccess
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
  // query is what "Find a setting" in the toolbar looks for, or '' while it looks for
  // nothing.
  query?: string
  // githubThreads is the window's one mirror of the GitHub notifications, which their
  // section shows and checks again; without it the section shows the rules alone.
  githubThreads?: GitHubThreadsState | undefined
  // onRunSetup opens the first-run setup again, which About offers, resolving with how
  // storing that went; without it About offers no way to.
  onRunSetup?: (() => Promise<Result<void>>) | undefined
  // update is the app's update, which About shows under the version; without it About
  // leaves it out.
  update?: UpdateControl | undefined
  // onShowCardGuide opens the card guide, which Cards offers; without it Cards offers no
  // way to.
  onShowCardGuide?: (() => void) | undefined
}

// SettingsView is the settings page: a sidebar of sections and the chosen section's
// settings, each saved as soon as it changes (the clone root on Save). Escape, or the
// back button, returns to the view it was opened from. The sidebar's sections and the open
// section each scroll on their own under a fade at an end with more beyond it
// (ScrollFades), the back button staying put, and the open section's place in the sidebar
// scrolls into view as it changes.
//
// While "Find a setting" looks for something, the sidebar lists only the sections with a
// matching setting, each with how many, and turns to the first of them when the open
// one has none; the matching settings are highlighted, and the first scrolls into view.
export function SettingsView({
  initialSection = 'account',
  opened = 0,
  settings,
  agents,
  profiles,
  prompts,
  defaultPrompts,
  launchDefaults,
  account,
  projectAccess = '',
  theme,
  onThemeChange,
  onSaved,
  onLaunchChanged,
  onClose,
  query = '',
  githubThreads,
  onRunSetup,
  update,
  onShowCardGuide,
}: SettingsViewProps) {
  const headingId = useId()
  const [chosen, setSection] = useState<SectionId>(initialSection)
  // openedAt is the opening the page last turned to its section for.
  const [openedAt, setOpenedAt] = useState(opened)
  // hidden is what the settings leave out now: Autopilot's section while Autopilot (beta)
  // is off, whose place Labs takes, where it is turned on.
  const hidden = hiddenOf(settings)
  const section = shownSection(chosen, hidden) ? chosen : 'labs'
  const match = matchSettings(query, hidden)
  // groups are the sidebar's groups with the sections listed, a group with none left out.
  const groups = sectionGroups
    .map((group) => ({
      ...group,
      sections: group.sections.filter((id) => shownSection(id, hidden) && (match === null || match.sections.has(id))),
    }))
    .filter((group) => group.sections.length > 0)
  const firstMatch = groups[0]?.sections[0]
  const contentRef = useRef<HTMLDivElement>(null)
  // sidebarRef is the scroller of the sidebar's sections.
  const sidebarRef = useRef<HTMLDivElement | null>(null)
  // The sidebar's sections and the open section each scroll on their own, faded at an end
  // with more beyond it, so a list taller than the window shows that it goes on.
  const [sidebarScroller, sidebarEdges] = useScrollEdges()
  const [contentScroller, contentEdges] = useScrollEdges()

  if (opened !== openedAt) {
    setOpenedAt(opened)
    setSection(initialSection)
  } else if (match !== null && firstMatch !== undefined && !match.sections.has(section)) {
    setSection(firstMatch)
  }

  const current = sectionInfo[section]

  // The first matching setting scrolls into view as the search or the section changes.
  useEffect(() => {
    if (query !== '') {
      contentRef.current?.querySelector('[data-match]')?.scrollIntoView({ block: 'nearest' })
    }
  }, [query, section])

  // The open section's place in the sidebar scrolls into view when it changes to one out of
  // sight, as when the page opens at About from the menu bar with the sidebar taller than
  // the window; its scroll margin keeps it clear of the fade.
  useEffect(() => {
    const sidebar = sidebarRef.current
    const open = sidebar?.querySelector('[aria-current="page"]')

    if (sidebar === null || open === null || open === undefined) {
      return
    }

    const shown = sidebar.getBoundingClientRect()
    const at = open.getBoundingClientRect()

    if (at.top < shown.top || at.bottom > shown.bottom) {
      open.scrollIntoView({ block: 'nearest' })
    }
  }, [section, openedAt])

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
      <nav aria-label="Settings sections" className="flex w-52 shrink-0 flex-col border-r border-border">
        {/* The way back stays put over the sections, which scroll beneath it. */}
        <div className="flex px-3 pt-3 pb-1">
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
        <div className="relative flex min-h-0 flex-1 flex-col">
          <div
            ref={(el) => {
              sidebarRef.current = el

              return sidebarScroller(el)
            }}
            className="min-h-0 flex-1 overflow-y-auto px-3 pt-1 pb-3"
          >
            <div className="flex flex-col gap-0.5">
              {match !== null && firstMatch === undefined && (
                <p className="px-2.5 py-1.5 text-sm text-muted">No setting matches “{query.trim()}”.</p>
              )}
              {groups.map((group, i) => (
                <div
                  key={group.sections.join(' ')}
                  role={group.heading === '' ? undefined : 'group'}
                  aria-labelledby={group.heading === '' ? undefined : `${headingId}-${group.heading}`}
                  className={`flex flex-col gap-0.5 ${i > 0 ? 'pt-4' : ''}`.trim()}
                >
                  {group.heading !== '' && (
                    <p
                      id={`${headingId}-${group.heading}`}
                      className="mx-2.5 mb-1 border-b border-border pb-1.5 text-[0.6875rem] font-semibold tracking-wider text-text uppercase"
                    >
                      {group.heading}
                    </p>
                  )}
                  {group.sections.map((id) => {
                    const count = match?.sections.get(id) ?? 0

                    return (
                      <button
                        key={id}
                        type="button"
                        aria-current={id === section ? 'page' : undefined}
                        onClick={() => {
                          setSection(id)
                        }}
                        className="flex scroll-my-12 items-center justify-between gap-2 rounded-md px-2.5 py-1.5 text-left text-sm text-muted transition-colors hover:bg-raised hover:text-text aria-[current=page]:bg-raised aria-[current=page]:font-medium aria-[current=page]:text-text"
                      >
                        {sectionInfo[id].label}
                        {count > 0 && (
                          <>
                            {' '}
                            <span
                              aria-hidden="true"
                              className="rounded-full bg-accent/15 px-1.5 text-[0.6875rem] leading-[1.125rem] font-medium text-text tabular-nums"
                            >
                              {formatCount(count)}
                            </span>
                            <span className="sr-only">({counted(count, 'match', 'matches')})</span>
                          </>
                        )}
                      </button>
                    )
                  })}
                </div>
              ))}
            </div>
          </div>
          <ScrollFades {...sidebarEdges} />
        </div>
      </nav>
      <SettingMatch value={(label) => match?.isMatch(section, label) ?? false}>
        <div className="relative flex min-w-0 flex-1 flex-col">
          {/* Each section gets a scroller of its own, so it opens at its top: one kept across
            sections would keep a long section's place, and macOS's web view shows a short
            section scrolled past its own end. */}
          <section
            key={section}
            ref={contentScroller}
            aria-labelledby={headingId}
            className="min-h-0 flex-1 overflow-y-auto"
          >
            <div className="mx-auto max-w-2xl px-8 py-7">
              <div className="flex items-center gap-2">
                <h2 id={headingId} className="text-lg font-semibold">
                  {current.label}
                </h2>
                {section === 'autopilot' && <BetaBadge />}
              </div>
              {current.description !== '' && <p className="mt-1 text-sm text-muted">{current.description}</p>}
              <div ref={contentRef} className="mt-6">
                {section === 'account' && <AccountSection account={account} projectAccess={projectAccess} />}
                {section === 'appearance' && (
                  <AppearanceSection
                    settings={settings}
                    onSaved={onSaved}
                    theme={theme}
                    onThemeChange={onThemeChange}
                  />
                )}
                {section === 'notifications' &&
                  (settings === null ? loading : <NotificationsSection settings={settings} onSaved={onSaved} />)}
                {section === 'github' &&
                  (settings === null ? (
                    loading
                  ) : (
                    <GitHubNotificationsSection settings={settings} onSaved={onSaved} threads={githubThreads ?? null} />
                  ))}
                {section === 'refreshing' &&
                  (settings === null ? loading : <RefreshingSection settings={settings} onSaved={onSaved} />)}
                {section === 'status' &&
                  (settings === null ? loading : <ServiceStatusSection settings={settings} onSaved={onSaved} />)}
                {section === 'snoozing' &&
                  (settings === null ? loading : <SnoozingSection settings={settings} onSaved={onSaved} />)}
                {section === 'cards' &&
                  (settings === null ? (
                    loading
                  ) : (
                    <CardsSection settings={settings} onSaved={onSaved} onShowCardGuide={onShowCardGuide} />
                  ))}
                {section === 'reviewing' &&
                  (settings === null ? loading : <ReviewingSection settings={settings} onSaved={onSaved} />)}
                {section === 'autopilot' &&
                  (settings === null ? loading : <AutopilotSection settings={settings} onSaved={onSaved} />)}
                {section === 'launching' &&
                  (settings === null ? (
                    loading
                  ) : (
                    <LaunchingSection
                      settings={settings}
                      agents={agents}
                      profiles={profiles}
                      prompts={prompts}
                      defaultPrompts={defaultPrompts}
                      onSaved={onSaved}
                      onLaunchChanged={onLaunchChanged}
                    />
                  ))}
                {section === 'worktrees' &&
                  (settings === null ? loading : <WorktreesSection settings={settings} onSaved={onSaved} />)}
                {section === 'profiles' && <ProfilesSection agents={agents} profiles={profiles} />}
                {section === 'repositories' &&
                  (settings === null ? (
                    loading
                  ) : (
                    <RepositoriesSection
                      settings={settings}
                      onSaved={onSaved}
                      agents={agents}
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
                {section === 'integrations' && <IntegrationsSection settings={settings} onSaved={onSaved} />}
                {section === 'labs' &&
                  (settings === null ? loading : <LabsSection settings={settings} onSaved={onSaved} />)}
                {section === 'backup' && (
                  <BackupSection
                    onSaved={onSaved}
                    onLaunchChanged={onLaunchChanged}
                    theme={theme}
                    onThemeChange={onThemeChange}
                  />
                )}
                {section === 'about' && (
                  <AboutSection onRunSetup={onRunSetup} update={update} settings={settings} onSaved={onSaved} />
                )}
              </div>
            </div>
          </section>
          <ScrollFades {...contentEdges} />
        </div>
      </SettingMatch>
    </div>
  )
}
