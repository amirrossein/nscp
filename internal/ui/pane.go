package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/amirrossein/nscp/internal/profile"
	"github.com/amirrossein/nscp/internal/sshclient"
	"github.com/amirrossein/nscp/internal/vfs"
)

const placeholderPrefix = "\x00"

type Pane struct {
	m   *MainWindow
	idx int

	fs      vfs.FS
	home    string
	root    string
	prof    *profile.Profile
	client  *sshclient.Client
	release func()
	gen     int

	children map[string][]string
	entries  map[string]vfs.Entry
	loading  map[string]bool
	errs     map[string]error
	selected string

	showHidden bool

	tree      *widget.Tree
	host      *hostChip
	crumbs    *fyne.Container
	pathEntry *keyEntry
	pathStack *fyne.Container
	editPath  *widget.Button
	hiddenBtn *widget.Button
	footer    *widget.Label
	frame     *canvas.Rectangle
	rows      map[*fileRow]struct{}
	content   fyne.CanvasObject
}

func newPane(m *MainWindow, idx int) *Pane {
	p := &Pane{m: m, idx: idx, rows: map[*fileRow]struct{}{}, showHidden: true}
	p.resetCache()

	p.tree = widget.NewTree(p.childUIDs, p.isBranch, p.createNode, p.updateNode)
	p.tree.OnSelected = func(uid string) {
		if strings.HasPrefix(uid, placeholderPrefix) {
			p.tree.Unselect(uid)
			return
		}
		p.selected = uid
	}
	p.tree.OnUnselected = func(uid string) {
		if p.selected == uid {
			p.selected = ""
		}
	}

	p.tree.HideSeparators = true
	p.buildChrome()
	return p
}

func (p *Pane) resetCache() {
	p.children = map[string][]string{}
	p.entries = map[string]vfs.Entry{}
	p.loading = map[string]bool{}
	p.errs = map[string]error{}
	p.selected = ""
}

func (p *Pane) setLocal() {
	local := vfs.LocalFS{}
	home, err := local.Home(context.Background())
	if err != nil {
		home = "/"
	}
	p.dropConnection()
	p.setFS(local, home, home)
	p.host.Set(theme.ComputerIcon(), "This computer", "Local files", false)
}

func (p *Pane) setRemote(prof profile.Profile, c *sshclient.Client, release func(), fsys *vfs.RemoteFS, home string) {
	p.dropConnection()
	p.prof = &prof
	p.client = c
	p.release = release
	p.setFS(fsys, home, home)
	user, addr, _ := p.m.deps.Dialer.Endpoint(prof)
	p.host.Set(theme.StorageIcon(), prof.Title(), user+"@"+addr, true)

	go func() {
		<-c.Done()
		runOnMain(func() {
			if p.client == c {
				p.connectionLost()
			}
		})
	}()
}

func (p *Pane) dropConnection() {
	if p.release != nil {
		p.release()
	}
	p.prof, p.client, p.release = nil, nil, nil
}

func (p *Pane) connectionLost() {
	prof := *p.prof
	dialog.ShowConfirm("Connection lost",
		fmt.Sprintf("The connection to %s was closed.\nReconnect?", prof.Title()),
		func(ok bool) {
			if ok {
				p.connect(prof)
			} else {
				p.setLocal()
			}
		}, p.m.win)
}

func (p *Pane) setFS(fsys vfs.FS, home, root string) {
	p.gen++
	p.fs = fsys
	p.home = home
	p.setRoot(root)
}

func (p *Pane) setRoot(root string) {
	p.gen++
	p.resetCache()
	p.root = root
	p.tree.UnselectAll()
	p.tree.ScrollToTop()
	p.pathEntry.SetText(root)
	p.setEditingPath(false)
	p.updateBreadcrumb()
	p.updateFooter()
	p.tree.Refresh()
}

func (p *Pane) navigate(dir string) {
	if dir == "" || dir == p.root {
		return
	}
	p.setRoot(dir)
}

