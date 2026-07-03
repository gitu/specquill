// Package webui embeds the Vite production build of the SPA.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the SPA build rooted at its index.html.
func Dist() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
