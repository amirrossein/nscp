package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/amirrossein/nscp/internal/vfs"
)

// dragCoordinator implements pane-to-pane drag and drop, which Fyne has no
// widget-level API for: rows report raw drag events and we hit-test panes and
// rows by their absolute canvas position.
type dragCoordinator struct {
	m     *MainWindow
	layer *fyne.Container
	ghost *fyne.Container
	label *widget.Label

	active    bool
	src       *Pane
	entry     vfs.Entry
	pos       fyne.Position
	hoverPane *Pane
	hoverRow  *fileRow
}

func newDragCoordinator(m *MainWindow) *dragCoordinator {
	d := &dragCoordinator{m: m, label: widget.NewLabel("")}
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameOverlayBackground))
	bg.StrokeColor = theme.Color(theme.ColorNamePrimary)
	bg.StrokeWidth = 1
	bg.CornerRadius = 4
	d.ghost = container.NewStack(bg, container.NewHBox(widget.NewIcon(theme.ContentCopyIcon()), d.label))
	d.ghost.Hide()
	d.layer = container.NewWithoutLayout(d.ghost)
	return d
}

func (d *dragCoordinator) dragged(src *Pane, uid string, ev *fyne.DragEvent) {
	if !d.active {
		e, ok := src.entries[uid]
		if !ok {
			return
		}
		d.active, d.src, d.entry = true, src, e
		d.label.SetText(e.Name)
		d.ghost.Resize(d.ghost.MinSize())
		d.ghost.Show()
	}
	d.pos = ev.AbsolutePosition
	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(d.layer)
	d.ghost.Move(d.pos.Subtract(origin).AddXY(14, 10))

	pane, row := d.m.hitTest(d.pos)
	if pane == d.src {
		pane, row = nil, nil
	}
	d.setHover(pane, row)
}

func (d *dragCoordinator) dragEnd() {
	if !d.active {
		return
	}
	d.ghost.Hide()
	pane, row := d.hoverPane, d.hoverRow
	d.setHover(nil, nil)
	src, entry := d.src, d.entry
	d.active, d.src = false, nil
	if pane == nil {
		return
	}
	d.m.startTransfers(src, src.fs, []vfs.Entry{entry}, pane, pane.dropDir(row))
}

func (d *dragCoordinator) setHover(p *Pane, row *fileRow) {
	if d.hoverRow != row {
		if d.hoverRow != nil {
			d.hoverRow.setHighlight(false)
		}
		if row != nil {
			row.setHighlight(true)
		}
	}
	if d.hoverPane != p {
		if d.hoverPane != nil {
			d.hoverPane.frame.Hide()
		}
		if p != nil {
			p.frame.Show()
		}
	}
	d.hoverPane, d.hoverRow = p, row
}

type box struct {
	pos  fyne.Position
	size fyne.Size
}

func (b box) contains(p fyne.Position) bool {
	return p.X >= b.pos.X && p.Y >= b.pos.Y && p.X < b.pos.X+b.size.Width && p.Y < b.pos.Y+b.size.Height
}

// pickRow returns the index of the row box containing pos that also lies
// within the visible viewport, or -1.
func pickRow(viewport box, rows []box, pos fyne.Position) int {
	if !viewport.contains(pos) {
		return -1
	}
	for i, r := range rows {
		if r.size.Width > 0 && r.contains(pos) {
			return i
		}
	}
	return -1
}

func (m *MainWindow) hitTest(pos fyne.Position) (*Pane, *fileRow) {
	drv := fyne.CurrentApp().Driver()
	for _, p := range m.panes {
		view := box{drv.AbsolutePositionForObject(p.tree), p.tree.Size()}
		if !view.contains(pos) {
			continue
		}
		var rows []*fileRow
		var boxes []box
		for r := range p.rows {
			if r.placeholder() || !r.Visible() {
				continue
			}
			rp := drv.AbsolutePositionForObject(r)
			if rp.IsZero() {
				continue
			}
			rows = append(rows, r)
			boxes = append(boxes, box{rp, r.Size()})
		}
		if i := pickRow(view, boxes, pos); i >= 0 {
			return p, rows[i]
		}
		return p, nil
	}
	return nil, nil
}
