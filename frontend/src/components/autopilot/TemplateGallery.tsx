import { useId } from 'react'

import { modeLabels } from '../../lib/autopilot'
import { suggestedFirst } from '../../lib/templates'
import type { AutopilotTemplate } from '../../platform'
import { captionClass } from '../controls'

export interface TemplateGalleryProps {
  templates: readonly AutopilotTemplate[]
  onPick: (template: AutopilotTemplate) => void
  // limit shows at most that many, the suggested first, as the empty tab does; every one
  // without it.
  limit?: number
}

// TemplateCard is one template: its icon in a tile, its name, which wraps rather than
// pushing the mode it suggests out of the card, and that mode as a quiet badge; what it
// does and what this build can't run of it yet; and, set apart at the card's foot, why it
// is suggested. A template whose step another part of the app still builds is listed all
// the same, never locked: its quick setup says what it needs.
//
// The card's three parts are rows of the list's grid (a subgrid), so cards side by side
// share each row's height: their summaries start, and their foot lines run, at one height
// whatever each says. A button laid over the whole card opens its quick setup, named by
// the template's name and described by the rest; the card's text lies outside it, since
// WebKit lays out a button's own children as it lays out a button's, not as their classes
// ask.
interface TemplateCardProps {
  template: AutopilotTemplate
  onPick: () => void
  // withWhy says the list has a row for why a template is suggested, as the suggested ones
  // have; a card without a why of its own leaves the row empty.
  withWhy: boolean
}

function TemplateCard({ template: t, onPick, withWhy }: TemplateCardProps) {
  const id = useId()
  const described = [
    `${id}-summary`,
    `${id}-mode`,
    t.needs.length > 0 ? `${id}-needs` : '',
    t.suggested !== '' ? `${id}-why` : '',
  ].filter((part) => part !== '')

  return (
    <li
      className={`relative ${withWhy ? 'row-span-3' : 'row-span-2'} grid min-w-0 grid-rows-subgrid gap-y-2.5 rounded-lg border border-border bg-surface p-4 transition-colors hover:border-accent/50 hover:bg-raised`}
    >
      <button
        type="button"
        onClick={onPick}
        aria-labelledby={`${id}-name`}
        aria-describedby={described.join(' ')}
        className="absolute inset-0 rounded-lg"
      />
      <div className="flex min-w-0 items-center gap-3">
        <span
          aria-hidden="true"
          className="flex size-8 shrink-0 items-center justify-center rounded-md bg-raised text-base leading-none"
        >
          {t.icon}
        </span>
        <span id={`${id}-name`} className="min-w-0 flex-1 text-sm leading-snug font-medium text-text">
          {t.name}
        </span>
        <span
          id={`${id}-mode`}
          className="shrink-0 rounded-full border border-border px-2 text-caption leading-[1.125rem] font-medium whitespace-nowrap text-muted"
        >
          {modeLabels[t.suggests]}
        </span>
      </div>
      <div className="flex min-w-0 flex-col gap-1.5">
        <p id={`${id}-summary`} className="text-body leading-relaxed text-muted">
          {t.summary}
        </p>
        {t.needs.length > 0 && (
          <p id={`${id}-needs`} className="text-xs text-warning">
            Needs {t.needs.join(', ')}
          </p>
        )}
      </div>
      {t.suggested !== '' && (
        <p className="flex min-w-0 items-start gap-2 border-t border-border pt-2.5 text-xs leading-relaxed text-muted">
          <span aria-hidden="true" className="mt-[0.4375rem] size-1.5 shrink-0 rounded-full bg-accent" />
          <span id={`${id}-why`}>{t.suggested}</span>
        </p>
      )}
    </li>
  )
}

// TemplateGallery lists the templates, the ones that fit what this Mac already knows first,
// under Suggested for you, each with why; picking one opens its quick setup.
export function TemplateGallery({ templates, onPick, limit }: TemplateGalleryProps) {
  const id = useId()
  const { suggested, rest } = suggestedFirst(templates)
  const shownSuggested = limit === undefined ? suggested : suggested.slice(0, limit)
  const shownRest = limit === undefined ? rest : rest.slice(0, Math.max(0, limit - shownSuggested.length))

  // list lays the cards out two to a row, each card's parts rows of the list's grid, with a
  // row for why only where a card has one.
  const list = (items: AutopilotTemplate[], label: string) => {
    const withWhy = items.some((t) => t.suggested !== '')

    return (
      <ul aria-label={label} className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {items.map((t) => (
          <TemplateCard
            key={t.key}
            template={t}
            withWhy={withWhy}
            onPick={() => {
              onPick(t)
            }}
          />
        ))}
      </ul>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {shownSuggested.length > 0 && (
        <section aria-labelledby={`${id}-suggested`} className="flex flex-col gap-2">
          <h3 id={`${id}-suggested`} className={captionClass}>
            Suggested for you
          </h3>
          {list(shownSuggested, 'Suggested templates')}
        </section>
      )}
      {shownRest.length > 0 && (
        <section
          aria-labelledby={shownSuggested.length > 0 ? `${id}-all` : undefined}
          aria-label={shownSuggested.length > 0 ? undefined : 'Templates'}
          className="flex flex-col gap-2"
        >
          {shownSuggested.length > 0 && (
            <h3 id={`${id}-all`} className={captionClass}>
              {limit === undefined ? 'More templates' : 'Templates'}
            </h3>
          )}
          {list(shownRest, 'Templates')}
        </section>
      )}
    </div>
  )
}
