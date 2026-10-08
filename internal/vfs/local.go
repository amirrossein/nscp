package vfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type LocalFS struct{}

var _ FS = LocalFS{}

func (LocalFS) Kind() Kind              { return Local }
func (LocalFS) Label() string           { return "Local" }
func (LocalFS) Join(e ...string) string { return filepath.Join(e...) }
func (LocalFS) Dir(p string) string     { return filepath.Dir(p) }
func (LocalFS) Base(p string) string    { return filepath.Base(p) }
func (LocalFS) IsRoot(p string) bool    { return filepath.Dir(p) == p }

func (LocalFS) Clean(p, home string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(home, p)
	}
	return filepath.Clean(p)
}

func (LocalFS) Home(context.Context) (string, error) {
	return os.UserHomeDir()
}

func (l LocalFS) List(_ context.Context, dir string) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		p := filepath.Join(dir, de.Name())
		e, err := l.entry(p, de.Name())
		if err != nil {
			continue
		}
		out = append(out, e)
	}
	SortEntries(out)
	return out, nil
}

func (l LocalFS) Stat(_ context.Context, p string) (Entry, error) {
	return l.entry(p, filepath.Base(p))
}

func (LocalFS) entry(p, name string) (Entry, error) {
	li, err := os.Lstat(p)
	if err != nil {
		return Entry{}, err
	}
	e := Entry{Name: name, Path: p, Size: li.Size(), Mode: li.Mode().Perm(), ModTime: li.ModTime(), IsDir: li.IsDir()}
	if li.Mode()&fs.ModeSymlink != 0 {
		e.IsLink = true
		if ti, err := os.Stat(p); err == nil {
			e.IsDir = ti.IsDir()
			e.Size = ti.Size()
			e.Mode = ti.Mode().Perm()
		}
	}
	return e, nil
}

func (LocalFS) SizeOf(_ context.Context, p string) (int64, fs.FileMode, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return 0, 0, err
	}
	return fi.Size(), fi.Mode().Perm(), nil
}

func (LocalFS) Mkdir(_ context.Context, p string) error    { return os.Mkdir(p, 0o755) }
func (LocalFS) MkdirAll(_ context.Context, p string) error { return os.MkdirAll(p, 0o755) }

func (LocalFS) Rename(_ context.Context, from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("%s already exists", filepath.Base(to))
	}
	return os.Rename(from, to)
}

func (l LocalFS) Remove(_ context.Context, p string) error {
	if p == "" || l.IsRoot(p) {
		return errors.New("refusing to delete a filesystem root")
	}
	return os.RemoveAll(p)
}

func (LocalFS) Walk(ctx context.Context, root string) ([]WalkItem, []string, error) {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, err
	}
	var items []WalkItem
	var warnings []string
	err = filepath.WalkDir(real, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			warnings = append(warnings, err.Error())
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if p == real {
			return nil
		}
		rel, _ := filepath.Rel(real, p)
		rel = filepath.ToSlash(rel)
		info, err := os.Stat(p)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipped %s: %v", rel, err))
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 && info.IsDir() {
			warnings = append(warnings, fmt.Sprintf("skipped symlinked directory %s", rel))
			return nil
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			warnings = append(warnings, fmt.Sprintf("skipped special file %s", rel))
			return nil
		}
		items = append(items, WalkItem{Rel: rel, IsDir: info.IsDir(), Size: info.Size(), Mode: info.Mode().Perm()})
		return nil
	})
	return items, warnings, err
}
