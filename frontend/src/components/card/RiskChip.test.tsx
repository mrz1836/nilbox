import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { InboxItem } from '../../platform'
import { at } from '../../test/clock'
import { agentGroup, fileCheck, item } from '../../test/fixtures'
import { RiskChip, RiskIcon } from './RiskChip'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const outside = { trust: 'outside', trusted: false, authorAssociation: 'NONE', isCrossRepository: true } as const
const outsideTitle = 'Launches in plan or read-only mode unless you lift the restriction'
const botTitle =
  "A bot that isn't trusted, so launches start in plan or read-only mode. Add it to your trusted bots in Settings → Trust."

const dependencies = agentGroup({
  category: 'dependencies',
  label: 'Dependencies',
  short: 'Dependencies',
  level: 'warning',
  files: [{ path: 'go.mod', previousPath: '', change: 'modified', pattern: 'go.mod', url: '' }],
})

function renderChip(overrides: Partial<InboxItem>, onFilesSettings?: () => void) {
  return render(<RiskChip item={item(overrides)} onFilesSettings={onFilesSettings} />, { wrapper: at() })
}

describe('RiskChip', () => {
  it('marks an outside contributor whose files were checked clean with a checked shield', () => {
    renderChip({ ...outside, files: fileCheck({ status: 'clear', groups: [] }) })

    const chip = screen.getByRole('button', { name: 'Outside contributor' })
    expect(chip).toHaveAttribute('title', `${outsideTitle}\nNo sensitive files among its 6 changed files`)
    expect(chip).toHaveAttribute('data-tone', 'warning')
    expect(chip).toHaveClass('text-warning')
  })

  it('folds the sensitive files an outside contributor changes into the chip, danger over warning', () => {
    renderChip({ ...outside, files: fileCheck({ groups: [dependencies, agentGroup()] }) })

    const chip = screen.getByRole('button', { name: 'Outside · Dependencies · Agent config' })
    expect(chip).toHaveAttribute('data-tone', 'danger')
    expect(chip).toHaveClass('text-danger')
    expect(chip).toHaveAttribute(
      'title',
      `${outsideTitle}\nChanges sensitive files: Dependencies (1 file), Agent config (1 file)`,
    )
  })

  it('stays a warning when every finding is', () => {
    renderChip({ ...outside, files: fileCheck({ groups: [dependencies] }) })

    expect(screen.getByRole('button', { name: 'Outside · Dependencies' })).toHaveAttribute('data-tone', 'warning')
  })

  it('names an untrusted bot in full, files and all', () => {
    renderChip({
      ...outside,
      authorIsBot: true,
      author: 'gen-bot',
      files: fileCheck({ status: 'partial', groups: [], checked: 300, total: 412, complete: false }),
    })

    const chip = screen.getByRole('button', { name: 'Untrusted bot · Not all files checked' })
    expect(chip).toHaveAttribute(
      'title',
      `${botTitle}\nOnly 300 of 412 changed files were checked, none of them sensitive`,
    )
    expect(chip).toHaveAttribute('data-tone', 'warning')
  })

  it.each([
    ['failed', fileCheck({ status: 'failed', groups: [], error: '' }), 'Outside · Files not checked'],
    ['pending', fileCheck({ status: 'pending', groups: [] }), 'Outside · Files not checked yet'],
  ])('says when the files were not checked (%s)', (_status, files, name) => {
    renderChip({ ...outside, files })

    expect(screen.getByRole('button', { name })).toHaveAttribute('data-tone', 'warning')
  })

  it('is a plain chip, with its tooltip, for an outside contributor’s item whose files are not checked', () => {
    renderChip({ ...outside, kind: 'issue', files: null })

    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.getByText('Outside contributor').parentElement).toHaveAttribute('title', outsideTitle)
  })

  it('shows a trusted author’s findings alone', () => {
    renderChip({ files: fileCheck({ groups: [agentGroup()] }) })

    expect(screen.getByRole('button', { name: 'Agent config' })).toHaveAttribute(
      'title',
      'Changes sensitive files: Agent config (1 file)',
    )
  })

  it('gives a trusted bot, and anyone trusted without a finding, no chip', () => {
    const { container, rerender } = renderChip({ trust: 'bot', authorIsBot: true, author: 'dependabot[bot]' })

    expect(container).toBeEmptyDOMElement()

    rerender(<RiskChip item={item({ files: fileCheck({ status: 'clear', groups: [] }) })} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('sits above the card’s stretched toggle', () => {
    const { container } = renderChip({ ...outside, files: null })

    expect(container.firstElementChild).toHaveClass('relative', 'z-10')
  })

  it('opens the sensitive-files panel', async () => {
    const onFilesSettings = vi.fn()
    renderChip({ ...outside, files: fileCheck() }, onFilesSettings)

    const chip = screen.getByRole('button', { name: 'Outside · Agent config · Workflows' })
    expect(chip).toHaveAttribute('aria-expanded', 'false')
    expect(chip).not.toHaveAttribute('aria-controls')

    await userEvent.click(chip)

    const panel = screen.getByRole('dialog', { name: 'Sensitive files' })
    expect(chip).toHaveAttribute('aria-expanded', 'true')
    expect(chip).toHaveAttribute('aria-controls', panel.id)
    expect(within(panel).getByRole('region', { name: 'Agent configuration' })).toBeInTheDocument()

    await userEvent.click(within(panel).getByRole('button', { name: 'What counts as sensitive' }))
    expect(onFilesSettings).toHaveBeenCalledOnce()
  })
})

describe('RiskIcon', () => {
  it('is the risk chip’s shield alone, in its tone, named and titled as the chip is', () => {
    const { rerender } = render(
      <RiskIcon item={item({ ...outside, files: fileCheck({ groups: [dependencies, agentGroup()] }) })} />,
    )

    const danger = screen.getByRole('img', { name: 'Outside · Dependencies · Agent config' })
    expect(danger).toHaveAttribute('data-tone', 'danger')
    expect(danger).toHaveClass('relative', 'z-10', 'text-danger')
    expect(danger).toHaveAttribute(
      'title',
      `${outsideTitle}\nChanges sensitive files: Dependencies (1 file), Agent config (1 file)`,
    )

    rerender(<RiskIcon item={item({ ...outside, files: fileCheck({ status: 'clear', groups: [] }) })} />)

    const clean = screen.getByRole('img', { name: 'Outside contributor' })
    expect(clean).toHaveAttribute('data-tone', 'warning')
    expect(clean).toHaveClass('text-warning')
  })

  it('shows nothing for a trusted author without a finding', () => {
    const { container } = render(<RiskIcon item={item()} />)

    expect(container).toBeEmptyDOMElement()
  })
})
