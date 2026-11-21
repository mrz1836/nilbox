import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { item, list, snapshot } from './test/fixtures'
import { platform, renderApp } from './test/platform'

// index holds the settings' index back until the test lets it load: its module loads once
// per test file, so only a file of its own sees the toolbar before the index is there.
const index = vi.hoisted(() => {
  let release: () => void = () => undefined
  const loaded = new Promise<void>((resolve) => {
    release = resolve
  })

  return { loaded, release }
})

vi.mock('./lib/settingsIndex', async (importOriginal) => {
  await index.loaded

  return importOriginal()
})

vi.mock('./platform', () => import('./test/platform').then((fake) => fake.platform))

beforeEach(() => {
  platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
})

// said is what the toolbar's field says it matches; its box holds it.
function said() {
  return within(screen.getByRole('banner')).getByRole('status')
}

describe('App’s settings search before the settings’ index has loaded', () => {
  it('counts nothing and tints nothing until the index is there, then counts what it finds', async () => {
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })

    await userEvent.keyboard('{Meta>},{/Meta}')
    await userEvent.type(await screen.findByRole('combobox', { name: 'Find a setting' }), 'theme')

    expect(said()).toBeEmptyDOMElement()
    expect(said().parentElement).not.toHaveClass('bg-accent/5')

    index.release()

    await waitFor(() => {
      expect(said()).toHaveTextContent(/^\d+ match(es)?$/)
    })
    expect(said().parentElement).toHaveClass('bg-accent/5')
  })
})
