import { describe, expect, it } from 'vitest'

import { agentGroup, fileCheck, workflowsGroup } from '../test/fixtures'
import { changeText, changeTone, filesChip, groupsOf, sensitiveCount, shortHead } from './sensitive'

describe('filesChip', () => {
  it('names the categories of the sensitive files, red when any runs', () => {
    expect(filesChip(fileCheck())).toEqual({
      tone: 'danger',
      icon: 'alert',
      short: 'Agent config · Workflows',
      long: 'Changes sensitive files: Agent config (1 file), Workflows (1 file)',
    })
  })

  it('folds categories past two into a count, and says when not every file was checked', () => {
    const deps = { ...workflowsGroup(), category: 'dependencies', short: 'Dependencies', level: 'warning' }
    const review = { ...deps, category: 'review', short: 'Review rules' }

    const chip = filesChip(fileCheck({ groups: [agentGroup(), deps, review], complete: false }))

    expect(chip.short).toBe('Agent config · Dependencies +1')
    expect(chip.long).toBe(
      'Changes sensitive files: Agent config (1 file), Dependencies (1 file), Review rules (1 file); not every file was checked',
    )
  })

  it('is amber when only warning categories are flagged', () => {
    const deps = { ...workflowsGroup(), category: 'dependencies', short: 'Dependencies', level: 'warning' }

    expect(filesChip(fileCheck({ groups: [deps] })).tone).toBe('warning')
  })

  it('warns about a check that failed or skipped files', () => {
    expect(filesChip(fileCheck({ status: 'failed', groups: [], error: 'GitHub answered 502' }))).toEqual({
      tone: 'warning',
      icon: 'alert',
      short: 'Files not checked',
      long: 'Couldn’t check the changed files: GitHub answered 502',
    })
    expect(filesChip(fileCheck({ status: 'failed', groups: [], error: '' })).long).toBe(
      'Couldn’t check the changed files',
    )
    expect(filesChip(fileCheck({ status: 'partial', groups: [], checked: 3000, total: 4210 }))).toEqual({
      tone: 'warning',
      icon: 'alert',
      short: 'Not all files checked',
      long: 'Only 3,000 of 4,210 changed files were checked, none of them sensitive',
    })
  })

  it('is quiet for a pull request not checked yet, or checked with nothing found', () => {
    expect(filesChip(fileCheck({ status: 'pending', groups: [] }))).toMatchObject({
      tone: 'muted',
      icon: 'none',
      short: 'Files not checked yet',
    })
    expect(filesChip(fileCheck({ status: 'clear', groups: null, total: 1 }))).toEqual({
      tone: 'muted',
      icon: 'clear',
      short: '',
      long: 'No sensitive files among its 1 changed file',
    })
  })
})

describe('changes', () => {
  it('words how a file changed, with where a moved file came from', () => {
    const file = { path: 'a', previousPath: '', change: 'added', pattern: '', url: '' }

    expect(changeText(file)).toBe('added')
    expect(changeText({ ...file, change: 'removed' })).toBe('removed')
    expect(changeText({ ...file, change: 'modified' })).toBe('modified')
    expect(changeText({ ...file, change: 'renamed', previousPath: 'b' })).toBe('moved from b')
    expect(changeText({ ...file, change: 'renamed' })).toBe('renamed')
    expect(changeText({ ...file, change: 'copied', previousPath: 'b' })).toBe('copied from b')
    expect(changeText({ ...file, change: 'copied' })).toBe('copied')
    expect(changeText({ ...file, change: 'changed' })).toBe('changed')
  })

  it('colors additions, removals, and moves', () => {
    expect(changeTone('added')).toBe('success')
    expect(changeTone('removed')).toBe('danger')
    expect(changeTone('renamed')).toBe('danger')
    expect(changeTone('modified')).toBe('muted')
  })
})

describe('helpers', () => {
  it('counts the sensitive files and shortens a head', () => {
    expect(sensitiveCount(fileCheck())).toBe(2)
    expect(sensitiveCount(fileCheck({ groups: [{ ...agentGroup(), files: null }] }))).toBe(0)
    expect(groupsOf(fileCheck({ groups: null }))).toEqual([])
    expect(shortHead('a1b2c3d4e5f6')).toBe('a1b2c3d')
  })
})
