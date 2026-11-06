package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPipedStdin(t *testing.T) {
	old := pipeWait
	pipeWait = 50 * time.Millisecond
	defer func() { pipeWait = old }()
	var out bytes.Buffer

	piped := func(text string) *os.File {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			w.WriteString(text)
			w.Close()
		}()
		return r
	}

	for _, c := range []struct {
		in, text string
		newline  bool
	}{
		{"test foo bar\n", "test foo bar", true},
		{"no break", "no break", false},
		{"two\nlines\n", "two\nlines", true},
		{"windows\r\n", "windows", true},
		{"two breaks\n\n", "two breaks\n", true},
	} {
		p := pipedStdin(piped(c.in), false, &out)
		if p == nil || p.text != c.text || p.newline != c.newline {
			t.Errorf("%q: %+v", c.in, p)
		}
	}

	// A file works like a pipe.
	path := filepath.Join(t.TempDir(), "in")
	os.WriteFile(path, []byte("from file\n"), 0o644)
	f, _ := os.Open(path)
	defer f.Close()
	if p := pipedStdin(f, false, &out); p == nil || p.text != "from file" {
		t.Errorf("file: %+v", p)
	}

	// Nothing given: the selection is used.
	if p := pipedStdin(piped(""), false, &out); p != nil {
		t.Errorf("closed pipe: %+v", p)
	}
	devNull, _ := os.Open(os.DevNull)
	defer devNull.Close()
	if p := pipedStdin(devNull, false, &out); p != nil {
		t.Errorf("null device: %+v", p)
	}
	if p := pipedStdin(strings.NewReader("x"), false, &out); p != nil {
		t.Errorf("not a file: %+v", p)
	}

	// A pipe nobody writes to or closes must not hang a key.
	r, w, _ := os.Pipe()
	defer w.Close()
	done := make(chan *pipeInput, 1)
	go func() { done <- pipedStdin(r, false, &out) }()
	select {
	case p := <-done:
		if p != nil {
			t.Errorf("silent pipe: %+v", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hung on a silent pipe")
	}

	// From a terminal, a slow command before the pipe is waited for.
	r2, w2, _ := os.Pipe()
	go func() {
		time.Sleep(3 * pipeWait)
		w2.WriteString("late\n")
		w2.Close()
	}()
	if p := pipedStdin(r2, true, &out); p == nil || p.text != "late" {
		t.Errorf("slow command: %+v", p)
	}
}

func TestMenuFromPipe(t *testing.T) {
	var out bytes.Buffer
	d, calls := fake(false, "xclip", "rofi", "xdotool")
	d.pipe = &pipeInput{"Hello World", true, &out}
	var input string
	second := ""
	d.runMenu = func(stdin, name string, a ...string) (string, error) {
		input = stdin
		for _, l := range strings.Split(stdin, "\n") {
			if strings.HasPrefix(l, "kebab-case") {
				second = l
			}
		}
		return second + "\n", nil
	}
	s := session{d, filepath.Join(t.TempDir(), "last-action"), en}
	if code := s.menu(true, true, ""); code != 0 {
		t.Fatalf("exit %d, %v", code, *calls)
	}
	if !strings.Contains(input, "hello-world") {
		t.Errorf("the menu did not get the piped text:\n%s", input)
	}
	if out.String() != "hello-world\n" {
		t.Errorf("stdout %q", out.String())
	}
	// The selection is not read; the clipboard gets the result without the line break; nothing is typed.
	if slices.ContainsFunc(*calls, func(c string) bool { return strings.HasPrefix(c, "xclip -o") }) {
		t.Errorf("read the selection: %v", *calls)
	}
	if !slices.Contains(*calls, "xclip -i -selection clipboard <hello-world") {
		t.Errorf("clipboard: %v", *calls)
	}
	if slices.ContainsFunc(*calls, func(c string) bool { return strings.HasPrefix(c, "xdotool") }) {
		t.Errorf("typed: %v", *calls)
	}

	// Repeat too, and a text without a line break gets none.
	out.Reset()
	*calls = nil
	d.pipe = &pipeInput{"Foo Bar", false, &out}
	if code := s.repeat(true, true); code != 0 {
		t.Fatalf("repeat: exit %d", code)
	}
	if out.String() != "foo-bar" || slices.ContainsFunc(*calls, func(c string) bool { return strings.HasPrefix(c, "xdotool") }) {
		t.Errorf("repeat: %q, %v", out.String(), *calls)
	}

	// Dismissing the menu prints nothing.
	out.Reset()
	d.runMenu = func(string, string, ...string) (string, error) { return "", nil }
	if code := s.menu(true, false, ""); code != 0 || out.Len() != 0 {
		t.Errorf("dismissed: %d, %q", code, out.String())
	}
}
