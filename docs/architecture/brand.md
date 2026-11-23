# internal/brand

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/brand`](../../internal/brand) · See also: [Build targets](../development/build-targets.md)

`internal/brand/` holds product identity, design tokens, and the logo mark. The palette tests
enforce WCAG AA contrast for every text color in both themes. `internal/brandgen/` generates
the frontend theme tokens (`frontend/src/generated/theme.css`, whose `@theme` block resets
Tailwind's own palette before it declares them), `build/darwin/Info.plist`, and the icon
artwork (the app icon `build/appicon.svg` and the menu bar icon `build/trayicon.svg`) from it
(`magex brand:generate`); `magex brand:icons` renders the artwork into the committed icons
(macOS, needs `rsvg-convert`). A build from source (`magex app:build`, so `app:run` and the targets
that open its bundle) is "Nilbox Dev", `bin/Nilbox Dev.app` with `brand.DevName` and
`brand.DevBundleID`, which `devStamps` (`magefiles/bundle.go`) stamps over the bundle's copy
of the generated `Info.plist` alone, so macOS (Spotlight, notifications, privacy prompts, Open
With) never takes it for the release; it keeps the release's executable name and data
directory (`brand.DataDirName`), so it shares the user's data and must not run beside the
release. A release and a rehearsal keep `brand.Name` and `brand.BundleID`, and `app:build`
removes an earlier build's `bin/Nilbox.app` once nothing runs from it.

`build/` holds that generated artwork and `build/appicon.svg.sha256`, the stamp of which
artwork the icons were rendered from. `build/darwin/` holds the bundle's generated
`Info.plist` and the rendered icon set, `icons.icns`.
