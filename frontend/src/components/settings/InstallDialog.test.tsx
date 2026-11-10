import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { InstallPlan } from '../../platform'
import { installPlan, integrationTool } from '../../test/fixtures'
import { InstallDialog, type InstallKind } from './InstallDialog'

// download is what the dialog says is downloaded and where it goes, for mage-x 1.30.0.
const download = `${brand.name} downloads mage-x_1.30.0_darwin_arm64.tar.gz from the acme-org/mage-x release on GitHub, checks it against the release’s SHA-256 checksums, and puts magex in ~/.local/bin. Nothing else on your Mac changes.`

// renderDialog asks about kind for mage-x, with its plan changed by plan, and returns
// what the dialog hands its card.
function renderDialog(kind: InstallKind, plan: Partial<InstallPlan> = {}) {
  const onConfirm = vi.fn()
  const onCancel = vi.fn()
  const tool = integrationTool({
    install: installPlan({
      version: '1.30.0',
      archive: 'mage-x_1.30.0_darwin_arm64.tar.gz',
      replaces: '',
      replacesPath: '',
      foreign: false,
      ...plan,
    }),
  })

  render(<InstallDialog tool={tool} kind={kind} onConfirm={onConfirm} onCancel={onCancel} />)

  return { onConfirm, onCancel }
}

// shown is the open dialog's title, the paragraphs of its message, and its buttons.
function shown() {
  const dialog = screen.getByRole('dialog')
  const body = document.getElementById(dialog.getAttribute('aria-describedby') ?? '')

  return {
    title: screen.getByRole('heading').textContent,
    lines: Array.from(body?.querySelectorAll('p') ?? [], (p) => p.textContent),
    buttons: screen.getAllByRole('button').map((b) => b.textContent),
  }
}

describe('InstallDialog', () => {
  it('asks before installing, naming the archive, the release, and the folder', () => {
    renderDialog('install')

    expect(screen.getByRole('dialog', { name: 'Install mage-x 1.30.0?' })).toBeInTheDocument()
    expect(shown()).toEqual({ title: 'Install mage-x 1.30.0?', lines: [download], buttons: ['Cancel', 'Install'] })
  })

  it('asks before an update, naming the copy it replaces', () => {
    renderDialog('update', { replaces: '1.29.5', replacesPath: '~/.local/bin/magex' })

    expect(shown()).toEqual({
      title: 'Update mage-x to 1.30.0?',
      lines: [download, 'Replaces magex 1.29.5 at ~/.local/bin/magex.'],
      buttons: ['Cancel', 'Update'],
    })
  })

  it('asks before reinstalling over a copy that won’t say its version', () => {
    renderDialog('reinstall', { replacesPath: '~/.local/bin/magex' })

    expect(shown()).toEqual({
      title: 'Reinstall mage-x 1.30.0?',
      lines: [download, 'Replaces magex at ~/.local/bin/magex.'],
      buttons: ['Cancel', 'Reinstall'],
    })
  })

  it('asks before replacing a copy the app didn’t install', () => {
    renderDialog('update', { replaces: '1.28.0', replacesPath: '/usr/local/bin/magex', foreign: true })

    expect(shown()).toEqual({
      title: 'Update mage-x to 1.30.0?',
      lines: [
        download,
        'Replaces magex 1.28.0 at /usr/local/bin/magex.',
        `This copy wasn’t installed by ${brand.name}. Replace it with 1.30.0?`,
      ],
      buttons: ['Cancel', 'Replace'],
    })
  })

  it.each<[InstallKind, string]>([
    ['install', 'Install mage-x?'],
    ['update', 'Update mage-x?'],
    ['reinstall', 'Reinstall mage-x?'],
  ])('asks to %s the latest release before it is known', (kind, title) => {
    renderDialog(kind, { version: '', archive: '' })

    expect(shown()).toEqual({
      title,
      lines: [
        `${brand.name} downloads the latest mage-x release from GitHub, checks it against the release’s SHA-256 checksums, and puts magex in ~/.local/bin. Nothing else on your Mac changes.`,
      ],
      buttons: ['Cancel', kind.charAt(0).toUpperCase() + kind.slice(1)],
    })
  })

  it('asks to replace a copy with the latest release before it is known', () => {
    renderDialog('reinstall', {
      version: '',
      archive: '',
      replaces: '1.28.0',
      replacesPath: '~/bin/magex',
      foreign: true,
    })

    expect(shown().lines.at(-1)).toBe(
      `This copy wasn’t installed by ${brand.name}. Replace it with the latest release?`,
    )
  })

  it('confirms, or cancels with the button or Escape', async () => {
    const { onConfirm, onCancel } = renderDialog('install')

    await userEvent.click(screen.getByRole('button', { name: 'Install' }))

    expect(onConfirm).toHaveBeenCalledOnce()
    expect(onCancel).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await userEvent.keyboard('{Escape}')

    expect(onCancel).toHaveBeenCalledTimes(2)
    expect(onConfirm).toHaveBeenCalledOnce()
  })
})
