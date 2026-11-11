import { brand } from '../../generated/brand'
import type { IntegrationTool } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'

// InstallKind is what a card asks to do with a tool's latest release: install it where
// there is no copy, update the copy to it, or install it again over the copy.
export type InstallKind = 'install' | 'update' | 'reinstall'

// confirmLabels name each kind's confirm button.
const confirmLabels: Readonly<Record<InstallKind, string>> = {
  install: 'Install',
  update: 'Update',
  reinstall: 'Reinstall',
}

// installTitle asks whether to go ahead, naming the version when the release is known.
function installTitle(kind: InstallKind, tool: IntegrationTool): string {
  const { version } = tool.install

  if (kind === 'update') {
    return version === '' ? `Update ${tool.name}?` : `Update ${tool.name} to ${version}?`
  }

  return `${`${confirmLabels[kind]} ${tool.name} ${version}`.trimEnd()}?`
}

// downloadText says what is downloaded, checked, and put where: the release's archive
// for this Mac, or, until the release is known, the latest release.
function downloadText(tool: IntegrationTool): string {
  const { archive, targetDir } = tool.install
  const what =
    archive === ''
      ? `the latest ${tool.name} release from GitHub`
      : `${archive} from the ${tool.repo} release on GitHub`

  return `${brand.name} downloads ${what}, checks it against the release’s SHA-256 checksums, and puts ${tool.binary} in ${targetDir}. Nothing else on your Mac changes.`
}

// SelfUpdateText says what a tool's own updater does: the command the app runs, which
// downloads the latest release, checks it, and swaps it in for the copy.
function SelfUpdateText({ tool }: { tool: IntegrationTool }) {
  const { version, updater } = tool.install
  const release = version === '' ? 'its latest release' : version

  return (
    <p>
      {tool.binary} updates itself: {brand.name} runs <span className="font-mono break-all text-text">{updater}</span>,
      which downloads {release} from the {tool.repo} release on GitHub, checks it against the release’s SHA-256
      checksums, and swaps it in for the copy. Its output shows here as it runs.
    </p>
  )
}

export interface InstallDialogProps {
  tool: IntegrationTool
  kind: InstallKind
  onConfirm: () => void
  onCancel: () => void
}

// InstallDialog asks before an install, an update, or a reinstall: what is downloaded
// and checked, where it goes, the copy it replaces, and, for a copy the app didn't
// install, whether to replace it at all. An update a copy runs itself says what its own
// updater does instead, and never asks that: the tool replaces its own copy. It only
// asks; the card runs the install and shows its progress.
export function InstallDialog({ tool, kind, onConfirm, onCancel }: InstallDialogProps) {
  const { binary } = tool
  const { version, replaces, replacesPath, foreign, updater } = tool.install
  const itself = kind === 'update' && updater !== ''
  const alarm = foreign && !itself

  return (
    <ConfirmDialog
      title={installTitle(kind, tool)}
      confirmLabel={alarm ? 'Replace' : confirmLabels[kind]}
      tone={alarm ? 'danger' : 'primary'}
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      {itself ? <SelfUpdateText tool={tool} /> : <p>{downloadText(tool)}</p>}
      {replacesPath !== '' && (
        <p className="mt-2">
          Replaces <span className="font-mono text-text">{`${binary} ${replaces}`.trimEnd()}</span> at{' '}
          <span className="font-mono break-all text-text">{replacesPath}</span>.
        </p>
      )}
      {alarm && (
        <p className="mt-2 text-text">
          This copy wasn’t installed by {brand.name}. Replace it with {version === '' ? 'the latest release' : version}?
        </p>
      )}
    </ConfirmDialog>
  )
}
