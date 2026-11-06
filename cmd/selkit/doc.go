// Selkit: tools for selected text. On Windows it works on the middle mouse button: Linux-style
// "primary selection" paste that leaves the clipboard alone, keyboard layout fixes, case conversion,
// transliteration and more (main_windows.go). Elsewhere it is a command to bind to a key, which shows
// the same text menu for the selection (main_other.go, internal/cli).
//
// Build without a console window:
//
//	go build -ldflags "-H=windowsgui" -o var/bin/selkit.exe ./cmd/selkit
//
// The icon comes from resources/icon/*.svg: "go generate ./cmd/selkit" rebuilds the ICO files and
// rsrc_windows_amd64.syso (icon, version info, manifest). Releases are built by .github/workflows/release.yml.
package main

import (
	"regexp"
	"runtime/debug"
)

// version is set at build time: -ldflags "-X main.version=1.2.3". Without it, a build by
// "go install ...@v1.2.3" takes the module version instead.
var version = "0.1.0-dev"

// releaseVersion matches a tagged module version such as v1.2.3 or v1.2.3-rc.1, but not a
// pseudo-version (v0.0.0-20261109...) or a local build (+dirty).
var releaseVersion = regexp.MustCompile(`^v(\d+\.\d+\.\d+(-[A-Za-z][0-9A-Za-z.]*)?)$`)

func init() {
	if version != "0.1.0-dev" {
		return
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if m := releaseVersion.FindStringSubmatch(bi.Main.Version); m != nil {
			version = m[1]
		}
	}
}
