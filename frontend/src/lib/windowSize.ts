import type { WindowSize } from '../platform'

// WindowPreset is one of the window sizes the settings offer.
export interface WindowPreset {
  label: string
  size: WindowSize
}

// windowPresets are the window sizes the settings offer, in points, smallest first.
// Compact suits a 13-inch laptop; the larger ones suit bigger screens. A size the screen
// cannot hold opens as large as the screen allows.
export const windowPresets: readonly WindowPreset[] = [
  { label: 'Compact', size: { width: 1100, height: 720 } },
  { label: 'Standard', size: { width: 1280, height: 800 } },
  { label: 'Large', size: { width: 1440, height: 900 } },
  { label: 'Extra large', size: { width: 1680, height: 1050 } },
]

// sizeKey identifies a size, as "1280x800": a preset's value in a control.
export function sizeKey(size: WindowSize): string {
  return `${String(size.width)}x${String(size.height)}`
}

// sizeLabel names a size for people, as "1280 × 800".
export function sizeLabel(size: WindowSize): string {
  return `${String(size.width)} × ${String(size.height)}`
}

// presetFor returns the preset a key names, or undefined for any other.
export function presetFor(key: string): WindowPreset | undefined {
  return windowPresets.find((p) => sizeKey(p.size) === key)
}
