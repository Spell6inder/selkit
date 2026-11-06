package history

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/Spell6inder/selkit/internal/clip"
)

// The test works with the real clipboard and Win+V history (adds and deletes its own entry, restores the clipboard contents),
// so it runs only when requested explicitly: SELKIT_CLIPBOARD_TEST=1 go test ./internal/history
func TestDeleteRecent(t *testing.T) {
	if os.Getenv("SELKIT_CLIPBOARD_TEST") == "" {
		t.Skip("SELKIT_CLIPBOARD_TEST not set")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initThread()

	statics, err := activate()
	if err != nil {
		t.Fatal(err)
	}
	if !isEnabled(statics) {
		t.Skip("clipboard history is disabled")
	}

	saved, ok := clip.Save()
	if !ok {
		t.Fatal("failed to save the clipboard")
	}
	defer clip.Set(saved.Items, true)

	marker := fmt.Sprintf("selkit-history-test-%d", time.Now().UnixNano())
	clip.Set([]clip.Item{clip.TextItem(marker)}, false) // no markers: it will land in history

	var deleted bool
	for _, d := range retryDelays {
		time.Sleep(d)
		if deleted, err = deleteRecent(statics, marker); err != nil {
			t.Fatal(err)
		}
		if deleted {
			break
		}
	}
	if !deleted {
		t.Fatal("test entry not found in history")
	}
	time.Sleep(300 * time.Millisecond)
	if again, _ := deleteRecent(statics, marker); again {
		t.Fatal("entry remained in history after deletion")
	}
}
