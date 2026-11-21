// Tones: the theme colors the window marks a state with, and each tone's text and fill
// class, spelled out in full so Tailwind finds them. A tint, a tone at a low alpha behind a
// chip or a tag, stays with the map of the domain that draws it.

// Tone names a theme color (its --color-* token) by the state it marks; muted marks none.
export type Tone = 'accent' | 'info' | 'success' | 'warning' | 'caution' | 'danger' | 'muted'

// toneText is each tone's text color.
export const toneText: Readonly<Record<Tone, string>> = {
  accent: 'text-accent',
  info: 'text-info',
  success: 'text-success',
  warning: 'text-warning',
  caution: 'text-caution',
  danger: 'text-danger',
  muted: 'text-muted',
}

// toneFill is each tone's background color, as a dot or a bar fills with it.
export const toneFill: Readonly<Record<Tone, string>> = {
  accent: 'bg-accent',
  info: 'bg-info',
  success: 'bg-success',
  warning: 'bg-warning',
  caution: 'bg-caution',
  danger: 'bg-danger',
  muted: 'bg-muted',
}
