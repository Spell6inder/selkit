package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Installing and updating. A plugin comes from a folder, a zip file, a link to a zip file or a GitHub repository;
// for one from GitHub, install.json in its folder keeps where it came from and which release it is, so that an
// update can take the next release. Nothing is downloaded unless asked for.

// installFile is written into the plugin folder by Install: it is not the author's.
const installFile = "install.json"

// Origin is where an installed plugin came from.
type Origin struct {
	Source  string `json:"source"`            // what was given to Install
	Version string `json:"version,omitempty"` // the release tag, for a plugin from GitHub
}

// Limits for what is downloaded and unpacked.
const (
	maxDownload = 50 << 20
	maxUnpacked = 200 << 20
	maxFiles    = 5000
)

// Installer installs into Dir. Client and API are replaced in tests.
type Installer struct {
	Dir    string
	Client *http.Client
	API    string // https://api.github.com
}

func NewInstaller(dir string) Installer {
	return Installer{dir, &http.Client{Timeout: 2 * time.Minute}, "https://api.github.com"}
}

// Staged is a plugin unpacked next to the plugins, not yet in their place: it can be shown to the user first.
type Staged struct {
	Plugin *Plugin
	ID     string
	Origin Origin
	tmp    string // everything Fetch made, removed by Discard
	path   string // the plugin folder inside tmp
}

// Discard removes what Fetch unpacked.
func (s *Staged) Discard() { os.RemoveAll(s.tmp) }

var (
	githubRepo  = regexp.MustCompile(`^(?:https?://)?github\.com/([\w.-]+)/([\w.-]+?)(?:\.git)?/?$`)
	versionTail = regexp.MustCompile(`[-_]v?\d+(\.\d+)*$`)
)

// Fetch gets the plugin and unpacks it into a temporary folder in Dir. id names the folder; "" takes it from
// the source.
func (in Installer) Fetch(ctx context.Context, source, id string) (*Staged, error) {
	if err := os.MkdirAll(in.Dir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(in.Dir, ".install-")
	if err != nil {
		return nil, err
	}
	s := &Staged{tmp: tmp, Origin: Origin{Source: source}}
	root, name, err := in.unpack(ctx, source, filepath.Join(tmp, "p"), &s.Origin)
	if err == nil {
		s.ID = or(id, name)
		err = s.load(root)
	}
	if err != nil {
		s.Discard()
		return nil, err
	}
	return s, nil
}

func (s *Staged) load(root string) error {
	if !validID.MatchString(s.ID) {
		return fmt.Errorf("%q cannot be the name of a plugin folder: letters, digits, - and _ only", s.ID)
	}
	p, err := load(root)
	if p == nil && err == nil {
		err = errors.New("no plugin.json")
	}
	if err != nil {
		return err
	}
	p.ID = s.ID
	if s.Origin.Version != "" || strings.Contains(s.Origin.Source, "://") || githubRepo.MatchString(s.Origin.Source) {
		data, _ := json.MarshalIndent(s.Origin, "", "  ")
		if err := os.WriteFile(filepath.Join(root, installFile), data, 0o644); err != nil {
			return err
		}
	}
	s.Plugin = p
	s.path = root
	return nil
}

// unpack puts the plugin into dest and returns the folder with plugin.json and a name for it.
func (in Installer) unpack(ctx context.Context, source, dest string, o *Origin) (string, string, error) {
	if m := githubRepo.FindStringSubmatch(source); m != nil {
		url, tag, err := in.latest(ctx, m[1], m[2])
		if err != nil {
			return "", "", err
		}
		o.Source, o.Version = "github.com/"+m[1]+"/"+m[2], tag
		root, _, err := in.unzipURL(ctx, url, dest)
		name := strings.TrimPrefix(strings.TrimPrefix(m[2], "selkit-plugin-"), "selkit-")
		return root, name, err
	}
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		root, top, err := in.unzipURL(ctx, source, dest)
		return root, or(top, zipName(path.Base(source))), err
	}
	st, err := os.Stat(source)
	if err != nil {
		return "", "", err
	}
	if st.IsDir() {
		abs, _ := filepath.Abs(source)
		return dest, filepath.Base(abs), copyDir(source, dest)
	}
	f, err := os.Open(source)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	root, top, err := unzip(f, st.Size(), dest)
	return root, or(top, zipName(filepath.Base(source))), err
}

// zipName is the plugin name in a file name such as quotes-1.2.0.zip.
func zipName(file string) string {
	return versionTail.ReplaceAllString(strings.TrimSuffix(file, ".zip"), "")
}

