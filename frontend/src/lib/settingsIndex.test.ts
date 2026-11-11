import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import { notificationKinds } from './notifications'
import { matchSettings, sectionInfo, sectionOrder, settingsIndex, type SettingsSection } from './settingsIndex'

// found is what a query finds: each matching section's count, in the sidebar's order.
function found(query: string): [SettingsSection, number][] {
  return [...(matchSettings(query)?.sections ?? new Map<SettingsSection, number>())]
}

describe('matchSettings', () => {
  it.each(['', '   ', '""'])('finds nothing to look for in %j', (query) => {
    expect(matchSettings(query)).toBeNull()
  })

  it.each([
    ['dark mode', 'appearance', 'Theme'],
    ['font', 'appearance', 'Text size'],
    ['zoom', 'appearance', 'Text size'],
    ['menu bar', 'notifications', 'Show in the menu bar'],
    ['force-push', 'snoozing', 'New commits'],
    ['iterm', 'launching', 'Terminal'],
    ['protect', 'worktrees', 'Protected branches'],
    ['clone root', 'repositories', 'Clone root'],
    ['dependabot', 'trust', 'Trusted bots and apps'],
    ['gitignore', 'files', 'Your patterns'],
    ['magex', 'integrations', 'mage-x'],
    ['git hooks', 'integrations', 'go-pre-commit'],
    ['passphrase', 'integrations', 'Runs in'],
    ['gh auth', 'account', 'GitHub account'],
    ['bug', 'about', 'Version'],
  ] as const)('finds %j by a keyword', (query, section, label) => {
    const match = matchSettings(query)

    expect(match?.sections.get(section)).toBeGreaterThan(0)
    expect(match?.isMatch(section, label)).toBe(true)
  })

  it('finds every setting of a section by its name, and counts them', () => {
    const match = matchSettings('appearance')

    expect(found('appearance')).toEqual([['appearance', 3]])
    expect(match?.matches).toBe(3)
    expect(match?.isMatch('appearance', 'Window size')).toBe(true)
  })

  it('counts across the sections, in the sidebar’s order', () => {
    const match = matchSettings('worktree')
    const sections = found('worktree').map(([section]) => section)

    expect(sections).toEqual(sectionOrder.filter((s) => sections.includes(s)))
    expect(sections).toEqual(['launching', 'worktrees'])
    expect(match?.matches).toBe(1 + settingsIndex.worktrees.length)
  })

  it('needs every word, ignoring case and accents, and leaves out excluded ones', () => {
    expect(found('TEXT size')).toEqual([['appearance', 1]])
    expect(found('thème')).toEqual([['appearance', 1]])
    expect(found('size text')).toEqual([['appearance', 1]])
    expect(found('text size zoom')).toEqual([['appearance', 1]])
    expect(found('size -text')).toEqual([['appearance', 1]])
    expect(matchSettings('size -text')?.isMatch('appearance', 'Window size')).toBe(true)
  })

  it('matches a quoted phrase as a phrase', () => {
    expect(found('"size text"')).toEqual([])
    expect(matchSettings('"size text"')?.matches).toBe(0)
  })

  it('reads a key:value as words, since settings have no keys', () => {
    expect(found('read:project')).toEqual([['account', 1]])
  })

  it('finds a number only as written in a label', () => {
    expect(matchSettings('#3')?.matches).toBe(0)
  })

  it('says which rows match, section by section', () => {
    const match = matchSettings('reviews')

    expect(match?.isMatch('snoozing', 'Reviews')).toBe(true)
    expect(match?.isMatch('snoozing', 'New comments')).toBe(false)
    // A label no section of that name shows is no match.
    expect(match?.isMatch('notifications', 'Reviews')).toBe(false)
  })

  it('finds the notification kinds by their switches’ labels and their titles', () => {
    for (const kind of notificationKinds) {
      expect(matchSettings(kind.title)?.isMatch('notifications', kind.label)).toBe(true)
    }
  })

  it('finds the product by name in About', () => {
    expect(matchSettings(brand.name)?.isMatch('about', 'Version')).toBe(true)
  })

  it('finds every tool the app runs, the scripts, and where runs go, by the section’s name, after Sensitive files and before About', () => {
    const match = matchSettings('integrations')

    expect(found('integrations')).toEqual([['integrations', 5]])
    expect(match?.isMatch('integrations', 'mage-x')).toBe(true)
    expect(match?.isMatch('integrations', 'go-pre-commit')).toBe(true)
    expect(match?.isMatch('integrations', 'Scripts')).toBe(true)
    expect(found('tasks.json')).toEqual([['integrations', 1]])
    expect(match?.isMatch('integrations', 'Runs in')).toBe(true)
    expect(match?.isMatch('integrations', 'Offer a release after you merge a pull request')).toBe(true)
    expect(sectionOrder.indexOf('integrations')).toBe(sectionOrder.indexOf('files') + 1)
    expect(sectionOrder.indexOf('about')).toBe(sectionOrder.indexOf('integrations') + 1)
    expect(sectionInfo.integrations.description).toBe(
      `Tools ${brand.name} runs for you: mage-x releases, go-pre-commit hooks, and scripts.`,
    )
  })

  it('finds a template’s rows by the agent each names', () => {
    const codex = matchSettings('codex')

    expect(codex?.sections.get('launching')).toBe(4)
    expect(codex?.isMatch('launching', 'Codex profile')).toBe(true)
    expect(codex?.isMatch('launching', 'Claude profile')).toBe(false)
    expect(codex?.isMatch('launching', 'Set Codex’s effort at launch')).toBe(true)
    expect(codex?.isMatch('profiles', 'Codex profiles')).toBe(true)
    expect(codex?.isMatch('profiles', 'Claude profiles')).toBe(false)
  })

  it('finds a template’s rows by the other words for the agent each names', () => {
    const home = matchSettings('codex_home')

    expect(home?.sections.get('profiles')).toBe(1)
    expect(home?.isMatch('profiles', 'Codex profiles')).toBe(true)
    expect(home?.isMatch('profiles', 'Claude profiles')).toBe(false)
    expect(matchSettings('claude code')?.isMatch('launching', 'Claude profile')).toBe(true)
    expect(matchSettings('claude code')?.isMatch('launching', 'Codex profile')).toBe(false)
  })

  it('finds a template’s rows by its own words', () => {
    const match = matchSettings('default profile')

    expect(match?.isMatch('launching', 'Codex profile')).toBe(true)
    expect(match?.isMatch('launching', 'Claude profile')).toBe(true)
    // A label the template's pattern does not take is not one of its rows.
    expect(match?.isMatch('launching', 'Default agent')).toBe(false)
  })

  it('gives every section a heading, and an order that holds each once', () => {
    expect(new Set(sectionOrder).size).toBe(sectionOrder.length)
    expect(Object.keys(sectionInfo).sort()).toEqual([...sectionOrder].sort())
    expect(Object.keys(settingsIndex).sort()).toEqual([...sectionOrder].sort())
  })
})
