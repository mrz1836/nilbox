import type { Settings } from '../../platform'

// SectionProps is what a section that changes settings is given: the settings as stored,
// and onSaved, which receives every setting as stored after a change, as the setter
// returned it.
export interface SectionProps {
  settings: Settings
  onSaved: (settings: Settings) => void
}
