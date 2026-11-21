import { use, useId, useState, type ReactNode } from 'react'

import { guideItem, guideLaunchOptions, guideNudgeItem, guideResolution, guideSession } from '../../lib/cardGuide'
import { CardPrefsContext, defaultCardPrefs, type CardPrefs } from '../../lib/cardPrefs'
import type { Edge } from '../../lib/nextStep'
import { CardPicturesContext, type Pictures } from '../../lib/pictures'
import { useClock } from '../../lib/useNow'
import { AutopilotRowsContext } from '../autopilot/AutopilotChip'
import { captionClass } from '../controls'
import { BotIcon, CheckIcon, ClockIcon, RereviewIcon, XIcon } from '../icons'
import type { EditActions } from '../ItemMenu'
import { ItemRow } from '../ItemRow'
import { faceOf, initialsOf } from './CardAvatar'
import { edgeBar } from './CardParts'
import { StatusDot } from '../StatusDot'
import { trackStepClass, type Look } from './StateTrack'

// Side is where a part's marker sits: centered on the edge, over the part's start, or under
// it.
type Side = 'center' | 'above' | 'below'

// GuidePart is one part of a card the guide numbers: the data-guide name the card's part
// carries, where its marker sits, and what the legend says of it.
export interface GuidePart {
  part: string
  side: Side
  title: string
  text: string
}

// guideParts are the parts the guide numbers, 1 to 8, in the legend's order.
export const guideParts: readonly GuidePart[] = [
  {
    part: 'edge',
    side: 'center',
    title: 'Whose move',
    text: 'The left edge: accent, your move; amber, something waits on you now; blue, an agent is on it; green, ready to ship; red, blocked; grey, waiting on others.',
  },
  {
    part: 'avatar',
    side: 'above',
    title: 'Who and what',
    text: 'The author’s picture, people round and bots square, with a pull request or issue badge (grey for a draft).',
  },
  {
    part: 'title',
    side: 'above',
    title: 'Title and labels',
    text: 'Up to two labels after the title, the rest as “+N”.',
  },
  {
    part: 'story',
    side: 'below',
    title: 'The story',
    text: 'What changed, or what’s asked of you, in one sentence.',
  },
  {
    part: 'byline',
    side: 'below',
    title: 'The byline',
    text: 'Repository, number, author, why it’s here, and when it last moved. Click the repository or the author to filter by it.',
  },
  {
    part: 'risk',
    side: 'below',
    title: 'Risk',
    text: 'Shown only when there is some: an outside contributor, or sensitive files.',
  },
  {
    part: 'track',
    side: 'above',
    title: 'Where it stands',
    text: 'Checks, review, merge: a tick when fine, a symbol when not, with the size below. Hover the card for Snooze, Dismiss, Pin, and more.',
  },
  {
    part: 'actions',
    side: 'above',
    title: 'Your move, and the agent’s',
    text: 'Your move is the filled button; the agent’s sits beside it, or its live status while it works.',
  },
]

// edgeKey is each edge's color with its words, the most urgent first.
const edgeKey: readonly { edge: Edge; words: string }[] = [
  { edge: 'urgent', words: 'Something waits on you now' },
  { edge: 'blocked', words: 'Blocked' },
  { edge: 'yours', words: 'Your move' },
  { edge: 'ready', words: 'Ready to ship' },
  { edge: 'agent', words: 'An agent is on it' },
  { edge: 'waiting', words: 'Waiting on others' },
]

// KeyLook is a look of the track's steps the key explains: every one but the dotted ring of
// nothing to say yet, and auto-merge's bolt.
type KeyLook = Extract<Look, 'ok' | 'bad' | 'rereview' | 'running' | 'held' | 'waiting'>

// keyIconClass is a symbol's mark, as the track draws it.
const keyIconClass = 'size-2.5 [stroke-width:2.4]'

// keyIcons are what a step of each look the key explains shows inside its circle.
const keyIcons: Readonly<Record<KeyLook, ReactNode>> = {
  ok: <CheckIcon className={keyIconClass} />,
  bad: <XIcon className={keyIconClass} />,
  rereview: <RereviewIcon className={keyIconClass} />,
  running: <StatusDot color="bg-warning" ping />,
  held: <ClockIcon className={keyIconClass} />,
  waiting: null,
}

