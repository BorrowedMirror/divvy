// Package web embeds the built React app (web/dist) for the Go shell.
//
// Build the frontend first (`pnpm --dir web build`): go:embed fails when web/dist is missing.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist is the contents of web/dist, rooted so that index.html is at the top.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // "dist" is a literal embedded directory; this cannot fail.
	}
	return sub
}
