import { describe, expect, it } from 'vitest'

import { exportLine } from './shell'

describe('exportLine', () => {
  it.each([
    ['~/.local/bin', 'export PATH="$HOME/.local/bin:$PATH"'],
    ['~', 'export PATH="$HOME:$PATH"'],
    ['/opt/tools/bin', 'export PATH="/opt/tools/bin:$PATH"'],
    ['~/my "$tools"', 'export PATH="$HOME/my \\"\\$tools\\":$PATH"'],
    ['~/back`tick\\slash', 'export PATH="$HOME/back\\`tick\\\\slash:$PATH"'],
    ['~other/bin', 'export PATH="~other/bin:$PATH"'],
  ])('puts %s on the PATH', (folder, line) => {
    expect(exportLine(folder)).toBe(line)
  })
})
