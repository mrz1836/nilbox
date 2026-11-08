import { describe, expect, it } from 'vitest'

import { item, linkedItem, mainWorktree, worktree, worktreeRepo, worktreesSnapshot } from '../test/fixtures'
import { itemKey, itemWorktrees, worktreeOf } from './itemWorktrees'

describe('itemWorktrees', () => {
  it('lists nothing before the first listing', () => {
    expect(itemWorktrees(null).size).toBe(0)
  })

  it('maps each linked worktree to its item, leaving out the main, prunable, and unlinked ones', () => {
    const linked = worktree({ item: linkedItem() })
    const snapshot = worktreesSnapshot({
      repos: [
        worktreeRepo({
          worktrees: [
            mainWorktree({ item: linkedItem({ number: 1 }) }),
            linked,
            worktree({ path: '/gone', prunable: true, item: linkedItem({ number: 9 }) }),
            worktree({ path: '/scratch', item: null }),
          ],
        }),
      ],
    })

    const all = itemWorktrees(snapshot)

    expect([...all.keys()]).toEqual(['acme-org/example-repo#7'])
    expect(worktreeOf(all, item())).toBe(linked)
    expect(worktreeOf(all, item({ number: 9 }))).toBeUndefined()
  })

  it('keeps the first worktree listed for an item', () => {
    const first = worktree({ item: linkedItem() })
    const second = worktree({ path: '/other', item: linkedItem() })
    const all = itemWorktrees(worktreesSnapshot({ repos: [worktreeRepo({ worktrees: [first, second] })] }))

    expect(worktreeOf(all, item())).toBe(first)
  })

  it('matches repositories ignoring case', () => {
    expect(itemKey('Acme-Org/Example-Repo', 7)).toBe(itemKey('acme-org/example-repo', 7))

    const all = itemWorktrees(
      worktreesSnapshot({
        repos: [worktreeRepo({ worktrees: [worktree({ item: linkedItem({ owner: 'Acme-Org' }) })] })],
      }),
    )

    expect(worktreeOf(all, item({ repo: 'acme-org/EXAMPLE-repo' }))).toBeDefined()
  })
})
