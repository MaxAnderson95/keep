// Package web embeds the built single-page app that `keep serve` serves.
// dist/ is a generated artifact (gitignored, built by Vite+ from the sources
// in this directory); only the .gitkeep placeholder is committed so a plain
// `go build` works before the frontend has been built.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// DistFS returns the built SPA rooted at dist/. When the frontend has not
// been built, the filesystem simply lacks index.html and serve degrades to
// API-only with an explanatory message.
func DistFS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the embed layout is fixed at compile time
	}
	return sub
}
