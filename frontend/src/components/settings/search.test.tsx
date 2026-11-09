import { act, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { sectionInfo, sectionOrder, settingsIndex, type SettingsSection } from '../../lib/settingsIndex'
import { account, agents, defaultPrompts, profile, prompts, settings } from '../../test/fixtures'
import { SettingsView } from '../SettingsView'
import { SettingMatch } from './match'
import { ChoiceRow, SectionIntro, SettingRow } from './rows'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// exemptions are the index's entries a section may not render with the fixtures, each
// with why. None is needed today: with settings loaded, every section shows every row,
// and the templates' rows render once per fixture agent.
const exemptions: Readonly<Partial<Record<SettingsSection, Readonly<Record<string, string>>>>> = {}

// shownLabels renders a section of the settings page with the shared fixtures and
// lists the labels of the rows it marks for "Find a setting".
async function shownLabels(section: SettingsSection): Promise<string[]> {
  const view = render(
    <SettingsView
      initialSection={section}
      settings={settings()}
      agents={agents()}
      profiles={[profile(), profile({ agent: 'codex', name: 'default', folder: '~/.codex' })]}
      prompts={prompts()}
      defaultPrompts={defaultPrompts()}
      launchDefaults={[]}
      account={account()}
      projectAccess="on"
      theme="system"
      onThemeChange={() => Promise.resolve({ ok: true, data: undefined })}
      onSaved={vi.fn()}
      onLaunchChanged={() => Promise.resolve()}
      onClose={vi.fn()}
    />,
  )

  // Let the sections that read something as they open (About, Profiles) take it in.
  await act(async () => {
    await Promise.resolve()
  })

  const region = screen.getByRole('region', { name: sectionInfo[section].label })
  const labels = [...region.querySelectorAll('[data-setting]')].map((el) => el.getAttribute('data-setting') ?? '')

  view.unmount()

  return labels
}

describe('the settings index', () => {
  it.each(sectionOrder)('indexes every setting the %s section shows, and nothing it does not', async (section) => {
    const labels = await shownLabels(section)
    const entries = settingsIndex[section]
    const exempt = exemptions[section] ?? {}

    expect(labels.length).toBeGreaterThan(0)

    // Every row the section marks is in the index, so a new row must be indexed.
    for (const label of labels) {
      const indexed = entries.some((e) =>
        e.template === undefined ? e.label === label : e.template.pattern.test(label),
      )

      expect(indexed, `${section}: “${label}” is not in the settings index`).toBe(true)
    }

    // Every entry of the index renders, so a renamed or removed row leaves none behind.
    for (const entry of entries) {
      if (entry.label in exempt) {
        continue
      }

      const { template } = entry
      const rendered =
        template === undefined ? labels.includes(entry.label) : labels.some((label) => template.pattern.test(label))

      expect(rendered, `${section}: “${entry.label}” is indexed but not shown`).toBe(true)
    }
  })
})

describe('the rows “Find a setting” marks', () => {
  it('marks each row with its label, and none as a match outside a search', () => {
    render(
      <>
        <SettingRow label="Terminal" controlId="terminal" description="Where sessions open.">
          <input id="terminal" />
        </SettingRow>
        <ChoiceRow label="Theme" description="Light or dark.">
          <span>choices</span>
        </ChoiceRow>
        <SectionIntro title="Branches" />
      </>,
    )

    const marked = [...document.querySelectorAll('[data-setting]')]

    expect(marked.map((el) => el.getAttribute('data-setting'))).toEqual(['Terminal', 'Theme', 'Branches'])
    expect(document.querySelector('[data-match]')).toBeNull()
  })

  it('marks the rows a search matches', () => {
    render(
      <SettingMatch value={(label) => label !== 'Theme'}>
        <SettingRow label="Terminal" controlId="terminal" description="Where sessions open.">
          <input id="terminal" />
        </SettingRow>
        <ChoiceRow label="Theme" description="Light or dark.">
          <span>choices</span>
        </ChoiceRow>
        <ChoiceRow label="Window size" description="Where it opens.">
          <span>sizes</span>
        </ChoiceRow>
      </SettingMatch>,
    )

    const matched = [...document.querySelectorAll('[data-match]')].map((el) => el.getAttribute('data-setting'))

    expect(matched).toEqual(['Terminal', 'Window size'])
  })

  // intro renders a section intro inside a search that matches it, and returns its
  // heading and the element its marks are on.
  function intro(props: Parameters<typeof SectionIntro>[0]) {
    render(
      <SettingMatch value={() => true}>
        <SectionIntro {...props} />
      </SettingMatch>,
    )

    const heading = screen.getByRole('heading', { name: props.title })
    const marked = document.querySelector('[data-setting]')

    return { heading, marked }
  }

  it('marks a heading alone, coloring it', () => {
    const { heading, marked } = intro({ title: 'How it works' })

    expect(marked).toBe(heading)
    expect(heading).toHaveAttribute('data-match')
    expect(heading).toHaveClass('text-accent')
  })

  it('marks a heading with its help as a whole', () => {
    const { heading, marked } = intro({ title: 'Banners', children: 'What newly needs you.' })

    expect(marked).toBe(heading.parentElement)
    expect(marked).toHaveTextContent('What newly needs you.')
    expect(heading).toHaveClass('text-accent')
  })

  it('marks a heading beside its action as a whole', () => {
    const { heading, marked } = intro({ title: 'Protected branches', action: <button type="button">Restore</button> })

    expect(marked).toBe(heading.parentElement)
    expect(marked).toContainElement(screen.getByRole('button', { name: 'Restore' }))
  })

  it('marks a heading with its help beside its action as a whole', () => {
    const { heading, marked } = intro({
      title: 'Launch defaults',
      children: 'What launches use.',
      action: <button type="button">Add defaults</button>,
    })

    expect(marked).toBe(heading.parentElement?.parentElement)
    expect(marked).toHaveTextContent('What launches use.')
    expect(marked).toContainElement(screen.getByRole('button', { name: 'Add defaults' }))
  })
})
