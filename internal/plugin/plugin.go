// Package plugin runs the user's own text actions: programs or scripts in the plugins folder that read the
// text on stdin and write the result to stdout, so they can be written in any language and work the same on
// Windows and Linux.
//
// A plugin is a folder with plugin.json (see docs/plugins.md):
//
//	{
//	  "name": "Kanji to kana",
//	  "command": ["powershell", "-NoProfile", "-File", "kana.ps1"],
//	  "timeout_ms": 3000,
//	  "preview": false,
//	  "actions": [{"id": "hiragana", "name": "Kanji to hiragana"}]
//	}
//
// The names are English; their translations are the plugin's own locales/<lang>.po, not the manifest.
package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/Spell6inder/selkit/internal/i18n"
)

// validID is an action ID: it becomes part of "plugin:<plugin>/<action>".
var validID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Exit code with which a plugin says that the action does not fit this text (the item is greyed out).
const exitNotApplicable = 3

const defaultTimeout = 2 * time.Second

// Plugin is one loaded plugin; names are already translated.
type Plugin struct {
	ID          string // folder name
	Name        string
	Description string
	Dir         string
	Command     []string
	Timeout     time.Duration
	Preview     bool   // the plugin's own default for running while the menu opens
	Builtin     bool   // a module of the program itself (internal/actions), shown among the plugins
	Version     string // from plugin.json, or the release it was installed from
	Actions     []Action
}

type Action struct {
	ID, Name string
}

type manifest struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Command     []string            `json:"command"`
	Commands    map[string][]string `json:"commands"` // per GOOS ("windows", "linux"), instead of command
	TimeoutMs   int                 `json:"timeout_ms"`
	Preview     *bool               `json:"preview"`
	Actions     []Action            `json:"actions"`
	Version     string              `json:"version"`
}

// Load reads every plugin in dir, sorted by ID. Broken plugins are skipped and reported together in the error.
func Load(dir string) ([]*Plugin, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []*Plugin
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p, err := load(filepath.Join(dir, e.Name()))
		if err != nil {
			errs = append(errs, fmt.Errorf("plugin %s: %w", e.Name(), err))
			continue
		}
		if p != nil {
			list = append(list, p)
		}
	}
	slices.SortFunc(list, func(a, b *Plugin) int { return strings.Compare(a.ID, b.ID) })
	return list, errors.Join(errs...)
}

// load reads one plugin folder; a folder without plugin.json is not a plugin (nil, nil).
func load(dir string) (*Plugin, error) {
	data, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("plugin.json: %w", err)
	}
	if cmd, ok := m.Commands[runtime.GOOS]; ok {
		m.Command = cmd
	}
	switch {
	case len(m.Command) == 0:
		return nil, errors.New("plugin.json: no command for " + runtime.GOOS)
	case len(m.Actions) == 0:
		return nil, errors.New("plugin.json: no actions")
	}
	tr, trErr := i18n.LoadCatalog(filepath.Join(dir, "locales"))
	p := &Plugin{
		ID:          filepath.Base(dir),
		Name:        tr.T(or(m.Name, filepath.Base(dir))),
		Description: tr.T(m.Description),
		Dir:         dir,
		Command:     m.Command,
		Timeout:     defaultTimeout,
		Preview:     m.Preview == nil || *m.Preview,
		Version:     m.Version,
	}
	if o, ok := OriginOf(dir); ok && o.Version != "" {
		p.Version = o.Version
	}
	if m.TimeoutMs > 0 {
		p.Timeout = time.Duration(m.TimeoutMs) * time.Millisecond
	}
	for _, a := range m.Actions {
		if !validID.MatchString(a.ID) {
			return nil, fmt.Errorf("plugin.json: bad action id %q", a.ID)
		}
		p.Actions = append(p.Actions, Action{a.ID, tr.T(or(a.Name, a.ID))})
	}
	return p, trErr
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// NotApplicable is the plugin's answer that the action does not fit this text; the message says why.
type NotApplicable string

func (e NotApplicable) Error() string { return string(e) }

// Run runs the action on the text. preview tells the plugin that the result is only shown next to the item.
func (p *Plugin) Run(ctx context.Context, action, text, lang string, preview bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	name := p.Command[0]
	// A program next to plugin.json is run from there; otherwise it is looked up in PATH.
	if local := filepath.Join(p.Dir, name); !filepath.IsAbs(name) && fileExists(local) {
		name = local
	}
	cmd := exec.CommandContext(ctx, name, p.Command[1:]...)
	cmd.Dir = p.Dir
	cmd.Stdin = strings.NewReader(text)
	cmd.Env = append(os.Environ(), "SELKIT_ACTION="+action, "SELKIT_LANG="+lang, "SELKIT_PREVIEW="+boolEnv(preview))
	hideWindow(cmd)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	// A child the plugin left running must not hold the menu through the output pipes.
	cmd.WaitDelay = 100 * time.Millisecond
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	msg := strings.TrimSpace(errOut.String())
	var exit *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return "", fmt.Errorf("%s: %s", p.Name, i18n.Tf("no answer in %v", p.Timeout))
	case errors.As(err, &exit) && exit.ExitCode() == exitNotApplicable:
		if msg == "" {
			msg = i18n.T("Not applicable to this text")
		}
		return "", NotApplicable(msg)
	case err != nil && msg != "":
		return "", fmt.Errorf("%s: %s", p.Name, msg)
	case err != nil:
		return "", fmt.Errorf("%s: %w", p.Name, err)
	}
	res := out.String()
	// print() in most languages adds a newline the selection did not have.
	if !strings.HasSuffix(text, "\n") {
		res = strings.TrimSuffix(strings.TrimSuffix(res, "\n"), "\r")
	}
	return res, nil
}

func boolEnv(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
