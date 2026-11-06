// Command generate builds the icon files from the SVG sources in resources/icon: the ICO files that the
// program embeds and the Windows resources of selkit.exe (icon, file version info and manifest) in an object file
// that "go build" links automatically. Run via "go generate ./cmd/selkit".
package main

import (
	"bytes"
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"

	"github.com/Spell6inder/selkit/internal/about"
	"github.com/Spell6inder/selkit/internal/icon/build"
)

func main() {
	dir := flag.String("icons", "../../resources/icon", "folder with the SVG sources; ICO files are written there")
	out := flag.String("o", "rsrc_windows_amd64.syso", "output object file")
	ver := flag.String("version", "0.1.0", "file and product version")
	flag.Parse()

	files, err := build.Generate(*dir)
	if err != nil {
		log.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(*dir, name), data, 0o644); err != nil {
			log.Fatal(err)
		}
	}

	ico, err := winres.LoadICO(bytes.NewReader(files[build.ActiveICO]))
	if err != nil {
		log.Fatal(err)
	}
	var rs winres.ResourceSet
	if err := rs.SetIcon(winres.Name("APPICON"), ico); err != nil {
		log.Fatal(err)
	}

	var vi version.Info
	vi.SetFileVersion(*ver)
	vi.SetProductVersion(*ver)
	for k, v := range map[string]string{
		version.ProductName:      "Selkit",
		version.FileDescription:  "Selkit — tools for selected text on the middle mouse button",
		version.CompanyName:      about.Credit,
		version.Comments:         about.Site,
		version.LegalCopyright:   "© 2026 " + about.Credit,
		version.OriginalFilename: "selkit.exe",
	} {
		vi.Set(version.LangNeutral, k, v)
	}
	rs.SetVersionInfo(vi)
	rs.SetManifest(winres.AppManifest{
		Description:         about.Name,
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
		LongPathAware:       true,
	})

	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := rs.WriteObject(f, winres.ArchAMD64); err != nil {
		log.Fatal(err)
	}
}
