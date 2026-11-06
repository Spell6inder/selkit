package actions

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Spell6inder/selkit/internal/config"
)

// The test binary doubles as a plugin: run with SELKIT_ACTION set, it does what the action says.
func TestMain(m *testing.M) {
	switch os.Getenv("SELKIT_ACTION") {
	case "":
		os.Exit(m.Run())
	case "upper":
		in, _ := io.ReadAll(os.Stdin)
		fmt.Print(strings.ToUpper(string(in)))
	case "slow":
		time.Sleep(3 * time.Second)
		fmt.Print("late")
	}
	os.Exit(0)
}

func plugins(t *testing.T, cfg *config.Config) Set {
	t.Helper()
	root := t.TempDir()
	path, _ := os.Executable()
	exe, _ := json.Marshal(path)
	for id, manifest := range map[string]string{
		"one":  `{"name": "One", "command": [%s], "actions": [{"id": "upper", "name": "Shout"}]}`,
		"two":  `{"name": "Two", "command": [%s], "actions": [{"id": "upper", "name": "Shout"}, {"id": "slow", "name": "Slow"}]}`,
		"lazy": `{"name": "Lazy", "command": [%s], "preview": false, "actions": [{"id": "upper", "name": "Shout"}]}`,
	} {
		os.MkdirAll(filepath.Join(root, id), 0o755)
		os.WriteFile(filepath.Join(root, id, "plugin.json"), []byte(fmt.Sprintf(manifest, exe)), 0o644)
	}
	set, err := LoadFrom(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestEval(t *testing.T) {
	s := Set{Lang: "en"}
	for _, c := range []struct{ id, in, want string }{
		{"translit", "Щука", "Shchuka"},
		{"translit", "Shchuka", "Щука"},
		{"translit:sr", "Ђорђе", "Đorđe"},
		{"snake", "fooBar", "foo_bar"},
		{"base64-encode", "hi", "aGk="},
	} {
		if r := s.Eval(c.id, c.in); r.Why != "" || r.Text != c.want {
			t.Errorf("Eval(%s, %q) = %+v; want %q", c.id, c.in, r, c.want)
		}
	}
	for _, id := range []string{"nope", "translit:nope", "translitx", "plugin:nope/x", "upper"} {
		if r := s.Eval(id, "ABC"); r.Why == "" {
			t.Errorf("Eval(%s) did not refuse: %+v", id, r)
		}
	}
}

func TestPlugins(t *testing.T) {
	cfg := config.Default()
	off := false
	cfg.SetPlugin("one", config.PluginSettings{Preview: &off})
	s := plugins(t, cfg)
	if r := s.Eval("plugin:one/upper", "abc"); r.Text != "ABC" || r.Name != "Shout" {
		t.Errorf("one: %+v", r)
	}
	if n := s.Name("plugin:two/slow"); n != "Two: Slow" {
		t.Errorf("name in a submenu plugin: %q", n)
	}

	tree := s.Tree("abc")
	var lazy []string
	var walk func([]Entry)
	walk = func(es []Entry) {
		for _, e := range es {
			walk(e.Sub)
			if e.Lazy {
				lazy = append(lazy, e.ID)
			}
		}
	}
	walk(tree)
	slices.Sort(lazy)
	if want := []string{"plugin:lazy/upper", "plugin:one/upper"}; !slices.Equal(lazy, want) {
		t.Errorf("lazy: %v, want %v (manifest and settings)", lazy, want)
	}

	t0 := time.Now()
	p := s.Preview(tree, "abc", 1500*time.Millisecond)
	if took := time.Since(t0); took > 2500*time.Millisecond {
		t.Errorf("preview waited %v for a slow plugin", took)
	}
	if p["plugin:two/upper"].Text != "ABC" || p["snake"].Why == "" {
		t.Errorf("preview: %+v", p)
	}
	for _, id := range []string{"plugin:two/slow", "plugin:one/upper", "plugin:lazy/upper"} {
		if _, ok := p[id]; ok {
			t.Errorf("%s should not be in the preview", id)
		}
	}

	cfg.SetPlugin("two", config.PluginSettings{Disabled: true})
	if s := plugins(t, cfg); s.Eval("plugin:two/upper", "abc").Why == "" {
		t.Error("a disabled plugin still runs")
	}
}

func TestModules(t *testing.T) {
	cfg := config.Default()
	cfg.SetPlugin("encoding", config.PluginSettings{Disabled: true})
	cfg.SetPlugin("translit", config.PluginSettings{Disabled: true})
	s, _ := LoadFrom(t.TempDir(), cfg)
	var ids []string
	var walk func([]Entry)
	walk = func(es []Entry) {
		for _, e := range es {
			if e.ID != "" {
				ids = append(ids, e.ID)
			}
			walk(e.Sub)
		}
	}
	walk(s.Tree("Hello World"))
	for _, gone := range []string{"translit", "translit:uk", "base64-encode", "url-decode"} {
		if slices.Contains(ids, gone) {
			t.Errorf("%s in the menu with its module off", gone)
		}
	}
	for _, kept := range []string{"snake", "guillemets", "unquote"} {
		if !slices.Contains(ids, kept) {
			t.Errorf("%s missing", kept)
		}
	}
	if first := s.Tree("x")[0]; first.ID == "" && first.Sub == nil {
		t.Error("a separator first, where the transliteration was")
	}
	// Turned off is turned off for repeat too.
	if r := s.Eval("base64-encode", "x"); r.Why == "" || r.Text != "" {
		t.Errorf("eval of an action that is off: %+v", r)
	}
	if r := s.Eval("guillemets", "x"); r.Text != "«x»" {
		t.Errorf("quotes: %+v", r)
	}
	if len(Modules()) != 3 || !Modules()[0].Builtin {
		t.Errorf("modules: %v", Modules())
	}
}

func TestModuleNameTaken(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "quotes")
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "plugin.json"), []byte(`{"name":"Mine","command":["true"],"actions":[{"id":"a","name":"A"}]}`), 0o644)
	s, err := LoadFrom(dir, config.Default())
	if len(s.Plugins) != 0 || err == nil || !strings.Contains(err.Error(), "quotes") {
		t.Errorf("plugins %v, error %v", s.Plugins, err)
	}
}
