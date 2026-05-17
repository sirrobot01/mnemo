// Package web embeds the built React UI bundle so the mnemo binary can serve
// it without depending on a Node.js toolchain at runtime.
//
// The dist directory is populated by `npm run build` inside the web/ folder.
// A placeholder index.html is committed so `go build` works on a fresh clone
// before the JS bundle has been built.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the dist subtree as an fs.FS, ready to hand to
// http.FileServer or to fs.ReadFile.
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
