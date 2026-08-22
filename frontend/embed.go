// Package frontend embeds the built web UI and exposes it as an fs.FS.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed dist
var distFs embed.FS

func Dist() (fs.FS, error) {
	return fs.Sub(distFs, "dist")
}