// latest is the zip of the newest release of a GitHub repository: an attached .zip, or else the source code.
func (in Installer) latest(ctx context.Context, owner, repo string) (url, tag string, err error) {
	var rel struct {
		Tag    string `json:"tag_name"`
		Zip    string `json:"zipball_url"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	body, err := in.get(ctx, in.API+"/repos/"+owner+"/"+repo+"/releases/latest", 1<<20)
	if err != nil {
		return "", "", fmt.Errorf("github.com/%s/%s: %w", owner, repo, err)
	}
	if err := json.Unmarshal(body, &rel); err != nil || rel.Tag == "" {
		return "", "", fmt.Errorf("github.com/%s/%s: no release", owner, repo)
	}
	for _, a := range rel.Assets {
		if strings.HasSuffix(a.Name, ".zip") {
			return a.URL, rel.Tag, nil
		}
	}
	return rel.Zip, rel.Tag, nil
}

func (in Installer) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "selkit")
	resp, err := in.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = fmt.Errorf("%s: larger than %d MB", url, limit>>20)
	}
	return data, err
}

func (in Installer) unzipURL(ctx context.Context, url, dest string) (string, string, error) {
	data, err := in.get(ctx, url, maxDownload)
	if err != nil {
		return "", "", err
	}
	return unzip(bytes.NewReader(data), int64(len(data)), dest)
}

// unzip unpacks into dest and returns the folder with plugin.json: dest itself, or the one folder the archive
// holds, as in a GitHub source archive. top is the name of that folder.
func unzip(r io.ReaderAt, size int64, dest string) (root, top string, err error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return "", "", err
	}
	if len(zr.File) > maxFiles {
		return "", "", fmt.Errorf("more than %d files in the archive", maxFiles)
	}
	var total int64
	for _, f := range zr.File {
		name := path.Clean(strings.ReplaceAll(f.Name, `\`, "/"))
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, ":") {
			return "", "", fmt.Errorf("unsafe path in the archive: %s", f.Name)
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		mode := f.Mode()
		switch {
		case mode.IsDir():
			err = os.MkdirAll(target, 0o755)
		case mode&fs.ModeSymlink != 0:
			continue // a link could point anywhere
		default:
			total += int64(f.UncompressedSize64)
			if total > maxUnpacked {
				return "", "", fmt.Errorf("the archive unpacks to more than %d MB", maxUnpacked>>20)
			}
			err = writeZipFile(f, target, mode)
		}
		if err != nil {
			return "", "", err
		}
	}
	if fileExists(filepath.Join(dest, "plugin.json")) {
		return dest, "", nil
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) == 1 && entries[0].IsDir() && fileExists(filepath.Join(dest, entries[0].Name(), "plugin.json")) {
		return filepath.Join(dest, entries[0].Name()), entries[0].Name(), nil
	}
	return "", "", errors.New("no plugin.json in the archive")
}

func writeZipFile(f *zip.File, target string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644|mode&0o111)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, io.LimitReader(src, maxUnpacked))
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	return err
}

func copyDir(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dest, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, _ := d.Info()
		return os.WriteFile(target, data, 0o644|info.Mode()&0o111)
	})
}

// Install puts a fetched plugin into its place; one of that name has to be removed first, or updated.
func (in Installer) Install(s *Staged) error {
	defer s.Discard()
	final := filepath.Join(in.Dir, s.ID)
	if _, err := os.Stat(final); err == nil {
		return fmt.Errorf("%s is already installed", s.ID)
	}
	return os.Rename(s.path, final)
}

// Replace puts a fetched plugin in place of the installed one of the same name.
func (in Installer) Replace(s *Staged) error {
	defer s.Discard()
	final := filepath.Join(in.Dir, s.ID)
	old := filepath.Join(s.tmp, "old")
	if err := os.Rename(final, old); err != nil {
		return err
	}
	if err := os.Rename(s.path, final); err != nil {
		os.Rename(old, final)
		return err
	}
	return nil
}

// Remove deletes an installed plugin.
func (in Installer) Remove(id string) error {
	if !validID.MatchString(id) || !fileExists(filepath.Join(in.Dir, id, "plugin.json")) {
		return fmt.Errorf("%s is not installed", id)
	}
	return os.RemoveAll(filepath.Join(in.Dir, id))
}

// OriginOf is where an installed plugin came from; ok=false for one copied in by hand.
func OriginOf(dir string) (Origin, bool) {
	var o Origin
	data, err := os.ReadFile(filepath.Join(dir, installFile))
	if err != nil || json.Unmarshal(data, &o) != nil || o.Source == "" {
		return Origin{}, false
	}
	return o, true
}

// Update is a newer release of an installed plugin.
type Update struct {
	ID, From, To string
}

// Updates asks GitHub for the newest release of every plugin installed from there.
func (in Installer) Updates(ctx context.Context, list []*Plugin) ([]Update, []error) {
	var ups []Update
	var errs []error
	for _, p := range list {
		o, ok := OriginOf(p.Dir)
		m := githubRepo.FindStringSubmatch(o.Source)
		if !ok || m == nil {
			continue
		}
		_, tag, err := in.latest(ctx, m[1], m[2])
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if tag != o.Version {
			ups = append(ups, Update{p.ID, o.Version, tag})
		}
	}
	return ups, errs
}

// FetchUpdate gets the newest release of an installed plugin, ready for Replace.
func (in Installer) FetchUpdate(ctx context.Context, id string) (*Staged, error) {
	o, ok := OriginOf(filepath.Join(in.Dir, id))
	if !ok || !githubRepo.MatchString(o.Source) {
		return nil, fmt.Errorf("%s was not installed from GitHub, so there is nothing to update it from", id)
	}
	return in.Fetch(ctx, o.Source, id)
}
