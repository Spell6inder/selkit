package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Spell6inder/selkit/internal/i18n"
)

// The test binary doubles as the plugin: run with SELKIT_ACTION set, it does what the action says.
func TestMain(m *testing.M) {
	if a := os.Getenv("SELKIT_ACTION"); a != "" {
		os.Exit(fakePlugin(a))
	}
	os.Exit(m.Run())
}

func fakePlugin(action string) int {
	in, _ := io.ReadAll(os.Stdin)
	switch action {
	case "upper":
		fmt.Println(strings.ToUpper(string(in))) // with the newline print adds
	case "skip":
		fmt.Fprintln(os.Stderr, "no digits here")
		return exitNotApplicable
	case "fail":
		fmt.Fprintln(os.Stderr, "boom")
		return 1
	case "slow":
		time.Sleep(5 * time.Second)
	case "env":
		fmt.Print(os.Getenv("SELKIT_LANG") + " " + os.Getenv("SELKIT_PREVIEW"))
	}
	return 0
}

// install writes a plugin whose command is this test binary.
func install(t *testing.T, root, id, extra string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(filepath.Join(dir, "locales"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	cmd, _ := json.Marshal(exe)
	manifest := `{"name": "Fake", "command": [` + string(cmd) + `], "timeout_ms": 1000, ` + extra + `
		"actions": [{"id": "upper", "name": "Shout"}, {"id": "skip"}, {"id": "fail"}, {"id": "slow"}, {"id": "env"}]}`
	os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644)
	os.WriteFile(filepath.Join(dir, "locales", "uk.po"), []byte("msgid \"Shout\"\nmsgstr \"Кричати\"\n"), 0o644)
}

func TestLoad(t *testing.T) {
	root := t.TempDir()
	install(t, root, "fake", `"preview": false,`)
	os.MkdirAll(filepath.Join(root, "broken"), 0o755)
	os.WriteFile(filepath.Join(root, "broken", "plugin.json"), []byte(`{"name": "x"}`), 0o644)
	os.MkdirAll(filepath.Join(root, "not-a-plugin"), 0o755)

	i18n.Init("uk", "")
	defer i18n.Init("en", "")
	list, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Errorf("broken plugin not reported: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("plugins: %v", list)
	}
	p := list[0]
	if p.ID != "fake" || p.Preview || p.Timeout != time.Second {
		t.Errorf("manifest: %+v", p)
	}
	if p.Actions[0].Name != "Кричати" || p.Actions[1].Name != "skip" {
		t.Errorf("names from the plugin's own translation: %+v", p.Actions)
	}
	if list, err := Load(filepath.Join(root, "missing")); list != nil || err != nil {
		t.Errorf("no plugins folder: %v, %v", list, err)
	}
}

func TestRun(t *testing.T) {
	root := t.TempDir()
	install(t, root, "fake", "")
	list, _ := Load(root)
	p := list[0]
	ctx := context.Background()

	if out, err := p.Run(ctx, "upper", "abc", "uk", false); out != "ABC" || err != nil {
		t.Errorf("upper: %q, %v", out, err)
	}
	if out, _ := p.Run(ctx, "upper", "abc\n", "uk", false); out != "ABC\n\n" {
		t.Errorf("a newline the text had is kept: %q", out)
	}
	if out, _ := p.Run(ctx, "env", "", "uk", true); out != "uk 1" {
		t.Errorf("env: %q", out)
	}
	var na NotApplicable
	if _, err := p.Run(ctx, "skip", "x", "en", false); !errors.As(err, &na) || string(na) != "no digits here" {
		t.Errorf("skip: %v", err)
	}
	if _, err := p.Run(ctx, "fail", "x", "en", false); err == nil || errors.As(err, &na) || !strings.Contains(err.Error(), "boom") {
		t.Errorf("fail: %v", err)
	}
	t0 := time.Now()
	if _, err := p.Run(ctx, "slow", "x", "en", false); err == nil || time.Since(t0) > 3*time.Second {
		t.Errorf("timeout: %v after %v", err, time.Since(t0))
	}
}

// The example plugin in examples/plugins must keep working.
func TestExample(t *testing.T) {
	list, err := Load(filepath.Join("..", "..", "examples", "plugins"))
	if err != nil || len(list) == 0 {
		t.Fatalf("examples: %v, %v", list, err)
	}
	p := list[0]
	if _, err := exec.LookPath(p.Command[0]); err != nil {
		t.Skip(p.Command[0], " not installed")
	}
	ctx := context.Background()
	for _, c := range []struct{ action, in, want string }{
		{"round", "Щука", "(Щука)"},
		{"square", "a b", "[a b]"},
		{"curly", "x", "{x}"},
		{"unwrap", " (Щука) ", "Щука"},
	} {
		if out, err := p.Run(ctx, c.action, c.in, "en", false); out != c.want || err != nil {
			t.Errorf("%s(%q) = %q, %v; want %q", c.action, c.in, out, err, c.want)
		}
	}
	var na NotApplicable
	if _, err := p.Run(ctx, "unwrap", "(plain]", "en", false); !errors.As(err, &na) {
		t.Errorf("unwrap of a text not in brackets: %v", err)
	}
}
