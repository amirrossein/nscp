package vfs

import (
	"context"
	"io/fs"
	"sort"
	"strings"
	"time"
)

type Kind int

const (
	Local Kind = iota
	Remote
)

type Entry struct {
	Name    string
	Path    string
	IsDir   bool
	IsLink  bool
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
}

// WalkItem is one descendant of a walked directory. Rel is slash-separated on
// every platform so it can be joined onto either a local or a remote root.
type WalkItem struct {
	Rel   string
	IsDir bool
	Size  int64
	Mode  fs.FileMode
}

type FS interface {
	Kind() Kind
	Label() string
	Home(ctx context.Context) (string, error)
	List(ctx context.Context, dir string) ([]Entry, error)
	Stat(ctx context.Context, p string) (Entry, error)
	// SizeOf follows symlinks and returns the size and permission bits of the
	// file content that a transfer would read.
	SizeOf(ctx context.Context, p string) (int64, fs.FileMode, error)
	Mkdir(ctx context.Context, p string) error
	MkdirAll(ctx context.Context, p string) error
	Rename(ctx context.Context, from, to string) error
	Remove(ctx context.Context, p string) error
	// Walk lists every descendant of root. Symlinked files are reported with
	// their target's size; symlinked directories are not descended and are
	// returned as warnings instead.
	Walk(ctx context.Context, root string) (items []WalkItem, warnings []string, err error)
	Join(elem ...string) string
	Dir(p string) string
	Base(p string) string
	IsRoot(p string) bool
	// Clean normalises a user-typed path, resolving it against home when relative.
	Clean(p, home string) string
}

func SortEntries(es []Entry) {
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].IsDir != es[j].IsDir {
			return es[i].IsDir
		}
		a, b := strings.ToLower(es[i].Name), strings.ToLower(es[j].Name)
		if a != b {
			return a < b
		}
		return es[i].Name < es[j].Name
	})
}

func ValidName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00\n")
}
