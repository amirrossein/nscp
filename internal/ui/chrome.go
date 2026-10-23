package ui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	sizeColWidth     = 84
	modifiedColWidth = 118
	maxCrumbs        = 4
)

func (p *Pane) buildChrome() {
	act := p.m.act
	p.host = newHostChip(act(p.showHostMenu))

	up := newFlatButton("", theme.MoveUpIcon(), act(p.goUp))
	home := newFlatButton("", theme.HomeIcon(), act(func() { p.navigate(p.home) }))
	refresh := newFlatButton("", theme.ViewRefreshIcon(), act(p.refreshAll))
	newFolder := newFlatButton("", theme.FolderNewIcon(), act(func() { p.newFolder(p.root) }))
	p.hiddenBtn = newFlatButton("", theme.VisibilityIcon(), act(p.toggleHidden))
	label := "Copy to right"
	if p.idx == 1 {
		label = "Copy to left"
	}
	copyBtn := widget.NewButtonWithIcon(label, theme.ContentCopyIcon(), act(p.copySelectedToOther))
	tools := container.New(layout.NewCustomPaddedHBoxLayout(2), refresh, newFolder, p.hiddenBtn, copyBtn)
	top := container.NewBorder(nil, nil, p.host, container.NewCenter(tools))

	p.crumbs = container.New(layout.NewCustomPaddedHBoxLayout(0))
	p.pathEntry = newKeyEntry()
	p.pathEntry.OnSubmitted = func(s string) {
		p.setEditingPath(false)
		p.goToTyped(s)
	}
	p.pathEntry.OnEscape = func() { p.setEditingPath(false) }
	p.pathEntry.OnFocusLost = p.pathFocusLost
	// Clicking anywhere in the address bar that is not a breadcrumb starts editing.
	crumbArea := container.NewStack(newTapArea(func() { p.setEditingPath(true) }), container.NewHScroll(p.crumbs))
	p.pathStack = container.NewStack(crumbArea, p.pathEntry)
	p.editPath = newFlatButton("", theme.DocumentCreateIcon(), func() { p.setEditingPath(p.pathEntry.Hidden) })
	pathBg := canvas.NewRectangle(colWindow)
	pathBg.CornerRadius = 8
	pathRow := container.NewStack(pathBg, container.NewBorder(nil, nil,
		container.New(layout.NewCustomPaddedHBoxLayout(0), up, home), p.editPath, p.pathStack))

	columns := container.NewBorder(nil, nil,
		container.New(layout.NewCustomPaddedLayout(0, 0, 44, 0), newCaption("Name")),
		container.NewHBox(
			fixedWidth(sizeColWidth, trailing(newCaption("Size"))),
			fixedWidth(modifiedColWidth, trailing(newCaption("Modified"))),
		))
	header := container.New(layout.NewCustomPaddedVBoxLayout(6), top, pathRow, columns, widget.NewSeparator())

	p.footer = newCaption("")
	p.frame = canvas.NewRectangle(colAccentWash)
	p.frame.StrokeColor = colAccent
	p.frame.StrokeWidth = 2
	p.frame.CornerRadius = 8
	p.frame.Hide()

	p.content = newCard(container.NewBorder(header, p.footer, nil, nil, container.NewStack(p.tree, p.frame)))
	p.setEditingPath(false)
}

func fixedWidth(w float32, o fyne.CanvasObject) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(w, o.MinSize().Height), o)
}

func trailing(l *widget.Label) *widget.Label {
	l.Alignment = fyne.TextAlignTrailing
	return l
}

func (p *Pane) setEditingPath(editing bool) {
	if p.pathEntry == nil {
		return
	}
	if editing {
		p.pathEntry.SetText(p.root)
		p.pathEntry.CursorColumn = len([]rune(p.root))
		p.pathEntry.Show()
		p.pathStack.Objects[0].Hide()
		p.editPath.SetIcon(theme.CancelIcon())
		// Use the window's canvas: CanvasForObject only knows recently drawn
		// objects, so it returns nil for an entry that has been hidden a while
		// and the first click would show the field without focusing it.
		p.m.win.Canvas().Focus(p.pathEntry)
		return
	}
	p.pathEntry.Hide()
	p.pathStack.Objects[0].Show()
	p.editPath.SetIcon(theme.DocumentCreateIcon())
}

// pathFocusLost leaves edit mode when focus moved elsewhere in the window.
// FocusLost fires before the canvas records the new focus, and also when the
// whole window loses focus, so the check runs one event later and keeps
// editing if the entry still owns the canvas focus (e.g. the user switched apps).
func (p *Pane) pathFocusLost() {
	go runOnMain(func() {
		if p.pathEntry.Hidden {
			return
		}
		if p.m.win.Canvas().Focused() == p.pathEntry {
			return
		}
		p.setEditingPath(false)
	})
}

type crumb struct {
	label string
	path  string
}

// crumbsFor splits dir into clickable ancestors, collapsing the home
// directory to "~" and long paths to a leading "…".
func crumbsFor(isRoot func(string) bool, parent func(string) string, home, dir string) []crumb {
	var rev []crumb
	for p := dir; ; p = parent(p) {
		if p == home && home != "" {
			rev = append(rev, crumb{"~", p})
			break
		}
		if isRoot(p) {
			rev = append(rev, crumb{p, p})
			break
		}
		rev = append(rev, crumb{baseName(p), p})
	}
	out := make([]crumb, 0, len(rev))
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out, rev[i])
	}
	if len(out) > maxCrumbs {
		hidden := out[len(out)-maxCrumbs]
		out = append([]crumb{{"…", parent(hidden.path)}}, out[len(out)-maxCrumbs+1:]...)
	}
	return out
}

func baseName(p string) string {
	p = strings.TrimRight(p, `/\`)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func (p *Pane) updateBreadcrumb() {
	cs := crumbsFor(p.fs.IsRoot, p.fs.Dir, p.home, p.root)
	objs := make([]fyne.CanvasObject, 0, 2*len(cs))
	for i, c := range cs {
		if i > 0 {
			sep := canvas.NewText("›", colTertiary)
			sep.TextSize = 14
			objs = append(objs, container.NewCenter(sep))
		}
		if i == len(cs)-1 {
			objs = append(objs, widget.NewLabelWithStyle(c.label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
			continue
		}
		path := c.path
		objs = append(objs, newFlatButton(c.label, nil, func() { p.navigate(path) }))
	}
	p.crumbs.Objects = objs
	p.crumbs.Refresh()
}

func (p *Pane) updateFooter() {
	ids, ok := p.children[p.root]
	if !ok {
		if p.errs[p.root] != nil {
			p.footer.SetText("Could not read this folder")
		} else {
			p.footer.SetText("Loading…")
		}
		return
	}
	hidden := 0
	for _, id := range ids {
		if strings.HasPrefix(p.entries[id].Name, ".") {
			hidden++
		}
	}
	text := plural(len(ids), "item")
	switch {
	case hidden > 0 && !p.showHidden:
		text = fmt.Sprintf("%s · %d hidden", plural(len(ids)-hidden, "item"), hidden)
	case hidden > 0:
		text += fmt.Sprintf(" · showing %d hidden", hidden)
	}
	p.footer.SetText(text)
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
