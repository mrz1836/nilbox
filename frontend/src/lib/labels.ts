import { createContext } from 'react'

// Hidden labels: the patterns of the labels the inbox's cards leave out, as Settings → Cards
// stores them (internal/settings' KeyHiddenLabels). Only a card's labels read them; the
// filter, the issue sheet's label picker, Autopilot, and the prompts' variables still see
// every label.

// LabelMatcher reports whether a label's name matches one of the patterns it was made from.
export type LabelMatcher = (name: string) => boolean

// hidesNone matches no label, as an empty list of patterns does.
export const hidesNone: LabelMatcher = () => false

// defaultHiddenLabels are the patterns a fresh install hides, the backend's
// DefaultHiddenLabels: the cards use them until the settings load, so the size labels
// never flash in.
export const defaultHiddenLabels: readonly string[] = ['size/*']

// specials are the characters a regular expression reads as more than themselves.
const specials = /[.+^${}()|[\]\\]/g

// patternSource is a pattern as a regular expression's source: * is any run of characters,
// / included, ? is one character, and anything else is itself.
function patternSource(pattern: string): string {
  return pattern.toLowerCase().replace(specials, '\\$&').replaceAll('*', '.*').replaceAll('?', '.')
}

// labelMatcher compiles patterns once into a matcher of a label's whole name, ignoring case,
// as the backend's Autopilot matches its label patterns (internal/autopilot/eval's
// globMatch): * matches any run of characters, / included, ? matches one character, and
// anything else matches exactly. Blank patterns match nothing.
export function labelMatcher(patterns: readonly string[]): LabelMatcher {
  const sources = patterns.map((p) => p.trim()).filter((p) => p !== '')

  if (sources.length === 0) {
    return hidesNone
  }

  const re = new RegExp(`^(?:${sources.map(patternSource).join('|')})$`, 'su')

  return (name) => re.test(name.toLowerCase())
}

// HiddenLabelsContext gives the cards the matcher of the labels they leave out. The window
// provides it from the settings; without a provider, no label is hidden.
export const HiddenLabelsContext = createContext<LabelMatcher>(hidesNone)