// trackKey is each of the track's symbols with its words.
const trackKey: readonly { look: KeyLook; words: string }[] = [
  { look: 'ok', words: 'Fine' },
  { look: 'bad', words: 'Failing, or changes requested' },
  { look: 'rereview', words: 'Pushed since your review' },
  { look: 'running', words: 'Running' },
  { look: 'held', words: 'Waiting for approval' },
  { look: 'waiting', words: 'Waiting' },
]

// noPictures are the sample card's pictures: none, so its avatar shows the author's
// initials and nothing is downloaded for it.
const noPictures: Pictures = {}

// Spot is where a marker sits in the sample's box, in the box's own CSS pixels: its anchor,
// which its side's classes (spotClass) place it against.
interface Spot {
  left: number
  top: number
}

// Scale is how the sample's box measures on the screen against its laid-out size, across
// and down: below 1 while the dialog's pop-in still scales it as it opens.
interface Scale {
  x: number
  y: number
}

// scaleOf is the box's scale: its measured size over its laid-out size, or 1 while it has
// no laid-out size.
function scaleOf(el: HTMLElement, box: DOMRect): Scale {
  return {
    x: el.offsetWidth > 0 ? box.width / el.offsetWidth : 1,
    y: el.offsetHeight > 0 ? box.height / el.offsetHeight : 1,
  }
}

// sameSpots reports two measures that place every marker alike, so a measure that moves
// nothing renders nothing.
function sameSpots(a: readonly (Spot | null)[], b: readonly (Spot | null)[]): boolean {
  return a.every((spot, i) => {
    const other = b[i] ?? null

    return spot === null || other === null ? spot === other : spot.left === other.left && spot.top === other.top
  })
}

// spotClass places a marker against its anchor: centered on it, above it, or below it,
// each just before the part's start.
const spotClass: Readonly<Record<Side, string>> = {
  center: '-translate-1/2',
  above: '-mt-1 -ml-1 -translate-y-full',
  below: 'mt-1 -ml-1',
}

// spotOf is where the marker of a part sits, measured against the sample's box, or null
// while the part isn't laid out: missing, hidden by a narrow list, or in a dialog not
// shown yet. The action column's marker goes over its first button, the primary, wherever
// the column puts it. Boxes are measured on the screen, where the dialog's pop-in still
// scales the sample as it opens, and a transform changes no size, so nothing measures them
// again once it ends: a part's distance from the box's corner is divided by the box's
// scale, or the markers furthest right would land short of their parts.
function spotOf(guide: GuidePart, el: Element | null, box: DOMRect, scale: Scale): Spot | null {
  const anchor = guide.part === 'actions' ? el?.querySelector('button') : el
  const r = anchor?.getBoundingClientRect()

  if (r === undefined || (r.width === 0 && r.height === 0)) {
    return null
  }

  const x = (at: number) => (at - box.left) / scale.x
  const y = (at: number) => (at - box.top) / scale.y

  switch (guide.side) {
    case 'center':
      return { left: x(r.left + r.width / 2), top: y(r.top + r.height / 2) }
    case 'above':
      return { left: x(r.left), top: y(r.top) }
    case 'below':
      return { left: x(r.left), top: y(r.bottom) }
  }
}

