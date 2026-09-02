package usecase

import (
	"fmt"

	"github.com/apcandsons/otellite/internal/domain"
)

// ReadStore is what Browser needs from a store. List results must be sorted
// and only include entries that currently hold at least one sample.
type ReadStore interface {
	Namespaces() []string
	Services(namespace string) []string
	Streams(namespace, service string, kind domain.Kind) []string
	Samples(domain.StreamID) ([]domain.Sample, bool)
}

// Entry is one row of an ls listing.
type Entry struct {
	Name string
	Dir  bool
}

// Browser answers ls and cat against the virtual filesystem.
type Browser struct {
	store ReadStore
}

func NewBrowser(store ReadStore) *Browser { return &Browser{store: store} }

// Ls lists a directory. Directories exist when they contain data, except
// the fixed logs/ and metrics/ directories which always exist under a
// service.
func (b *Browser) Ls(path string) ([]Entry, error) {
	p, err := domain.ParsePath(path)
	if err != nil {
		return nil, err
	}
	switch p.Depth() {
	case domain.DepthRoot:
		return dirs(b.store.Namespaces()), nil
	case domain.DepthNamespace:
		if !contains(b.store.Namespaces(), p.Namespace) {
			return nil, notFound(path)
		}
		return dirs(b.store.Services(p.Namespace)), nil
	case domain.DepthService:
		if err := b.checkService(p); err != nil {
			return nil, err
		}
		return []Entry{{Name: domain.Logs.String(), Dir: true}, {Name: domain.Metrics.String(), Dir: true}}, nil
	case domain.DepthKind:
		if err := b.checkService(p); err != nil {
			return nil, err
		}
		names := b.store.Streams(p.Namespace, p.Service, p.Kind)
		out := make([]Entry, 0, len(names))
		for _, n := range names {
			out = append(out, Entry{Name: domain.FileName(n)})
		}
		return out, nil
	default:
		if _, ok := b.store.Samples(p.StreamID()); !ok {
			return nil, notFound(path)
		}
		return nil, fmt.Errorf("%s: %w", path, domain.ErrNotDir)
	}
}

// Cat returns the samples of one stream, oldest first.
func (b *Browser) Cat(path string) ([]domain.Sample, error) {
	p, err := domain.ParsePath(path)
	if err != nil {
		return nil, err
	}
	if p.Depth() != domain.DepthStream {
		return nil, fmt.Errorf("%s: %w", path, domain.ErrIsDir)
	}
	ss, ok := b.store.Samples(p.StreamID())
	if !ok {
		return nil, notFound(path)
	}
	return ss, nil
}

func (b *Browser) checkService(p domain.Path) error {
	if !contains(b.store.Services(p.Namespace), p.Service) {
		return notFound(domain.Path{Namespace: p.Namespace, Service: p.Service}.String())
	}
	return nil
}

func notFound(path string) error { return fmt.Errorf("%s: %w", path, domain.ErrNotFound) }

func dirs(names []string) []Entry {
	out := make([]Entry, 0, len(names))
	for _, n := range names {
		out = append(out, Entry{Name: n, Dir: true})
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
