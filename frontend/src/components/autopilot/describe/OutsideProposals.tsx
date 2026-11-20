import { useEffect, useEffectEvent, useId } from 'react'

import { attempt } from '../../../lib/call'
import { formatAgo, formatFull } from '../../../lib/time'
import { useQuery } from '../../../lib/useQuery'
import { describeItProposals, discardDescribeItProposal, on, type DescribeCard } from '../../../platform'
import { captionClass } from '../../controls'
import { TerminalIcon } from '../../icons'
import { RelativeTime } from '../../RelativeTime'
import type { CardKit } from './parts'
import { ProposalCard } from './ProposalCard'

// Source says where and when a card from the user's own Claude Code came from: "A session
// in example-repo, 5m ago".
function Source({ card }: { card: DescribeCard }) {
  return (
    <p className="flex items-center gap-1.5 text-xs text-muted" title={formatFull(card.at)}>
      <TerminalIcon className="size-3.5 shrink-0" />
      <span>{card.where === '' ? 'A session' : `A session in ${card.where}`},</span>
      <RelativeTime iso={card.at} show={formatAgo} />
    </p>
  )
}

// OutsideProposals are the cards from the user's own Claude Code, the newest first, read
// again each time the shell says they changed, each with where and when it came from, and
// Discard for all of it; nothing shows while there are none.
export function OutsideProposals({ kit }: { kit: Omit<CardKit, 'onSettled'> }) {
  const id = useId()
  const cards = useQuery(() => describeItProposals(), [])
  const readAgain = useEffectEvent(() => {
    void cards.refetch()
  })

  useEffect(
    () =>
      on('describeit:proposals', () => {
        readAgain()
      }),
    [],
  )

  if (cards.data === undefined || cards.data.length === 0) {
    return cards.error === '' ? null : (
      <p className="text-xs text-muted">{`The proposals from your Claude Code couldn’t be read: ${cards.error}`}</p>
    )
  }

  const own: CardKit = {
    ...kit,
    onSettled: () => {
      void cards.refetch()
    },
  }

  return (
    <section aria-labelledby={`${id}-title`} className="flex flex-col gap-3">
      <h3 id={`${id}-title`} className={captionClass}>
        From your Claude Code
      </h3>
      {cards.data.map((card) => (
        <ProposalCard
          key={card.ref.proposal}
          card={card}
          kit={own}
          source={<Source card={card} />}
          onDiscard={async () => {
            const done = await attempt(discardDescribeItProposal(card.ref))

            void cards.refetch()

            return done.ok ? '' : done.error
          }}
        />
      ))}
    </section>
  )
}
