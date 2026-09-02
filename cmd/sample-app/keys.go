package main

import (
	"bytes"
	"io"
	"log"
	"os"

	"golang.org/x/term"
)

// readKeys forwards single bytes from r to keys until r hits EOF or fails.
// When stdin is a terminal, main has already put it in raw mode so a key
// arrives without waiting for Enter.
func readKeys(r io.Reader, keys chan<- byte) {
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n == 1 {
			keys <- buf[0]
		}
		if err != nil {
			return
		}
	}
}

// rawTerminal puts stdin in raw mode so keypresses are delivered immediately.
// It returns a restore func, which is a no-op when stdin is not a terminal.
// Raw mode also turns off "\n" -> "\r\n" translation, so log output is
// rewritten to keep lines aligned.
func rawTerminal() func() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return func() {}
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		log.Printf("sample-app: raw mode unavailable: %v", err)
		return func() {}
	}
	log.SetOutput(crlfWriter{os.Stderr})
	return func() {
		term.Restore(fd, state)
		log.SetOutput(os.Stderr)
	}
}

type crlfWriter struct{ w io.Writer }

func (c crlfWriter) Write(p []byte) (int, error) {
	_, err := c.w.Write(bytes.ReplaceAll(p, []byte("\n"), []byte("\r\n")))
	return len(p), err
}