// Sample is the real card, ItemRow, drawn from the guide's sample (lib/cardGuide) inside an
// inert wrapper: nothing in it can be clicked, focused, or fetched. It has no triage, no
// menu, and no sheet to open; an empty CardPicturesContext, so its avatar shows initials and
// asks for no picture; and no AutopilotRowsContext, so no routine's chip. It is always the
// comfortable card, the one the legend numbers, and shows the unread dot while Unread dots
// is on. With markers, it numbers its parts: each marker is placed from the part's measured
// box, in the sample's own pixels however the dialog scales it, after layout and again
// whenever the sample's box changes size, and stays hidden until its part is measured.
function Sample({ markers }: { markers: boolean }) {
  const { unread } = use(CardPrefsContext)
  const clock = useClock()
  // The sample is made once, as the guide opens, so the card keeps it while it shows.
  const [sample] = useState(() => {
    const now = clock.getSnapshot()

    return {
      item: guideItem(now),
      sessions: [guideSession(now)],
      resolution: guideResolution(),
      launch: guideLaunchOptions(),
    }
  })
  const [spots, setSpots] = useState<readonly (Spot | null)[]>(() => guideParts.map(() => null))
  const prefs: CardPrefs = { ...defaultCardPrefs, avatars: false, density: 'comfortable', unread }

  // measure follows the box the markers sit in, while there are markers.
  const measure = (el: HTMLDivElement | null) => {
    if (el === null || !markers) {
      return undefined
    }

    const place = () => {
      const box = el.getBoundingClientRect()
      const scale = scaleOf(el, box)
      const next = guideParts.map((guide) =>
        spotOf(guide, el.querySelector(`[data-guide="${guide.part}"]`), box, scale),
      )

      setSpots((was) => (sameSpots(was, next) ? was : next))
    }

    const sizes = new ResizeObserver(place)

    sizes.observe(el)
    place()

    return () => {
      sizes.disconnect()
    }
  }

  return (
    <div ref={measure} className={`relative ${markers ? 'py-7' : ''}`}>
      <div inert className="isolate overflow-hidden rounded-lg border border-border bg-surface text-left">
        <CardPrefsContext value={prefs}>
          <CardPicturesContext value={noPictures}>
            <AutopilotRowsContext value={null}>
              <ul className="@container">
                <ItemRow
                  item={sample.item}
                  launch={sample.launch}
                  resolution={sample.resolution}
                  sessions={sample.sessions}
                />
              </ul>
            </AutopilotRowsContext>
          </CardPicturesContext>
        </CardPrefsContext>
      </div>
      {markers &&
        guideParts.map((guide, i) => {
          const spot = spots[i] ?? null

          return (
            <span
              key={guide.part}
              aria-hidden="true"
              data-marker={guide.part}
              hidden={spot === null}
              style={spot === null ? undefined : { left: spot.left, top: spot.top }}
              className={`pointer-events-none absolute z-10 grid size-4.5 place-items-center rounded-full bg-accent text-marker leading-none font-bold text-background ring-3 ring-accent/25 ${spotClass[guide.side]}`}
            >
              {i + 1}
            </span>
          )
        })}
    </div>
  )
}

// guideEdits are the second sample's edits: none do anything, since the guide's cards take
// no clicks, but with them its card can offer Nudge.
const guideEdits: EditActions = {
  onCopyLink: () => undefined,
  onUnassign: () => Promise.resolve(),
  onRemoveReview: () => Promise.resolve(),
  onNudge: () => undefined,
}

// NudgeSample is the guide's second sample, in full: a pull request the user asked to
// change, whose author has been quiet two days, so its card offers Nudge in place of a
// quiet Open, as it does a day in with Settings → Reviewing's Offer a nudge after as it
// comes.
function NudgeSample() {
  const prefs = use(CardPrefsContext)
  const clock = useClock()
  const [sample] = useState(() => guideNudgeItem(clock.getSnapshot()))
  const shown: CardPrefs = { ...prefs, avatars: false, density: 'comfortable', nudgeAfterHours: 24 }

  return (
    <section aria-label="When the author goes quiet" className="flex flex-col gap-2">
      <p className={captionClass}>When the author goes quiet</p>
      <div inert className="isolate overflow-hidden rounded-lg border border-border bg-surface text-left">
        <CardPrefsContext value={shown}>
          <CardPicturesContext value={noPictures}>
            <AutopilotRowsContext value={null}>
              <ul className="@container">
                <ItemRow item={sample} launch={null} edits={guideEdits} />
              </ul>
            </AutopilotRowsContext>
          </CardPicturesContext>
        </CardPrefsContext>
      </div>
      <p className="text-xs text-muted">
        Once a pull request you asked to change has been quiet long enough, Nudge posts a comment asking its author
        about it, which you read first. Settings › Reviewing says how long.
      </p>
    </section>
  )
}

