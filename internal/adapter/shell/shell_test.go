package shell_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apcandsons/otellite/internal/adapter/shell"
	"github.com/apcandsons/otellite/internal/domain"
	"github.com/apcandsons/otellite/internal/usecase"
)

var (
	jst = time.FixedZone("JST", 9*3600)
	t0  = time.Date(2026, 4, 1, 3, 34, 56, 0, time.UTC)
)

type fakeBrowser struct{}

func (fakeBrowser) Ls(path string) ([]usecase.Entry, error) {
	switch path {
	case "/":
		return []usecase.Entry{{Name: "iam", Dir: true}}, nil
	case "/iam":
		return []usecase.Entry{{Name: "iam-api", Dir: true}}, nil
	case "/iam/iam-api":
		return []usecase.Entry{{Name: "logs", Dir: true}, {Name: "metrics", Dir: true}}, nil
	case "/iam/iam-api/metrics":
		return []usecase.Entry{{Name: "go.memory.used.dat"}}, nil
	case "/iam/iam-api/metrics/go.memory.used.dat":
		return nil, fmt.Errorf("%s: %w", path, domain.ErrNotDir)
	}
	return nil, fmt.Errorf("%s: %w", path, domain.ErrNotFound)
}

func (fakeBrowser) Cat(path string) ([]domain.Sample, error) {
	if path == "/iam/iam-api/metrics/go.memory.used.dat" {
		return []domain.Sample{{Time: t0, Value: "43122688", Unit: "Bytes"}, {Time: t0.Add(time.Second), Value: "INFO started"}}, nil
	}
	return nil, fmt.Errorf("%s: %w", path, domain.ErrNotFound)
}

func run(t *testing.T, input string) string {
	t.Helper()
	var out bytes.Buffer
	sh := shell.New(fakeBrowser{}, jst)
	if err := sh.Run(strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestLsListsDirsWithSlash(t *testing.T) {
	out := run(t, "ls\nls /iam/iam-api\nls /iam/iam-api/metrics\n")
	for _, want := range []string{"iam/\n", "logs/\nmetrics/\n", "go.memory.used.dat\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCdChangesPromptAndResolvesRelative(t *testing.T) {
	out := run(t, "cd iam\npwd\ncd iam-api/metrics\npwd\ncd ..\npwd\ncd\npwd\n")
	if !strings.Contains(out, "> /iam\n") || !strings.Contains(out, "> /iam/iam-api/metrics\n") || !strings.Contains(out, "> /iam/iam-api\n") {
		t.Errorf("pwd output wrong:\n%s", out)
	}
	if !strings.Contains(out, "/iam/iam-api/metrics> ") {
		t.Errorf("prompt should show cwd:\n%s", out)
	}
	if !strings.HasSuffix(out, "/> /\n/> \n") {
		t.Errorf("cd with no args should return to root:\n%q", out)
	}
}

func TestCdErrors(t *testing.T) {
	out := run(t, "cd nope\ncd /iam/iam-api/metrics/go.memory.used.dat\n")
	if !strings.Contains(out, "cd: /nope: no such file or directory") {
		t.Errorf("missing not-found error:\n%s", out)
	}
	if !strings.Contains(out, "not a directory") {
		t.Errorf("missing not-a-dir error:\n%s", out)
	}
}

func TestCatFormatsSamplesInLocalZone(t *testing.T) {
	out := run(t, "cd /iam/iam-api/metrics\ncat go.memory.used.dat\n")
	want := "[2026-04-01 12:34:56 JST] 41.1 MB\n[2026-04-01 12:34:57 JST] INFO started\n"
	if !strings.Contains(out, want) {
		t.Errorf("want %q in:\n%s", want, out)
	}
}

func TestUnknownAndExit(t *testing.T) {
	out := run(t, "frobnicate\ncat\nexit\nls\n")
	if !strings.Contains(out, "frobnicate: command not found") {
		t.Errorf("missing unknown command message:\n%s", out)
	}
	if !strings.Contains(out, "cat: missing operand") {
		t.Errorf("missing operand message:\n%s", out)
	}
	if strings.Contains(out, "iam/") {
		t.Errorf("ls ran after exit:\n%s", out)
	}
}
