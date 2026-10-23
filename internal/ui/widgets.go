package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const cardRadius = 12

func newCard(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(colCard)
	bg.StrokeColor = colCardBorder
	bg.StrokeWidth = 1
	bg.CornerRadius = cardRadius
	return container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(8, 8, 8, 8), content))
}

func newCaption(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.SizeName = theme.SizeNameCaptionText
	l.Importance = widget.LowImportance
	return l
}

func newFlatButton(label string, icon fyne.Resource, tapped func()) *widget.Button {
	b := widget.NewButtonWithIcon(label, icon, tapped)
	b.Importance = widget.LowImportance
	return b
}

// pill is a small rounded badge, e.g. a transfer count.
type pill struct {
	widget.BaseWidget
	bg   *canvas.Rectangle
	text *canvas.Text
}

func newPill(text string, fg, bg color.Color) *pill {
	p := &pill{bg: canvas.NewRectangle(bg), text: canvas.NewText(text, fg)}
	p.bg.CornerRadius = 8
	p.text.TextSize = 11
	p.text.TextStyle.Bold = true
	p.ExtendBaseWidget(p)
	return p
}

func (p *pill) SetText(s string) {
	p.text.Text = s
	p.Refresh()
}

func (p *pill) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(p.bg,
		container.New(layout.NewCustomPaddedLayout(1, 1, 7, 7), container.NewCenter(p.text))))
}

// thinBar is a slim rounded progress bar.
type thinBar struct {
	widget.BaseWidget
	value float64
	track *canvas.Rectangle
	fill  *canvas.Rectangle
}

func newThinBar() *thinBar {
	b := &thinBar{track: canvas.NewRectangle(colTrack), fill: canvas.NewRectangle(colAccent)}
	b.track.CornerRadius = 2
	b.fill.CornerRadius = 2
	b.ExtendBaseWidget(b)
	return b
}

func (b *thinBar) Set(v float64, c color.Color) {
	b.value = min(max(v, 0), 1)
	b.fill.FillColor = c
	b.Refresh()
}

func (b *thinBar) CreateRenderer() fyne.WidgetRenderer { return &thinBarRenderer{b} }

type thinBarRenderer struct{ b *thinBar }

func (r *thinBarRenderer) Layout(s fyne.Size) {
	const h = 4
	y := (s.Height - h) / 2
	r.b.track.Move(fyne.NewPos(0, y))
	r.b.track.Resize(fyne.NewSize(s.Width, h))
	r.b.fill.Move(fyne.NewPos(0, y))
	r.b.fill.Resize(fyne.NewSize(s.Width*float32(r.b.value), h))
}

func (r *thinBarRenderer) MinSize() fyne.Size { return fyne.NewSize(60, 10) }

func (r *thinBarRenderer) Refresh() {
	r.Layout(r.b.Size())
	r.b.track.Refresh()
	r.b.fill.Refresh()
}

func (r *thinBarRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.b.track, r.b.fill}
}

func (r *thinBarRenderer) Destroy() {}

// hostChip is the clickable server name at the top of a pane.
type hostChip struct {
	widget.BaseWidget
	onTap func()

	bg       *canvas.Rectangle
	icon     *widget.Icon
	title    *canvas.Text
	subtitle *canvas.Text
	dot      *canvas.Circle
}

var (
	_ fyne.Tappable      = (*hostChip)(nil)
	_ desktop.Hoverable  = (*hostChip)(nil)
	_ desktop.Cursorable = (*hostChip)(nil)
)

func newHostChip(onTap func()) *hostChip {
	c := &hostChip{
		onTap:    onTap,
		bg:       canvas.NewRectangle(color.Transparent),
		icon:     widget.NewIcon(theme.ComputerIcon()),
		title:    canvas.NewText("", colText),
		subtitle: canvas.NewText("", colSecondary),
		dot:      canvas.NewCircle(colTertiary),
	}
	c.bg.CornerRadius = 8
	c.title.TextStyle.Bold = true
	c.title.TextSize = 14
	c.subtitle.TextSize = 11
	c.ExtendBaseWidget(c)
	return c
}

