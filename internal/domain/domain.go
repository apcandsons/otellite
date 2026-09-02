// Package domain holds the entities of OTel Lite: the virtual filesystem
// layout (namespace / service / kind / stream), samples, and the retention
// rules. It has no dependencies beyond the standard library.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Kind is the signal type of a stream.
type Kind int

const (
	KindNone Kind = iota
	Logs
	Metrics
)

// String returns the directory name used for the kind.
func (k Kind) String() string {
	switch k {
	case Logs:
		return "logs"
	case Metrics:
		return "metrics"
	}
	return ""
}

// ParseKind maps a directory name to a Kind.
func ParseKind(s string) (Kind, bool) {
	switch s {
	case "logs":
		return Logs, true
	case "metrics":
		return Metrics, true
	}
	return KindNone, false
}

// Sample is one point in a stream. Value is already rendered as text: a
// number for metrics, "SEVERITY body" for logs. Unit is empty for logs.
type Sample struct {
	Time  time.Time
	Value string
	Unit  string
}

// StreamID uniquely identifies a .dat file.
type StreamID struct {
	Namespace string
	Service   string
	Kind      Kind
	Name      string
}

// Path returns the filesystem path of the stream.
func (id StreamID) Path() Path {
	return Path{Namespace: id.Namespace, Service: id.Service, Kind: id.Kind, Name: id.Name}
}

// Depth is how far down the virtual filesystem a Path points.
type Depth int

const (
	DepthRoot Depth = iota
	DepthNamespace
	DepthService
	DepthKind
	DepthStream
)

// Path is a parsed location in the virtual filesystem. Unset trailing
// fields mean the path points at a directory above that level.
type Path struct {
	Namespace string
	Service   string
	Kind      Kind
	Name      string
}

const datSuffix = ".dat"

var (
	ErrBadPath  = errors.New("bad path")
	ErrNotFound = errors.New("no such file or directory")
	ErrNotDir   = errors.New("not a directory")
	ErrIsDir    = errors.New("is a directory")
)

// ParsePath parses an absolute, clean path such as
// "/iam/iam-api/metrics/go.memory.used.dat". Malformed paths yield
// ErrBadPath; well-formed paths that cannot exist in the layout (a third
// signal kind, a file without .dat, too many levels) yield ErrNotFound.
func ParsePath(s string) (Path, error) {
	if !strings.HasPrefix(s, "/") {
		return Path{}, fmt.Errorf("%q is not absolute: %w", s, ErrBadPath)
	}
	if s == "/" {
		return Path{}, nil
	}
	parts := strings.Split(strings.TrimPrefix(s, "/"), "/")
	if len(parts) > 4 {
		return Path{}, fmt.Errorf("%s: %w", s, ErrNotFound)
	}
	var p Path
	for i, part := range parts {
		if part == "" {
			return Path{}, fmt.Errorf("%q has an empty component: %w", s, ErrBadPath)
		}
		switch Depth(i + 1) {
		case DepthNamespace:
			p.Namespace = part
		case DepthService:
			p.Service = part
		case DepthKind:
			k, ok := ParseKind(part)
			if !ok {
				return Path{}, fmt.Errorf("%s: %w", s, ErrNotFound)
			}
			p.Kind = k
		case DepthStream:
			if !strings.HasSuffix(part, datSuffix) || len(part) == len(datSuffix) {
				return Path{}, fmt.Errorf("%s: %w", s, ErrNotFound)
			}
			p.Name = strings.TrimSuffix(part, datSuffix)
		}
	}
	return p, nil
}

// Depth reports the level the path points at.
func (p Path) Depth() Depth {
	switch {
	case p.Name != "":
		return DepthStream
	case p.Kind != KindNone:
		return DepthKind
	case p.Service != "":
		return DepthService
	case p.Namespace != "":
		return DepthNamespace
	}
	return DepthRoot
}

// StreamID converts a stream-depth path to its identifier.
func (p Path) StreamID() StreamID {
	return StreamID{Namespace: p.Namespace, Service: p.Service, Kind: p.Kind, Name: p.Name}
}

// String renders the path back to its slash form.
func (p Path) String() string {
	var b strings.Builder
	b.WriteString("/")
	if p.Namespace == "" {
		return b.String()
	}
	b.WriteString(p.Namespace)
	if p.Service == "" {
		return b.String()
	}
	b.WriteString("/" + p.Service)
	if p.Kind == KindNone {
		return b.String()
	}
	b.WriteString("/" + p.Kind.String())
	if p.Name == "" {
		return b.String()
	}
	b.WriteString("/" + p.Name + datSuffix)
	return b.String()
}

// Window is the time-based retention rule: samples older than Duration
// before now are forgotten.
type Window struct {
	Duration time.Duration
}

// Cutoff returns the oldest timestamp still retained at the given moment.
func (w Window) Cutoff(now time.Time) time.Time {
	return now.Add(-w.Duration)
}

// Budget is the memory-based retention rule: at most MaxSamples samples
// are kept across the whole store. Zero means unlimited.
type Budget struct {
	MaxSamples int
}

// Excess returns how many samples must be dropped to fit the budget.
func (b Budget) Excess(count int) int {
	if b.MaxSamples <= 0 || count <= b.MaxSamples {
		return 0
	}
	return count - b.MaxSamples
}

// FileName returns the .dat file name for a stream name.
func FileName(streamName string) string { return streamName + datSuffix }
