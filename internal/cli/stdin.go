package cli

import (
	"bufio"
	"io"
	"os"
	"strings"
	"time"
)

// pipeInput is text given on stdin to "selkit menu" or "repeat": it stands in for the selection, and the result
// also goes to stdout.
type pipeInput struct {
	text    string
	newline bool // the text ended with a line break, so the result gets one on stdout
	out     io.Writer
}

// How long a program without a terminal waits for the first byte on a pipe.
var pipeWait = 300 * time.Millisecond

// pipedStdin reads stdin when it is a pipe or a file; nil means there is no text there and the selection is used.
//
// A desktop can start a program with a pipe that nobody writes to or closes. Without a terminal, then, the text has
// to start coming soon, or the key would seem to do nothing. From a terminal the wait can be as long as the
// command before the pipe needs: Ctrl+C is at hand.
func pipedStdin(stdin io.Reader, terminal bool, out io.Writer) *pipeInput {
	f, ok := stdin.(*os.File)
	if !ok {
		return nil
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() && st.Mode()&os.ModeNamedPipe == 0 {
		return nil
	}
	r := bufio.NewReader(f)
	first := make(chan error, 1)
	go func() {
		_, err := r.Peek(1)
		first <- err
	}()
	if terminal {
		err = <-first
	} else {
		select {
		case err = <-first:
		case <-time.After(pipeWait):
			return nil
		}
	}
	if err != nil { // closed at once: nothing was given
		return nil
	}
	data, err := io.ReadAll(r)
	if err != nil && len(data) == 0 {
		return nil
	}
	text := string(data)
	trimmed := strings.TrimSuffix(strings.TrimSuffix(text, "\n"), "\r")
	return &pipeInput{trimmed, trimmed != text, out}
}
