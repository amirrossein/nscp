package ui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// fileRow is the content of every tree node. It owns tap handling because a
// DoubleTappable child shadows the tree node's own Tapped.
type fileRow struct {
	widget.BaseWidget
	pane *Pane
	uid  string

	bg       *canvas.Rectangle
	icon     *widget.Icon
	name     *widget.Label
	size     *widget.Label
	modified *widget.Label
}

var (
	_ fyne.Tappable          = (*fileRow)(nil)
	_ fyne.DoubleTappable    = (*fileRow)(nil)
	_ fyne.SecondaryTappable = (*fileRow)(nil)
	_ fyne.Draggable         = (*fileRow)(nil)
)

func newFileRow(p *Pane) *fileRow {
	r := &fileRow{
		pane:     p,
		bg:       canvas.NewRectangle(color.Transparent),
		icon:     widget.NewIcon(theme.FileIcon()),
		name:     widget.NewLabel(""),
		size:     trailing(newCaption("")),
		modified: trailing(newCaption("")),
	}
	r.bg.CornerRadius = 6
	r.name.Truncation = fyne.TextTruncateEllipsis
	r.ExtendBaseWidget(r)
	return r
}

func (r *fileRow) CreateRenderer() fyne.WidgetRenderer {
	meta := container.NewHBox(fixedWidth(sizeColWidth, r.size), fixedWidth(modifiedColWidth, r.modified))
	return widget.NewSimpleRenderer(container.NewStack(r.bg,
		container.NewBorder(nil, nil, r.icon, meta, r.name)))
}

func (r *fileRow) placeholder() bool {
	return r.uid == "" || strings.HasPrefix(r.uid, placeholderPrefix)
}

func (r *fileRow) Tapped(*fyne.PointEvent) {
	if !r.placeholder() {
		r.pane.tree.Select(r.uid)
	}
}

func (r *fileRow) DoubleTapped(*fyne.PointEvent) {
	if r.placeholder() {
		return
	}
	if e, ok := r.pane.entries[r.uid]; ok && e.IsDir {
		r.pane.navigate(e.Path)
	}
}

func (r *fileRow) TappedSecondary(ev *fyne.PointEvent) {
	if r.placeholder() {
		return
	}
	r.pane.tree.Select(r.uid)
	r.pane.showContextMenu(r.uid, ev.AbsolutePosition)
}

func (r *fileRow) Dragged(ev *fyne.DragEvent) {
	if !r.placeholder() {
		r.pane.m.dnd.dragged(r.pane, r.uid, ev)
	}
}

func (r *fileRow) DragEnd() {
	r.pane.m.dnd.dragEnd()
}

func (r *fileRow) setHighlight(on bool) {
	if on {
		r.bg.FillColor = theme.Color(theme.ColorNameSelection)
	} else {
		r.bg.FillColor = color.Transparent
	}
	r.bg.Refresh()
}

func (p *Pane) createNode(bool) fyne.CanvasObject {
	r := newFileRow(p)
	p.rows[r] = struct{}{}
	return r
}

func (p *Pane) updateNode(uid string, _ bool, o fyne.CanvasObject) {
	r := o.(*fileRow)
	r.uid = uid
	r.setHighlight(false)
	r.name.Importance = widget.MediumImportance
	r.name.TextStyle = fyne.TextStyle{}
	r.size.SetText("")
	r.modified.SetText("")

	switch {
	case strings.HasPrefix(uid, placeholderPrefix+"loading:"):
		r.icon.SetResource(theme.NewColoredResource(theme.HistoryIcon(), theme.ColorNamePlaceHolder))
		r.name.SetText("Loading…")
		r.name.Importance = widget.LowImportance
		r.name.Refresh()
		return
	case strings.HasPrefix(uid, placeholderPrefix+"error:"):
		dir := strings.TrimPrefix(uid, placeholderPrefix+"error:")
		r.icon.SetResource(theme.NewErrorThemedResource(theme.ErrorIcon()))
		msg := "Error"
		if err := p.errs[dir]; err != nil {
			msg = firstLine(err.Error())
		}
		r.name.SetText(msg)
		r.name.Importance = widget.DangerImportance
		r.name.Refresh()
		return
	}

	e := p.entries[uid]
	r.name.SetText(e.Name)
	r.icon.SetResource(iconFor(e.Name, e.IsDir, e.IsLink))
	switch {
	case e.IsDir:
		r.size.SetText("—")
	case e.IsLink:
		r.size.SetText("link")
	default:
		r.size.SetText(humanBytes(e.Size))
	}
	if !e.ModTime.IsZero() {
		r.modified.SetText(shortTime(e.ModTime))
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
