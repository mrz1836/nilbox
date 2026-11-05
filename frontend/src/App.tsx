import { brand } from './generated/brand'

// App is the window's root component. It shows the product identity until the inbox and
// worktree views are added.
export function App() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-2 bg-[var(--brand-background)] text-[var(--brand-text)]">
      <h1 className="text-2xl font-semibold">{brand.name}</h1>
      <p className="text-[var(--brand-muted)]">{brand.tagline}</p>
    </main>
  )
}
