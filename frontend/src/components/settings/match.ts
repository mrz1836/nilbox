import { createContext, useContext } from 'react'

// SettingMatch tells the rows of the section on screen which of them a "Find a setting"
// search matched, by their labels. Outside the settings page, or with no search, none
// does.
export const SettingMatch = createContext<(label: string) => boolean>(() => false)

// SettingMarks are the attributes a row the search finds carries: its label, and
// data-match while the search matches it, which styles it and which the page scrolls to.
export interface SettingMarks {
  'data-setting': string
  'data-match': '' | undefined
}

// useSetting marks a block of a section the search finds by label: the attributes it
// carries, and whether the search matches it.
export function useSetting(label: string): [SettingMarks, boolean] {
  const match = useContext(SettingMatch)(label)

  return [{ 'data-setting': label, 'data-match': match ? '' : undefined }, match]
}

// rowMatchClass tints a matching row of a card; its corners follow the card's.
export const rowMatchClass = 'transition-colors first:rounded-t-lg last:rounded-b-lg data-match:bg-accent/10'

// cardMatchClass rings a matching card.
export const cardMatchClass = 'data-match:ring-2 data-match:ring-accent/40'

// headingMatchClass colors a matching heading.
export function headingMatchClass(match: boolean): string {
  return match ? 'text-accent' : ''
}
