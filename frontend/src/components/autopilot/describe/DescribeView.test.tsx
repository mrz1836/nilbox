import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Activity } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../../generated/brand'
import type { AutopilotDraft, AutopilotRunEvent, DescribeConversation, Settings } from '../../../platform'
import { deferred } from '../../../test/async'
import { renderAt } from '../../../test/clock'
import {
  autopilotCatalog,
  describeAttachment,
  describeCard,
  describeConversation,
  describeStatus,
  describeTurn,
  kitChecked,
  kitCheckedRoutine,
  kitCheckedSettings,
  routineSpec,
  runEvent,
  runEventsPage,
  settings,
} from '../../../test/fixtures'
import { platform, send } from '../../../test/platform'
import { DescribeView, type DescribeRequest } from './DescribeView'

vi.mock('../../../platform', () => import('../../../test/platform').then((fake) => fake.platform))

const id = '00000000-0000-4000-8000-000000000001'

// tool is the app's own tool named name, as an in-app turn names it.
function tool(name: string): string {
  return `mcp__${brand.slug}__${name}`
}

// events numbers a turn's events in order, each its own line of the log.
function events(...list: Partial<AutopilotRunEvent>[]): AutopilotRunEvent[] {
  return list.map((e, i) => runEvent({ seq: i + 1, offset: (i + 1) * 100, ...e }))
}

// conversation is the current conversation with the turns given.
function conversation(turns = [describeTurn()], overrides: Partial<DescribeConversation> = {}): DescribeConversation {
  return describeConversation({ id, turns, ...overrides })
}

// held are what the tab hands Describe it as it holds them: the settings stored, and the
// catalog read.
function held() {
  return { settings: settings(), catalog: autopilotCatalog() }
}

// show renders Describe it with request, and returns its callbacks.
function show(request?: DescribeRequest) {
  const calls = {
    onEditInBuilder: vi.fn<(draft: AutopilotDraft) => void>(),
    onShowRoutine: vi.fn<(id: number) => void>(),
    onSettingsSaved: vi.fn<(settings: Settings) => void>(),
  }
  const view = renderAt(<DescribeView {...(request === undefined ? {} : { request })} {...held()} {...calls} />)

  return { ...calls, ...view }
}

// composer is the message's form.
function composer() {
  return within(screen.getByRole('form', { name: 'Your message' }))
}

