package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Spell6inder/selkit/internal/actions"
)

var en = actions.Set{Lang: "en"}

func TestChoices(t *testing.T) {
	var ids []string
	for _, a := range choices(en, "Привіт") {
		if !a.header {
			ids = append(ids, a.ID)
		}
	}
	if ids[0] != "translit" || !slices.Contains(ids, "translit:uk") || !slices.Contains(ids, "upper") {
		t.Errorf("actions: %v", ids)
	}
	if slices.Contains(ids, "title") || slices.Contains(ids, "sort") {
		t.Errorf("actions that change nothing: %v", ids)
	}
}

// fake is a desktop with the given programs installed; it records the commands.
func fake(wayland bool, installed ...string) (*desktop, *[]string) {
	var calls []string
	d := &desktop{
		wayland: wayland,
		lookPath: func(name string) (string, error) {
			if slices.Contains(installed, name) {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		run: func(stdin, name string, args ...string) (string, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " ")+" <"+stdin)
			switch name {
			case "xclip", "wl-paste":
				return "fooBar", nil
			case "rofi": // the second item that can be chosen
				var items []string
				for _, l := range strings.Split(stdin, "\n") {
					if !strings.Contains(l, "\x00") {
						items = append(items, l)
					}
				}
				return items[1] + "\n", nil
			}
			return "", nil
		},
	}
	d.runMenu = d.run
	return d, &calls
}

func TestTools(t *testing.T) {
	d, calls := fake(false, "wl-paste", "xclip")
	if s, _ := d.Read(true); s != "fooBar" || !strings.HasPrefix((*calls)[0], "xclip -o -selection primary") {
		t.Errorf("X11 read: %q via %v", s, *calls)
	}
	d, calls = fake(true, "wl-paste", "wl-copy", "xclip")
	d.Read(true)
	d.Write("x")
	want := []string{"wl-paste --no-newline --primary <", "wl-copy <x", "wl-copy --primary <x"}
	if !slices.Equal(*calls, want) {
		t.Errorf("Wayland: %q, want %q", *calls, want)
	}
	if d, _ := fake(false); d.Write("x") == nil {
		t.Error("no clipboard tool, but no error")
	}
}

func TestTypeLines(t *testing.T) {
	d, calls := fake(false, "xdotool")
	if err := d.Type("ab\r\n\ncd\n"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"xdotool type --clearmodifiers -- ab <",
		"xdotool key --clearmodifiers Return <",
		"xdotool key --clearmodifiers Return <",
		"xdotool type --clearmodifiers -- cd <",
		"xdotool key --clearmodifiers Return <",
	}
	if !slices.Equal(*calls, want) {
		t.Errorf("xdotool:\n got %q\nwant %q", *calls, want)
	}

	d, calls = fake(true, "wtype")
	if err := d.Type("ab\ncd"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"wtype -- ab\ncd <"}; !slices.Equal(*calls, want) {
		t.Errorf("wtype: got %q, want %q", *calls, want)
	}
}

