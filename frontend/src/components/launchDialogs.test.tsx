import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { Suspense, type ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { noChoices } from '../lib/launchChoices'
import { agents, item, itemResolution, launchHold, profile, prompts, resolution } from '../test/fixtures'
import type { LaunchControlsProps } from './LaunchControls'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// fresh loads the dialogs' module, and the launch controls that open them, anew: what a
// dialog's code brought in is the module's own, so each test starts with none of it in.
async function fresh() {
  vi.resetModules()

  const dialogs = await import('./launchDialogs')
  const { LaunchControls } = await import('./LaunchControls')

  return { ...dialogs, LaunchControls }
}

// settle waits for the code asked for to come in, and for all that waits on it to run.
async function settle() {
  await act(async () => {
    await vi.dynamicImportSettled()
  })
}

function controlsProps(overrides: Partial<LaunchControlsProps> = {}): LaunchControlsProps {
  return {
    item: item(),
    resolution: itemResolution(),
    agents: agents(),
    profiles: [profile()],
    prompts: prompts(),
    launchDefaults: [],
    onDefaultsSaved: vi.fn(() => Promise.resolve()),
    onManagePrompts: vi.fn(),
    onSettingsSaved: vi.fn(),
    ...overrides,
  }
}

// optionsButton is the arrow beside "Open in Claude", which opens the launch options.
function optionsButton(): HTMLElement {
  return screen.getByRole('button', { name: /^Launch options/ })
}

// waiting is a boundary whose fallback says so, where the dialogs' callers show nothing,
// so a test sees whether a dialog ever waited behind it.
function waiting(children: ReactNode) {
  return <Suspense fallback={<p>Waiting</p>}>{children}</Suspense>
}

// parts loads the dialogs anew (fresh) and draws each as its callers do: the launch
// options with their menu, the menu on its own, and the hold and take-over dialogs.
async function parts() {
  const loaded = await fresh()
  const { LaunchDialog, LaunchMenu, LaunchHoldDialog, TakeOverDialog } = loaded

  const menu = (
    <LaunchMenu
      item={item()}
      resolved={resolution()}
      choices={noChoices}
      onChoices={vi.fn()}
      agents={agents()}
      profiles={[profile()]}
      prompts={prompts()}
      agentName="Claude"
      onLaunch={vi.fn()}
      busy={false}
      onSave={vi.fn()}
      saving={false}
      status={null}
      onUndo={null}
      resolveError=""
      onManagePrompts={vi.fn()}
      onCancel={vi.fn()}
    />
  )

  return {
    ...loaded,
    menu,
    launchDialog: (
      <LaunchDialog id="launch" title="Open in Claude" item={item()} onClose={vi.fn()}>
        {menu}
      </LaunchDialog>
    ),
    holdDialog: <LaunchHoldDialog hold={launchHold()} agentName="Claude" onConfirm={vi.fn()} onCancel={vi.fn()} />,
    takeOverDialog: (
      <TakeOverDialog
        sessionId="s"
        live
        terminal="iTerm2"
        where="pr-7"
        agentName="Claude"
        onTaken={vi.fn()}
        onCancel={vi.fn()}
      />
    ),
  }
}

describe('launchDialogs', () => {
  it('opens the launch options in the click that opens them once their code is in', async () => {
    const { LaunchControls } = await fresh()
    render(<LaunchControls {...controlsProps()} />)

    // The control's first paint asked for the dialogs' code.
    await settle()
    fireEvent.click(optionsButton())

    const dialog = screen.getByRole('dialog', { name: 'Launch options' })
    expect(dialog).toHaveAttribute('open')
    expect(within(dialog).getByRole('combobox', { name: 'Prompt' })).toHaveFocus()
  })

  it('draws each preloaded dialog at once, never its boundary’s fallback', async () => {
    const { preloadDialogs, launchDialog, menu, holdDialog, takeOverDialog } = await parts()
    preloadDialogs()
    await settle()

    // Each render below is one act: what it shows as it returns is what it first drew.
    const { rerender } = render(waiting(launchDialog))
    expect(screen.getByRole('dialog', { name: 'Launch options' })).toHaveAttribute('open')
    expect(screen.getByRole('combobox', { name: 'Prompt' })).toBeInTheDocument()
    expect(screen.queryByText('Waiting')).not.toBeInTheDocument()

    rerender(waiting(menu))
    expect(screen.getByRole('combobox', { name: 'Prompt' })).toBeInTheDocument()
    expect(screen.queryByText('Waiting')).not.toBeInTheDocument()

    rerender(waiting(holdDialog))
    expect(screen.getByRole('button', { name: 'Open anyway' })).toBeInTheDocument()
    expect(screen.queryByText('Waiting')).not.toBeInTheDocument()

    rerender(waiting(takeOverDialog))
    expect(screen.getByRole('dialog', { name: 'Take over this run?' })).toHaveAttribute('open')
    expect(screen.queryByText('Waiting')).not.toBeInTheDocument()
  })

  it('keeps the launch options open, and focus in them, as the settings they wait for come', async () => {
    const { LaunchControls } = await fresh()
    const { rerender } = render(
      <LaunchControls {...controlsProps({ resolution: itemResolution({ problem: 'No prompt applies.' }) })} />,
    )

    // Opened before the dialogs' code is in, the options wait for it.
    fireEvent.click(optionsButton())

    // The item's settings load again, and the dialog says so in place of the problem.
    const dialog = await screen.findByRole('dialog', { name: 'Launch options' })
    rerender(<LaunchControls {...controlsProps({ resolution: null })} />)
    expect(within(dialog).getByText('Loading the launch settings…')).toBeInTheDocument()

    const opener = optionsButton()
    const refocused = vi.fn()
    opener.addEventListener('focus', refocused)

    // The settings come, and the menu takes Loading's place in the dialog still open.
    rerender(<LaunchControls {...controlsProps()} />)
    expect(dialog).toHaveAttribute('open')
    expect(within(dialog).getByRole('combobox', { name: 'Prompt' })).toBeInTheDocument()

    // Nothing hid the dialog in the meantime, which would have closed it, or given focus
    // back to the button it opened from.
    await settle()
    expect(screen.getByRole('dialog', { name: 'Launch options' })).toBe(dialog)
    expect(dialog).toHaveAttribute('open')
    expect(dialog.contains(document.activeElement)).toBe(true)
    expect(refocused).not.toHaveBeenCalled()
  })

  it('opens the launch options whole once their code comes, when they open before it', async () => {
    const { launchDialog } = await parts()
    render(waiting(launchDialog))

    // The dialog's code came with its menu's, so the menu is in it as it shows.
    expect(await screen.findByRole('dialog', { name: 'Launch options' })).toHaveAttribute('open')
    expect(screen.getByRole('combobox', { name: 'Prompt' })).toBeInTheDocument()
  })

  it('opens each other dialog once its code comes, when it opens before it', async () => {
    const { menu, holdDialog, takeOverDialog } = await parts()

    const { rerender } = render(waiting(menu))
    expect(await screen.findByRole('combobox', { name: 'Prompt' })).toBeInTheDocument()

    rerender(waiting(holdDialog))
    expect(await screen.findByRole('button', { name: 'Open anyway' })).toBeInTheDocument()

    rerender(waiting(takeOverDialog))
    expect(await screen.findByRole('dialog', { name: 'Take over this run?' })).toHaveAttribute('open')
  })

  it('leaves a dialog whose preload failed to load on its first opening', async () => {
    vi.resetModules()
    vi.doMock('./LaunchHoldDialog', () => {
      throw new Error('the chunk did not load')
    })

    const { preloadDialogs, LaunchHoldDialog } = await import('./launchDialogs')
    preloadDialogs()
    await settle()
    vi.doUnmock('./LaunchHoldDialog')

    render(waiting(<LaunchHoldDialog hold={launchHold()} agentName="Claude" onConfirm={vi.fn()} onCancel={vi.fn()} />))
    expect(await screen.findByRole('button', { name: 'Open anyway' })).toBeInTheDocument()
  })
})
