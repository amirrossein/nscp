package ui

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/amirrossein/nscp/internal/transfer"
)

type transfersPanel struct {
	m       *MainWindow
	jobs    []*transfer.Job
	list    *widget.List
	summary *widget.Label
	count   *pill
	clear   *widget.Button
	toggle  *widget.Button
	header  *fyne.Container
	body    *fyne.Container
	size    *fixedHeight

	collapsed bool
	onResize  func()
	empty     *widget.Label
	content   fyne.CanvasObject
	done      chan struct{}
}

const (
	transfersHeight  = 190
	prefTransfersMin = "transfers.collapsed"
)

func newTransfersPanel(m *MainWindow) *transfersPanel {
	t := &transfersPanel{m: m, done: make(chan struct{})}
	t.list = widget.NewList(
		func() int { return len(t.jobs) },
		t.createRow,
		t.updateRow,
	)
	t.list.HideSeparators = true
	t.list.OnSelected = func(id widget.ListItemID) {
		m.endPathEditing()
		t.list.UnselectAll()
		if id < len(t.jobs) {
			t.showDetails(t.jobs[id])
		}
	}
	t.count = newPill("0", colSecondary, colRaised)
	t.count.Hide()
	t.summary = newCaption("")
	t.empty = newCaption("Drag a file or folder onto the other pane to copy it.")
	t.empty.Alignment = fyne.TextAlignCenter
	t.clear = newFlatButton("Clear", theme.DeleteIcon(), m.act(t.clearAll))
	t.clear.Disable()
	t.toggle = newFlatButton("", theme.MoveDownIcon(), m.act(func() { t.setCollapsed(!t.collapsed) }))

	title := widget.NewLabelWithStyle("Transfers", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	left := container.New(layout.NewCustomPaddedHBoxLayout(4), t.toggle, title, container.NewCenter(t.count), t.summary)
	headerTap := newTapArea(m.act(func() { t.setCollapsed(!t.collapsed) }))
	headerTap.cursor = desktop.PointerCursor
	t.header = container.NewBorder(nil, nil, left, t.clear)
	t.body = container.NewStack(t.list, container.NewCenter(t.empty))
	t.size = &fixedHeight{transfersHeight}
	t.content = container.New(t.size, newCard(container.NewBorder(
		container.NewStack(headerTap, t.header), nil, nil, nil, t.body)))
	t.setCollapsed(fyne.CurrentApp().Preferences().Bool(prefTransfersMin))
	go t.tick()
	return t
}

func (t *transfersPanel) setCollapsed(collapsed bool) {
	t.collapsed = collapsed
	fyne.CurrentApp().Preferences().SetBool(prefTransfersMin, collapsed)
	if collapsed {
		t.body.Hide()
		t.toggle.SetIcon(theme.NavigateNextIcon())
		t.size.h = t.header.MinSize().Height + 16
	} else {
		t.body.Show()
		t.toggle.SetIcon(theme.MoveDownIcon())
		t.size.h = transfersHeight
	}
	if t.onResize != nil {
		t.onResize()
	}
}

func (t *transfersPanel) clearAll() {
	active := 0
	for _, j := range t.jobs {
		if !j.Status().Terminal() {
			active++
		}
	}
	if active == 0 {
		t.m.queue.Clear()
		t.refresh()
		return
	}
	d := dialog.NewConfirm("Clear transfers",
		fmt.Sprintf("%s still in progress or waiting. Stop and clear everything?", plural(active, "transfer")),
		func(ok bool) {
			if ok {
				t.m.queue.Clear()
				t.refresh()
			}
		}, t.m.win)
	d.SetConfirmText("Stop and clear")
	d.Show()
}

func (t *transfersPanel) stop() { close(t.done) }

func (t *transfersPanel) tick() {
	tk := time.NewTicker(200 * time.Millisecond)
	defer tk.Stop()
	for {
		select {
		case <-t.done:
			return
		case <-tk.C:
			runOnMain(func() {
				for _, j := range t.jobs {
					if !j.Status().Terminal() {
						t.refresh()
						return
					}
				}
			})
		}
	}
}

func (t *transfersPanel) refresh() {
	t.jobs = t.m.queue.Jobs()
	active := 0
	for _, j := range t.jobs {
		if !j.Status().Terminal() {
			active++
		}
	}
	switch {
	case len(t.jobs) == 0:
		t.summary.SetText("")
		t.count.Hide()
		t.empty.Show()
	default:
		t.empty.Hide()
		t.count.SetText(fmt.Sprint(len(t.jobs)))
		t.count.Show()
		if active > 0 {
			t.summary.SetText(fmt.Sprintf("%d in progress", active))
		} else {
			t.summary.SetText("All finished")
		}
	}
	if len(t.jobs) > 0 {
		t.clear.Enable()
	} else {
		t.clear.Disable()
	}
	t.header.Refresh()
	t.list.Refresh()
}

type jobRow struct {
	widget.BaseWidget
	content fyne.CanvasObject
	icon    *widget.Icon
	title   *widget.Label
	detail  *widget.Label
	bar     *thinBar
	percent *widget.Label
	cancel  *widget.Button
}

func (r *jobRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.content)
}

