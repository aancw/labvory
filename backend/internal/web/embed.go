//go:build embed

package web

import (
	"embed"
	"io/fs"
)

// dist holds the built web UI, copied from frontend/dist before the build
// (see `make release` and the release workflow). The all: prefix keeps
// underscore-prefixed files, which go:embed otherwise skips.
//
//go:embed all:dist
var dist embed.FS

func init() {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("web: embedded dist unusable: " + err.Error())
	}
	embeddedFS = sub
}
