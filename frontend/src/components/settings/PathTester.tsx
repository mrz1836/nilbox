import { useId, useState } from 'react'

import { useDebouncedQuery } from '../../lib/useQuery'
import { matchSensitivePath, type SensitiveLevel, type SensitivePathMatch, type Settings } from '../../platform'
import { fieldClass } from '../controls'
import { SectionIntro } from './rows'

// tryDelay is how long the path tester waits after a keystroke before asking.
const tryDelay = 200

// levelText colors a match by its category's level.
const levelText: Readonly<Record<SensitiveLevel, string>> = {
  danger: 'text-danger',
  warning: 'text-warning',
  '': 'text-text',
}

// PathTester says whether a path counts as sensitive under the stored settings, as it is
// typed, and which category and pattern it matches.
export function PathTester({ settings }: { settings: Settings }) {
  const id = useId()
  const [path, setPath] = useState('')
  const trimmed = path.trim()
  // Ask again after typing stops, and whenever the stored settings change.
  const tried = useDebouncedQuery(
    () => matchSensitivePath(trimmed).then((match) => ({ path: trimmed, match })),
    [trimmed, settings],
    tryDelay,
    { enabled: trimmed !== '' },
  )
  // A failure is shown once the latest ask settles; an answer while it is about the path
  // in the field.
  const shown =
    tried.error !== '' && !tried.pending
      ? { match: null, error: tried.error }
      : tried.data?.path === trimmed
        ? { match: tried.data.match, error: '' }
        : null

  return (
    <section aria-labelledby={`${id}-heading`} className="flex flex-col gap-2">
      <SectionIntro id={`${id}-heading`} title="Try a path">
        See whether a file in a pull request would be pointed out, and why.
      </SectionIntro>
      <label htmlFor={`${id}-path`} className="sr-only">
        Path to try
      </label>
      <input
        id={`${id}-path`}
        value={path}
        placeholder="A path in a repository, such as .github/workflows/release.yml"
        spellCheck={false}
        autoComplete="off"
        aria-describedby={`${id}-result`}
        onChange={(e) => {
          setPath(e.target.value)
        }}
        className={`${fieldClass} w-full font-mono text-[0.8125rem]`}
      />
      <p id={`${id}-result`} role="status" className="min-h-4 text-xs">
        {trimmed !== '' && shown !== null && <TryResult result={shown} />}
      </p>
    </section>
  )
}

function TryResult({ result }: { result: { match: SensitivePathMatch | null; error: string } }) {
  if (result.error !== '') {
    return <span className="text-danger">{result.error}</span>
  }

  const match = result.match

  if (match === null || !match.sensitive) {
    return <span className="text-muted">Not sensitive with these settings.</span>
  }

  return (
    <span className={levelText[match.level]}>
      Sensitive: {match.label}, matched by <code className="font-mono">{match.pattern}</code>.
    </span>
  )
}
