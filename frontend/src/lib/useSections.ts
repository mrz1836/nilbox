import { useState } from 'react'

import type { ScriptsSection } from '../platform'

// StoredSections are the Scripts tab's sections the user opened (true) or folded (false),
// as the settings keep them across starts; one left out shows as its page starts it.
export type StoredSections = Readonly<Partial<Record<ScriptsSection, boolean>>>

// OnSection stores a section the user opened or folded.
export type OnSection = (section: ScriptsSection, open: boolean) => void

// Sections says whether each of a page's sections is open, and opens or folds one.
export interface Sections {
  isOpen: (section: ScriptsSection) => boolean
  toggle: (section: ScriptsSection) => void
}

// useSections keeps a Scripts page's sections open or folded as the user left them: a
// click shows at once, and is stored through onSection for the next time the page, or the
// app, opens. Until the user clicks one, a section shows as stored, else as the page
// starts it (defaults, else open), so settings that load after the page still apply.
export function useSections(
  stored: StoredSections,
  onSection: OnSection,
  defaults: Readonly<Partial<Record<ScriptsSection, boolean>>> = {},
): Sections {
  // clicked holds what the user clicked while the page is open, ahead of what is stored.
  const [clicked, setClicked] = useState<Partial<Record<ScriptsSection, boolean>>>({})
  const isOpen = (section: ScriptsSection) => clicked[section] ?? stored[section] ?? defaults[section] ?? true

  return {
    isOpen,
    toggle: (section) => {
      const open = !isOpen(section)

      setClicked((was) => ({ ...was, [section]: open }))
      onSection(section, open)
    },
  }
}
