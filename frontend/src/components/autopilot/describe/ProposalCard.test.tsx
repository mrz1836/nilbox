import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi, type Mock } from 'vitest'

import type { AutopilotDraft, AutopilotSpec, DescribeCard, Settings } from '../../../platform'
import {
  autopilotCatalog,
  described,
  describeCard,
  kitChecked,
  kitCheckedPrompt,
  kitCheckedRoutine,
  kitCheckedRules,
  kitCheckedSettings,
  kitNewPrompt,
  kitOutcome,
  kitRoutine,
  readyCheck,
  routine,
  routineReady,
  routineSpec,
  settings,
} from '../../../test/fixtures'
import { renderAt } from '../../../test/clock'
import { platform } from '../../../test/platform'
import type { CardKit } from './parts'
import { ProposalCard } from './ProposalCard'

vi.mock('../../../platform', () => import('../../../test/platform').then((fake) => fake.platform))

// Kit is a card's kit with each of its calls a mock.
type Kit = CardKit & {
  onEditInBuilder: Mock<(draft: AutopilotDraft) => void>
  onShowRoutine: Mock<(id: number) => void>
  onSettled: Mock<() => void>
  onSettingsSaved: Mock<(settings: Settings) => void>
}

// kitOf is a card's kit: new routines start in Ask first, and a draft is read at once.
function kitOf(overrides: Partial<CardKit> = {}): Kit {
  return {
    defaultMode: 'ask',
    catalog: autopilotCatalog(),
    onEditInBuilder: vi.fn<(draft: AutopilotDraft) => void>(),
    onShowRoutine: vi.fn<(id: number) => void>(),
    onSettled: vi.fn<() => void>(),
    onSettingsSaved: vi.fn<(settings: Settings) => void>(),
    describeDelay: 0,
    ...overrides,
  } as Kit
}

// show renders card with kit, and returns the kit.
function show(card: DescribeCard, kit: Kit = kitOf(), onDiscard?: () => Promise<string>): Kit {
  renderAt(<ProposalCard card={card} kit={kit} {...(onDiscard === undefined ? {} : { onDiscard })} />)

  return kit
}

// changedSpec is the spec routine 3 would have once a card changes it: it watches the user's
// own pull requests too.
function changedSpec(): AutopilotSpec {
  return { ...routineSpec(), watch: { sources: [{ lists: ['reviewRequested', 'authored'] }] } }
}

// changeCard is a card that changes routine 3, which is on, in Autopilot.
function changeCard(overrides: Partial<Parameters<typeof kitCheckedRoutine>[0]> = {}): DescribeCard {
  return describeCard({
    proposal: {
      summary: 'Snooze your own pull requests too.',
      routines: [{ id: 3, base: '2026-06-15T08:00:00Z', name: 'Snooze passing bot PRs', spec: changedSpec() }],
    },
    checked: kitChecked({
      routines: [
        kitCheckedRoutine({
          spec: changedSpec(),
          sentence: 'When checks pass on pull requests in Review requested or Yours, snooze it for 3 days.',
          before: kitRoutine(),
          ...overrides,
        }),
      ],
    }),
  })
}

// fullCard is a card with a prompt, a new routine, a settings change, and a rules change.
function fullCard(): DescribeCard {
  return describeCard({
    proposal: {
      summary: 'Fix the checks with a new prompt, one agent at a time, and trust Renovate.',
      prompts: [kitNewPrompt()],
      routines: [{ name: 'Fix checks', spec: routineSpec() }],
      settings: { agentsAtOnce: 1 },
      rules: { trust: ['renovate[bot]'], mute: ['acme-org/legacy'] },
    },
    checked: kitChecked({
      prompts: [kitCheckedPrompt()],
      routines: [kitCheckedRoutine({ name: 'Fix checks' })],
      settings: kitCheckedSettings(),
      rules: kitCheckedRules(),
    }),
  })
}

// syncSpec is a routine that replies with Sync OK when checks pass.
function syncSpec(): AutopilotSpec {
  return { ...routineSpec(), steps: [{ github: [{ reply: { prompt: { nickname: 'Sync OK' } } }] }] }
}

// syncCard is a card that adds the reply Sync OK, and a routine that replies with it: a new
// one, or, with change, a change to routine 3. Kit counts the card's prompt in the library,
// so the routine has no problem.
function syncCard(change = false): DescribeCard {
  const reply = kitNewPrompt({
    name: 'Say the sync is fine',
    nickname: 'Sync OK',
    purpose: 'reply',
    replyAction: 'comment',
    appliesTo: 'any',
  })

  return describeCard({
    proposal: {
      summary: 'Reply that the sync is fine.',
      prompts: [reply],
      routines: [
        change
          ? { id: 3, base: '2026-06-15T08:00:00Z', name: 'Snooze passing bot PRs', spec: syncSpec() }
          : { name: 'Reply when it syncs', spec: syncSpec() },
      ],
    },
    checked: kitChecked({
      prompts: [kitCheckedPrompt({ prompt: reply })],
      routines: [
        kitCheckedRoutine({
          name: change ? 'Snooze passing bot PRs' : 'Reply when it syncs',
          spec: syncSpec(),
          sentence: 'When checks pass on pull requests in Review requested, reply with “Sync OK”.',
          ...(change ? { before: kitRoutine() } : {}),
        }),
      ],
    }),
  })
}