func TestRepeatMark(t *testing.T) {
	last := filepath.Join(t.TempDir(), "last-action")
	second := choices(en, "fooBar")[2]
	os.WriteFile(last, []byte(second.ID+"\n"), 0o644)

	var input string
	var args []string
	d, _ := fake(false, "xclip", "rofi")
	d.runMenu = func(stdin, name string, a ...string) (string, error) {
		input, args = stdin, a
		return "", nil
	}
	s := session{d, last, en}
	s.menu(true, false, "")
	rows := strings.Split(input, "\n")
	if !strings.HasPrefix(rows[2], "↻ "+second.Name) {
		t.Errorf("row %q", rows[2])
	}
	if strings.Count(input, "↻") != 1 {
		t.Errorf("marks: %q", input)
	}
	if !strings.Contains(strings.Join(args, " "), "-a 2") {
		t.Errorf("rofi args %v", args)
	}

	// The marked row is still chosen as its action, in a program that only returns the text.
	d, _ = fake(false, "xclip", "zenity")
	d.runMenu = func(stdin, name string, a ...string) (string, error) {
		input, args = stdin, a
		return strings.Split(stdin, "\n")[2] + "\n", nil
	}
	s = session{d, last, en}
	if code := s.menu(true, false, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if id, _ := os.ReadFile(last); strings.TrimSpace(string(id)) != second.ID {
		t.Errorf("chose %q", id)
	}
	if slices.Contains(args, "-a") {
		t.Errorf("zenity got rofi options: %v", args)
	}

	// An action that does not fit the text leaves no mark; a missing file neither.
	os.WriteFile(last, []byte("nosuch"), 0o644)
	d, _ = fake(false, "xclip", "zenity")
	d.runMenu = func(stdin, name string, a ...string) (string, error) { input = stdin; return "", nil }
	session{d, last, en}.menu(true, false, "")
	if strings.Contains(input, "↻") {
		t.Errorf("mark without its action: %q", input)
	}
}

func TestTypeWayland(t *testing.T) {
	// wtype and ydotool fail, as on KDE without /dev/uinput: xdotool pastes, it does not type.
	d, calls := fake(true, "wtype", "ydotool", "xdotool")
	base := d.run
	d.run = func(stdin, name string, args ...string) (string, error) {
		base(stdin, name, args...)
		if name != "xdotool" {
			return "", errors.New(name + " failed")
		}
		return "", nil
	}
	if err := d.Type("Щука"); err != nil {
		t.Fatal(err)
	}
	want := []string{"wtype -- Щука <", "ydotool key 42:1 110:1 110:0 42:0 <", "xdotool key --clearmodifiers shift+Insert <"}
	if !slices.Equal(*calls, want) {
		t.Errorf("got %q, want %q", *calls, want)
	}

	// All fail: the last error is reported.
	d, _ = fake(true, "wtype")
	d.run = func(string, string, ...string) (string, error) { return "", errors.New("no protocol") }
	if err := d.Type("x"); err == nil || err.Error() != "no protocol" {
		t.Errorf("error: %v", err)
	}

	// Nothing installed.
	d, _ = fake(true)
	if err := d.Type("x"); err == nil {
		t.Error("no tools, but no error")
	}
}

func TestMenu(t *testing.T) {
	d, calls := fake(false, "xclip", "rofi")
	last := filepath.Join(t.TempDir(), "last-action")
	s := session{d, last, en}
	if code := s.menu(true, false, ""); code != 0 {
		t.Fatalf("menu: exit %d, calls %v", code, *calls)
	}
	id, _ := os.ReadFile(last)
	second := choices(en, "fooBar")[2] // after the first action and a header
	if string(id) != second.ID || !slices.Contains(*calls, "xclip -i -selection clipboard <"+second.Text) {
		t.Errorf("chose %q, calls %v; want %s", id, *calls, second.ID)
	}

	*calls = nil
	if code := s.repeat(true, false); code != 0 || !slices.Contains(*calls, "xclip -i -selection primary <"+second.Text) {
		t.Errorf("repeat: exit %d, calls %v", code, *calls)
	}
}

func TestRun(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"apply", "kebab"}, en, t.TempDir(), strings.NewReader("Hello World"), &out, &errOut); code != 0 || out.String() != "hello-world" {
		t.Errorf("apply: %d %q %q", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := Run([]string{"apply", "kebab"}, en, t.TempDir(), strings.NewReader("Hello World\n"), &out, &errOut); code != 0 || out.String() != "hello-world\n" {
		t.Errorf("apply: %d %q %q", code, out.String(), errOut.String())
	}
	out.Reset()
	if Run([]string{"list"}, en, "", nil, &out, &errOut); !strings.Contains(out.String(), "translit:uk") {
		t.Errorf("list: %s", out.String())
	}
}

func TestErrors(t *testing.T) {
	d, _ := fake(false, "xclip", "rofi")
	d.run = func(string, string, ...string) (string, error) { return "", errors.New("xclip: Can't open display") }
	if _, err := d.Read(true); err == nil {
		t.Error("no display, but no error")
	}
	d.run = func(string, string, ...string) (string, error) {
		return "", errors.New("xclip: target STRING not available")
	}
	if s, err := d.Read(true); s != "" || err != nil {
		t.Errorf("an empty selection is not an error: %q, %v", s, err)
	}
	d.runMenu = func(string, string, ...string) (string, error) { return "", errors.New("not found") }
	if _, err := d.Pick([]menuLine{{text: "a"}}, "nosuch"); err == nil {
		t.Error("a menu program that cannot start, but no error")
	}
}

func TestHeaders(t *testing.T) {
	list := choices(en, "fooBar")
	if list[0].header || !list[1].header || list[1].Name == "" || list[1].ID != "" {
		t.Errorf("first items: %+v", list[:2])
	}
	for i, c := range list {
		if c.header && (i+1 == len(list) || list[i+1].header) {
			t.Errorf("a header with no actions: %q", c.Name)
		}
	}

	// Choosing a header shows the menu again; only rofi is told about the headers.
	d, calls := fake(false, "xclip", "zenity")
	var inputs []string
	d.runMenu = func(stdin, name string, args ...string) (string, error) {
		inputs = append(inputs, stdin)
		lines := strings.Split(stdin, "\n")
		if len(inputs) == 1 {
			return lines[1] + "\n", nil
		}
		return lines[0] + "\n", nil
	}
	s := session{d, filepath.Join(t.TempDir(), "last-action"), en}
	if code := s.menu(true, false, ""); code != 0 || len(inputs) != 2 {
		t.Fatalf("exit %d, menus shown %d, calls %v", code, len(inputs), *calls)
	}
	if strings.Contains(inputs[0], "\x00") {
		t.Error("rofi markup sent to another menu program")
	}
	if id, _ := os.ReadFile(s.lastPath); string(id) != list[0].ID {
		t.Errorf("chose %q", id)
	}

	d, _ = fake(false, "xclip", "rofi")
	inputs = nil
	d.runMenu = func(stdin, name string, args ...string) (string, error) {
		inputs = append(inputs, stdin)
		return "", nil
	}
	if i, err := d.Pick([]menuLine{{text: "a"}, {text: "b", header: true}}, ""); i != -1 || err != nil {
		t.Errorf("dismissed: %d, %v", i, err)
	}
	if want := "a\nb\x00nonselectable\x1ftrue\n"; inputs[0] != want {
		t.Errorf("rofi input %q, want %q", inputs[0], want)
	}
}

func TestPlugins(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plugins", "brackets")
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "plugin.json"), []byte(`{"name":"Brackets","command":["true"],"actions":[{"id":"a","name":"A"}]}`), 0o644)
	cfg := filepath.Join(dir, "config.json")

	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := Run(append([]string{"plugins"}, args...), en, dir, nil, &out, &errOut)
		return code, out.String(), errOut.String()
	}

	// Listing writes nothing.
	if code, out, _ := run(); code != 0 || !strings.Contains(out, "brackets") || !strings.Contains(out, "Brackets") {
		t.Errorf("list: %d %q", code, out)
	}
	if _, err := os.Stat(cfg); err == nil {
		t.Error("listing created the config")
	}

	if code, out, _ := run("disable", "brackets"); code != 0 || !strings.Contains(out, "disabled") {
		t.Errorf("disable: %d %q", code, out)
	}
	if data, _ := os.ReadFile(cfg); strings.Contains(string(data), "terminals") || !strings.Contains(string(data), `"disabled": true`) {
		t.Errorf("config: %s", data)
	}
	if code, out, _ := run("preview", "brackets", "off"); code != 0 || !strings.Contains(out, "no preview") || !strings.Contains(out, "disabled") {
		t.Errorf("preview: %d %q", code, out)
	}

	// Enabling keeps the rest of the file; with nothing left to store, the section goes.
	os.WriteFile(cfg, []byte(`{"language":"uk","plugins":{"brackets":{"disabled":true}}}`), 0o644)
	if code, _, _ := run("enable", "brackets"); code != 0 {
		t.Errorf("enable: %d", code)
	}
	if data, _ := os.ReadFile(cfg); !strings.Contains(string(data), `"language": "uk"`) || strings.Contains(string(data), "plugins") {
		t.Errorf("config after enable: %s", data)
	}

	if code, _, _ := run("disable", "nosuch"); code != 1 {
		t.Errorf("unknown plugin: exit %d", code)
	}
	if code, _, _ := run("preview", "brackets", "maybe"); code != 2 {
		t.Errorf("bad usage: exit %d", code)
	}

	// A broken file is reported and left as it is.
	broken := `{"plugins":`
	os.WriteFile(cfg, []byte(broken), 0o644)
	if code, _, errOut := run("disable", "brackets"); code != 1 || !strings.Contains(errOut, "config.json") {
		t.Errorf("broken config: %d %q", code, errOut)
	}
	if data, _ := os.ReadFile(cfg); string(data) != broken {
		t.Errorf("broken config was overwritten: %s", data)
	}
}

func TestPluginsModules(t *testing.T) {
	dir := t.TempDir()
	// A folder named like a built-in module is not a plugin.
	p := filepath.Join(dir, "plugins", "quotes")
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "plugin.json"), []byte(`{"name":"Mine","command":["true"],"actions":[{"id":"a","name":"A"}]}`), 0o644)
	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := Run(append([]string{"plugins"}, args...), en, dir, nil, &out, &errOut)
		return code, out.String(), errOut.String()
	}
	code, out, _ := run()
	for _, want := range []string{"translit", "encoding", "quotes", "built-in", "Transliteration"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("list: no %q in %q", want, out)
		}
	}
	if strings.Contains(out, "Mine") {
		t.Errorf("a plugin under a module's name was listed: %q", out)
	}
	if code, out, _ := run("disable", "encoding"); code != 0 || !strings.Contains(out, "disabled") {
		t.Errorf("disable a module: %d %q", code, out)
	}
	if code, _, errOut := run("preview", "encoding", "off"); code != 1 || !strings.Contains(errOut, "built in") {
		t.Errorf("preview of a module: %d %q", code, errOut)
	}
}
