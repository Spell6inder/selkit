package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginsInstall(t *testing.T) {
	openTTY = func() (io.ReadWriteCloser, error) { return nil, errors.New("no terminal") }
	configDir := t.TempDir()
	src := filepath.Join(t.TempDir(), "mine")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "plugin.json"), []byte(`{"name":"Mine","version":"2.0","command":["true"],"actions":[{"id":"a","name":"Do A"}]}`), 0o644)
	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := Run(append([]string{"plugins"}, args...), en, configDir, nil, &out, &errOut)
		return code, out.String(), errOut.String()
	}
	installed := func(id string) bool {
		_, err := os.Stat(filepath.Join(configDir, "plugins", id, "plugin.json"))
		return err == nil
	}

	// Without a terminal and without --yes, nothing is installed; it says what to do.
	code, out, errOut := run("install", src)
	if code != 1 || installed("mine") || !strings.Contains(errOut, "--yes") {
		t.Errorf("no answer: %d %q %q", code, out, errOut)
	}
	// What would run is shown before anything else.
	for _, want := range []string{"Mine 2.0 (mine)", "Runs: true", "Do A", src} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}

	if code, out, _ := run("install", src, "--yes"); code != 0 || !installed("mine") || !strings.Contains(out, "Installed: mine") {
		t.Fatalf("install: %d %q", code, out)
	}
	if code, out, _ := run(); code != 0 || !strings.Contains(out, "mine") {
		t.Errorf("list after install: %q", out)
	}
	if code, _, errOut := run("install", src, "--yes"); code != 1 || !strings.Contains(errOut, "already installed") {
		t.Errorf("twice: %d %q", code, errOut)
	}
	if code, _, _ := run("install", "--yes", "--name=second", src); code != 0 || !installed("second") {
		t.Errorf("another name, flags first: %d", code)
	}
	if code, _, errOut := run("install", src, "--yes", "--name=quotes"); code != 1 || !strings.Contains(errOut, "built-in") || installed("quotes") {
		t.Errorf("a module's name: %d %q", code, errOut)
	}

	// Nothing came from GitHub, so there is nothing to update.
	if code, out, _ := run("update"); code != 0 || !strings.Contains(out, "No updates") {
		t.Errorf("update: %d %q", code, out)
	}

	// Removing takes its settings too.
	run("disable", "mine")
	if code, _, _ := run("remove", "mine"); code != 1 || !installed("mine") {
		t.Error("removed without an answer")
	}
	if code, out, _ := run("remove", "mine", "--yes"); code != 0 || installed("mine") || !strings.Contains(out, "Removed: mine") {
		t.Errorf("remove: %d %q", code, out)
	}
	if data, _ := os.ReadFile(filepath.Join(configDir, "config.json")); strings.Contains(string(data), `"mine"`) {
		t.Errorf("settings left: %s", data)
	}

	// With a terminal, the answer is read there, not from stdin.
	var asked bytes.Buffer
	openTTY = func() (io.ReadWriteCloser, error) { return fakeTTY{strings.NewReader("так\n"), &asked}, nil }
	if code, out, _ := run("install", src, "--name=third"); code != 0 || !strings.Contains(out, "Installed: third") || !strings.Contains(asked.String(), "[y/N]") {
		t.Errorf("answered yes: %d %q %q", code, out, asked.String())
	}
	openTTY = func() (io.ReadWriteCloser, error) { return fakeTTY{strings.NewReader("\n"), &asked}, nil }
	if code, _, _ := run("remove", "third"); code != 1 || !installed("third") {
		t.Error("removed on an empty answer")
	}

	for _, args := range [][]string{{"install"}, {"install", "a", "b"}, {"remove"}, {"update", "--name=x"}, {"install", src, "--nosuch"}} {
		if code, _, _ := run(args...); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	if code, _, _ := run("install", filepath.Join(t.TempDir(), "nosuch.zip"), "--yes"); code != 1 {
		t.Errorf("missing file: exit %d", code)
	}
}

type fakeTTY struct {
	io.Reader
	io.Writer
}

func (fakeTTY) Close() error { return nil }
