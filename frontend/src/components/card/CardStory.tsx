import type { ReactNode } from 'react'

import type { Edge, Story, StoryIcon } from '../../lib/nextStep'
import { ArchiveIcon, AutopilotIcon, ClockIcon } from '../icons'
import { StatusDot } from '../StatusDot'

// edgeText is each edge's story color, and edgeDot its dot's, spelled out in full so
// Tailwind finds them.
const edgeText: Readonly<Record<Edge, string>> = {
  urgent: 'text-warning',
  blocked: 'text-danger',
  yours: 'text-accent',
  ready: 'text-success',
  agent: 'text-info',
  waiting: 'text-muted',
  none: 'text-muted',
}

const edgeDot: Readonly<Record<Edge, string>> = {
  urgent: 'bg-warning',
  blocked: 'bg-danger',
  yours: 'bg-accent',
  ready: 'bg-success',
  agent: 'bg-info',
  waiting: 'bg-muted',
  none: 'bg-muted',
}

// edgeTint is each edge's tint behind a compact card's story chip, spelled out in full so
// Tailwind finds them; a card that waits on others has none.
const edgeTint: Readonly<Record<Edge, string>> = {
  urgent: 'bg-warning/14',
  blocked: 'bg-danger/10',
  yours: 'bg-accent/10',
  ready: 'bg-success/10',
  agent: 'bg-info/10',
  waiting: '',
  none: '',
}

// Mark is the story's icon: a dot in the edge's color, one with a ring swelling out of it
// for something at work or waiting now (StatusDot), or a clock, a bolt, or an archive box.
function Mark({ icon, edge }: { icon: StoryIcon; edge: Edge }) {
  switch (icon) {
    case 'dot':
      return <span aria-hidden="true" data-icon="dot" className={`size-1.5 shrink-0 rounded-full ${edgeDot[edge]}`} />
    case 'ping':
      return (
        <span data-icon="ping" className="inline-flex">
          <StatusDot color={edgeDot[edge]} ping />
        </span>
      )
    case 'clock':
      return (
        <span data-icon="clock" className="inline-flex">
          <ClockIcon className="size-3 shrink-0" />
        </span>
      )
    case 'bolt':
      return (
        <span data-icon="bolt" className="inline-flex">
          <AutopilotIcon className="size-3 shrink-0" />
        </span>
      )
    case 'archive':
      return (
        <span data-icon="archive" className="inline-flex">
          <ArchiveIcon className="size-3 shrink-0" />
        </span>
      )
  }
}

export interface CardStoryProps {
  story: Story
  edge: Edge
  // children is what a triage story shows first, in the Snoozed and Dismissed tabs: when
  // the item comes back, or when it was dismissed (the row's TriageTag). The story's state
  // follows it.
  children?: ReactNode
  // chip draws the story as a compact card's chip: tinted with the edge's color, its words
  // cut short when they don't fit.
  chip?: boolean
}

// State is where a set-aside item stands, after its triage tag: " · " and its own story's
// lead, muted. A compact card's (chip) is cut short when it doesn't fit.
function State({ state, chip }: { state: string; chip: boolean }) {
  return (
    <span data-story-state className={`font-medium text-muted ${chip ? 'min-w-0 truncate text-meta' : 'shrink-0'}`}>
      <span aria-hidden="true" className={`opacity-60 ${chip ? 'mr-1.25' : 'mr-2'}`}>
        ·
      </span>
      {state}
    </span>
  )
}

// CardStory is a card's one sentence, at the start of its second line: its mark, its lead
// in the edge's color, and its detail after " · ", muted. It never shrinks; the byline
// after it gives way first. A compact card's (chip) is a chip in its own column, tinted
// with the edge's color, whose words are cut short instead. A triage story shows children
// instead, then where the item stands (State).
export function CardStory({ story, edge, children, chip = false }: CardStoryProps) {
  if (story.triage) {
    if (story.state === '') {
      return children
    }

    return chip ? (
      <span data-story-chip className="inline-flex h-5 max-w-60 min-w-0 items-center gap-1.25 whitespace-nowrap">
        {children}
        <State state={story.state} chip />
      </span>
    ) : (
      <>
        {children}
        <State state={story.state} chip={false} />
      </>
    )
  }

  if (chip) {
    return (
      <span
        data-story-chip
        className={`inline-flex h-5 max-w-60 min-w-0 items-center gap-1.25 rounded-md px-2 text-meta font-semibold whitespace-nowrap ${edgeText[edge]} ${edgeTint[edge]}`}
      >
        <Mark icon={story.icon} edge={edge} />
        <span className="truncate">
          {story.text}
          {story.detail !== '' && <span className="font-medium text-muted">{` · ${story.detail}`}</span>}
        </span>
      </span>
    )
  }

  return (
    <span
      data-guide="story"
      className={`inline-flex shrink-0 items-center gap-1.5 font-semibold whitespace-nowrap ${edgeText[edge]}`}
    >
      <Mark icon={story.icon} edge={edge} />
      {story.text}
      {story.detail !== '' && <span className="font-medium text-muted">{` · ${story.detail}`}</span>}
    </span>
  )
}
