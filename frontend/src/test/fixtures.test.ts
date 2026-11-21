import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import { agentName, managed } from './fixtures'

describe('managed', () => {
  it('is a worktree’s path under the managed root, for example-repo unless said', () => {
    expect(managed('pr-7')).toBe(`~/projects/.${brand.slug}/worktrees/example-repo/pr-7`)
    expect(managed('issue-9', 'example-repo-2')).toBe(`~/projects/.${brand.slug}/worktrees/example-repo-2/issue-9`)
  })
})

describe('agentName', () => {
  it('names Codex, and Claude for any other agent', () => {
    expect(agentName('codex')).toBe('Codex')
    expect(agentName('claude')).toBe('Claude')
    expect(agentName('')).toBe('Claude')
  })
})
