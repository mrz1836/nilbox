//go:build desktop

// Package frontend embeds the built desktop frontend into the desktop app.
//
// The embed lives in a desktop-tagged file, so builds without the desktop tag never need
// a built frontend: `vite build` writes frontend/dist, and only the desktop build embeds
// it.
package frontend

import "embed"

// Assets holds the built frontend under dist/: index.html and its bundled scripts and
// styles.
//
//go:embed all:dist
var Assets embed.FS
