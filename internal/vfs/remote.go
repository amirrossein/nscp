package vfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type Conn interface {
	Run(ctx context.Context, cmd string) ([]byte, error)
	RawSSHClient() *ssh.Client
}

type RemoteFS struct {
	conn  Conn
	label string

	mu   sync.Mutex
	home string
}

var _ FS = (*RemoteFS)(nil)

func NewRemoteFS(conn Conn, label string) *RemoteFS {
	return &RemoteFS{conn: conn, label: label}
}

func (r *RemoteFS) Conn() Conn              { return r.conn }
func (r *RemoteFS) Kind() Kind              { return Remote }
func (r *RemoteFS) Label() string           { return r.label }
func (r *RemoteFS) Join(e ...string) string { return path.Join(e...) }
func (r *RemoteFS) Dir(p string) string     { return path.Dir(p) }
func (r *RemoteFS) Base(p string) string    { return path.Base(p) }
func (r *RemoteFS) IsRoot(p string) bool    { return path.Clean(p) == "/" }

func (r *RemoteFS) Clean(p, home string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = home + p[1:]
	}
	if !path.IsAbs(p) {
		p = path.Join(home, p)
	}
	return path.Clean(p)
}

func (r *RemoteFS) Home(ctx context.Context) (string, error) {
	r.mu.Lock()
	h := r.home
	r.mu.Unlock()
	if h != "" {
		return h, nil
	}
	out, err := r.conn.Run(ctx, `printf %s "$HOME"`)
	if err != nil {
		return "", err
	}
	h = string(out)
	if h == "" {
		h = "/"
	}
	r.mu.Lock()
	r.home = h
	r.mu.Unlock()
	return h, nil
}

const (
	listFormat = `%f\0%y\0%Y\0%s\0%m\0%T@\0`
	listFields = 6
	// exitNotFound is returned by our own shell snippets, distinct from any
	// status find or stat use.
	exitNotFound = 44
)

func ListCommand(dir string) string {
	return "find " + ShellQuote(dir) + " -mindepth 1 -maxdepth 1 -printf '" + listFormat + "'"
}

func statCommand(p string) string {
	q := ShellQuote(p)
	return "{ test -e " + q + " || test -L " + q + "; } || exit " + strconv.Itoa(exitNotFound) +
		"; find " + q + " -maxdepth 0 -printf '" + listFormat + "'"
}

func (r *RemoteFS) List(ctx context.Context, dir string) ([]Entry, error) {
	out, err := r.conn.Run(ctx, ListCommand(dir))
	if err != nil {
		return nil, err
	}
	es, err := ParseList(out, dir)
	if err != nil {
		return nil, err
	}
	SortEntries(es)
	return es, nil
}

func (r *RemoteFS) Stat(ctx context.Context, p string) (Entry, error) {
	out, err := r.conn.Run(ctx, statCommand(p))
	if exitStatus(err) == exitNotFound {
		return Entry{}, fmt.Errorf("%s: %w", p, fs.ErrNotExist)
	}
	if err != nil {
		return Entry{}, err
	}
	es, err := ParseList(out, path.Dir(p))
	if err != nil {
		return Entry{}, err
	}
	if len(es) != 1 {
		return Entry{}, fmt.Errorf("stat %s: unexpected output", p)
	}
	es[0].Path = p
	es[0].Name = path.Base(p)
	return es[0], nil
}

func (r *RemoteFS) SizeOf(ctx context.Context, p string) (int64, fs.FileMode, error) {
	out, err := r.conn.Run(ctx, "stat -L --printf '%s\\0%a\\0' -- "+ShellQuote(p))
	if err != nil {
		return 0, 0, err
	}
	fields := bytes.Split(out, []byte{0})
	if len(fields) < 2 {
		return 0, 0, fmt.Errorf("stat %s: unexpected output", p)
	}
	size, _ := strconv.ParseInt(string(fields[0]), 10, 64)
	mode, _ := strconv.ParseUint(string(fields[1]), 8, 32)
	return size, fs.FileMode(mode).Perm(), nil
}

