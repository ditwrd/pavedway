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
