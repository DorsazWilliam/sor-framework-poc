package main

import (
	"embed"
	"io/fs"
)

// dist is the built SPA; the image build copies ui/dist here over the placeholder.
//
//go:embed all:dist
var dist embed.FS

func uiFS() fs.FS {
	sub, _ := fs.Sub(dist, "dist")
	return sub
}