func (c *hostChip) Set(icon fyne.Resource, title, subtitle string, connected bool) {
	c.icon.SetResource(icon)
	c.title.Text = title
	c.subtitle.Text = subtitle
	if connected {
		c.dot.FillColor = colSuccess
	} else {
		c.dot.FillColor = colTertiary
	}
	c.Refresh()
}

func (c *hostChip) CreateRenderer() fyne.WidgetRenderer {
	dot := container.NewGridWrap(fyne.NewSize(8, 8), c.dot)
	titleRow := container.New(layout.NewCustomPaddedHBoxLayout(6), c.title, container.NewCenter(dot))
	text := container.New(layout.NewCustomPaddedVBoxLayout(1), titleRow, c.subtitle)
	chevron := widget.NewIcon(theme.NewColoredResource(chevronDown, theme.ColorNamePlaceHolder))
	iconBox := container.NewGridWrap(fyne.NewSize(28, 28), c.icon)
	row := container.New(layout.NewCustomPaddedHBoxLayout(8),
		container.NewCenter(iconBox), text, container.NewCenter(container.NewGridWrap(fyne.NewSize(14, 14), chevron)))
	return widget.NewSimpleRenderer(container.NewStack(c.bg,
		container.New(layout.NewCustomPaddedLayout(4, 4, 6, 10), row)))
}

func (c *hostChip) Tapped(*fyne.PointEvent) {
	if c.onTap != nil {
		c.onTap()
	}
}

func (c *hostChip) MouseIn(*desktop.MouseEvent) {
	c.bg.FillColor = theme.Color(theme.ColorNameHover)
	c.bg.Refresh()
}

func (c *hostChip) MouseMoved(*desktop.MouseEvent) {}

func (c *hostChip) MouseOut() {
	c.bg.FillColor = color.Transparent
	c.bg.Refresh()
}

func (c *hostChip) Cursor() desktop.Cursor { return desktop.PointerCursor }

// fillFirst stacks objects vertically: every object after the first keeps its
// minimum height and the first takes the remaining space.
type fillFirst struct{ gap float32 }

func (f *fillFirst) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	rest := float32(0)
	for _, o := range objs[1:] {
		rest += o.MinSize().Height + f.gap
	}
	y := float32(0)
	for i, o := range objs {
		h := o.MinSize().Height
		if i == 0 {
			h = size.Height - rest
		}
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h + f.gap
	}
}

func (f *fillFirst) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var s fyne.Size
	for i, o := range objs {
		m := o.MinSize()
		s.Width = max(s.Width, m.Width)
		s.Height += m.Height
		if i > 0 {
			s.Height += f.gap
		}
	}
	return s
}

type fixedHeight struct{ h float32 }

func (f *fixedHeight) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

func (f *fixedHeight) MinSize(objs []fyne.CanvasObject) fyne.Size {
	w := float32(0)
	for _, o := range objs {
		w = max(w, o.MinSize().Width)
	}
	return fyne.NewSize(w, f.h)
}

// keyEntry is an Entry that also reports Escape, so text prompts can be
// confirmed with Enter and dismissed with Escape.
type keyEntry struct {
	widget.Entry
	OnEscape    func()
	OnFocusLost func()
}

func newKeyEntry() *keyEntry {
	e := &keyEntry{}
	e.ExtendBaseWidget(e)
	return e
}

func newKeyPasswordEntry() *keyEntry {
	e := newKeyEntry()
	e.Password = true
	return e
}

func (e *keyEntry) TypedKey(k *fyne.KeyEvent) {
	if k.Name == fyne.KeyEscape && e.OnEscape != nil {
		e.OnEscape()
		return
	}
	e.Entry.TypedKey(k)
}

func (e *keyEntry) FocusLost() {
	e.Entry.FocusLost()
	if e.OnFocusLost != nil {
		e.OnFocusLost()
	}
}

// tapArea is an invisible tappable filler placed behind other widgets so a
// click on their empty space still does something.
type tapArea struct {
	widget.BaseWidget
	onTap  func()
	cursor desktop.Cursor
}

var _ desktop.Cursorable = (*tapArea)(nil)

func newTapArea(onTap func()) *tapArea {
	t := &tapArea{onTap: onTap, cursor: desktop.TextCursor}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapArea) Tapped(*fyne.PointEvent) { t.onTap() }

func (t *tapArea) Cursor() desktop.Cursor { return t.cursor }

func (t *tapArea) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}
