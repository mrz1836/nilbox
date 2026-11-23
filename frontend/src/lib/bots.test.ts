import { describe, expect, it } from 'vitest'

import { foldBot } from './bots'

describe('foldBot', () => {
  it('writes a login as the bot lists keep it', () => {
    expect(foldBot('  Acme-Org-Automerge[bot] ')).toBe('acme-org-automerge')
    expect(foldBot('app/renovate')).toBe('renovate')
    expect(foldBot('dependabot')).toBe('dependabot')
    expect(foldBot('  ')).toBe('')
  })
})
