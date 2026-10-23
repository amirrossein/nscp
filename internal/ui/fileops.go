package ui

import (
	"context"
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/amirrossein/nscp/internal/vfs"
)

func (p *Pane) showContextMenu(uid string, pos fyne.Position) {
	e, ok := p.entries[uid]
	if !ok {
		return
	}
	dir := e.Path
	if !e.IsDir {
		dir = p.fs.Dir(e.Path)
	}
	other := p.m.other(p)
	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Copy to other pane", func() {
			p.m.startTransfers(p, p.fs, []vfs.Entry{e}, other, other.root)
		}),
	}
	if e.IsDir {
		items = append(items, fyne.NewMenuItem("Open", func() { p.navigate(e.Path) }))
	}
	items = append(items,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("New folder…", func() { p.newFolder(dir) }),
		fyne.NewMenuItem("Rename…", func() { p.rename(e) }),
		fyne.NewMenuItem("Delete…", func() { p.delete(e) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Refresh", func() { p.reload(dir) }),
	)
	c := fyne.CurrentApp().Driver().CanvasForObject(p.tree)
	widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", items...), c, pos)
}

// runOp performs a filesystem operation off the main goroutine and reloads
// the affected directory when it completes.
func (p *Pane) runOp(reloadDir string, op func(ctx context.Context, fsys vfs.FS) error) {
	fsys, gen := p.fs, p.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ioTimeout)
		defer cancel()
		err := op(ctx, fsys)
		runOnMain(func() {
			if err != nil {
				p.m.showError(err)
			}
			if gen == p.gen {
				p.reload(reloadDir)
			}
		})
	}()
}

func (p *Pane) askName(title, initial string, ok func(string)) {
	entry := newKeyEntry()
	entry.SetText(initial)
	entry.Validator = func(s string) error {
		if !vfs.ValidName(s) {
			return errors.New("invalid name")
		}
		return nil
	}
	d := dialog.NewForm(title, "OK", "Cancel", []*widget.FormItem{widget.NewFormItem("Name", entry)}, func(confirmed bool) {
		if confirmed && entry.Text != initial {
			ok(entry.Text)
		}
	}, p.m.win)
	entry.OnSubmitted = func(string) { d.Submit() }
	entry.OnEscape = d.Hide
	d.Resize(fyne.NewSize(420, d.MinSize().Height))
	d.Show()
	p.m.win.Canvas().Focus(entry)
}

func (p *Pane) newFolder(dir string) {
	p.askName("New folder", "", func(name string) {
		p.runOp(dir, func(ctx context.Context, fsys vfs.FS) error {
			return fsys.Mkdir(ctx, fsys.Join(dir, name))
		})
	})
}

func (p *Pane) rename(e vfs.Entry) {
	dir := p.fs.Dir(e.Path)
	p.askName("Rename", e.Name, func(name string) {
		p.runOp(dir, func(ctx context.Context, fsys vfs.FS) error {
			return fsys.Rename(ctx, e.Path, fsys.Join(dir, name))
		})
	})
}

func (p *Pane) delete(e vfs.Entry) {
	what := "file"
	if e.IsDir && !e.IsLink {
		what = "folder and everything in it"
	}
	where := "on this computer"
	if p.fs.Kind() == vfs.Remote {
		where = "on " + p.fs.Label()
	}
	msg := fmt.Sprintf("Permanently delete the %s %q %s?\nThis cannot be undone.", what, e.Name, where)
	d := dialog.NewConfirm("Delete", msg, func(ok bool) {
		if !ok {
			return
		}
		dir := p.fs.Dir(e.Path)
		p.runOp(dir, func(ctx context.Context, fsys vfs.FS) error {
			return fsys.Remove(ctx, e.Path)
		})
	}, p.m.win)
	d.SetConfirmText("Delete")
	d.Show()
}