// Legend says what each numbered part of the card is, in the markers' order.
function Legend() {
  return (
    <ol
      aria-label="The parts of a card"
      className="grid grid-cols-2 gap-x-4.5 gap-y-3 text-xs text-muted @3xl:grid-cols-4"
    >
      {guideParts.map((guide, i) => (
        <li key={guide.part} className="flex items-start gap-2.25">
          <span
            aria-hidden="true"
            className="grid size-4.5 shrink-0 place-items-center rounded-full bg-accent text-marker leading-none font-bold text-background"
          >
            {i + 1}
          </span>
          <span>
            <span className="block text-body font-semibold text-text">{guide.title}</span>
            {guide.text}
          </span>
        </li>
      ))}
    </ol>
  )
}

// KeyGroup is one group of the key, under its caption.
function KeyGroup({ caption, children }: { caption: string; children: ReactNode }) {
  const id = useId()

  return (
    <div>
      <p id={id} className={`${captionClass} mb-2`}>
        {caption}
      </p>
      <ul aria-labelledby={id} className="flex flex-col gap-1.5">
        {children}
      </ul>
    </div>
  )
}

// keyRowClass lays out one entry of the key: its swatch or symbol, then its words.
const keyRowClass = 'flex items-center gap-2'

// Key says what the card's colors and symbols mean: the edge's colors, the track's
// symbols, a person's avatar and a bot's, the unread dot while Unread dots is on, and where
// the rest is.
function Key() {
  const { unread } = use(CardPrefsContext)

  return (
    <div className="grid grid-cols-2 gap-4 text-xs text-muted @3xl:grid-cols-4">
      <KeyGroup caption="Whose move">
        {edgeKey.map(({ edge, words }) => (
          <li key={edge} className={keyRowClass}>
            <span
              aria-hidden="true"
              data-swatch={edge}
              className={`size-2.75 shrink-0 rounded-[0.1875rem] ${edgeBar[edge]}`}
            />
            {words}
          </li>
        ))}
      </KeyGroup>
      <KeyGroup caption="The track">
        {trackKey.map(({ look, words }) => (
          <li key={look} className={keyRowClass}>
            <span aria-hidden="true" className={trackStepClass(look)}>
              {keyIcons[look]}
            </span>
            {words}
          </li>
        ))}
      </KeyGroup>
      <KeyGroup caption="Who">
        <li className={keyRowClass}>
          <span
            aria-hidden="true"
            className={`grid size-5 shrink-0 place-items-center rounded-full text-micro font-semibold ${faceOf('contributor-a')}`}
          >
            {initialsOf('contributor-a')}
          </span>
          A person
        </li>
        <li className={keyRowClass}>
          <span aria-hidden="true" className="grid size-5 shrink-0 place-items-center rounded-md bg-info/18 text-info">
            <BotIcon className="size-3" />
          </span>
          A bot
        </li>
        {unread && (
          <li className={keyRowClass}>
            <span aria-hidden="true" className="grid size-5 shrink-0 place-items-center">
              <span className="block size-1.75 rounded-full bg-info ring-3 ring-info/20" />
            </span>
            New since you last opened it
          </li>
        )}
      </KeyGroup>
      <KeyGroup caption="Everything else">
        <li>Click a card’s empty space to see everything else.</li>
      </KeyGroup>
    </div>
  )
}

export interface CardGuideProps {
  // variant is how much the guide says: the sample card with its parts numbered, the
  // legend, and the key (full, the dialog's), or the sample card and the key alone
  // (compact, setup's Finish step's).
  variant: 'full' | 'compact'
}

// CardGuide teaches how to read a card: a legend, not a tour. It shows the real card on a
// fixed sample, then, in full, its parts numbered and a legend of what each says, and in
// both variants a key to the card's colors and symbols; in full, after the key, a second
// sample whose card offers a nudge (NudgeSample). It makes no request.
export function CardGuide({ variant }: CardGuideProps) {
  const full = variant === 'full'

  return (
    <div className="@container flex flex-col gap-4.5">
      <Sample markers={full} />
      {full && <Legend />}
      <div className={full ? 'border-t border-border pt-4' : ''}>
        <Key />
      </div>
      {full && (
        <div className="border-t border-border pt-4">
          <NudgeSample />
        </div>
      )}
    </div>
  )
}