func (t *transfersPanel) createRow() fyne.CanvasObject {
	r := &jobRow{
		icon:    widget.NewIcon(theme.UploadIcon()),
		title:   widget.NewLabel(""),
		detail:  newCaption(""),
		bar:     newThinBar(),
		percent: trailing(newCaption("")),
		cancel:  newFlatButton("", theme.CancelIcon(), nil),
	}
	r.title.Truncation = fyne.TextTruncateEllipsis
	r.title.TextStyle.Bold = true
	r.detail.Truncation = fyne.TextTruncateEllipsis
	text := container.New(layout.NewCustomPaddedVBoxLayout(-8), r.title, r.detail)
	progress := container.NewHBox(
		container.NewCenter(fixedWidth(180, r.bar)),
		fixedWidth(44, r.percent),
		r.cancel,
	)
	iconBox := container.NewCenter(container.NewGridWrap(fyne.NewSize(20, 20), r.icon))
	r.content = container.NewBorder(nil, nil, iconBox, container.NewCenter(progress), text)
	r.ExtendBaseWidget(r)
	return r
}

func (t *transfersPanel) updateRow(id widget.ListItemID, o fyne.CanvasObject) {
	if id >= len(t.jobs) {
		return
	}
	j := t.jobs[id]
	r := o.(*jobRow)
	s := j.Snapshot()

	r.title.SetText(fmt.Sprintf("%s  →  %s", j.Source.Name, j.Dst.Label()))
	frac := 0.0
	if s.BytesTotal > 0 {
		frac = float64(s.BytesDone) / float64(s.BytesTotal)
	} else if s.Status == transfer.Done {
		frac = 1
	}
	r.percent.SetText(fmt.Sprintf("%d%%", int(frac*100)))
	r.detail.Importance = widget.LowImportance
	barColor := colAccent
	switch s.Status {
	case transfer.Queued:
		r.icon.SetResource(theme.NewColoredResource(theme.HistoryIcon(), theme.ColorNamePlaceHolder))
		r.detail.SetText("Waiting · " + j.DstDir)
	case transfer.Running:
		r.icon.SetResource(theme.NewPrimaryThemedResource(theme.DownloadIcon()))
		text := fmt.Sprintf("%s of %s", humanBytes(s.BytesDone), humanBytes(s.BytesTotal))
		if s.FilesTotal > 1 {
			text = fmt.Sprintf("%d of %d files · %s · %s", s.FilesDone, s.FilesTotal, text, s.Current)
		}
		r.detail.SetText(text)
	case transfer.Done:
		barColor = colSuccess
		r.icon.SetResource(theme.NewSuccessThemedResource(theme.ConfirmIcon()))
		text := humanBytes(s.BytesTotal) + " · " + j.DstPath()
		if n := len(s.Warnings); n > 0 {
			text = plural(n, "warning") + " · " + text
			r.detail.Importance = widget.WarningImportance
		}
		r.detail.SetText(text)
	case transfer.Failed:
		barColor = colDanger
		r.icon.SetResource(theme.NewErrorThemedResource(theme.ErrorIcon()))
		r.detail.Importance = widget.DangerImportance
		r.detail.SetText(firstLine(s.Err.Error()))
	case transfer.Cancelled:
		barColor = colTertiary
		r.icon.SetResource(theme.NewColoredResource(theme.CancelIcon(), theme.ColorNamePlaceHolder))
		r.detail.SetText("Cancelled")
	}
	r.bar.Set(frac, barColor)
	r.detail.Refresh()
	if s.Status.Terminal() {
		r.cancel.Hide()
	} else {
		r.cancel.Show()
		r.cancel.OnTapped = j.Cancel
	}
}

func (t *transfersPanel) showDetails(j *transfer.Job) {
	s := j.Snapshot()
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s:%s\nTo: %s:%s\nStatus: %s\n", j.Src.Label(), j.Source.Path, j.Dst.Label(), j.DstPath(), s.Status)
	fmt.Fprintf(&b, "Files: %d/%d\nBytes: %s / %s\n", s.FilesDone, s.FilesTotal, humanBytes(s.BytesDone), humanBytes(s.BytesTotal))
	if s.Err != nil {
		fmt.Fprintf(&b, "\nError: %v\n", s.Err)
	}
	if len(s.Warnings) > 0 {
		b.WriteString("\nWarnings:\n")
		for _, w := range s.Warnings {
			b.WriteString("• " + w + "\n")
		}
	}
	text := widget.NewLabel(b.String())
	text.Wrapping = fyne.TextWrapWord
	scroll := container.NewVScroll(text)
	scroll.SetMinSize(fyne.NewSize(520, 220))
	dialog.ShowCustom("Transfer details", "Close", scroll, t.m.win)
}
