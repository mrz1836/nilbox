export interface WarningsToAcceptProps {
  warnings: readonly string[]
  // heading, when there is one, introduces the list, as in "GitHub would hold this merge
  // back:".
  heading?: string
  // accept labels the box the user ticks to accept them.
  accept: string
  accepted: boolean
  onAccept: (accepted: boolean) => void
}

// WarningsToAccept lists what the user must accept before an action runs, with the box they
// tick to accept it: the backend refuses the action unless they did. It shows nothing when
// there is nothing to accept.
export function WarningsToAccept({ warnings, heading = '', accept, accepted, onAccept }: WarningsToAcceptProps) {
  if (warnings.length === 0) {
    return null
  }

  return (
    <div className="mt-3 rounded-md border border-warning/60 p-2 text-warning">
      {heading !== '' && <p className="mb-1">{heading}</p>}
      <ul className="list-disc space-y-0.5 pl-4">
        {warnings.map((w) => (
          <li key={w}>{w}</li>
        ))}
      </ul>
      <label className="mt-2 flex items-center gap-2 text-text">
        <input
          type="checkbox"
          className="checkbox"
          checked={accepted}
          onChange={(e) => {
            onAccept(e.target.checked)
          }}
        />
        {accept}
      </label>
    </div>
  )
}