func ParseList(out []byte, dir string) ([]Entry, error) {
	fields := bytes.Split(out, []byte{0})
	if len(fields) > 0 && len(fields[len(fields)-1]) == 0 {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%listFields != 0 {
		return nil, fmt.Errorf("unexpected listing output (%d fields); is GNU find installed on the server?", len(fields))
	}
	es := make([]Entry, 0, len(fields)/listFields)
	for i := 0; i < len(fields); i += listFields {
		name := string(fields[i])
		typ, target := string(fields[i+1]), string(fields[i+2])
		size, _ := strconv.ParseInt(string(fields[i+3]), 10, 64)
		mode, _ := strconv.ParseUint(string(fields[i+4]), 8, 32)
		mt, _ := strconv.ParseFloat(string(fields[i+5]), 64)
		sec, frac := math.Modf(mt)
		es = append(es, Entry{
			Name:    name,
			Path:    path.Join(dir, name),
			IsDir:   typ == "d" || (typ == "l" && target == "d"),
			IsLink:  typ == "l",
			Size:    size,
			Mode:    fs.FileMode(mode).Perm(),
			ModTime: time.Unix(int64(sec), int64(frac*1e9)),
		})
	}
	return es, nil
}

func (r *RemoteFS) Mkdir(ctx context.Context, p string) error {
	_, err := r.conn.Run(ctx, "mkdir -- "+ShellQuote(p))
	return err
}

func (r *RemoteFS) MkdirAll(ctx context.Context, p string) error {
	_, err := r.conn.Run(ctx, "mkdir -p -- "+ShellQuote(p))
	return err
}

func (r *RemoteFS) Rename(ctx context.Context, from, to string) error {
	qt := ShellQuote(to)
	cmd := "if test -e " + qt + " || test -L " + qt + "; then echo " + ShellQuote(path.Base(to)+" already exists") +
		" >&2; exit 1; fi; mv -- " + ShellQuote(from) + " " + qt
	_, err := r.conn.Run(ctx, cmd)
	return err
}

func (r *RemoteFS) Remove(ctx context.Context, p string) error {
	if p == "" || r.IsRoot(p) {
		return errors.New("refusing to delete a filesystem root")
	}
	_, err := r.conn.Run(ctx, "rm -rf -- "+ShellQuote(p))
	return err
}

const walkFormat = `%P\0%y\0%Y\0%s\0%m\0`

func (r *RemoteFS) Walk(ctx context.Context, root string) ([]WalkItem, []string, error) {
	// A trailing slash makes find follow root itself when it is a symlink.
	cmd := "find " + ShellQuote(strings.TrimSuffix(root, "/")+"/") + " -mindepth 1 -printf '" + walkFormat + "'"
	out, err := r.conn.Run(ctx, cmd)
	var warnings []string
	if err != nil {
		if len(out) == 0 {
			return nil, nil, err
		}
		warnings = append(warnings, err.Error())
	}
	items, more, linked, err := parseWalk(out)
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, more...)
	if len(linked) > 0 {
		if err := r.fillLinkSizes(ctx, root, items, linked); err != nil {
			return nil, nil, err
		}
	}
	return items, warnings, nil
}

func parseWalk(out []byte) (items []WalkItem, warnings []string, linked []int, err error) {
	const n = 5
	fields := bytes.Split(out, []byte{0})
	if len(fields) > 0 && len(fields[len(fields)-1]) == 0 {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%n != 0 {
		return nil, nil, nil, fmt.Errorf("unexpected walk output (%d fields)", len(fields))
	}
	var skippedDirs []string
	for i := 0; i < len(fields); i += n {
		rel := string(fields[i])
		typ, target := string(fields[i+1]), string(fields[i+2])
		size, _ := strconv.ParseInt(string(fields[i+3]), 10, 64)
		mode, _ := strconv.ParseUint(string(fields[i+4]), 8, 32)
		if underAny(rel, skippedDirs) {
			continue
		}
		switch {
		case typ == "d":
			items = append(items, WalkItem{Rel: rel, IsDir: true, Mode: fs.FileMode(mode).Perm()})
		case typ == "f":
			items = append(items, WalkItem{Rel: rel, Size: size, Mode: fs.FileMode(mode).Perm()})
		case typ == "l" && target == "f":
			linked = append(linked, len(items))
			items = append(items, WalkItem{Rel: rel, Mode: 0o644})
		case typ == "l" && target == "d":
			skippedDirs = append(skippedDirs, rel)
			warnings = append(warnings, "skipped symlinked directory "+rel)
		default:
			warnings = append(warnings, "skipped special file "+rel)
		}
	}
	return items, warnings, linked, nil
}

func underAny(rel string, dirs []string) bool {
	for _, d := range dirs {
		if strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

func (r *RemoteFS) fillLinkSizes(ctx context.Context, root string, items []WalkItem, linked []int) error {
	var b strings.Builder
	b.WriteString("stat -L --printf '%s\\0%a\\0' --")
	for _, i := range linked {
		b.WriteString(" ")
		b.WriteString(ShellQuote(path.Join(root, items[i].Rel)))
	}
	out, err := r.conn.Run(ctx, b.String())
	if err != nil {
		return err
	}
	fields := bytes.Split(out, []byte{0})
	if len(fields) < 2*len(linked) {
		return errors.New("unexpected stat output for symlinked files")
	}
	for k, i := range linked {
		items[i].Size, _ = strconv.ParseInt(string(fields[2*k]), 10, 64)
		mode, _ := strconv.ParseUint(string(fields[2*k+1]), 8, 32)
		items[i].Mode = fs.FileMode(mode).Perm()
	}
	return nil
}

func exitStatus(err error) int {
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		return ee.ExitStatus()
	}
	return -1
}
