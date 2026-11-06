package clip

import (
	"os"
	"testing"
	"time"
)

// The test works with the real clipboard (contents are saved and restored),
// so it runs only when requested explicitly: SELKIT_CLIPBOARD_TEST=1 go test ./internal/clip
func TestLazyRendersOnRead(t *testing.T) {
	if os.Getenv("SELKIT_CLIPBOARD_TEST") == "" {
		t.Skip("SELKIT_CLIPBOARD_TEST not set")
	}
	saved, ok := Save()
	if !ok {
		t.Fatal("failed to save the clipboard")
	}
	defer Set(saved.Items, true)

	l, err := StartLazy()
	if err != nil {
		t.Fatal(err)
	}
	rendered, ok := l.Offer("привіт, primary")
	if !ok {
		t.Fatal("Offer failed")
	}
	select {
	case <-rendered:
		t.Fatal("rendered before read")
	default:
	}

	if got := Text(); got != "привіт, primary" {
		t.Fatalf("Text() = %q", got)
	}
	select {
	case <-rendered:
	case <-time.After(time.Second):
		t.Fatal("rendered channel was not closed after read")
	}
	if l.RenderSeq() == 0 {
		t.Fatal("RenderSeq not recorded")
	}
}