describe('DescribeView', () => {
  it('says what it does before the first message, with the agent new conversations start with', async () => {
    show()

    expect(screen.getByRole('heading', { name: 'Describe it' })).toBeInTheDocument()
    expect(await screen.findByText(/Claude proposes the routines, prompts, and settings it needs/)).toBeInTheDocument()
    expect(screen.getByText('Claude · default · sonnet')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'New conversation' })).toBeDisabled()
    expect(
      composer().getByText(
        'Claude sees what you type, how Autopilot works, and the names in your setup and inbox, never your items’ text or code.',
      ),
    ).toBeInTheDocument()
    expect(composer().getByRole('button', { name: /Send/ })).toBeDisabled()
  })

  it('sends what you type with ⌘↩, shows the turn running, and Stop stops it', async () => {
    const user = userEvent.setup()

    show()

    const box = composer().getByRole('textbox', { name: 'Tell Autopilot what you want' })

    await user.click(box)
    await user.keyboard('{Meta>}{Enter}{/Meta}')
    fireEvent.keyDown(box, { key: 'Enter', metaKey: true, isComposing: true })
    await user.type(box, 'Pin my failing pull requests.')
    fireEvent.keyDown(box, { key: 'Enter', metaKey: true, isComposing: true })
    await user.keyboard('{Enter}')
    expect(platform.sendToDescribeIt).not.toHaveBeenCalled()

    await user.keyboard('{Meta>}{Enter}{/Meta}')

    expect(platform.sendToDescribeIt).toHaveBeenCalledWith({ text: 'Pin my failing pull requests.\n', attach: [] })
    expect(await screen.findByText('Claude is working on it…')).toBeInTheDocument()
    expect(box).toHaveValue('')
    expect(screen.getByRole('log', { name: 'Your conversation with Claude' })).toHaveTextContent(
      'Pin my failing pull requests.',
    )

    await user.click(composer().getByRole('button', { name: 'Stop' }))
    expect(platform.stopDescribeIt).toHaveBeenCalledWith(id)
    expect(screen.getByRole('button', { name: 'New conversation' })).toBeDisabled()
  })

  it('brings the newest turn into sight as each one starts, and never before the first', async () => {
    const user = userEvent.setup()
    const scrolled = vi.spyOn(Element.prototype, 'scrollIntoView')

    show()
    await screen.findByText(/Claude proposes the routines/)
    expect(scrolled).not.toHaveBeenCalled()

    await user.type(composer().getByRole('textbox', { name: 'Tell Autopilot what you want' }), 'Pin them.')
    await user.click(composer().getByRole('button', { name: /Send/ }))

    await waitFor(() => {
      expect(scrolled).toHaveBeenCalledWith({ block: 'end' })
    })
  })

  it('sends with Send, and says why a message couldn’t be sent, keeping it', async () => {
    const user = userEvent.setup()

    platform.sendToDescribeIt.mockRejectedValueOnce(new Error('Claude Code is signed out.'))
    show()

    const box = composer().getByRole('textbox', { name: 'Tell Autopilot what you want' })

    await user.type(box, 'Trust Renovate.')
    await user.click(composer().getByRole('button', { name: /Send/ }))

    expect(await composer().findByRole('alert')).toHaveTextContent('Claude Code is signed out.')
    expect(box).toHaveValue('Trust Renovate.')
    expect(platform.describeItStatus).toHaveBeenCalledTimes(2)
  })

  it('keeps what you typed while a message was sent', async () => {
    const user = userEvent.setup()
    const answer = deferred<DescribeConversation>()

    platform.sendToDescribeIt.mockReturnValueOnce(answer.promise)
    show()

    const box = composer().getByRole('textbox', { name: 'Tell Autopilot what you want' })

    await user.type(box, 'First.')
    await user.click(composer().getByRole('button', { name: /Send/ }))
    expect(composer().getByRole('button', { name: /Sending/ })).toBeDisabled()
    await user.type(box, ' And more.')

    await act(async () => {
      answer.resolve(conversation([describeTurn({ text: 'First.', state: 'running' })], { running: true }))
      await Promise.resolve()
    })
    expect(box).toHaveValue('First. And more.')
  })

  it('is off while Claude Code isn’t ready, with the problem, the fix, and Check again', async () => {
    const user = userEvent.setup()

    platform.describeItStatus.mockResolvedValueOnce(
      describeStatus({ ready: false, problem: 'Claude Code isn’t installed.', fix: 'Install it, then check again.' }),
    )
    show()

    const alert = await composer().findByRole('alert')

    expect(alert).toHaveTextContent('Claude Code isn’t installed.')
    expect(alert).toHaveTextContent('Install it, then check again.')
    expect(composer().getByRole('textbox', { name: 'Tell Autopilot what you want' })).toBeDisabled()
    expect(composer().getByRole('button', { name: /Send/ })).toBeDisabled()

    await user.click(within(alert).getByRole('button', { name: 'Check again' }))
    await waitFor(() => {
      expect(composer().getByRole('textbox', { name: 'Tell Autopilot what you want' })).toBeEnabled()
    })
  })

  it('says a problem without a fix', async () => {
    platform.describeItStatus.mockResolvedValue(
      describeStatus({ ready: false, problem: 'Claude Code is too old.', fix: '' }),
    )
    show()

    expect(await composer().findByRole('alert')).toHaveTextContent('Claude Code is too old.')
  })

  it('sends a request’s message at once, and only once', async () => {
    const request: DescribeRequest = { seq: 1, text: '  Fix failing checks in the background.  ', attach: [] }
    const { rerender, unmount } = show(request)

    await waitFor(() => {
      expect(platform.sendToDescribeIt).toHaveBeenCalledWith({
        text: 'Fix failing checks in the background.',
        attach: [],
      })
    })
    expect(await screen.findByText('Claude is working on it…')).toBeInTheDocument()

    rerender(
      <DescribeView
        request={request}
        {...held()}
        onEditInBuilder={vi.fn()}
        onShowRoutine={vi.fn()}
        onSettingsSaved={vi.fn()}
      />,
    )
    unmount()
    show(request)
    await screen.findByText('Claude · default · sonnet')
    expect(platform.sendToDescribeIt).toHaveBeenCalledOnce()
  })

  it('does nothing for a request that holds nothing, and keeps a message that couldn’t be sent', async () => {
    const { rerender } = show({ seq: 1 })

    await screen.findByText('Claude · default · sonnet')
    expect(platform.sendToDescribeIt).not.toHaveBeenCalled()
    expect(composer().queryByRole('list', { name: 'Attached to your message' })).not.toBeInTheDocument()

    platform.sendToDescribeIt.mockRejectedValueOnce(new Error('A turn is running.'))
    rerender(
      <DescribeView
        request={{ seq: 2, text: 'Skip drafts.' }}
        {...held()}
        onEditInBuilder={vi.fn()}
        onShowRoutine={vi.fn()}
        onSettingsSaved={vi.fn()}
      />,
    )
    expect(await composer().findByRole('alert')).toHaveTextContent('A turn is running.')
    expect(composer().getByRole('textbox', { name: 'Tell Autopilot what you want' })).toHaveValue('Skip drafts.')
    expect(platform.routineNames).not.toHaveBeenCalled()
  })

  it('puts a request’s message and chips in the composer when it couldn’t be sent', async () => {
    platform.sendToDescribeIt.mockRejectedValueOnce(new Error('A turn is running.'))
    platform.routineNames.mockResolvedValue([{ id: 3, name: 'Snooze passing bot PRs', deleted: false }])
    show({ seq: 1, text: 'Skip drafts.', attach: [{ kind: 'routine', id: 3 }] })

    expect(await composer().findByRole('alert')).toHaveTextContent('A turn is running.')
    expect(composer().getByRole('textbox', { name: 'Tell Autopilot what you want' })).toHaveValue('Skip drafts.')
    expect(await composer().findByRole('list', { name: 'Attached to your message' })).toHaveTextContent(
      'Snooze passing bot PRs',
    )
  })

  it('puts a request’s attachments in the composer as chips, which send with the message and can be removed', async () => {
    const user = userEvent.setup()

    platform.routineNames.mockResolvedValue([{ id: 3, name: 'Snooze passing bot PRs', deleted: false }])
    platform.itemsByID.mockRejectedValue(new Error('The inbox can’t be read.'))

    const { rerender } = show({ seq: 1, attach: [{ kind: 'routine', id: 3 }] })
    const chips = await composer().findByRole('list', { name: 'Attached to your message' })

    expect(chips).toHaveTextContent('Snooze passing bot PRs')
    expect(platform.sendToDescribeIt).not.toHaveBeenCalled()

    rerender(
      <DescribeView
        request={{
          seq: 2,
          attach: [
            { kind: 'item', id: 42 },
            { kind: 'routine', id: 3 },
          ],
        }}
        {...held()}
        onEditInBuilder={vi.fn()}
        onShowRoutine={vi.fn()}
        onSettingsSaved={vi.fn()}
      />,
    )
    expect(await within(chips).findByText('Item 42')).toBeInTheDocument()
    expect(within(chips).getAllByRole('listitem')).toHaveLength(2)

    await user.click(within(chips).getByRole('button', { name: 'Remove Item 42' }))
    expect(within(chips).getAllByRole('listitem')).toHaveLength(1)

    await user.type(composer().getByRole('textbox', { name: 'Tell Autopilot what you want' }), 'Skip drafts.')
    await user.click(composer().getByRole('button', { name: /Send/ }))

    expect(platform.sendToDescribeIt).toHaveBeenCalledWith({
      text: 'Skip drafts.',
      attach: [{ kind: 'routine', id: 3 }],
    })
    await waitFor(() => {
      expect(composer().queryByRole('list', { name: 'Attached to your message' })).not.toBeInTheDocument()
    })
  })

  it('shows the conversation: your message, what Claude did and said, its card, and what it cost', async () => {
    const user = userEvent.setup()
    const reply = 'Here is a routine that pins them. It starts switched off, in Ask first.'

    platform.describeItConversation.mockResolvedValue(
      conversation(
        [
          describeTurn({
            attach: [describeAttachment()],
            feedback: 'Added “Fix checks”, switched off.',
            cards: [describeCard()],
          }),
        ],
        {
          cost: 0.041,
          estimated: true,
          tokensIn: 12000,
          tokensOut: 300,
          profile: 'work',
          model: 'opus',
          effort: 'high',
        },
      ),
    )
    platform.describeItEvents.mockResolvedValue(
      runEventsPage({
        done: true,
        events: events(
          { kind: 'tool', tool: tool('autopilot_guide') },
          { kind: 'toolResult', text: '# How Autopilot works' },
          { kind: 'retry', text: 'Retrying (1 of 10): overloaded' },
          { kind: 'limit', text: "Out of usage: its plan's five-hour limit is reached", error: true },
          { kind: 'tool', tool: tool('autopilot_inbox_names') },
          { kind: 'toolResult', text: 'The inbox’s names couldn’t be read. Try again.', error: true },
          { kind: 'text', text: 'Let me *check* it.' },
          { kind: 'tool', tool: tool('autopilot_propose') },
          { kind: 'toolResult', text: 'Proposed 1 routine. It shows in the Autopilot tab.' },
          { kind: 'text', text: reply },
        ),
      }),
    )

    const { onShowRoutine } = show()
    const log = within(await screen.findByRole('log', { name: 'Your conversation with Claude' }))

    expect(await log.findByText('Read how Autopilot works')).toBeInTheDocument()
    expect(log.getByText('Retrying (1 of 10): overloaded')).toHaveClass('text-muted')
    expect(log.getByText("Out of usage: its plan's five-hour limit is reached")).toHaveClass('text-danger')
    expect(log.getByText('Read the names in your inbox').parentElement).toHaveClass('text-danger')
    expect(log.getByRole('img', { name: 'failed' })).toBeInTheDocument()
    expect(log.getByText('The inbox’s names couldn’t be read')).toBeInTheDocument()
    expect(log.getByRole('list', { name: 'Attached' })).toHaveTextContent('Snooze passing bot PRs')
    expect(log.getByText('Claude heard: Added “Fix checks”, switched off.')).toBeInTheDocument()
    expect(log.getByText('check').tagName).toBe('EM')
    expect(log.getByText('Proposed 1 routine')).toBeInTheDocument()
    expect(log.getAllByText(reply)).toHaveLength(1)
    expect(log.getAllByRole('img', { name: 'done' })).toHaveLength(2)
    expect(screen.getByText('Claude · work · opus · high')).toBeInTheDocument()
    expect(screen.getByText('12.3k tokens')).toBeInTheDocument()
    expect(screen.getByText('about $0.04')).toBeInTheDocument()

    const card = within(log.getByRole('article', { name: /pins your pull requests/ }))

    await user.click(card.getByRole('button', { name: 'Add' }))
    await user.click(await card.findByRole('button', { name: 'Show it' }))
    expect(onShowRoutine).toHaveBeenCalledWith(11)
    expect(platform.describeItConversation).toHaveBeenCalledTimes(2)
  })

  it('follows a running turn as the shell says it changed, its words as they come, then its reply', async () => {
    platform.describeItConversation.mockResolvedValue(
      conversation([describeTurn({ state: 'running', reply: '', cost: null })], { running: true }),
    )
    platform.describeItEvents.mockResolvedValueOnce(
      runEventsPage({ events: events({ kind: 'text', text: 'Reading your setup first.' }) }),
    )
    show()

    const log = within(await screen.findByRole('log', { name: 'Your conversation with Claude' }))

    expect(await log.findByText('Reading your setup first.')).toBeInTheDocument()
    expect(log.getByText('Claude is working on it…')).toBeInTheDocument()

    platform.describeItEvents.mockResolvedValueOnce(
      runEventsPage({
        events: [
          runEvent({ seq: 2, offset: 200, kind: 'tool', tool: tool('autopilot_check') }),
          runEvent({ seq: 3, offset: 300, kind: 'toolResult', text: 'Checked 1 routine: no problems.' }),
          runEvent({ seq: 4, offset: 400, kind: 'tool', tool: tool('autopilot_propose') }),
        ],
      }),
    )
    send('describeit:updated', { conversationId: id, turn: 1 })
    expect(await log.findByText('Checked 1 routine: no problems')).toBeInTheDocument()
    expect(log.getByText('Proposing it')).toBeInTheDocument()
    expect(log.getByRole('img', { name: 'under way' })).toBeInTheDocument()
    expect(platform.describeItEvents).toHaveBeenLastCalledWith(id, 1, 1)

    platform.describeItConversation.mockResolvedValue(
      conversation([describeTurn({ reply: 'Done: one routine, waiting for you.', cards: [describeCard()] })]),
    )
    platform.describeItEvents.mockResolvedValueOnce(
      runEventsPage({
        done: true,
        events: [runEvent({ seq: 5, offset: 500, kind: 'toolResult', text: 'Proposed 1 routine.' })],
      }),
    )
    send('describeit:updated', { conversationId: id, turn: 1 })

    expect(await log.findByText('Done: one routine, waiting for you.')).toBeInTheDocument()
    expect(log.queryByText('Claude is working on it…')).not.toBeInTheDocument()
    expect(log.getByText('Proposed 1 routine')).toBeInTheDocument()
    expect(log.getByRole('article', { name: /pins your pull requests/ })).toBeInTheDocument()
    expect(platform.describeItEvents).toHaveBeenLastCalledWith(id, 1, 4)
  })

  it('says how a turn that didn’t finish ended, and why its events couldn’t be read', async () => {
    platform.describeItConversation.mockResolvedValue(
      conversation([
        describeTurn({ state: 'failed', reply: '', error: 'The app’s tools didn’t start, so the turn stopped.' }),
        describeTurn({ n: 2, state: 'failed', reply: '', error: '' }),
        describeTurn({ n: 3, state: 'stopped', reply: '' }),
      ]),
    )
    platform.describeItEvents.mockRejectedValue(new Error('The turn’s stream couldn’t be read.'))
    show()

    const log = within(await screen.findByRole('log', { name: 'Your conversation with Claude' }))

    expect(await log.findByText('The app’s tools didn’t start, so the turn stopped.')).toBeInTheDocument()
    expect(log.getByText('This turn ended in an error.')).toBeInTheDocument()
    expect(log.getByText('Stopped. Send another message to go on.')).toBeInTheDocument()
    expect(
      await log.findAllByText('What Claude did couldn’t be read: The turn’s stream couldn’t be read.'),
    ).toHaveLength(3)
  })

  it('shows an older card a later one revises in one line, which opens it', async () => {
    const user = userEvent.setup()
    const first = describeCard({
      ref: { source: id, proposal: '20260615T120010Z-000000000001' },
      summary: 'The first try.',
    })
    const second = describeCard({
      ref: { source: id, proposal: '20260615T120510Z-000000000002' },
      summary: 'The second try.',
      turn: 2,
    })

    platform.describeItConversation.mockResolvedValue(
      conversation([describeTurn({ cards: [first] }), describeTurn({ n: 2, cards: [second] })]),
    )
    show()

    const log = within(await screen.findByRole('log', { name: 'Your conversation with Claude' }))

    expect(await log.findByText('Revised below: The first try.')).toBeInTheDocument()
    expect(log.getAllByRole('article')).toHaveLength(1)

    await user.click(log.getByRole('button', { name: 'Show this card' }))
    expect(log.getAllByRole('article')).toHaveLength(2)
  })

  it('opens a new routine in the builder from a card', async () => {
    const user = userEvent.setup()

    platform.describeItConversation.mockResolvedValue(conversation([describeTurn({ cards: [describeCard()] })]))

    const { onEditInBuilder } = show()

    await user.click(await screen.findByRole('button', { name: 'Edit in builder' }))
    expect(onEditInBuilder).toHaveBeenCalledWith(expect.objectContaining({ name: 'Snooze passing bot PRs', id: 0 }))
  })

  it('hands the window the settings a card’s settings change left, reading none of its own', async () => {
    const user = userEvent.setup()
    const card = describeCard({
      proposal: { summary: 'One at a time.', settings: { agentsAtOnce: 1 } },
      checked: kitChecked({ routines: [], settings: kitCheckedSettings() }),
    })
    const stored = settings({ trustedBots: ['stored'] })

    platform.describeItConversation.mockResolvedValue(conversation([describeTurn({ cards: [card] })]))
    platform.setAutopilotAgentsAtOnce.mockResolvedValue(stored)

    const { onSettingsSaved } = show()
    const row = within(await screen.findByRole('group', { name: 'Autopilot’s settings' }))

    await user.click(row.getByRole('button', { name: 'Apply' }))
    expect(await row.findByText('Applied')).toBeInTheDocument()
    expect(onSettingsSaved).toHaveBeenCalledExactlyOnceWith(stored)
    expect(platform.getSettings).not.toHaveBeenCalled()
    expect(platform.getAutopilotCatalog).not.toHaveBeenCalled()
  })

  it('takes the settings as they change elsewhere, while it is hidden too', async () => {
    platform.describeItConversation.mockResolvedValue(conversation([describeTurn({ cards: [describeCard()] })]))

    const props = { onEditInBuilder: vi.fn(), onShowRoutine: vi.fn(), onSettingsSaved: vi.fn() }
    const inMode = (defaultMode: Settings['autopilot']['defaultMode']) =>
      settings({ autopilot: { ...settings().autopilot, defaultMode } })
    const { rerender } = renderAt(
      <Activity mode="visible">
        <DescribeView settings={inMode('ask')} catalog={autopilotCatalog()} {...props} />
      </Activity>,
    )
    const mode = () =>
      within(
        within(screen.getByRole('group', { name: 'Snooze passing bot PRs' })).getByRole('radiogroup', { name: 'Mode' }),
      )

    await screen.findByRole('group', { name: 'Snooze passing bot PRs' })
    expect(mode().getByRole('radio', { name: 'Ask first' })).toBeChecked()

    rerender(
      <Activity mode="hidden">
        <DescribeView settings={inMode('dryRun')} catalog={autopilotCatalog()} {...props} />
      </Activity>,
    )
    rerender(
      <Activity mode="visible">
        <DescribeView settings={inMode('dryRun')} catalog={autopilotCatalog()} {...props} />
      </Activity>,
    )
    expect(mode().getByRole('radio', { name: 'Dry run' })).toBeChecked()
    expect(platform.getSettings).not.toHaveBeenCalled()
  })

  it('starts a new routine in Ask first while the settings load', async () => {
    platform.describeItConversation.mockResolvedValue(conversation([describeTurn({ cards: [describeCard()] })]))
    renderAt(
      <DescribeView
        settings={null}
        catalog={undefined}
        onEditInBuilder={vi.fn()}
        onShowRoutine={vi.fn()}
        onSettingsSaved={vi.fn()}
      />,
    )

    const row = within(await screen.findByRole('group', { name: 'Snooze passing bot PRs' }))

    expect(
      within(row.getByRole('radiogroup', { name: 'Mode' })).getByRole('radio', { name: 'Ask first' }),
    ).toBeChecked()
  })

  it('starts a new conversation with the profile, model, and effort chosen', async () => {
    const user = userEvent.setup()

    platform.describeItConversation.mockResolvedValue(
      conversation([], { agent: { profile: 'default', model: '', effort: '' } }),
    )
    show()

    await user.click(await screen.findByRole('button', { name: 'Change' }))

    const form = within(screen.getByRole('form', { name: 'The agent for a new conversation' }))

    expect(await form.findByRole('option', { name: 'Opus' })).toBeInTheDocument()
    expect(form.getByRole('combobox', { name: 'Profile' })).toHaveValue('default')
    await user.selectOptions(form.getByRole('combobox', { name: 'Model' }), 'opus')
    await user.selectOptions(form.getByRole('combobox', { name: 'Effort' }), 'high')

    platform.newDescribeItConversation.mockRejectedValueOnce(new Error('That profile isn’t Claude Code’s.'))
    await user.click(form.getByRole('button', { name: 'Start a new conversation' }))
    expect(await form.findByRole('alert')).toHaveTextContent('That profile isn’t Claude Code’s.')

    await user.click(form.getByRole('button', { name: 'Start a new conversation' }))
    await waitFor(() => {
      expect(screen.queryByRole('form', { name: 'The agent for a new conversation' })).not.toBeInTheDocument()
    })
    expect(platform.newDescribeItConversation).toHaveBeenLastCalledWith({
      profile: 'default',
      model: 'opus',
      effort: 'high',
    })

    await user.click(screen.getByRole('button', { name: 'Change' }))
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('form', { name: 'The agent for a new conversation' })).not.toBeInTheDocument()
  })

  it('starts the form from the agent new conversations start with, before there is a conversation', async () => {
    const user = userEvent.setup()

    platform.describeItStatus.mockResolvedValue(
      describeStatus({ agent: { profile: 'default', model: 'haiku', effort: '' }, model: 'haiku' }),
    )
    show()

    await screen.findByText('Claude · default · haiku')
    await user.click(screen.getByRole('button', { name: 'Change' }))

    const form = within(screen.getByRole('form', { name: 'The agent for a new conversation' }))

    expect(await form.findByRole('option', { name: 'Haiku' })).toBeInTheDocument()
    expect(form.getByRole('combobox', { name: 'Model' })).toHaveValue('haiku')
    expect(form.getByRole('combobox', { name: 'Profile' })).toHaveValue('default')
  })

  it('starts the form from Inherit while nothing is read yet', async () => {
    const user = userEvent.setup()

    platform.describeItStatus.mockReturnValue(new Promise(() => undefined))
    show()

    await user.click(screen.getByRole('button', { name: 'Change' }))

    const form = within(screen.getByRole('form', { name: 'The agent for a new conversation' }))

    expect(form.getByRole('combobox', { name: 'Model' })).toHaveValue('')
    expect(form.getByRole('combobox', { name: 'Effort' })).toHaveValue('')
  })

  it('offers a model or effort the agent doesn’t list, as the conversation has it', async () => {
    const user = userEvent.setup()

    platform.describeItConversation.mockResolvedValue(
      conversation([], { agent: { profile: 'gone', model: 'claude-fable-9', effort: 'turbo' } }),
    )
    show()

    await user.click(await screen.findByRole('button', { name: 'Change' }))

    const form = within(screen.getByRole('form', { name: 'The agent for a new conversation' }))

    expect(await form.findByRole('option', { name: 'claude-fable-9' })).toBeInTheDocument()
    expect(form.getByRole('combobox', { name: 'Profile' })).toHaveValue('gone')
    expect(form.getByRole('combobox', { name: 'Effort' })).toHaveValue('turbo')
  })

  it('asks before setting a conversation with turns aside, and starts an empty one at once', async () => {
    const user = userEvent.setup()

    platform.describeItConversation.mockResolvedValue(conversation())
    show()

    await user.click(await screen.findByRole('button', { name: 'New conversation' }))

    const question = screen.getByRole('dialog', { name: 'Start a new conversation?' })

    await user.click(within(question).getByRole('button', { name: 'Cancel' }))
    expect(platform.newDescribeItConversation).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'New conversation' }))
    await user.click(
      within(screen.getByRole('dialog', { name: 'Start a new conversation?' })).getByRole('button', {
        name: 'Start a new conversation',
      }),
    )
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(platform.newDescribeItConversation).toHaveBeenCalledWith({ profile: '', model: '', effort: '' })

    await user.click(screen.getByRole('button', { name: 'New conversation' }))
    expect(platform.newDescribeItConversation).toHaveBeenCalledTimes(2)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('says why a new conversation couldn’t start', async () => {
    const user = userEvent.setup()

    platform.describeItConversation.mockResolvedValue(conversation([]))
    platform.newDescribeItConversation.mockRejectedValueOnce(new Error('A turn is running.'))
    show()

    await user.click(await screen.findByRole('button', { name: 'New conversation' }))
    expect(await screen.findByText('A turn is running.')).toBeInTheDocument()
  })

  it('says why the conversation couldn’t be read', async () => {
    platform.describeItConversation.mockRejectedValue(new Error('Describe it isn’t ready in this build yet.'))
    show()

    expect(await screen.findByRole('alert')).toHaveTextContent('Describe it isn’t ready in this build yet.')
    expect(screen.queryByText('Reading the conversation…')).not.toBeInTheDocument()
  })

  it('reads the conversation while it waits for it', () => {
    platform.describeItConversation.mockReturnValue(new Promise(() => undefined))
    platform.describeItStatus.mockReturnValue(new Promise(() => undefined))
    show()

    expect(screen.getByText('Reading the conversation…')).toBeInTheDocument()
    expect(screen.queryByText(/^Claude ·/)).not.toBeInTheDocument()
  })

  it('shows the cards from your own Claude Code above the conversation', async () => {
    platform.describeItProposals.mockResolvedValue([
      describeCard({
        ref: { source: 'outside', proposal: '20260615T115500Z-000000000003' },
        where: 'example-repo',
        at: '2026-06-15T11:55:00Z',
        turn: 0,
        checked: kitChecked({ routines: [kitCheckedRoutine({ spec: routineSpec() })] }),
      }),
    ])
    show()

    const outside = within(await screen.findByRole('region', { name: 'From your Claude Code' }))

    expect(outside.getByText('A session in example-repo,')).toBeInTheDocument()
  })
})