// readsTheLibrary has Describe read a draft against the prompt library as it is: one that
// replies with Sync OK has a problem until Sync OK is added, and a warning either way.
function readsTheLibrary(): void {
  const warnings = [{ part: 'steps' as const, field: '[0]', message: 'it replies on every pull request' }]

  platform.describeRoutine.mockImplementation(() =>
    Promise.resolve(
      platform.createPrompt.mock.calls.length > 0
        ? described({ warnings })
        : described({
            sentence: '',
            problems: [
              {
                part: 'steps',
                field: '[0].github[0].reply.prompt',
                message: 'No prompt in your library has the nickname “Sync OK”',
              },
            ],
            warnings,
          }),
    ),
  )
}

describe('ProposalCard', () => {
  it('doesn’t hold a routine back on a prompt its card adds, and its Add adds the prompt first', async () => {
    const user = userEvent.setup()

    readsTheLibrary()

    const card = syncCard()
    const kit = show(card)
    const row = within(screen.getByRole('group', { name: 'Reply when it syncs' }))
    const promptRow = within(screen.getByRole('group', { name: 'Sync OK' }))

    // The live reading is in, and drives the warnings, but not the library's problem.
    expect(await row.findByText('It replies on every pull request.')).toBeInTheDocument()
    expect(row.queryByText(/No prompt in your library/)).not.toBeInTheDocument()
    expect(row.queryByText(/It can’t be applied like this/)).not.toBeInTheDocument()
    expect(row.getByText('It runs “Sync OK”, which this card adds: Add adds it first.')).toBeInTheDocument()
    expect(row.getByText(/reply with “Sync OK”/)).toBeInTheDocument()

    await user.click(row.getByRole('button', { name: 'Add' }))

    expect(await row.findByText('Added, switched off')).toBeInTheDocument()
    expect(promptRow.getByText('Added to your prompts')).toBeInTheDocument()
    expect(platform.createPrompt).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ nickname: 'Sync OK', purpose: 'reply', replyAction: 'comment' }),
    )
    expect(platform.saveRoutine).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ id: 0, name: 'Reply when it syncs', mode: 'ask', spec: syncSpec() }),
    )
    expect(platform.createPrompt.mock.invocationCallOrder[0]).toBeLessThan(
      platform.saveRoutine.mock.invocationCallOrder[0] ?? 0,
    )
    expect(platform.settleDescribeItCard.mock.calls.map(([req]) => req)).toEqual([
      { ref: card.ref, part: 'prompt', index: 0, kind: 'added', words: 'Added the prompt “Sync OK”', routineId: 0 },
      {
        ref: card.ref,
        part: 'routine',
        index: 0,
        kind: 'added',
        words: 'Added “Reply when it syncs”, switched off',
        routineId: 11,
      },
    ])
    expect(kit.onSettled).toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: 'Apply all' })).not.toBeInTheDocument()
  })

  it('saves nothing when a prompt it adds first fails, and says why', async () => {
    const user = userEvent.setup()

    readsTheLibrary()
    platform.createPrompt.mockRejectedValueOnce(new Error('Another prompt has that nickname.'))
    show(syncCard())

    const row = within(screen.getByRole('group', { name: 'Reply when it syncs' }))

    expect(await row.findByText('It replies on every pull request.')).toBeInTheDocument()
    await user.click(row.getByRole('button', { name: 'Add' }))

    expect(await row.findByRole('alert')).toHaveTextContent(
      'Stopped at the prompt “Sync OK”: Another prompt has that nickname.',
    )
    expect(platform.saveRoutine).not.toHaveBeenCalled()
    expect(platform.settleDescribeItCard).not.toHaveBeenCalled()
    expect(row.getByRole('button', { name: 'Add' })).toBeEnabled()
    expect(row.getByText('It runs “Sync OK”, which this card adds: Add adds it first.')).toBeInTheDocument()
    expect(within(screen.getByRole('group', { name: 'Sync OK' })).getByRole('button', { name: 'Add' })).toBeEnabled()
  })

  it('holds the prompt’s own Add while the routine’s row adds it first', async () => {
    const user = userEvent.setup()
    let added: (() => void) | undefined

    readsTheLibrary()
    platform.createPrompt.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          added = () => {
            resolve(undefined as never)
          }
        }),
    )
    show(syncCard())

    const row = within(screen.getByRole('group', { name: 'Reply when it syncs' }))
    const promptRow = within(screen.getByRole('group', { name: 'Sync OK' }))

    expect(await row.findByText('It replies on every pull request.')).toBeInTheDocument()
    await user.click(row.getByRole('button', { name: 'Add' }))

    await waitFor(() => {
      expect(promptRow.getByRole('button', { name: 'Add' })).toBeDisabled()
    })

    added?.()

    expect(await row.findByText('Added, switched off')).toBeInTheDocument()
    expect(platform.createPrompt).toHaveBeenCalledOnce()
  })

  it('reads a routine again once the prompt it waits on is added, and adds the routine alone', async () => {
    const user = userEvent.setup()

    readsTheLibrary()
    show(syncCard())

    const row = within(screen.getByRole('group', { name: 'Reply when it syncs' }))
    const promptRow = within(screen.getByRole('group', { name: 'Sync OK' }))

    expect(await row.findByText('It replies on every pull request.')).toBeInTheDocument()

    const reads = platform.describeRoutine.mock.calls.length

    await user.click(promptRow.getByRole('button', { name: 'Add' }))
    expect(await promptRow.findByText('Added to your prompts')).toBeInTheDocument()

    await waitFor(() => {
      expect(platform.describeRoutine.mock.calls.length).toBeGreaterThan(reads)
    })
    expect(row.queryByText(/which this card adds/)).not.toBeInTheDocument()
    expect(row.queryByText(/No prompt in your library/)).not.toBeInTheDocument()

    await user.click(row.getByRole('button', { name: 'Add' }))
    expect(await row.findByText('Added, switched off')).toBeInTheDocument()
    expect(platform.createPrompt).toHaveBeenCalledOnce()
    expect(platform.saveRoutine).toHaveBeenCalledOnce()
  })

  it('holds a routine back on the library’s problem again once the prompt it waits on is discarded', async () => {
    const user = userEvent.setup()

    readsTheLibrary()
    show(syncCard())

    const row = within(screen.getByRole('group', { name: 'Reply when it syncs' }))
    const promptRow = within(screen.getByRole('group', { name: 'Sync OK' }))

    expect(await row.findByText('It replies on every pull request.')).toBeInTheDocument()
    expect(row.getByRole('button', { name: 'Add' })).toBeEnabled()

    await user.click(promptRow.getByRole('button', { name: 'Discard' }))
    expect(await promptRow.findByText('Discarded')).toBeInTheDocument()

    expect(await row.findByText('No prompt in your library has the nickname “Sync OK”.')).toBeInTheDocument()
    expect(row.getByText(/It can’t be applied like this/)).toBeInTheDocument()
    expect(row.getByRole('button', { name: 'Add' })).toBeDisabled()
    expect(row.queryByText(/which this card adds/)).not.toBeInTheDocument()
    expect(platform.createPrompt).not.toHaveBeenCalled()
  })

  it('adds the prompt a change runs before it saves the change', async () => {
    const user = userEvent.setup()

    readsTheLibrary()
    platform.listRoutines.mockResolvedValue([routine({ id: 3, enabled: false })])
    show(syncCard(true))

    const row = within(screen.getByRole('group', { name: 'Snooze passing bot PRs' }))

    expect(await row.findByText('It replies on every pull request.')).toBeInTheDocument()
    expect(row.getByText('It runs “Sync OK”, which this card adds: Save changes adds it first.')).toBeInTheDocument()

    await user.click(row.getByRole('button', { name: 'Save changes' }))

    expect(await row.findByText('Changes saved')).toBeInTheDocument()
    expect(platform.createPrompt.mock.invocationCallOrder[0]).toBeLessThan(
      platform.saveRoutine.mock.invocationCallOrder[0] ?? 0,
    )
    expect(platform.saveRoutine.mock.lastCall?.[0]).toMatchObject({ id: 3, spec: syncSpec() })
    expect(platform.settleDescribeItCard.mock.calls.map(([req]) => `${req.part}:${req.kind}`)).toEqual([
      'prompt:added',
      'routine:saved',
    ])
  })

  it('shows what Claude says and a new routine, and adds it switched off, in the mode chosen', async () => {
    const user = userEvent.setup()

    platform.routineMatchStored.mockResolvedValue({
      count: 2,
      pullRequests: 2,
      issues: 0,
      unknown: 0,
      incomplete: false,
    })

    const card = describeCard({
      proposal: {
        summary: 'Pins them.',
        routines: [{ name: 'Snooze passing bot PRs', mode: 'auto', spec: routineSpec() }],
      },
    })
    const kit = show(card)
    const article = within(
      screen.getByRole('article', { name: 'A routine that pins your pull requests when their checks fail.' }),
    )
    const row = within(article.getByRole('group', { name: 'Snooze passing bot PRs' }))

    expect(article.getByText('Claude says')).toBeInTheDocument()
    expect(row.getByText('New')).toBeInTheDocument()
    expect(row.getByText(/When checks pass on pull requests in Review requested/)).toBeInTheDocument()
    expect(
      within(row.getByRole('radiogroup', { name: 'Mode' })).getByRole('radio', { name: 'Ask first' }),
    ).toBeChecked()
    expect(
      row.getByText(
        'Claude suggests Autopilot. It starts in Ask first, as new routines do: change it here if you like.',
      ),
    ).toBeInTheDocument()
    expect(await row.findByText('Ready to run here.')).toBeInTheDocument()
    expect(await row.findByText('Matches 2 pull requests in your inbox now.')).toBeInTheDocument()
    expect(platform.routineMatchStored.mock.lastCall?.[0]).toMatchObject({ id: 0, name: 'Snooze passing bot PRs' })
    expect(article.queryByRole('button', { name: 'Apply all' })).not.toBeInTheDocument()

    await user.click(within(row.getByRole('radiogroup', { name: 'Mode' })).getByRole('radio', { name: 'Dry run' }))
    await user.click(row.getByRole('button', { name: 'Add' }))

    expect(await row.findByText('Added, switched off')).toBeInTheDocument()
    expect(platform.saveRoutine).toHaveBeenCalledWith({
      id: 0,
      name: 'Snooze passing bot PRs',
      description: '',
      mode: 'dryRun',
      spec: routineSpec(),
      templateKey: '',
    })
    expect(platform.settleDescribeItCard).toHaveBeenCalledWith({
      ref: card.ref,
      part: 'routine',
      index: 0,
      kind: 'added',
      words: 'Added “Snooze passing bot PRs”, switched off',
      routineId: 11,
    })
    expect(kit.onSettled).toHaveBeenCalledOnce()
    expect(row.queryByRole('button', { name: 'Add' })).not.toBeInTheDocument()

    await user.click(row.getByRole('button', { name: 'Show it' }))
    expect(kit.onShowRoutine).toHaveBeenCalledWith(11)
  })

  it('opens a new routine in the builder, and tries it on the items when asked', async () => {
    const user = userEvent.setup()
    const kit = show(describeCard())
    const row = within(screen.getByRole('group', { name: 'Snooze passing bot PRs' }))

    await user.click(row.getByRole('button', { name: 'Test' }))
    expect(platform.testRoutine).toHaveBeenCalledOnce()
    expect(
      await within(row.getByRole('region', { name: 'Test' })).findByRole('list', { name: 'Items it watches' }),
    ).toBeInTheDocument()

    await user.click(row.getByRole('button', { name: 'Hide the test' }))
    expect(row.queryByRole('region', { name: 'Test' })).not.toBeInTheDocument()

    await user.click(row.getByRole('button', { name: 'Edit in builder' }))
    expect(kit.onEditInBuilder).toHaveBeenCalledWith(
      expect.objectContaining({ id: 0, name: 'Snooze passing bot PRs', mode: 'ask' }),
    )
    expect(await row.findByText('Opened in the builder')).toBeInTheDocument()
    expect(platform.settleDescribeItCard.mock.lastCall?.[0]).toMatchObject({
      kind: 'builder',
      words: 'Opened “Snooze passing bot PRs” in the builder',
      routineId: 0,
    })
  })

  it('discards a part, and says why a discard or an add failed, keeping the buttons', async () => {
    const user = userEvent.setup()

    show(describeCard())

    const row = within(screen.getByRole('group', { name: 'Snooze passing bot PRs' }))

    platform.saveRoutine.mockRejectedValueOnce(new Error('Another routine has that name.'))
    await user.click(row.getByRole('button', { name: 'Add' }))
    expect(await row.findByRole('alert')).toHaveTextContent('Another routine has that name.')
    expect(platform.settleDescribeItCard).not.toHaveBeenCalled()

    platform.settleDescribeItCard.mockRejectedValueOnce(new Error('The card is gone.'))
    await user.click(row.getByRole('button', { name: 'Discard' }))
    expect(await row.findByText('It couldn’t be discarded: The card is gone.')).toBeInTheDocument()
    expect(row.getByRole('button', { name: 'Add' })).toBeEnabled()

    await user.click(row.getByRole('button', { name: 'Discard' }))
    expect(await row.findByText('Discarded')).toBeInTheDocument()
    expect(platform.settleDescribeItCard.mock.lastCall?.[0]).toMatchObject({
      kind: 'discarded',
      words: 'Discarded the routine “Snooze passing bot PRs”',
    })
  })

  it('keeps what it did when Claude can’t hear of it, and says so', async () => {
    const user = userEvent.setup()

    show(describeCard())

    const row = within(screen.getByRole('group', { name: 'Snooze passing bot PRs' }))

    platform.settleDescribeItCard.mockRejectedValueOnce(new Error('The card is gone.'))
    await user.click(row.getByRole('button', { name: 'Add' }))
    expect(await row.findByText('Added, switched off')).toBeInTheDocument()
  })

  it('keeps a routine with problems from being added, saying them where they are', async () => {
    platform.describeRoutine.mockResolvedValue(
      described({
        sentence: '',
        problems: [{ part: 'steps', field: '[0]', message: 'add a step' }],
        warnings: [{ part: 'watch', field: '', message: 'it watches a lot' }],
      }),
    )
    show(describeCard())

    const row = within(screen.getByRole('group', { name: 'Snooze passing bot PRs' }))

    expect(await row.findByText('Add a step.')).toBeInTheDocument()
    expect(row.getByText('It watches a lot.')).toBeInTheDocument()
    expect(row.getByText(/It can’t be applied like this/)).toBeInTheDocument()
    expect(row.getByRole('button', { name: 'Add' })).toBeDisabled()
    expect(row.queryByText(/in your inbox now/)).not.toBeInTheDocument()
  })

  it('says a search’s matches are Test’s to read, and what this Mac lacks', async () => {
    platform.draftReadiness.mockResolvedValue(
      routineReady({ ready: false, problems: [readyCheck({ id: 'gh', label: 'gh', found: 'isn’t installed' })] }),
    )
    platform.routineMatchStored.mockRejectedValue(new Error('The inbox can’t be read.'))

    const search = { ...routineSpec(), watch: { sources: [{ search: { kind: 'pr' as const, where: ['@me'] } }] } }

    show(describeCard({ checked: kitChecked({ routines: [kitCheckedRoutine({ spec: search })] }) }))

    const row = within(screen.getByRole('group', { name: 'Snooze passing bot PRs' }))

    expect(row.getByText('It watches a search, which Test reads.')).toBeInTheDocument()
    expect(await row.findByRole('list', { name: 'What this Mac lacks for it' })).toHaveTextContent(
      'gh: isn’t installed',
    )
    expect(platform.routineMatchStored).not.toHaveBeenCalled()
  })

  it('says why the inbox’s matches couldn’t be counted', async () => {
    platform.routineMatchStored.mockRejectedValue(new Error('The inbox can’t be read.'))
    show(describeCard())

    expect(await screen.findByText('The inbox can’t be read.')).toBeInTheDocument()
  })

  it('shows a change’s old and new sentences, keeps its mode, and saves it through the question about what it matches now', async () => {
    const user = userEvent.setup()

    platform.listRoutines.mockResolvedValue([routine({ id: 3, enabled: true, mode: 'auto', description: 'Its own.' })])
    platform.routineMatching.mockResolvedValue({ count: 3, pullRequests: 3, issues: 0, unknown: 0, incomplete: false })
    platform.describeRoutine.mockResolvedValue(
      described({ sentence: 'When checks pass on pull requests in Review requested or Yours, snooze it for 3 days.' }),
    )

    const card = changeCard({ stale: true })
    const kit = show(card)
    const row = within(screen.getByRole('group', { name: 'Snooze passing bot PRs' }))

    expect(row.getByText('Changes “Snooze passing bot PRs”')).toBeInTheDocument()
    expect(row.getByText('changed since Claude read it')).toBeInTheDocument()
    expect(row.getByText('Now').nextElementSibling).toHaveTextContent(
      /in Review requested, snooze it for 3 days\. Once per head commit/,
    )
    expect(row.getByText('With the change').nextElementSibling).toHaveTextContent(/Review requested or Yours/)
    expect(row.getByText('It keeps its own mode, Autopilot, and stays switched on.')).toBeInTheDocument()
    expect(row.queryByRole('radiogroup', { name: 'Mode' })).not.toBeInTheDocument()

    await user.click(row.getByRole('button', { name: 'Open the routine' }))
    expect(kit.onShowRoutine).toHaveBeenCalledWith(3)

    await user.click(row.getByRole('button', { name: 'Save changes' }))

    const question = await screen.findByRole('dialog', { name: '3 pull requests match right now.' })

    expect(platform.routineMatching.mock.lastCall?.[0]).toMatchObject({ id: 3, spec: changedSpec() })
    await user.click(within(question).getByRole('button', { name: 'Run on them too' }))

    expect(await row.findByText('Changes saved')).toBeInTheDocument()
    expect(row.queryByText('Now')).not.toBeInTheDocument()
    expect(
      row.getByText('When checks pass on pull requests in Review requested or Yours, snooze it for 3 days.'),
    ).toBeInTheDocument()
    expect(platform.saveRoutine).toHaveBeenCalledWith({
      id: 3,
      name: 'Snooze passing bot PRs',
      description: 'Its own.',
      mode: 'auto',
      spec: changedSpec(),
      current: 'run',
    })
    expect(platform.settleDescribeItCard.mock.lastCall?.[0]).toMatchObject({
      part: 'routine',
      kind: 'saved',
      words: 'Saved the changes to “Snooze passing bot PRs”',
      routineId: 3,
    })
  })

  it('says why what a routine that is on matches now couldn’t be counted, saving nothing', async () => {
    const user = userEvent.setup()

    platform.listRoutines.mockResolvedValue([routine({ id: 3, enabled: true })])
    platform.routineMatching.mockRejectedValueOnce(new Error('GitHub didn’t answer the search.'))
    show(changeCard())

    const row = within(screen.getByRole('group', { name: 'Snooze passing bot PRs' }))

    await user.click(row.getByRole('button', { name: 'Save changes' }))
    expect(await row.findByRole('alert')).toHaveTextContent('GitHub didn’t answer the search.')
    expect(platform.saveRoutine).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('changes nothing when the question is closed unanswered', async () => {
    const user = userEvent.setup()

    platform.listRoutines.mockResolvedValue([routine({ id: 3, enabled: true })])
    platform.routineMatching.mockResolvedValue({ count: 1, pullRequests: 1, issues: 0, unknown: 0, incomplete: false })
    show(changeCard())

    const row = within(screen.getByRole('group', { name: 'Snooze passing bot PRs' }))

    await user.click(row.getByRole('button', { name: 'Save changes' }))
    await screen.findByRole('dialog', { name: '1 pull request matches right now.' })
    await user.keyboard('{Escape}')

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(platform.saveRoutine).not.toHaveBeenCalled()
    expect(platform.settleDescribeItCard).not.toHaveBeenCalled()
    expect(row.queryByRole('alert')).not.toBeInTheDocument()
    expect(row.getByRole('button', { name: 'Save changes' })).toBeEnabled()
  })

  it('says why a change couldn’t be saved: the routines unread, the routine gone, or the save refused', async () => {
    const user = userEvent.setup()

    show(changeCard())

    const row = within(screen.getByRole('group', { name: 'Snooze passing bot PRs' }))

    platform.listRoutines.mockRejectedValueOnce(new Error('The routines couldn’t be read.'))
    await user.click(row.getByRole('button', { name: 'Save changes' }))
    expect(await row.findByRole('alert')).toHaveTextContent('The routines couldn’t be read.')

    platform.listRoutines.mockResolvedValueOnce([])
    await user.click(row.getByRole('button', { name: 'Save changes' }))
    expect(await row.findByText('“Snooze passing bot PRs” is gone, so the change can’t be saved.')).toBeInTheDocument()

    platform.listRoutines.mockResolvedValueOnce([routine({ id: 3, enabled: false })])
    platform.saveRoutine.mockRejectedValueOnce(new Error('The routine isn’t valid yet.'))
    await user.click(row.getByRole('button', { name: 'Save changes' }))
    expect(await row.findByText('The routine isn’t valid yet.')).toBeInTheDocument()
    expect(platform.routineMatching).not.toHaveBeenCalled()
  })

  it('names a change by its own name when Claude didn’t read the routine, and says when it doesn’t read yet', async () => {
    const user = userEvent.setup()

    platform.describeRoutine.mockResolvedValue(described({ sentence: '' }))
    show(changeCard({ before: null, name: 'Snooze it', sentence: '' }))

    const row = within(screen.getByRole('group', { name: 'Snooze it' }))

    expect(row.getByText('Changes “Snooze it”')).toBeInTheDocument()
    expect(row.getByText('With the change').nextElementSibling).toHaveTextContent('It doesn’t read yet.')
    expect(row.queryByText(/It keeps its own mode/)).not.toBeInTheDocument()

    await user.click(row.getByRole('button', { name: 'Save changes' }))
    expect(await row.findByText('“Snooze it” is gone, so the change can’t be saved.')).toBeInTheDocument()
  })

  it('says a change keeps a routine that is off switched off', () => {
    show(changeCard({ before: kitRoutine({ enabled: false, mode: 'ask' }) }))

    expect(screen.getByText('It keeps its own mode, Ask first, and stays switched off.')).toBeInTheDocument()
  })

  it('says a new routine doesn’t read yet while neither check reads it', async () => {
    platform.describeRoutine.mockResolvedValue(described({ sentence: '' }))
    show(describeCard({ checked: kitChecked({ routines: [kitCheckedRoutine({ sentence: '' })] }) }))

    expect(await screen.findByText('It doesn’t read yet.')).toBeInTheDocument()
  })

  it('says a routine a change changes is gone, and offers nothing to save', () => {
    show(changeCard({ before: null, gone: true }))

    const row = within(screen.getByRole('group', { name: 'Snooze passing bot PRs' }))

    expect(row.getByText('“Snooze passing bot PRs” is gone, so this change can’t be saved.')).toBeInTheDocument()
    expect(row.getByRole('button', { name: 'Save changes' })).toBeDisabled()
    expect(row.getByRole('button', { name: 'Open the routine' })).toBeDisabled()
    expect(row.queryByText(/It keeps its own mode/)).not.toBeInTheDocument()
  })

  it('shows a new prompt, its text on request, and adds it with the library’s defaults', async () => {
    const user = userEvent.setup()

    show(
      describeCard({
        proposal: { summary: 'A prompt.', prompts: [kitNewPrompt()] },
        checked: kitChecked({
          routines: [],
          prompts: [
            kitCheckedPrompt({ risks: [{ level: 'warning', text: 'An agent will run it on your pull requests.' }] }),
          ],
        }),
      }),
    )

    const row = within(screen.getByRole('group', { name: 'Fix it' }))

    expect(row.getByText('New prompt')).toBeInTheDocument()
    expect(row.getByText('Fix the failing checks · Pull requests · a coding agent runs it')).toBeInTheDocument()
    expect(row.getByRole('list', { name: 'Before you apply it' })).toHaveTextContent('Warning: An agent will run it')
    expect(row.queryByText(/Fix the failing checks of/)).not.toBeInTheDocument()

    await user.click(row.getByRole('button', { name: 'Show the prompt' }))
    expect(row.getByText('Fix the failing checks of {{.Repo}} #{{.Number}}.')).toBeInTheDocument()

    await user.click(row.getByRole('button', { name: 'Add' }))
    expect(await row.findByText('Added to your prompts')).toBeInTheDocument()
    expect(platform.createPrompt).toHaveBeenCalledWith({
      name: 'Fix the failing checks',
      nickname: 'Fix it',
      appliesTo: 'pr',
      body: 'Fix the failing checks of {{.Repo}} #{{.Number}}.',
      purpose: 'agent',
      replyAction: '',
      models: {},
      effort: '',
      planMode: null,
      worktree: null,
      extraFlags: {},
    })
    expect(platform.settleDescribeItCard.mock.lastCall?.[0]).toMatchObject({
      part: 'prompt',
      kind: 'added',
      words: 'Added the prompt “Fix it”',
    })
    expect(row.queryByRole('button', { name: 'Show it' })).not.toBeInTheDocument()
  })

  it('says why a prompt couldn’t be added, and keeps one with problems back', async () => {
    const user = userEvent.setup()

    show(
      describeCard({
        proposal: {
          summary: 'Two prompts.',
          prompts: [kitNewPrompt(), kitNewPrompt({ nickname: '', name: 'Review it' })],
        },
        checked: kitChecked({
          routines: [],
          prompts: [
            kitCheckedPrompt(),
            kitCheckedPrompt({
              index: 1,
              prompt: kitNewPrompt({ nickname: '', name: 'Review it' }),
              problems: ['Give it a nickname.'],
            }),
          ],
        }),
      }),
    )

    const fix = within(screen.getByRole('group', { name: 'Fix it' }))
    const review = within(screen.getByRole('group', { name: 'Review it' }))

    expect(review.getByRole('button', { name: 'Add' })).toBeDisabled()
    expect(review.getByText('Give it a nickname.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Apply all' })).toBeDisabled()
    expect(screen.getByText(/One part has problems, so Apply all is off/)).toBeInTheDocument()

    platform.createPrompt.mockRejectedValueOnce(new Error('Another prompt has that nickname.'))
    await user.click(fix.getByRole('button', { name: 'Add' }))
    expect(await fix.findByRole('alert')).toHaveTextContent('Another prompt has that nickname.')

    await user.click(review.getByRole('button', { name: 'Discard' }))
    expect(await review.findByText('Discarded')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Apply all' })).toBeEnabled()
  })

  it('says how many parts keep Apply all off', () => {
    const blocked = (nickname: string, index: number) =>
      kitCheckedPrompt({ index, prompt: kitNewPrompt({ nickname }), problems: ['Another prompt has that nickname.'] })

    show(
      describeCard({
        proposal: {
          summary: 'Three prompts.',
          prompts: [kitNewPrompt(), kitNewPrompt({ nickname: 'A' }), kitNewPrompt({ nickname: 'B' })],
        },
        checked: kitChecked({ routines: [], prompts: [kitCheckedPrompt(), blocked('A', 1), blocked('B', 2)] }),
      }),
    )

    expect(screen.getByText(/2 parts have problems, so Apply all is off/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Apply all' })).toBeDisabled()
  })

  it('shows a settings change, and applies each setting through its own setter', async () => {
    const user = userEvent.setup()
    const stored = settings({ trustedBots: ['stored'] })

    platform.setAutopilotAgentsAtOnce.mockResolvedValue(stored)

    const kit = show(
      describeCard({
        proposal: { summary: 'One at a time.', settings: { agentsAtOnce: 1 } },
        checked: kitChecked({
          routines: [],
          settings: kitCheckedSettings({ risks: [{ level: 'caution', text: 'Runs queue up.' }] }),
        }),
      }),
    )

    const row = within(screen.getByRole('group', { name: 'Autopilot’s settings' }))

    expect(row.getByRole('list', { name: 'What it changes' })).toHaveTextContent('Agents at once 2 → 1')
    expect(row.getByText('Runs queue up.')).toBeInTheDocument()

    platform.setAutopilotAgentsAtOnce.mockRejectedValueOnce(new Error('Keep it to 8 at once.'))
    await user.click(row.getByRole('button', { name: 'Apply' }))
    expect(await row.findByRole('alert')).toHaveTextContent('Keep it to 8 at once.')

    expect(kit.onSettingsSaved).not.toHaveBeenCalled()

    await user.click(row.getByRole('button', { name: 'Apply' }))
    expect(await row.findByText('Applied')).toBeInTheDocument()
    expect(platform.setAutopilotAgentsAtOnce).toHaveBeenLastCalledWith(1)
    expect(kit.onSettingsSaved).toHaveBeenCalledExactlyOnceWith(stored)
    expect(platform.settleDescribeItCard.mock.lastCall?.[0]).toMatchObject({
      part: 'settings',
      index: 0,
      kind: 'applied',
      words: 'Applied the settings change',
    })
  })

  it('discards a settings change and a rules change', async () => {
    const user = userEvent.setup()

    show(
      describeCard({
        proposal: { summary: 'Both.', settings: { agentsAtOnce: 1 }, rules: { trust: ['renovate[bot]'] } },
        checked: kitChecked({ routines: [], settings: kitCheckedSettings(), rules: kitCheckedRules() }),
      }),
    )

    const settingsRow = within(screen.getByRole('group', { name: 'Autopilot’s settings' }))
    const rulesRow = within(screen.getByRole('group', { name: 'The rules routines lean on' }))

    await user.click(settingsRow.getByRole('button', { name: 'Discard' }))
    expect(await settingsRow.findByText('Discarded')).toBeInTheDocument()
    await user.click(rulesRow.getByRole('button', { name: 'Discard' }))
    expect(await rulesRow.findByText('Discarded')).toBeInTheDocument()
    expect(platform.settleDescribeItCard.mock.calls.map(([req]) => req.words)).toEqual([
      'Discarded the settings change',
      'Discarded the rules change',
    ])
    expect(platform.setAutopilotAgentsAtOnce).not.toHaveBeenCalled()
    expect(platform.setTrustedBots).not.toHaveBeenCalled()
  })

  it('shows a rules change with its warning, and applies it', async () => {
    const user = userEvent.setup()
    const stored = settings({ trustedBots: ['stored'] })

    platform.setRepoRule.mockResolvedValue(stored)

    const kit = show(
      describeCard({
        proposal: { summary: 'Trust Renovate.', rules: { trust: ['renovate[bot]'], mute: ['acme-org/legacy'] } },
        checked: kitChecked({ routines: [], rules: kitCheckedRules() }),
      }),
    )

    const row = within(screen.getByRole('group', { name: 'The rules routines lean on' }))

    expect(row.getByRole('list', { name: 'What it changes' })).toHaveTextContent(
      'Trust renovate[bot]Mute acme-org/legacy',
    )
    expect(row.getByRole('list', { name: 'Before you apply it' })).toHaveTextContent(
      'Warning: Routines that act on trusted bots act on renovate[bot]’s pull requests too.',
    )

    platform.setTrustedBots.mockRejectedValueOnce(new Error('That isn’t a bot.'))
    await user.click(row.getByRole('button', { name: 'Apply' }))
    expect(await row.findByRole('alert')).toHaveTextContent('That isn’t a bot.')

    await user.click(row.getByRole('button', { name: 'Apply' }))
    expect(await row.findByText('Applied')).toBeInTheDocument()
    expect(platform.setTrustedBots).toHaveBeenLastCalledWith([
      'dependabot',
      'github-actions',
      'renovate',
      'renovate[bot]',
    ])
    expect(platform.setRepoRule).toHaveBeenLastCalledWith('acme-org/legacy', 'muted')
    expect(kit.onSettingsSaved).toHaveBeenCalledExactlyOnceWith(stored)
    expect(platform.settleDescribeItCard.mock.lastCall?.[0]).toMatchObject({
      part: 'rules',
      words: 'Applied the rules change',
    })
  })

  it('applies every part with Apply all, in order, each with its outcome', async () => {
    const user = userEvent.setup()
    const order: string[] = []

    platform.settleDescribeItCard.mockImplementation((req) => {
      order.push(`${req.part}:${String(req.index)}`)

      return Promise.resolve()
    })

    const kit = show(fullCard())

    await user.click(
      within(screen.getByRole('group', { name: 'Fix checks' })).getByRole('radio', { name: 'Autopilot' }),
    )
    await user.click(screen.getByRole('button', { name: 'Apply all' }))

    await waitFor(() => {
      expect(order).toEqual(['prompt:0', 'routine:0', 'settings:0', 'rules:0'])
    })
    expect(within(screen.getByRole('group', { name: 'Fix it' })).getByText('Added to your prompts')).toBeInTheDocument()
    expect(
      within(screen.getByRole('group', { name: 'Fix checks' })).getByText('Added, switched off'),
    ).toBeInTheDocument()
    expect(within(screen.getByRole('group', { name: 'Autopilot’s settings' })).getByText('Applied')).toBeInTheDocument()
    expect(
      within(screen.getByRole('group', { name: 'The rules routines lean on' })).getByText('Applied'),
    ).toBeInTheDocument()
    expect(platform.saveRoutine.mock.lastCall?.[0]).toMatchObject({ name: 'Fix checks', mode: 'auto' })
    expect(screen.queryByRole('button', { name: 'Apply all' })).not.toBeInTheDocument()
    expect(kit.onSettled).toHaveBeenCalled()
    expect(kit.onSettingsSaved).toHaveBeenCalledTimes(2)
  })

  it('stops Apply all at the first part that fails, saying which and why', async () => {
    const user = userEvent.setup()

    platform.setAutopilotAgentsAtOnce.mockRejectedValueOnce(new Error('Keep it to 8 at once.'))
    show(fullCard())

    await user.click(screen.getByRole('button', { name: 'Apply all' }))

    expect(await screen.findByText('Stopped at the settings change: Keep it to 8 at once.')).toBeInTheDocument()
    expect(
      within(screen.getByRole('group', { name: 'Fix checks' })).getByText('Added, switched off'),
    ).toBeInTheDocument()
    expect(platform.setTrustedBots).not.toHaveBeenCalled()
    expect(
      within(screen.getByRole('group', { name: 'Autopilot’s settings' })).getByRole('button', { name: 'Apply' }),
    ).toBeEnabled()
  })

  it('saves a change through the question within Apply all', async () => {
    const user = userEvent.setup()

    platform.listRoutines.mockResolvedValue([routine({ id: 3, enabled: true })])
    platform.routineMatching.mockResolvedValue({ count: 2, pullRequests: 2, issues: 0, unknown: 0, incomplete: false })

    const card = changeCard()

    show({
      ...card,
      proposal: { ...card.proposal, settings: { agentsAtOnce: 1 } },
      checked: { ...card.checked, settings: kitCheckedSettings() },
    })

    await user.click(screen.getByRole('button', { name: 'Apply all' }))
    await user.click(await screen.findByRole('button', { name: 'Only what changes from now on' }))

    expect(
      await within(screen.getByRole('group', { name: 'Autopilot’s settings' })).findByText('Applied'),
    ).toBeInTheDocument()
    expect(platform.saveRoutine.mock.lastCall?.[0]).toMatchObject({ id: 3, current: 'fromNow' })
  })

  it('shows what the card recorded in place of the buttons', async () => {
    const user = userEvent.setup()
    const card = fullCard()
    const kit = show({
      ...card,
      outcomes: [
        kitOutcome({ part: 'routine', index: 0, kind: 'added', routineId: 12 }),
        {
          proposal: card.ref.proposal,
          part: 'prompt',
          index: 0,
          kind: 'discarded',
          words: 'Discarded the prompt “Fix it”',
          at: '2026-06-15T12:01:00Z',
        },
      ],
    })

    expect(within(screen.getByRole('group', { name: 'Fix it' })).getByText('Discarded')).toBeInTheDocument()

    const routineRow = within(screen.getByRole('group', { name: 'Fix checks' }))

    expect(routineRow.getByText('Added, switched off')).toBeInTheDocument()
    expect(routineRow.queryByRole('radiogroup', { name: 'Mode' })).not.toBeInTheDocument()
    await user.click(routineRow.getByRole('button', { name: 'Show it' }))
    expect(kit.onShowRoutine).toHaveBeenCalledWith(12)
    expect(screen.getByRole('button', { name: 'Apply all' })).toBeEnabled()
  })

  it('discards a whole card from the user’s own Claude Code, and says why it couldn’t', async () => {
    const user = userEvent.setup()
    const discard = vi.fn<() => Promise<string>>().mockResolvedValueOnce('The card is gone.').mockResolvedValueOnce('')

    show(describeCard(), kitOf(), discard)

    await user.click(screen.getByRole('button', { name: 'Discard this card' }))
    expect(await screen.findByText('The card is gone.')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Discard this card' }))
    await waitFor(() => {
      expect(screen.queryByText('The card is gone.')).not.toBeInTheDocument()
    })
    expect(discard).toHaveBeenCalledTimes(2)
    expect(screen.queryByRole('button', { name: 'Apply all' })).not.toBeInTheDocument()
  })

  it('names a test’s items plainly while what a routine can do isn’t read yet, and by their kind once it is', async () => {
    const user = userEvent.setup()

    show(describeCard(), kitOf({ catalog: undefined }))

    const row = within(screen.getByRole('group', { name: 'Snooze passing bot PRs' }))

    await user.click(row.getByRole('button', { name: 'Test' }))
    expect(await row.findByText(/1 of 2 open items matches now/)).toBeInTheDocument()
  })

  it('names a test’s items by the kind a routine acts on once what it can do is read', async () => {
    const user = userEvent.setup()

    show(describeCard())

    const row = within(screen.getByRole('group', { name: 'Snooze passing bot PRs' }))

    await user.click(row.getByRole('button', { name: 'Test' }))
    expect(await row.findByText(/1 of 2 open pull requests matches now/)).toBeInTheDocument()
  })
})
