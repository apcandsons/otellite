// Package shell is the interactive CLI: a filesystem-style REPL over a
// Browser (normally a remote SoR via fsapi.Client).
package shell

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/apcandsons/otellite/internal/domain"
	"github.com/apcandsons/otellite/internal/usecase"
)

// Browser is the only thing the shell needs from the outside world.
type Browser interface {
	Ls(path string) ([]usecase.Entry, error)
	Cat(path string) ([]domain.Sample, error)
}

const timeLayout = "2006-01-02 15:04:05 MST"

// Shell holds the session state: the current directory.
type Shell struct {
	b   Browser
	loc *time.Location
	cwd string
}

func New(b Browser, loc *time.Location) *Shell {
	if loc == nil {
		loc = time.Local
	}
	return &Shell{b: b, loc: loc, cwd: "/"}
}

// Run reads commands line by line until EOF or exit.
func (s *Shell) Run(in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	for {
		fmt.Fprintf(out, "%s> ", s.cwd)
		if !sc.Scan() {
			fmt.Fprintln(out)
			return sc.Err()
		}
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		if !s.exec(out, fields[0], fields[1:]) {
			return nil
		}
	}
}

// exec runs one command and reports whether the shell should keep going.
func (s *Shell) exec(out io.Writer, cmd string, args []string) bool {
	switch cmd {
	case "exit", "quit":
		return false
	case "pwd":
		fmt.Fprintln(out, s.cwd)
	case "help":
		fmt.Fprintln(out, "commands: ls [path]  cd [path]  cat <file>  pwd  help  exit")
	case "cd":
		s.cd(out, args)
	case "ls":
		s.ls(out, args)
	case "cat":
		s.cat(out, args)
	default:
		fmt.Fprintf(out, "%s: command not found\n", cmd)
	}
	return true
}

func (s *Shell) cd(out io.Writer, args []string) {
	target := "/"
	if len(args) > 0 {
		target = s.resolve(args[0])
	}
	if _, err := s.b.Ls(target); err != nil {
		fmt.Fprintf(out, "cd: %v\n", err)
		return
	}
	s.cwd = target
}

func (s *Shell) ls(out io.Writer, args []string) {
	target := s.cwd
	if len(args) > 0 {
		target = s.resolve(args[0])
	}
	entries, err := s.b.Ls(target)
	if err != nil {
		fmt.Fprintf(out, "ls: %v\n", err)
		return
	}
	for _, e := range entries {
		if e.Dir {
			fmt.Fprintf(out, "%s/\n", e.Name)
		} else {
			fmt.Fprintln(out, e.Name)
		}
	}
}

func (s *Shell) cat(out io.Writer, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(out, "cat: missing operand")
		return
	}
	for _, arg := range args {
		samples, err := s.b.Cat(s.resolve(arg))
		if err != nil {
			fmt.Fprintf(out, "cat: %v\n", err)
			continue
		}
		for _, sm := range samples {
			fmt.Fprintln(out, Format(sm, s.loc))
		}
	}
}

// resolve turns a user-supplied path into an absolute, clean path.
func (s *Shell) resolve(p string) string {
	if !strings.HasPrefix(p, "/") {
		p = path.Join(s.cwd, p)
	}
	return path.Clean(p)
}

// Format renders one sample as "[local time] value unit".
func Format(sm domain.Sample, loc *time.Location) string {
	line := "[" + sm.Time.In(loc).Format(timeLayout) + "] " + sm.Value
	if sm.Unit != "" {
		line += " " + sm.Unit
	}
	return line
}
