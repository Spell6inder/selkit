package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testManifest = `{"name":"Up","command":["true"],"actions":[{"id":"a","name":"A"}]}`

// zipOf makes an archive from name → content; a name ending in / is a folder.
func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(content))
	}
	w.Close()
	return buf.Bytes()
}

// github is a fake GitHub with one repository whose latest release can change.
type github struct {
	tag    string
	asset  bool // the release has a .zip attached; otherwise only the source archive
	server *httptest.Server
}

func newGithub(t *testing.T) *github {
	g := &github{tag: "v1.0"}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/someone/selkit-plugin-up/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "selkit" {
			http.Error(w, "no agent", http.StatusForbidden)
			return
		}
		rel := map[string]any{"tag_name": g.tag, "zipball_url": g.server.URL + "/source.zip"}
		if g.asset {
			rel["assets"] = []map[string]string{
				{"name": "notes.txt", "browser_download_url": g.server.URL + "/notes.txt"},
				{"name": "up.zip", "browser_download_url": g.server.URL + "/up.zip"},
			}
		}
		json.NewEncoder(w).Encode(rel)
	})
	mux.HandleFunc("/source.zip", func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipOf(t, map[string]string{"someone-up-1a2b3c/plugin.json": testManifest, "someone-up-1a2b3c/v.txt": g.tag}))
	})
	mux.HandleFunc("/up.zip", func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipOf(t, map[string]string{"plugin.json": testManifest, "v.txt": "asset " + g.tag}))
	})
	g.server = httptest.NewServer(mux)
	t.Cleanup(g.server.Close)
	return g
}

func TestInstallGithub(t *testing.T) {
	g := newGithub(t)
	in := Installer{t.TempDir(), g.server.Client(), g.server.URL}
	ctx := context.Background()

	s, err := in.Fetch(ctx, "https://github.com/someone/selkit-plugin-up", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "up" || s.Plugin.Name != "Up" || s.Origin.Version != "v1.0" || s.Origin.Source != "github.com/someone/selkit-plugin-up" {
		t.Errorf("staged: %s %+v %+v", s.ID, s.Plugin, s.Origin)
	}
	if err := in.Install(s); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(in.Dir, "up")
	if v, _ := os.ReadFile(filepath.Join(dir, "v.txt")); string(v) != "v1.0" {
		t.Errorf("source archive: %q", v)
	}
	if o, ok := OriginOf(dir); !ok || o.Version != "v1.0" {
		t.Errorf("origin: %+v", o)
	}
	list, err := Load(in.Dir)
	if err != nil || len(list) != 1 || list[0].ID != "up" {
		t.Fatalf("after install: %v, %v", list, err)
	}

	// The same again is refused; nothing is left behind.
	s, _ = in.Fetch(ctx, "github.com/someone/selkit-plugin-up", "")
	if err := in.Install(s); err == nil {
		t.Error("installed twice")
	}

	ups, errs := in.Updates(ctx, list)
	if len(ups) != 0 || len(errs) != 0 {
		t.Errorf("no new release, yet %v %v", ups, errs)
	}
	g.tag, g.asset = "v1.1", true
	ups, _ = in.Updates(ctx, list)
	if len(ups) != 1 || ups[0] != (Update{"up", "v1.0", "v1.1"}) {
		t.Fatalf("updates: %v", ups)
	}
	s, err = in.FetchUpdate(ctx, "up")
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Replace(s); err != nil {
		t.Fatal(err)
	}
	if v, _ := os.ReadFile(filepath.Join(dir, "v.txt")); string(v) != "asset v1.1" {
		t.Errorf("after the update, from the attached zip: %q", v)
	}
	if o, _ := OriginOf(dir); o.Version != "v1.1" {
		t.Errorf("origin after the update: %+v", o)
	}
	entries, _ := os.ReadDir(in.Dir)
	if len(entries) != 1 {
		t.Errorf("left in the plugins folder: %v", entries)
	}

	if _, statErr := os.Stat(dir); in.Remove("up") != nil || statErr != nil || !removed(dir) {
		t.Errorf("remove: %v", err)
	}
	if err := in.Remove("up"); err == nil {
		t.Error("removed what is not there")
	}
	if err := in.Remove("../x"); err == nil {
		t.Error("removed a path")
	}
}

func TestInstallLocal(t *testing.T) {
	in := NewInstaller(t.TempDir())
	ctx := context.Background()

	// A folder keeps its name, and gets no install.json: there is nothing to update it from.
	src := filepath.Join(t.TempDir(), "mine")
	os.MkdirAll(filepath.Join(src, "locales"), 0o755)
	os.WriteFile(filepath.Join(src, "plugin.json"), []byte(testManifest), 0o644)
	os.WriteFile(filepath.Join(src, "locales", "uk.po"), []byte(""), 0o644)
	s, err := in.Fetch(ctx, src, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Install(s); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(in.Dir, "mine", "locales", "uk.po")) {
		t.Error("folder not copied")
	}
	if _, ok := OriginOf(filepath.Join(in.Dir, "mine")); ok {
		t.Error("an origin for a folder")
	}
	if _, err := in.FetchUpdate(ctx, "mine"); err == nil {
		t.Error("an update for a plugin from a folder")
	}

	// A zip names the plugin after the file, without the version.
	z := filepath.Join(t.TempDir(), "brackets-1.2.0.zip")
	os.WriteFile(z, zipOf(t, map[string]string{"plugin.json": testManifest}), 0o644)
	s, err = in.Fetch(ctx, z, "")
	if err != nil || s.ID != "brackets" {
		t.Fatalf("zip: %v %+v", err, s)
	}
	s.Discard()

	// The name can be chosen, and has to be a plain folder name.
	s, err = in.Fetch(ctx, z, "other")
	if err != nil || s.ID != "other" {
		t.Errorf("chosen name: %v %+v", err, s)
	} else {
		s.Discard()
	}
	if _, err := in.Fetch(ctx, z, "../evil"); err == nil {
		t.Error("a name with a path")
	}

	for name, files := range map[string]map[string]string{
		"escape":   {"../evil.txt": "x", "plugin.json": testManifest},
		"absolute": {"/etc/evil": "x", "plugin.json": testManifest},
		"empty":    {"readme.txt": "no plugin"},
		"broken":   {"plugin.json": "{"},
	} {
		z := filepath.Join(t.TempDir(), name+".zip")
		os.WriteFile(z, zipOf(t, files), 0o644)
		if s, err := in.Fetch(ctx, z, ""); err == nil {
			s.Discard()
			t.Errorf("%s: installed", name)
		}
	}
	entries, _ := os.ReadDir(in.Dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".install-") {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}

func removed(dir string) bool {
	_, err := os.Stat(dir)
	return os.IsNotExist(err)
}