func (p *Pane) goUp() {
	if p.fs.IsRoot(p.root) {
		return
	}
	p.navigate(p.fs.Dir(p.root))
}

func (p *Pane) goToTyped(text string) {
	target := p.fs.Clean(text, p.home)
	fsys, gen := p.fs, p.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ioTimeout)
		defer cancel()
		e, err := fsys.Stat(ctx, target)
		runOnMain(func() {
			if gen != p.gen {
				return
			}
			switch {
			case err != nil:
				p.pathEntry.SetText(p.root)
				p.m.showError(err)
			case !e.IsDir:
				p.pathEntry.SetText(p.root)
				p.m.showError(fmt.Errorf("%s is not a folder", target))
			default:
				p.navigate(target)
			}
		})
	}()
}

func (p *Pane) refreshAll() {
	p.gen++
	p.resetCache()
	p.tree.Refresh()
}

// reload re-lists dir if this pane has it loaded, keeping open branches.
func (p *Pane) reload(dir string) {
	if _, ok := p.children[dir]; !ok && dir != p.root {
		return
	}
	delete(p.children, dir)
	delete(p.errs, dir)
	p.load(dir)
}

func (p *Pane) toggleHidden() {
	p.showHidden = !p.showHidden
	if p.showHidden {
		p.hiddenBtn.SetIcon(theme.VisibilityIcon())
	} else {
		p.hiddenBtn.SetIcon(theme.VisibilityOffIcon())
	}
	p.updateFooter()
	p.tree.Refresh()
}

func (p *Pane) load(dir string) {
	if p.loading[dir] {
		return
	}
	p.loading[dir] = true
	fsys, gen := p.fs, p.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ioTimeout)
		defer cancel()
		es, err := fsys.List(ctx, dir)
		runOnMain(func() {
			if gen != p.gen {
				return
			}
			delete(p.loading, dir)
			if err != nil {
				p.errs[dir] = err
				delete(p.children, dir)
			} else {
				delete(p.errs, dir)
				ids := make([]string, len(es))
				for i, e := range es {
					ids[i] = e.Path
					p.entries[e.Path] = e
				}
				p.children[dir] = ids
			}
			if dir == p.root {
				p.updateFooter()
			}
			p.tree.Refresh()
		})
	}()
}

// The tree's Root stays "" because Fyne renders a non-empty root as a row;
// "" stands for p.root.
func (p *Pane) childUIDs(uid string) []string {
	if p.fs == nil || strings.HasPrefix(uid, placeholderPrefix) {
		return nil
	}
	if uid == "" {
		uid = p.root
	}
	if err, ok := p.errs[uid]; ok && err != nil {
		return []string{placeholderPrefix + "error:" + uid}
	}
	ids, ok := p.children[uid]
	if !ok {
		p.load(uid)
		return []string{placeholderPrefix + "loading:" + uid}
	}
	if p.showHidden {
		return ids
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !strings.HasPrefix(p.entries[id].Name, ".") {
			out = append(out, id)
		}
	}
	return out
}

func (p *Pane) isBranch(uid string) bool {
	if uid == "" || uid == p.root {
		return true
	}
	e, ok := p.entries[uid]
	return ok && e.IsDir
}

// dropDir is where a drop lands: the folder under the pointer, the folder
// containing the file under the pointer, or the pane's root.
func (p *Pane) dropDir(row *fileRow) string {
	if row == nil || row.uid == "" || strings.HasPrefix(row.uid, placeholderPrefix) {
		return p.root
	}
	e, ok := p.entries[row.uid]
	if !ok {
		return p.root
	}
	if e.IsDir {
		return e.Path
	}
	return p.fs.Dir(e.Path)
}

func (p *Pane) copySelectedToOther() {
	e, ok := p.entries[p.selected]
	if !ok {
		p.m.showError(errors.New("select a file or folder to copy"))
		return
	}
	dst := p.m.other(p)
	p.m.startTransfers(p, p.fs, []vfs.Entry{e}, dst, dst.root)
}
